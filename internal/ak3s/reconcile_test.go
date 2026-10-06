package ak3s

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"strings"
	"testing"
)

type effects struct {
	commands    []Command
	writes      []string
	states      []State
	failCommand string
	failWrite   string
}

// Run records commands and injects the configured command failure.
func (e *effects) Run(_ context.Context, c Command) ([]byte, error) {
	e.commands = append(e.commands, c)
	if strings.Contains(c.Name+" "+strings.Join(c.Args, " "), e.failCommand) && e.failCommand != "" {
		return nil, errors.New("injected command failure")
	}
	return nil, nil
}
func (e *effects) write(path string, b []byte, _ fs.FileMode) error {
	e.writes = append(e.writes, path)
	if path == e.failWrite {
		return errors.New("injected write failure")
	}
	if path == StatePath {
		var s State
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		e.states = append(e.states, s)
	}
	return nil
}
func (e *effects) has(needle string) bool {
	for _, c := range e.commands {
		if strings.Contains(c.Name+" "+strings.Join(c.Args, " "), needle) {
			return true
		}
	}
	return false
}

// TestRealReconcileOrderAndIdempotency checks readiness, chart ordering and restart checkpoints across repeated apply operations.
func TestRealReconcileOrderAndIdempotency(t *testing.T) {
	c := testConfig(t)
	p, err := BuildPlan(c, testPins(t), managedSnapshot(t, c), "apply")
	if err != nil {
		t.Fatal(err)
	}
	b := &Bundle{Dir: t.TempDir(), Helm: "/fake/helm", Manifests: map[string][]byte{"issuers.yaml": []byte("issuers"), "headlamp-rbac.yaml": []byte("headlamp"), "metrics-rbac.yaml": []byte("metrics"), "dashboard.yaml": []byte("dashboard")}}
	for iteration := 0; iteration < 2; iteration++ {
		e := &effects{}
		if err = reconcile(context.Background(), p, b, e, e.write, nil); err != nil {
			t.Fatal(err)
		}
		if e.has("restart") || e.has("/bin/k3s --version") || e.has("/bin/sh") {
			t.Fatal("unchanged node restarted")
		}
		if !e.has("-n ak3s rollout status deployment/dashboard --timeout=180s") {
			t.Fatal("dashboard readiness check missing")
		}
		if !e.has("wait --for=condition=Ready node/node1") {
			t.Fatal("readiness check missing")
		}
		helmCalls := 0
		certIndex := -1
		issuerIndex := -1
		for i, cmd := range e.commands {
			if cmd.Name == b.Helm {
				helmCalls++
				args := strings.Join(cmd.Args, " ")
				for _, s := range []string{"--reset-values", "--atomic", "--wait", "--kubeconfig " + KubeconfigPath} {
					if !strings.Contains(args, s) {
						t.Fatal(args)
					}
				}
				if strings.Contains(args, "--install cert-manager ") {
					certIndex = i
				}
			}
			if string(cmd.Input) == "issuers" {
				issuerIndex = i
			}
		}
		if helmCalls != 6 || certIndex < 0 || issuerIndex <= certIndex {
			t.Fatal("bad platform ordering")
		}
		if len(e.states) != 2 || e.states[0].PendingRestart || e.states[1].PendingRestart {
			t.Fatal("restart checkpoint wrong", e.states)
		}
	}
}

