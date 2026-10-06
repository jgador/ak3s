package ak3s

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type runFunc func(context.Context, Command) ([]byte, error)

// Run delegates command execution to the test callback.
func (f runFunc) Run(c context.Context, cmd Command) ([]byte, error) { return f(c, cmd) }

type rootedReader struct{ root string }

func (r rootedReader) path(p string) string { return filepath.Join(r.root, p) }

// ReadFile reads a file beneath the fixture root.
func (r rootedReader) ReadFile(p string) ([]byte, error) { return os.ReadFile(r.path(p)) }

// Lstat inspects a fixture without following symlinks.
func (r rootedReader) Lstat(p string) (fs.FileInfo, error) { return os.Lstat(r.path(p)) }

// Stat returns file information beneath the fixture root.
func (r rootedReader) Stat(p string) (fs.FileInfo, error) { return os.Stat(r.path(p)) }

// ReadDir lists directory entries beneath the fixture root.
func (r rootedReader) ReadDir(p string) ([]fs.DirEntry, error) { return os.ReadDir(r.path(p)) }
func fixtureHost(t *testing.T) rootedReader {
	t.Helper()
	r := rootedReader{t.TempDir()}
	put(t, r, "/etc/os-release", []byte("ID=ubuntu\nVERSION_ID=\"24.04\"\n"))
	put(t, r, "/proc/swaps", []byte("Filename Type Size Used Priority\n"))
	return r
}
func put(t *testing.T, r rootedReader, p string, data []byte) {
	t.Helper()
	if err := AtomicWrite(r.path(p), data, 0600); err != nil {
		t.Fatal(err)
	}
}

// TestInspectFreshAndUnownedNeverExecuted checks fresh host discovery and ensures binaries from unmanaged installations are never executed.
func TestInspectFreshAndUnownedNeverExecuted(t *testing.T) {
	r := fixtureHost(t)
	runner := runFunc(func(context.Context, Command) ([]byte, error) { t.Fatal("unexpected command"); return nil, nil })
	s, err := Inspect(context.Background(), r, runner, testConfig(t), freshSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	if s.Managed || s.Existing || s.Swap || s.Systemd {
		t.Fatal(s)
	}
	put(t, r, K3sPath, []byte("untrusted"))
	s, err = Inspect(context.Background(), r, runner, testConfig(t), freshSnapshot())
	if err != nil || !s.Existing {
		t.Fatal(s, err)
	}
}

// TestInspectExistingAndFailClosed checks managed host discovery, rejection of drop-in configuration and rejection of corrupt state.
func TestInspectExistingAndFailClosed(t *testing.T) {
	r := fixtureHost(t)
	c := testConfig(t)
	for path, data := range map[string][]byte{K3sPath: []byte("binary"), MarkerPath: []byte("Managed by AK3S."), unitPath("server"): []byte("unit"), KubeconfigPath: []byte("kube")} {
		put(t, r, path, data)
	}
	os.MkdirAll(r.path("/run/systemd/system"), 0700)
	runner := runFunc(func(_ context.Context, cmd Command) ([]byte, error) {
		if cmd.Name == K3sPath {
			return []byte("k3s version v1.36.5+k3s1 (abc)\ngo version go1.25\n"), nil
		}
		return []byte("active\n"), nil
	})
	s, err := Inspect(context.Background(), r, runner, c, freshSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	if !s.Managed || !s.Systemd || !s.ServiceActive || !s.Kubeconfig || s.Role != "server" || s.K3sVersion != "v1.36.5+k3s1" {
		t.Fatal(s)
	}
	put(t, r, "/etc/rancher/k3s/config.yaml.d/extra.yaml", []byte("server: unsafe"))
	put(t, r, "/proc/swaps", []byte("Filename Type Size Used Priority\n/swap file 1024 0 -2\n"))
	s, err = Inspect(context.Background(), r, runner, c, freshSnapshot())
	if err != nil || !s.Swap || len(s.Problems) == 0 {
		t.Fatal(s, err)
	}
	put(t, r, StatePath, []byte("corrupted"))
	if _, err = Inspect(context.Background(), r, runner, c, freshSnapshot()); err == nil {
		t.Fatal("corrupt state ignored")
	}
	put(t, r, StatePath, []byte("null"))
	if _, err = Inspect(context.Background(), r, runner, c, freshSnapshot()); err == nil {
		t.Fatal("null state ignored")
	}
}

// TestTokenPermissionAndHash checks token fingerprints, private file permissions and token validation.
func TestTokenPermissionAndHash(t *testing.T) {
	r := fixtureHost(t)
	c := testConfig(t)
	c.Node.TokenFile = "/root/token"
	put(t, r, c.Node.TokenFile, []byte("secret\n"))
	runner := runFunc(func(context.Context, Command) ([]byte, error) { t.Fatal("unexpected command"); return nil, nil })
	s, err := Inspect(context.Background(), r, runner, c, freshSnapshot())
	if err != nil || len(s.TokenSHA256) != 64 {
		t.Fatal(err)
	}
	os.Chmod(r.path(c.Node.TokenFile), 0644)
	if _, err = Inspect(context.Background(), r, runner, c, freshSnapshot()); err == nil {
		t.Fatal("public token accepted")
	}
	os.Chmod(r.path(c.Node.TokenFile), 0600)
	put(t, r, c.Node.TokenFile, []byte("invalid token\n"))
	if _, err = Inspect(context.Background(), r, runner, c, freshSnapshot()); err == nil {
		t.Fatal("empty token accepted")
	}
}

// TestAtomicWritePermissionsSymlinksAndNoop checks private writes, unchanged-file handling and rejection of symlink or directory destinations.
func TestAtomicWritePermissionsSymlinksAndNoop(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "state")
	os.WriteFile(p, []byte("before"), 0644)
	if err := AtomicWrite(p, []byte("after"), 0600); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(p)
	if info.Mode().Perm() != 0600 {
		t.Fatal("not private")
	}
	before := info.ModTime()
	if err := AtomicWrite(p, []byte("after"), 0600); err != nil {
		t.Fatal(err)
	}
	info, _ = os.Stat(p)
	if !info.ModTime().Equal(before) {
		t.Fatal("unchanged file replaced")
	}
	link := filepath.Join(dir, "link")
	os.Symlink(p, link)
	if AtomicWrite(link, []byte("bad"), 0600) == nil {
		t.Fatal("followed symlink")
	}
	data, _ := os.ReadFile(p)
	if string(data) != "after" {
		t.Fatal("modified symlink target")
	}
	if AtomicWrite(dir, []byte("bad"), 0600) == nil {
		t.Fatal("overwrote directory")
	}
}

// TestExecIsolationAndSecretRedaction checks child environment isolation, suppressed failure output and stdin handling.
func TestExecIsolationAndSecretRedaction(t *testing.T) {
	t.Setenv("KUBECONFIG", "/some/other/cluster")
	t.Setenv("K3S_TOKEN", "private")
	t.Setenv("HELM_PLUGINS", "/unsafe")
	t.Setenv("HTTPS_PROXY", "http://proxy.example.com")
	env := cleanEnv([]string{"HOME=/tmp/isolated"})
	for _, x := range env {
		if strings.HasPrefix(x, "KUBECONFIG=") || strings.HasPrefix(x, "K3S_TOKEN=") || strings.HasPrefix(x, "HELM_PLUGINS=") {
			t.Fatal(x)
		}
	}
	out, err := (ExecRunner{}).Run(context.Background(), Command{Name: "/bin/sh", Args: []string{"-c", "printf private; exit 1"}})
	if err == nil || strings.Contains(err.Error(), "private") || len(out) != 0 {
		t.Fatal("secret leaked", err)
	}
	out, err = (ExecRunner{}).Run(context.Background(), Command{Name: "/bin/cat", Input: []byte("stdin")})
	if err != nil || string(out) != "stdin" {
		t.Fatal(err)
	}
}

func TestExecCancellationStopsInstallerChildren(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := (ExecRunner{}).Run(ctx, Command{
			Name: "/bin/sh", Args: []string{"-c", `touch "$TEST_DIR/started"; (sleep 0.5; touch "$TEST_DIR/late-write") & wait`},
			IsolatedEnv: true, Env: []string{"TEST_DIR=" + dir},
		})
		done <- err
	}()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(dir, "started")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("test process failed to start")
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	if err := <-done; err == nil {
		t.Fatal("cancelled installer reported success")
	}
	time.Sleep(600 * time.Millisecond)
	if _, err := os.Stat(filepath.Join(dir, "late-write")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("installer child kept writing after cancellation")
	}
}

