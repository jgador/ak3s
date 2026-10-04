package ak3s

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

func lock(dir string) (func(), error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(dir, "apply.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, errors.New("another AK3S reconciliation is running")
	}
	return func() { syscall.Flock(int(f.Fd()), syscall.LOCK_UN); f.Close() }, nil
}
func (h *NativeHost) Apply(ctx context.Context, p Plan, b *Bundle) error {
	if !p.Snapshot.Root {
		return errors.New("run install/apply/upgrade as root")
	}
	if err := p.Check(); err != nil {
		return err
	}
	unlock, err := lock(filepath.Dir(StatePath))
	if err != nil {
		return err
	}
	defer unlock()
	// Recheck state after acquiring the lock: do not apply a stale ownership/token plan.
	fresh, err := h.Inspect(ctx, p.Config)
	if err != nil {
		return err
	}
	again, err := BuildPlan(p.Config, p.Pins, fresh, upgradeCommand(p))
	if err != nil {
		return err
	}
	if err = again.Check(); err != nil {
		return err
	}
	if !sameSnapshot(p.Snapshot, fresh) {
		return errors.New("host changed while planning; rerun AK3S")
	}
	return reconcile(ctx, p, b, h.Runner, AtomicWrite)
}

// Reconcile has two explicit side-effect boundaries, commands and file writes.
// Tests execute this real ordering with fake operations and no root privileges.
func reconcile(ctx context.Context, p Plan, b *Bundle, runner Runner, write func(string, []byte, fs.FileMode) error) error {
	var err error
	if p.InstallBinary {
		if err := Verify(b.K3s, p.Pins.K3sSHA256[p.Snapshot.Arch]); err != nil {
			return err
		}
	}
	run := func(name string, args ...string) error {
		_, err := runner.Run(ctx, Command{Name: name, Args: args})
		return err
	}
	if err = run("apt-get", "update"); err != nil {
		return err
	}
	if _, err = runner.Run(ctx, Command{Name: "apt-get", Args: []string{"install", "-y", "ca-certificates", "iptables", "kmod"}, Env: []string{"DEBIAN_FRONTEND=noninteractive"}}); err != nil {
		return err
	}
	for _, module := range []string{"overlay", "br_netfilter"} {
		if err = run("modprobe", module); err != nil {
			return err
		}
	}
	if err = write(MarkerPath, []byte("Managed by AK3S. Configuration: "+ConfigPath+"\n"), 0600); err != nil {
		return err
	}
	// Checkpoint identity before K3s starts, so interrupted installs retain guards.
	if err = write(StatePath, p.StateJSON(), 0600); err != nil {
		return err
	}
	for _, path := range []string{ModulesPath, SysctlPath, ConfigPath, unitPath(p.Config.Node.Role)} {
		mode := fs.FileMode(0644)
		if path == ConfigPath {
			mode = 0600
		}
		if err = write(path, p.Files[path], mode); err != nil {
			return err
		}
	}
	if err = run("sysctl", "-p", SysctlPath); err != nil {
		return err
	}
	if p.InstallBinary {
		if err = Verify(b.K3s, p.Pins.K3sSHA256[p.Snapshot.Arch]); err != nil {
			return err
		}
		if err = write(K3sPath, b.K3s, 0755); err != nil {
			return err
		}
	}
	if err = run("systemctl", "daemon-reload"); err != nil {
		return err
	}
	service := serviceName(p.Config.Node.Role)
	if err = run("systemctl", "enable", service); err != nil {
		return err
	}
	action := "start"
	if p.Restart {
		action = "restart"
	}
	if err = run("systemctl", action, service); err != nil {
		return err
	}
	if p.Config.Node.Role == "agent" {
		if err = run("systemctl", "is-active", "--quiet", service); err != nil {
			return err
		}
		p.State.PendingRestart = false
		return write(StatePath, p.StateJSON(), 0600)
	}
	kube := func(input []byte, args ...string) error {
		_, err := runner.Run(ctx, Command{Name: K3sPath, Args: append([]string{"kubectl", "--kubeconfig", KubeconfigPath, "--cache-dir", filepath.Join(b.Dir, "kube-cache")}, args...), Input: input})
		return err
	}
	if err = waitReady(ctx, func() error { return kube(nil, "get", "--raw=/readyz", "--request-timeout=10s") }, 5*time.Minute); err != nil {
		return err
	}
	name := p.Config.Node.Name
	if name == "" {
		name = p.Snapshot.Hostname
	}
	if err = kube(nil, "wait", "--for=condition=Ready", "node/"+name, "--timeout=180s"); err != nil {
		return err
	}
	p.State.PendingRestart = false
	if err = write(StatePath, p.StateJSON(), 0600); err != nil {
		return err
	}
	if !p.Config.Platform {
		return nil
	}
	for _, chart := range p.Pins.Charts {
		if _, err = runner.Run(ctx, Command{Name: b.Helm, Args: helmUpgradeArgs(chart, b), Env: b.HelmEnv()}); err != nil {
			return fmt.Errorf("reconcile %s: %w; fix the error and rerun", chart.Release, err)
		}
		if chart.Release == "cert-manager" {
			if err = kube(nil, "wait", "--for=condition=Established", "crd/clusterissuers.cert-manager.io", "--timeout=120s"); err != nil {
				return err
			}
			if err = kube(b.Manifests["issuers.yaml"], "apply", "-f", "-"); err != nil {
				return err
			}
		}
	}
	for _, name := range []string{"headlamp-rbac.yaml", "metrics-rbac.yaml"} {
		if err = kube(b.Manifests[name], "apply", "-f", "-"); err != nil {
			return err
		}
	}
	return nil
}
func upgradeCommand(p Plan) string {
	if p.Snapshot.K3sVersion != "" && p.InstallBinary {
		return "upgrade"
	}
	return "apply"
}
func sameSnapshot(a, b Snapshot) bool {
	if a.K3sVersion != b.K3sVersion || a.Managed != b.Managed || a.Existing != b.Existing || a.TokenSHA256 != b.TokenSHA256 || a.ServiceActive != b.ServiceActive || a.Hostname != b.Hostname || a.Role != b.Role {
		return false
	}
	for path, data := range a.Files {
		if string(data) != string(b.Files[path]) {
			return false
		}
	}
	return true
}
func waitReady(ctx context.Context, probe func() error, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var last error
	for {
		if last = probe(); last == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("K3s did not become ready: %w: %v", ctx.Err(), last)
		case <-time.After(2 * time.Second):
		}
	}
}
