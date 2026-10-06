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

// lock prevents concurrent local apply operations and returns a function to release the lock.
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

// Apply locks the host, revalidates its snapshot, then executes the prepared plan.
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

	// Recheck ownership and token state after acquiring the lock to reject a stale plan.
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
	return reconcile(ctx, p, b, h.Runner, AtomicWrite, h.Reader)
}

// reconcile changes the host through commands and file writes.
// Tests verify this execution order with simulated operations and no root privileges.
func reconcile(ctx context.Context, p Plan, b *Bundle, runner Runner, write func(string, []byte, fs.FileMode) error, reader Reader) error {
	var err error
	if p.RunInstaller {
		if err := Verify(b.Installer, p.Pins.Installer.SHA256); err != nil {
			return err
		}
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
	if _, err = runner.Run(ctx, Command{Name: "apt-get", Args: []string{"install", "-y", "ca-certificates", "curl", "iptables", "kmod"}, Env: []string{"DEBIAN_FRONTEND=noninteractive"}}); err != nil {
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

	// Record identity before K3s starts so interrupted installs retain identity checks.
	if err = write(StatePath, p.StateJSON(), 0600); err != nil {
		return err
	}
	for _, path := range []string{ModulesPath, SysctlPath, ConfigPath} {
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
	if p.RunInstaller {
		if err = installK3s(ctx, p, b, runner, write); err != nil {
			return err
		}
		installed, err := reader.ReadFile(K3sPath)
		if err != nil {
			return err
		}
		if err = Verify(installed, p.Pins.K3sSHA256[p.Snapshot.Arch]); err != nil {
			return fmt.Errorf("verify installed K3s: %w", err)
		}
		unit, err := reader.ReadFile(unitPath(p.Config.Node.Role))
		if err != nil {
			return err
		}
		p.State.ServiceSHA256 = fingerprint(unit)
		p.State.GeneratedScripts, err = generatedScripts(reader, p.Config.Node.Role)
		if err != nil {
			return err
		}
		// Retain verified removal scripts even if readiness fails afterwards.
		// PendingRestart remains set until the local readiness checks succeed.
		if err = write(StatePath, p.StateJSON(), 0600); err != nil {
			return err
		}
	}
	service := serviceName(p.Config.Node.Role)
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

	// Shared add-ons require both a ready API and a ready local server node.
	if err = waitReady(ctx, func() error { return kube(nil, "get", "--raw=/readyz", "--request-timeout=10s") }, 5*time.Minute); err != nil {
		return err
	}
	name := p.Config.Node.Name
	if name == "" {
		name = p.Snapshot.Hostname
	}
	// The API can be ready before the local kubelet registers its node.
	// A condition wait fails immediately if the named node does not exist.
	if err = kube(nil, "wait", "--for=create", "node/"+name, "--timeout=180s"); err != nil {
		return fmt.Errorf("wait for local node registration: %w", err)
	}
	if err = kube(nil, "wait", "--for=condition=Ready", "node/"+name, "--timeout=180s"); err != nil {
		return fmt.Errorf("wait for local node readiness: %w", err)
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

		// Issuers cannot be applied until cert-manager's custom resource exists.
		if chart.Release == "cert-manager" {
			if err = kube(nil, "wait", "--for=condition=Established", "crd/clusterissuers.cert-manager.io", "--timeout=120s"); err != nil {
				return fmt.Errorf("wait for ClusterIssuer resource registration: %w", err)
			}
			if err = kube(b.Manifests["issuers.yaml"], "apply", "-f", "-"); err != nil {
				return fmt.Errorf("apply ClusterIssuers: %w", err)
			}
		}
	}
	for _, name := range []string{"headlamp-rbac.yaml", "metrics-rbac.yaml", "dashboard.yaml"} {
		if err = kube(b.Manifests[name], "apply", "-f", "-"); err != nil {
			return fmt.Errorf("apply %s: %w", name, err)
		}
	}
	if err = kube(nil, "-n", "ak3s", "rollout", "status", "deployment/dashboard", "--timeout=180s"); err != nil {
		return fmt.Errorf("wait for AK3S dashboard: %w; check the dashboard image and rerun", err)
	}
	return nil
}
func upgradeCommand(p Plan) string {
	if p.Snapshot.K3sVersion != "" && p.InstallBinary {
		return "upgrade"
	}
	return "apply"
}

// sameSnapshot detects relevant host changes between planning and lock acquisition.
func sameSnapshot(a, b Snapshot) bool {
	if a.K3sVersion != b.K3sVersion || a.Managed != b.Managed || a.Existing != b.Existing || a.DataPresent != b.DataPresent || a.EmptyDataOnly != b.EmptyDataOnly || a.TokenSHA256 != b.TokenSHA256 || a.ServiceActive != b.ServiceActive || a.ServiceEnabled != b.ServiceEnabled || a.Hostname != b.Hostname || a.Role != b.Role || len(a.Files) != len(b.Files) {
		return false
	}
	for path, data := range a.Files {
		if string(data) != string(b.Files[path]) {
			return false
		}
	}
	return true
}

// waitReady retries a readiness probe until it succeeds, times out, or is cancelled.
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
