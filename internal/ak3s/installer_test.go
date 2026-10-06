package ak3s

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"reflect"
	"strings"
	"syscall"
	"testing"
)

// ownedReader simulates root-owned files even when tests run as an ordinary user.
type ownedReader struct{ rootedReader }
type ownedInfo struct{ fs.FileInfo }

func (i ownedInfo) Sys() any { return &syscall.Stat_t{Uid: 0} }
func (r ownedReader) Lstat(path string) (fs.FileInfo, error) {
	i, err := r.rootedReader.Lstat(path)
	if err != nil {
		return nil, err
	}
	return ownedInfo{i}, nil
}

func installerFixture(t *testing.T, c Config, s Snapshot, command string) (Plan, *Bundle) {
	t.Helper()
	b := &Bundle{Dir: "/bundle", K3s: []byte("verified binary"), Installer: []byte("verified official installer")}
	pins := testPins(t)
	pins.K3sSHA256[s.Arch] = fingerprint(b.K3s)
	pins.Installer.SHA256 = fingerprint(b.Installer)
	p, err := BuildPlan(c, pins, s, command)
	if err != nil {
		t.Fatal(err)
	}
	return p, b
}

func TestInstallerMigrationAndRepeatedRuns(t *testing.T) {
	for _, mode := range []string{"fresh", "schema1", "legacy", "upgrade", "agent", "arm64"} {
		t.Run(mode, func(t *testing.T) {
			c := testConfig(t)
			c.Platform = false
			s := freshSnapshot()
			command := "apply"
			if mode == "agent" {
				c.Node.Role, c.Node.Join, c.Node.TokenFile, c.APIEndpoint = "agent", true, "/root/join-token", "10.0.0.1"
			}
			if mode == "arm64" {
				s.Arch = "arm64"
			}
			if mode == "schema1" || mode == "legacy" || mode == "upgrade" {
				s = managedSnapshot(t, c)
				s.State.Schema = 1
				s.State.InstallerSHA256 = ""
				s.State.GeneratedScripts = nil
				delete(s.Files, uninstallPath(c.Node.Role))
				delete(s.Files, KillallPath)
				if mode == "legacy" {
					s.State = nil
				}
				if mode == "upgrade" {
					s.K3sVersion, s.State.K3sVersion = "v1.36.5+k3s1", "v1.36.5+k3s1"
					command = "upgrade"
				}
			}
			p, b := installerFixture(t, c, s, command)
			r := ownedReader{fixtureHost(t)}
			const database = "/var/lib/rancher/k3s/server/db/state.db"
			put(t, r.rootedReader, database, []byte("existing cluster data"))
			put(t, r.rootedReader, "/var/lib/rancher/k3s/server/token", []byte("existing token"))
			e := &effects{}
			write := func(path string, data []byte, mode fs.FileMode) error {
				if err := e.write(path, data, mode); err != nil {
					return err
				}
				return AtomicWrite(r.path(path), data, mode)
			}
			runner := runFunc(func(ctx context.Context, cmd Command) ([]byte, error) {
				if cmd.Name == "/bin/sh" {
					if string(cmd.Input) != string(b.Installer) || !reflect.DeepEqual(cmd.Args, []string{"-s", "-", c.Node.Role, "--config", ConfigPath}) || !cmd.IsolatedEnv {
						t.Fatal("incorrect upstream invocation")
					}
					for _, want := range []string{"INSTALL_K3S_VERSION=" + p.Pins.K3s, "INSTALL_K3S_ARTIFACT_URL=file:///bundle/k3s-artifacts"} {
						if !strings.Contains(strings.Join(cmd.Env, "\n"), want) {
							t.Fatal("missing pinned installer option", want)
						}
					}
					for _, path := range []string{uninstallPath(c.Node.Role), KillallPath, unitPath(c.Node.Role)} {
						put(t, r.rootedReader, path, []byte("generated "+path))
					}
					put(t, r.rootedReader, K3sPath, b.K3s)
				}
				return e.Run(ctx, cmd)
			})
			if err := reconcile(context.Background(), p, b, runner, write, r); err != nil {
				t.Fatal(err)
			}
			if !e.has("/bin/sh") || len(e.states) != 3 || !e.states[0].PendingRestart || !e.states[1].PendingRestart || e.states[2].PendingRestart {
				t.Fatal("installer or readiness checkpoint missing")
			}
			for _, path := range e.writes {
				if path == K3sPath || path == unitPath(c.Node.Role) || path == KillallPath || path == uninstallPath(c.Node.Role) {
					t.Fatal("AK3S wrote an upstream-owned file", path)
				}
			}
			if e.has("systemctl restart") || e.has("systemctl enable") {
				t.Fatal("AK3S managed the service directly")
			}
			data, _ := r.ReadFile(database)
			if string(data) != "existing cluster data" {
				t.Fatal("migration changed cluster data")
			}
			data, _ = r.ReadFile("/var/lib/rancher/k3s/server/token")
			if string(data) != "existing token" {
				t.Fatal("migration changed server token")
			}
			s = managedSnapshot(t, c)
			s.Arch = p.Snapshot.Arch
			s.State = &e.states[2]
			s.Files[StatePath], _ = r.ReadFile(StatePath)
			for _, path := range []string{uninstallPath(c.Node.Role), KillallPath, unitPath(c.Node.Role)} {
				s.Files[path], _ = r.ReadFile(path)
			}
			again, err := BuildPlan(c, p.Pins, s, "apply")
			if err != nil || again.RunInstaller || again.Restart {
				t.Fatal("unchanged migration would rerun installer", err)
			}
		})
	}
}

