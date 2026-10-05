package ak3s

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeHost struct {
	s                    Snapshot
	inspectErr, applyErr error
	applies, inspections int
	plan                 Plan
}

// Inspect records inspection calls and returns the configured snapshot or error.
func (h *fakeHost) Inspect(context.Context, Config) (Snapshot, error) {
	h.inspections++
	return h.s, h.inspectErr
}

// Apply records the supplied plan and returns the configured apply error.
func (h *fakeHost) Apply(_ context.Context, p Plan, _ *Bundle) error {
	h.applies++
	h.plan = p
	return h.applyErr
}

type prepareFunc func(context.Context, Plan) (*Bundle, error)

// Prepare delegates artifact preparation to the test callback.
func (f prepareFunc) Prepare(ctx context.Context, p Plan) (*Bundle, error) { return f(ctx, p) }
func appFixture(t *testing.T) (*App, *fakeHost, *bytes.Buffer, *int) {
	t.Helper()
	out := &bytes.Buffer{}
	h := &fakeHost{s: freshSnapshot()}
	calls := new(int)
	app := &App{Host: h, Out: out, Err: out, ReadFile: func(string) ([]byte, error) { return []byte("acme_email: ops@example.com"), nil }, Runner: runFunc(func(context.Context, Command) ([]byte, error) { return []byte("ready"), nil }), Preparer: prepareFunc(func(_ context.Context, p Plan) (*Bundle, error) {
		*calls++
		return &Bundle{Rendered: map[string][]byte{"example": []byte("validated")}, Releases: map[string]string{}}, nil
	})}
	return app, h, out, calls
}

// TestCLIDryRunCannotMutate checks that dry-run plans and prepares without applying changes.
func TestCLIDryRunCannotMutate(t *testing.T) {
	for _, cmd := range []string{"install", "apply", "upgrade"} {
		t.Run(cmd, func(t *testing.T) {
			app, host, out, prepared := appFixture(t)
			if cmd == "upgrade" {
				host.s = managedSnapshot(t, testConfig(t))
			}
			if err := app.Run(context.Background(), []string{cmd, "--dry-run"}); err != nil {
				t.Fatal(err)
			}
			if host.applies != 0 || *prepared != 1 || host.inspections != 1 || !strings.Contains(out.String(), "no persistent changes") {
				t.Fatal("dry-run bypassed shared planning or mutated host")
			}
		})
	}
}

// TestCLIDryRunStillRendersBlockedHost checks that dry-run renders blocked hosts while real installs stop before preparation.
func TestCLIDryRunStillRendersBlockedHost(t *testing.T) {
	app, h, out, calls := appFixture(t)
	h.s.Systemd = false
	err := app.Run(context.Background(), []string{"install", "--dry-run"})
	if err == nil || *calls != 1 || h.applies != 0 || !strings.Contains(out.String(), "BLOCKED") {
		t.Fatal(err)
	}
	app, h, _, calls = appFixture(t)
	h.s.Systemd = false
	if err = app.Run(context.Background(), []string{"install"}); err == nil || *calls != 0 || h.applies != 0 {
		t.Fatal("unsupported host mutated")
	}
}

// TestCLIFailureBoundaries checks that CLI failures prevent later apply operations.
func TestCLIFailureBoundaries(t *testing.T) {
	for _, stage := range []string{"configuration", "inspect", "ownership", "prepare", "apply", "root"} {
		t.Run(stage, func(t *testing.T) {
			app, h, _, _ := appFixture(t)
			switch stage {
			case "configuration":
				app.ReadFile = func(string) ([]byte, error) { return []byte("unknown: true"), nil }
			case "inspect":
				h.inspectErr = errors.New("inspect failure")
			case "ownership":
				h.s.Existing = true
			case "prepare":
				app.Preparer = prepareFunc(func(context.Context, Plan) (*Bundle, error) { return nil, errors.New("checksum failure") })
			case "apply":
				h.applyErr = errors.New("service failure")
			case "root":
				h.s.Root = false
			}
			if err := app.Run(context.Background(), []string{"install"}); err == nil {
				t.Fatal("failure ignored")
			}
			if stage != "apply" && h.applies != 0 {
				t.Fatal("applied after failure")
			}
		})
	}
}