func TestReconcileSharedDashboardIngressLifecycle(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		for _, fail := range []bool{false, true} {
			c := testConfig(t)
			if enabled {
				c.DashboardHostname = "ak3s.example.com"
			}
			p, err := BuildPlan(c, testPins(t), managedSnapshot(t, c), "apply")
			if err != nil {
				t.Fatal(err)
			}
			b := &Bundle{Dir: t.TempDir(), Helm: "/fake/helm", Manifests: map[string][]byte{}}
			e := &effects{}
			if fail {
				e.failCommand = "delete ingress"
			}
			err = reconcile(context.Background(), p, b, e, e.write, nil)
			if !enabled && fail {
				if err == nil || !strings.Contains(err.Error(), "remove disabled dashboard ingress") {
					t.Fatal("ingress removal failure ignored")
				}
				continue
			}
			if err != nil {
				t.Fatal(err)
			}
			if e.has("delete ingress") == enabled {
				t.Fatal("ingress removal does not follow configuration")
			}
			if !enabled && !e.has("--selector=app.kubernetes.io/managed-by=ak3s,app.kubernetes.io/name=ak3s-dashboard --ignore-not-found") {
				t.Fatal("ingress removal is not restricted to managed dashboard resources")
			}
		}
	}
}

// TestReconcileWaitsForNodeRegistration simulates an API that becomes ready before its local node exists.
func TestReconcileWaitsForNodeRegistration(t *testing.T) {
	c := testConfig(t)
	p, err := BuildPlan(c, testPins(t), managedSnapshot(t, c), "apply")
	if err != nil {
		t.Fatal(err)
	}
	b := &Bundle{Dir: t.TempDir(), Helm: "/fake/helm", Manifests: map[string][]byte{}}
	e := &effects{}
	registered := false
	ready := false
	runner := runFunc(func(ctx context.Context, cmd Command) ([]byte, error) {
		args := strings.Join(cmd.Args, " ")
		if cmd.Name == K3sPath && strings.Contains(args, "wait --for=create node/"+p.Config.Node.Name) {
			registered = true
		}
		if cmd.Name == K3sPath && strings.Contains(args, "wait --for=condition=Ready node/"+p.Config.Node.Name) {
			if !registered {
				return nil, errors.New("node not found")
			}
			ready = true
		}
		if cmd.Name == b.Helm && !ready {
			t.Fatal("installed a chart before the local node was Ready")
		}
		return e.Run(ctx, cmd)
	})
	if err = reconcile(context.Background(), p, b, runner, e.write, nil); err != nil {
		t.Fatal(err)
	}
	if !ready || !e.has("upgrade --install nginx-ingress") {
		t.Fatal("reconciliation did not continue after node registration and readiness")
	}
}

// TestReconcileStopsBeforeChartsOnNodeFailure preserves the restart checkpoint when node registration or readiness fails.
func TestReconcileStopsBeforeChartsOnNodeFailure(t *testing.T) {
	c := testConfig(t)
	p, err := BuildPlan(c, testPins(t), managedSnapshot(t, c), "apply")
	if err != nil {
		t.Fatal(err)
	}
	b := &Bundle{Dir: t.TempDir(), Helm: "/fake/helm", Manifests: map[string][]byte{}}
	for _, stage := range []struct {
		command string
		message string
	}{
		{"wait --for=create node/", "wait for local node registration"},
		{"wait --for=condition=Ready node/", "wait for local node readiness"},
	} {
		t.Run(stage.message, func(t *testing.T) {
			e := &effects{failCommand: stage.command}
			err := reconcile(context.Background(), p, b, e, e.write, nil)
			if err == nil || !strings.Contains(err.Error(), stage.message) {
				t.Fatalf("missing failure context: %v", err)
			}
			if e.has("upgrade --install") {
				t.Fatal("installed charts after a node wait failed")
			}
			if len(e.states) != 1 || e.states[0].PendingRestart {
				t.Fatal("cleared restart checkpoint before node readiness", e.states)
			}
		})
	}
}