func TestInstallerFailureAndRecovery(t *testing.T) {
	c := testConfig(t)
	c.Platform = false
	for _, stage := range []string{"script checksum", "binary checksum", "installer", "readiness"} {
		t.Run(stage, func(t *testing.T) {
			p, b := installerFixture(t, c, freshSnapshot(), "install")
			r := ownedReader{fixtureHost(t)}
			e := &effects{}
			switch stage {
			case "script checksum":
				b.Installer = []byte("corrupt")
			case "binary checksum":
				b.K3s = []byte("corrupt")
			case "installer":
				e.failCommand = "/bin/sh"
			case "readiness":
				e.failCommand = "wait --for=create"
				put(t, r.rootedReader, K3sPath, b.K3s)
				put(t, r.rootedReader, unitPath("server"), []byte("unit"))
				put(t, r.rootedReader, uninstallPath("server"), []byte("uninstall"))
				put(t, r.rootedReader, KillallPath, []byte("killall"))
			}
			if err := reconcile(context.Background(), p, b, e, e.write, r); err == nil {
				t.Fatal("failure ignored")
			}
			if strings.Contains(stage, "checksum") {
				if len(e.commands) != 0 || len(e.writes) != 0 {
					t.Fatal("changed host before checksum verification")
				}
				return
			}
			wantStates := 1
			if stage == "readiness" {
				wantStates = 2
			}
			if len(e.states) != wantStates || !e.states[len(e.states)-1].PendingRestart {
				t.Fatal("lost pending restart")
			}
			s := managedSnapshot(t, c)
			s.State = &e.states[len(e.states)-1]
			delete(s.Files, unitPath("server")) // interrupted unit replacement
			again, err := BuildPlan(c, p.Pins, s, "apply")
			if err != nil || !again.RunInstaller || !again.Restart {
				t.Fatal("cannot recover interrupted installer", err)
			}
		})
	}
}

func TestInstallerEnvironmentIsolation(t *testing.T) {
	for _, key := range []string{"INSTALL_K3S_SKIP_DOWNLOAD", "INSTALL_K3S_EXEC", "K3S_TOKEN", "K3S_DATA_DIR", "CONTAINERD_ADDRESS", "KINE_DATA_SOURCE", "HTTPS_PROXY"} {
		t.Setenv(key, "unwanted")
	}
	out, err := (ExecRunner{}).Run(context.Background(), Command{Name: "/usr/bin/env", IsolatedEnv: true})
	if err != nil || strings.Contains(string(out), "unwanted") {
		t.Fatal("installer inherited shell configuration", err)
	}
}

func TestInstallerDestinationSymlinks(t *testing.T) {
	for _, path := range []string{K3sPath, unitPath("server"), unitPath("server") + ".env", KillallPath, uninstallPath("server")} {
		t.Run(path, func(t *testing.T) {
			r := fixtureHost(t)
			put(t, r, path, []byte("original"))
			os.Remove(r.path(path))
			if err := os.Symlink("/missing-target", r.path(path)); err != nil {
				t.Fatal(err)
			}
			_, err := Inspect(context.Background(), r, &effects{}, testConfig(t), freshSnapshot())
			if err == nil {
				t.Fatal("upstream could follow destination symlink")
			}
		})
	}
}