// TestCLICommandsFlagsAndConfigSelection checks read-only commands, flag validation and configuration file selection.
func TestCLICommandsFlagsAndConfigSelection(t *testing.T) {
	for _, cmd := range []string{"config", "version", "help", "--help"} {
		app, h, out, _ := appFixture(t)
		if err := app.Run(context.Background(), []string{cmd}); err != nil {
			t.Fatal(err)
		}
		if out.Len() == 0 || h.inspections > 0 {
			t.Fatal("read-only command inspected host")
		}
	}
	for _, args := range [][]string{{"invalid"}, {"install", "extra"}, {"install", "--output", "bad"}, {"status", "--dry-run"}, {"config", "--defaults", "--config", "file"}, {"render", "--dry-run"}} {
		app, _, _, _ := appFixture(t)
		if err := app.Run(context.Background(), args); err == nil {
			t.Fatal("invalid flags accepted", args)
		}
	}
	app, _, out, _ := appFixture(t)
	app.ReadFile = func(path string) ([]byte, error) {
		if path != "/etc/ak3s/values.yaml" {
			t.Fatal(path)
		}
		return nil, fs.ErrNotExist
	}
	if err := app.Run(context.Background(), []string{"config"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "metrics_retention: 7d") {
		t.Fatal(out)
	}
	app.ReadFile = func(string) ([]byte, error) { return nil, fs.ErrNotExist }
	if err := app.Run(context.Background(), []string{"config", "--config", "missing"}); err == nil {
		t.Fatal("explicit missing file ignored")
	}
	if err := app.Run(context.Background(), []string{"config", "--defaults"}); err != nil {
		t.Fatal(err)
	}
}

// TestCLIApplyAndTempCleanup checks that apply runs once and removes its temporary bundle.
func TestCLIApplyAndTempCleanup(t *testing.T) {
	app, h, _, _ := appFixture(t)
	dir := filepath.Join(t.TempDir(), "temporary")
	os.Mkdir(dir, 0700)
	app.Preparer = prepareFunc(func(context.Context, Plan) (*Bundle, error) { return &Bundle{Dir: dir}, nil })
	if err := app.Run(context.Background(), []string{"apply"}); err != nil {
		t.Fatal(err)
	}
	if h.applies != 1 {
		t.Fatal("apply missing")
	}
	if _, err := os.Stat(dir); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("workspace not removed")
	}
}

// TestCLIStatusNeverUsesAmbientContext checks that status uses the managed kubeconfig without preparing or applying changes.
func TestCLIStatusNeverUsesAmbientContext(t *testing.T) {
	app, h, out, calls := appFixture(t)
	h.s = managedSnapshot(t, testConfig(t))
	app.Runner = runFunc(func(_ context.Context, c Command) ([]byte, error) {
		args := strings.Join(c.Args, " ")
		if !strings.Contains(args, "--kubeconfig "+KubeconfigPath) || !strings.Contains(args, "--cache-dir") || !strings.Contains(args, " get ") {
			t.Fatal(c)
		}
		return []byte("observed"), nil
	})
	if err := app.Run(context.Background(), []string{"status"}); err != nil {
		t.Fatal(err)
	}
	if *calls != 0 || h.applies != 0 || !strings.Contains(out.String(), "observed") {
		t.Fatal("status mutated")
	}
	h.s.Managed = false
	if err := app.Run(context.Background(), []string{"status"}); err == nil {
		t.Fatal("unmanaged status reported healthy")
	}
}

// TestCLIStatusIdentifiesMissingResource keeps partial status output and identifies a failed resource query.
func TestCLIStatusIdentifiesMissingResource(t *testing.T) {
	app, h, out, calls := appFixture(t)
	h.s = managedSnapshot(t, testConfig(t))
	failure := errors.New("resource unavailable")
	app.Runner = runFunc(func(_ context.Context, c Command) ([]byte, error) {
		if strings.Contains(strings.Join(c.Args, " "), "get clusterissuers ") {
			return nil, failure
		}
		return []byte("observed"), nil
	})
	err := app.Run(context.Background(), []string{"status"})
	if !errors.Is(err, failure) || !strings.Contains(err.Error(), "read cluster clusterissuers") {
		t.Fatalf("missing resource query context: %v", err)
	}
	if !strings.Contains(out.String(), "observed") || *calls != 0 || h.applies != 0 {
		t.Fatal("lost partial status or changed the host")
	}
}

// TestRenderOutput checks that render exports configuration and resources without inspecting or changing the host.
func TestRenderOutput(t *testing.T) {
	app, h, _, _ := appFixture(t)
	dir := t.TempDir()
	app.Preparer = prepareFunc(func(context.Context, Plan) (*Bundle, error) {
		return &Bundle{Rendered: map[string][]byte{"example": []byte("apiVersion: v1\nkind: Namespace\nmetadata: {name: test}\n")}, Values: map[string][]byte{"example": []byte("enabled: true\n")}, Manifests: map[string][]byte{"roles.yaml": []byte("test")}}, nil
	})
	if err := app.Run(context.Background(), []string{"render", "--output", dir}); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"k3s-config.yaml", "rendered/example.yaml", "values/example.yaml", "manifests/roles.yaml"} {
		data, err := os.ReadFile(filepath.Join(dir, p))
		if err != nil || len(data) == 0 {
			t.Fatal("missing rendered output", p, err)
		}
	}
	if h.inspections != 0 || h.applies != 0 {
		t.Fatal("render touched host")
	}
}