// TestSnapshotRaceDetection checks that snapshot comparison detects ownership and file changes.
func TestSnapshotRaceDetection(t *testing.T) {
	a := freshSnapshot()
	b := freshSnapshot()
	if !sameSnapshot(a, b) {
		t.Fatal()
	}
	b.Managed = true
	if sameSnapshot(a, b) {
		t.Fatal()
	}
	b = a
	b.Files = map[string][]byte{ConfigPath: []byte("new")}
	a.Files = map[string][]byte{ConfigPath: []byte("old")}
	if sameSnapshot(a, b) {
		t.Fatal()
	}
}

// TestUnsupportedDistributionAndEnvironment checks that old distributions and legacy service settings fail host checks.
func TestUnsupportedDistributionAndEnvironment(t *testing.T) {
	r := fixtureHost(t)
	put(t, r, "/etc/os-release", []byte("ID=ubuntu\nVERSION_ID=22.04\n"))
	put(t, r, "/etc/default/k3s", []byte("K3S_TOKEN=secret\n"))
	s, err := Inspect(context.Background(), r, runFunc(func(context.Context, Command) ([]byte, error) { return nil, errors.New("unexpected") }), testConfig(t), freshSnapshot())
	if err != nil || len(s.Problems) != 2 {
		t.Fatal(s, err)
	}
}

func TestAdditionalK3sServicesBlockLifecycle(t *testing.T) {
	r := fixtureHost(t)
	put(t, r, "/etc/systemd/system/k3s-other.service", []byte("another installation"))
	s, err := Inspect(context.Background(), r, &effects{}, testConfig(t), freshSnapshot())
	if err != nil || len(s.Problems) != 1 || !strings.Contains(s.Problems[0], "additional K3s service") {
		t.Fatal("shared service namespace was not blocked", err)
	}
}

func TestRepeatedUninstallWithEmptyDataMount(t *testing.T) {
	r := fixtureHost(t)
	if err := os.MkdirAll(r.path("/var/lib/rancher/k3s"), 0700); err != nil {
		t.Fatal(err)
	}
	s, err := Inspect(context.Background(), r, &effects{}, testConfig(t), freshSnapshot())
	if err != nil || !s.Existing || !s.EmptyDataOnly || checkUninstall(s) != nil {
		t.Fatal("empty retained data directory prevents repeat uninstall", err)
	}
	put(t, r, K3sPath, []byte("unmanaged binary"))
	s, err = Inspect(context.Background(), r, &effects{}, testConfig(t), freshSnapshot())
	if err != nil || s.EmptyDataOnly || checkUninstall(s) == nil {
		t.Fatal("unmanaged installation treated as empty", err)
	}
}