func TestPendingUninstallBlocksApply(t *testing.T) {
	c := testConfig(t)
	s := managedSnapshot(t, c)
	s.State.PendingUninstall = true
	if _, err := BuildPlan(c, testPins(t), s, "apply"); err == nil {
		t.Fatal("reinstalled during incomplete removal")
	}
}

func TestCheckpointVersionProtectsMissingBinary(t *testing.T) {
	c := testConfig(t)
	s := managedSnapshot(t, c)
	s.K3sVersion = ""
	s.State.K3sVersion = "v1.38.1+k3s1"
	if _, err := BuildPlan(c, testPins(t), s, "upgrade"); err == nil {
		t.Fatal("missing binary bypassed downgrade protection")
	}
}

func TestUninstallRecovery(t *testing.T) {
	for _, failure := range []string{"none", "command", "leftover", "bookkeeping"} {
		t.Run(failure, func(t *testing.T) {
			s := managedSnapshot(t, testConfig(t))
			r := ownedReader{fixtureHost(t)}
			for path, data := range s.Files {
				put(t, r.rootedReader, path, data)
			}
			put(t, r.rootedReader, "/var/lib/rancher/k3s/server/db", []byte("data"))
			put(t, r.rootedReader, "/etc/ak3s/values.yaml", []byte("operator overrides"))
			write := func(path string, data []byte, mode fs.FileMode) error { return AtomicWrite(r.path(path), data, mode) }
			cleanupFailed := false
			remove := func(path string) error {
				if failure == "bookkeeping" && path == StatePath && !cleanupFailed {
					cleanupFailed = true
					return errors.New("state removal failed")
				}
				return os.Remove(r.path(path))
			}
			calls := 0
			runner := runFunc(func(_ context.Context, cmd Command) ([]byte, error) {
				calls++
				if cmd.Name != "/bin/sh" || !reflect.DeepEqual(cmd.Args, []string{uninstallPath("server")}) || !cmd.IsolatedEnv {
					t.Fatal("custom removal command")
				}
				// Simulate upstream deleting itself and some data before interruption.
				for _, path := range []string{uninstallPath("server"), KillallPath, unitPath("server"), "/etc/rancher/k3s"} {
					os.RemoveAll(r.path(path))
				}
				if failure == "command" && calls == 1 {
					return nil, errors.New("interrupted")
				}
				if failure != "leftover" || calls > 1 {
					os.RemoveAll(r.path("/var/lib/rancher/k3s"))
				}
				return nil, nil
			})
			err := uninstall(context.Background(), s, r, runner, write, remove)
			if failure != "none" {
				if err == nil {
					t.Fatal("incomplete removal reported success")
				}
				data, _ := r.ReadFile(StatePath)
				if err = json.Unmarshal(data, &s.State); err != nil || !s.State.PendingUninstall {
					t.Fatal("lost removal checkpoint", err)
				}
				s.Managed = false
				for _, path := range []string{RemovalScriptPath, RemovalKillallPath} {
					s.Files[path], _ = r.ReadFile(path)
				}
				err = uninstall(context.Background(), s, r, runner, write, remove)
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err = r.Stat(StatePath); !errors.Is(err, fs.ErrNotExist) {
				t.Fatal("state retained after successful removal")
			}
			data, _ := r.ReadFile("/etc/ak3s/values.yaml")
			if string(data) != "operator overrides" {
				t.Fatal("operator configuration removed")
			}
			if err = uninstall(context.Background(), freshSnapshot(), r, runner, write, remove); err != nil {
				t.Fatal("repeated removal failed", err)
			}
		})
	}
}

func TestUninstallGuards(t *testing.T) {
	for _, mode := range []string{"unmanaged", "legacy", "tampered", "missing", "role", "additional service"} {
		t.Run(mode, func(t *testing.T) {
			s := managedSnapshot(t, testConfig(t))
			switch mode {
			case "unmanaged":
				s.Managed = false
			case "legacy":
				s.State.Schema = 1
			case "tampered":
				s.Files[KillallPath] = []byte("changed")
			case "missing":
				delete(s.Files, uninstallPath("server"))
			case "role":
				s.Role = "agent"
			case "additional service":
				s.Problems = []string{"additional K3s service"}
			}
			if err := checkUninstall(s); err == nil {
				t.Fatal("unsafe uninstall allowed")
			}
		})
	}
}
