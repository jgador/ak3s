package ak3s

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type runFunc func(context.Context, Command) ([]byte, error)

func (f runFunc) Run(c context.Context, cmd Command) ([]byte, error) { return f(c, cmd) }

type rootedReader struct{ root string }

func (r rootedReader) path(p string) string                    { return filepath.Join(r.root, p) }
func (r rootedReader) ReadFile(p string) ([]byte, error)       { return os.ReadFile(r.path(p)) }
func (r rootedReader) Stat(p string) (fs.FileInfo, error)      { return os.Stat(r.path(p)) }
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
func TestUnsupportedDistributionAndEnvironment(t *testing.T) {
	r := fixtureHost(t)
	put(t, r, "/etc/os-release", []byte("ID=ubuntu\nVERSION_ID=22.04\n"))
	put(t, r, "/etc/default/k3s", []byte("K3S_TOKEN=secret\n"))
	s, err := Inspect(context.Background(), r, runFunc(func(context.Context, Command) ([]byte, error) { return nil, errors.New("unexpected") }), testConfig(t), freshSnapshot())
	if err != nil || len(s.Problems) != 2 {
		t.Fatal(s, err)
	}
}
func TestServiceUnit(t *testing.T) {
	for _, role := range []string{"server", "agent"} {
		unit := string(serviceUnit(role))
		for _, want := range []string{"Delegate=yes", "KillMode=process", "--config " + ConfigPath, "ExecStart=" + K3sPath + " " + role} {
			if !strings.Contains(unit, want) {
				t.Fatal(unit)
			}
		}
	}
	if !reflect.DeepEqual(serviceUnit("server"), serviceUnit("server")) {
		t.Fatal("unstable service unit")
	}
}
