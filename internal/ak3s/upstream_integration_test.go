//go:build integration

package ak3s

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestPinnedInstaller executes the verified, unmodified upstream script against
// temporary destinations and simulated host commands. It never runs K3s or a
// generated cleanup script and never invokes the real service manager.
func TestPinnedInstaller(t *testing.T) {
	if _, err := os.Stat("/sbin/openrc-run"); err == nil {
		t.Skip("this contract test requires the systemd installer path")
	}
	pins := testPins(t)
	script, err := (HTTPDownloader{}).Get(context.Background(), pins.Installer.URL(), pins.Installer.SHA256)
	if err != nil {
		t.Fatal(err)
	}
	for _, role := range []string{"server", "agent"} {
		t.Run(role, func(t *testing.T) {
			root := t.TempDir()
			tools := filepath.Join(root, "tools")
			bin := filepath.Join(root, "bin")
			units := filepath.Join(root, "systemd")
			for _, path := range []string{tools, bin, units} {
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			}
			stubs := map[string]string{
				"id":        "#!/bin/sh\nprintf '0\\n'\n",
				"chown":     "#!/bin/sh\nexit 0\n",
				"systemctl": "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$AK3S_TEST_ROOT/service-calls\"\n",
				"rm": `#!/bin/sh
for arg do
  case "$arg" in
    -*) ;;
    "$AK3S_TEST_ROOT"/*) ;;
    /etc/systemd/system/k3s.service|/etc/systemd/system/k3s.service.env|/etc/systemd/system/k3s-agent.service|/etc/systemd/system/k3s-agent.service.env) exit 0 ;;
    *) echo 'Refusing removal outside test directory' >&2; exit 1 ;;
  esac
done
exec /bin/rm "$@"
`,
			}
			for _, name := range []string{"iptables-save", "iptables-restore", "ip6tables-save", "ip6tables-restore"} {
				stubs[name] = "#!/bin/sh\nexit 0\n"
			}
			for name, data := range stubs {
				if err := os.WriteFile(filepath.Join(tools, name), []byte(data), 0700); err != nil {
					t.Fatal(err)
				}
			}
			b := &Bundle{Dir: root, Installer: script, K3s: []byte("fake binary; never executed")}
			p := Plan{Config: Config{Node: NodeConfig{Role: role}}, Pins: pins, Snapshot: freshSnapshot(), Restart: true}
			p.Pins.K3sSHA256[p.Snapshot.Arch] = fingerprint(b.K3s)
			runner := runFunc(func(ctx context.Context, c Command) ([]byte, error) {
				// Replace only filesystem destinations and system tools. Exercise
				// the production artifact URL, pinned version, args and stdin.
				cmd := exec.CommandContext(ctx, c.Name, c.Args...)
				cmd.Stdin = strings.NewReader(string(c.Input))
				for _, value := range c.Env {
					if !strings.HasPrefix(value, "INSTALL_K3S_BIN_DIR=") && !strings.HasPrefix(value, "INSTALL_K3S_SYSTEMD_DIR=") {
						cmd.Env = append(cmd.Env, value)
					}
				}
				cmd.Env = append(cmd.Env, "PATH="+tools+":/usr/bin:/bin", "TMPDIR="+root, "AK3S_TEST_ROOT="+root,
					"INSTALL_K3S_BIN_DIR="+bin, "INSTALL_K3S_SYSTEMD_DIR="+units, "INSTALL_K3S_SKIP_SELINUX_RPM=true")
				out, err := cmd.CombinedOutput()
				if err != nil {
					t.Log(string(out)) // only fictional test data and temporary paths
				}
				return out, err
			})
			for _, step := range []string{"fresh", "unchanged", "config", "upgrade"} {
				before, _ := os.ReadFile(filepath.Join(root, "service-calls"))
				if step == "upgrade" {
					b.K3s = []byte("upgraded fake binary; never executed")
					p.Pins.K3s = "v1.37.2+k3s1"
					p.Pins.K3sSHA256[p.Snapshot.Arch] = fingerprint(b.K3s)
				}
				p.Restart = step == "fresh" || step == "config"
				if err := installK3s(context.Background(), p, b, runner, AtomicWrite); err != nil {
					t.Fatal(step, err)
				}
				after, _ := os.ReadFile(filepath.Join(root, "service-calls"))
				newCalls := string(after[len(before):])
				if strings.Contains(newCalls, "restart "+serviceName(role)) != (step != "unchanged") {
					t.Fatal("unexpected restart behavior", step, newCalls)
				}
				installed, err := os.ReadFile(filepath.Join(bin, "k3s"))
				if err != nil || Verify(installed, p.Pins.K3sSHA256[p.Snapshot.Arch]) != nil {
					t.Fatal("upstream failed to install prepared artifact", err)
				}
				p.Snapshot.ServiceActive = true
			}
			unit, _ := os.ReadFile(filepath.Join(units, serviceName(role)+".service"))
			if !strings.Contains(string(unit), "Type=notify") || !strings.Contains(string(unit), "'--config'") || !strings.Contains(string(unit), ConfigPath) {
				t.Fatal("upstream unit does not use managed configuration")
			}
			env, err := os.ReadFile(filepath.Join(units, serviceName(role)+".service.env"))
			if err != nil || len(env) != 0 {
				t.Fatal("unexpected service environment", err)
			}
			for _, name := range []string{serviceName(role) + "-uninstall.sh", "k3s-killall.sh"} {
				cmd := exec.Command("/bin/sh", "-n", filepath.Join(bin, name))
				cmd.Env = []string{"PATH=/usr/bin:/bin"}
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("generated script syntax: %v: %s", err, out)
				}
			}
		})
	}
}