// TestReconcileStopsOnFailures checks that command and file write failures stop later reconciliation steps.
func TestReconcileStopsOnFailures(t *testing.T) {
	c := testConfig(t)
	p, _ := BuildPlan(c, testPins(t), managedSnapshot(t, c), "apply")
	b := &Bundle{Dir: t.TempDir(), Helm: "/fake/helm", Manifests: map[string][]byte{}}
	for _, failure := range []string{"apt-get update", "modprobe overlay", "sysctl -p", "upgrade --install nginx-ingress", "upgrade --install cert-manager"} {
		t.Run(failure, func(t *testing.T) {
			e := &effects{failCommand: failure}
			if err := reconcile(context.Background(), p, b, e, e.write, nil); err == nil {
				t.Fatal("failure ignored")
			}
			if e.has("upgrade --install headlamp") {
				t.Fatal("continued after failure")
			}
			if failure == "apt-get update" && len(e.writes) > 0 {
				t.Fatal("wrote files before package preparation")
			}
		})
	}
	for _, path := range []string{MarkerPath, StatePath, ConfigPath} {
		e := &effects{failWrite: path}
		if err := reconcile(context.Background(), p, b, e, e.write, nil); err == nil {
			t.Fatal("write failure ignored")
		}
		if e.has("systemctl") {
			t.Fatal("started with incomplete configuration")
		}
	}
}

// TestUnverifiedBundleCannotStartK3s rejects a bundle before writing or starting K3s.
func TestUnverifiedBundleCannotStartK3s(t *testing.T) {
	c := testConfig(t)
	p, _ := BuildPlan(c, testPins(t), freshSnapshot(), "install")
	e := &effects{}
	err := reconcile(context.Background(), p, &Bundle{K3s: []byte("corrupt")}, e, e.write, nil)
	if err == nil || e.has("systemctl") {
		t.Fatal("corrupt binary executed")
	}
	for _, path := range e.writes {
		if path == K3sPath {
			t.Fatal("corrupt binary written")
		}
	}
}

// TestAgentReconcileDoesNotRunPlatform checks that agents verify their service without managing shared platform resources.
func TestAgentReconcileDoesNotRunPlatform(t *testing.T) {
	c := testConfig(t)
	c.Platform = false
	c.Node.Role = "agent"
	c.Node.Join = true
	c.Node.TokenFile = "/root/token"
	c.KubernetesAPIEndpoint = "10.0.0.1"
	p, _ := BuildPlan(c, testPins(t), managedSnapshot(t, c), "apply")
	e := &effects{}
	if err := reconcile(context.Background(), p, &Bundle{}, e, e.write, nil); err != nil {
		t.Fatal(err)
	}
	if e.has("kubectl") || !e.has("systemctl is-active --quiet k3s-agent") {
		t.Fatal("agent behavior incorrect")
	}
}

// TestWaitReadyCancellation checks cancelled readiness waits and successful probes.
func TestWaitReadyCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := waitReady(ctx, func() error { return errors.New("unready") }, 0); err == nil {
		t.Fatal("cancellation ignored")
	}
	if err := waitReady(context.Background(), func() error { return nil }, 0); err != nil {
		t.Fatal(err)
	}
}

// TestApplyLock checks that only one reconciliation can hold the local lock at a time.
func TestApplyLock(t *testing.T) {
	dir := t.TempDir()
	unlock, err := lock(dir)
	if err != nil {
		t.Fatal(err)
	}
	if second, err := lock(dir); err == nil {
		second()
		t.Fatal("concurrent reconciliation allowed")
	}
	unlock()
	next, err := lock(dir)
	if err != nil {
		t.Fatal(err)
	}
	next()
}

func TestDashboardRolloutFailureRequiresRerun(t *testing.T) {
	c := testConfig(t)
	p, err := BuildPlan(c, testPins(t), managedSnapshot(t, c), "apply")
	if err != nil {
		t.Fatal(err)
	}
	b := &Bundle{Dir: t.TempDir(), Helm: "/fake/helm", Manifests: map[string][]byte{"dashboard.yaml": []byte("dashboard")}}
	e := &effects{failCommand: "rollout status deployment/dashboard"}
	if err := reconcile(context.Background(), p, b, e, e.write, nil); err == nil || !strings.Contains(err.Error(), "dashboard") {
		t.Fatal("dashboard failure ignored")
	}
	e.failCommand = ""
	if err := reconcile(context.Background(), p, b, e, e.write, nil); err != nil {
		t.Fatal(err)
	}
}
