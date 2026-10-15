package ak3s

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

const testApplicationsYAML = "apps:\n  app-db:\n    recipe: postgres\n    settings:\n      existing_secret: app-db-auth\n"

func testApplication(t *testing.T) (ApplicationsConfig, preparedApplication) {
	t.Helper()
	c, err := LoadApplicationsConfig([]byte(testApplicationsYAML))
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := loadApplicationCatalog()
	if err != nil {
		t.Fatal(err)
	}
	app := preparedApplication{Name: "app-db", Release: "ak3s-app-app-db", Namespace: "applications", Recipe: catalog["postgres"], Spec: c.Apps["app-db"]}
	app.PVC, err = applicationPVC(app)
	if err != nil {
		t.Fatal(err)
	}
	app.Inventory, err = applicationInventory(app)
	if err != nil {
		t.Fatal(err)
	}
	return c, app
}

func applicationObservation(t *testing.T, manifests ...[]byte) []byte {
	t.Helper()
	items := []map[string]any{}
	for _, manifest := range manifests {
		var object map[string]any
		if err := yaml.Unmarshal(manifest, &object); err != nil {
			t.Fatal(err)
		}
		items = append(items, object)
	}
	data, err := json.Marshal(map[string]any{"kind": "List", "items": items})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestApplicationsConfiguration(t *testing.T) {
	c, app := testApplication(t)
	if len(c.Apps) != 1 || app.Spec.Settings.Storage != "10Gi" || app.Spec.Settings.StorageClass != "local-path" || app.Spec.Settings.Database != "app" {
		t.Fatal("missing application defaults", c)
	}
	for _, data := range []string{
		"unknown: true", "apps: null", "apps: []", "apps: {db: {recipe: redis}}", "apps: {db: {recipe: postgres}}",
		strings.ReplaceAll(testApplicationsYAML, "existing_secret", "password"),
		strings.ReplaceAll(testApplicationsYAML, "recipe: postgres", "recipe: postgres\n    namespace: kube-system"),
		strings.ReplaceAll(testApplicationsYAML, "recipe: postgres", "recipe: postgres\n    namespace: observability"),
		strings.ReplaceAll(testApplicationsYAML, "existing_secret: app-db-auth", "existing_secret: app-db-auth\n      storage: 0Gi"),
		strings.ReplaceAll(testApplicationsYAML, "existing_secret: app-db-auth", "existing_secret: app-db-auth\n      storage: 99999999999999999999999Ti"),
		strings.ReplaceAll(testApplicationsYAML, "existing_secret: app-db-auth", "existing_secret: app-db-auth\n      database: template1"),
		strings.ReplaceAll(testApplicationsYAML, "existing_secret: app-db-auth", "existing_secret: app-db-auth\n      username: pg_internal"),
		testApplicationsYAML + "---\napps: {}\n",
	} {
		if _, err := LoadApplicationsConfig([]byte(data)); err == nil {
			t.Fatal("invalid application configuration accepted", data)
		}
	}
	if empty, err := LoadApplicationsConfig(nil); err != nil || len(empty.Apps) != 0 {
		t.Fatal("empty application configuration", err)
	}
}

func TestApplicationOwnershipStorageAndUpgradeChecks(t *testing.T) {
	for _, mode := range []string{"new", "existing", "missing-pvc-checkpoint", "missing-installed-pvc", "missing-pvc-after-uninstall", "foreign", "missing-inventory", "release-without-inventory", "size", "class", "database", "secret", "equivalent-size", "update", "explicit-update", "major", "downgrade"} {
		t.Run(mode, func(t *testing.T) {
			_, app := testApplication(t)
			app.Observed = applicationObservation(t, app.Inventory, app.PVC)
			installed, upgrade, wantError := true, false, true
			switch mode {
			case "new":
				app.Observed, installed, wantError = applicationObservation(t), false, false
			case "existing":
				wantError = false
			case "missing-pvc-checkpoint":
				checkpoint, err := applicationInventoryForPhase(app, "provisioning")
				if err != nil {
					t.Fatal(err)
				}
				app.Observed, installed, wantError = applicationObservation(t, checkpoint), false, false
			case "missing-installed-pvc":
				app.Observed = applicationObservation(t, app.Inventory)
			case "missing-pvc-after-uninstall":
				app.Observed, installed = applicationObservation(t, app.Inventory), false
			case "foreign":
				app.PVC = bytes.ReplaceAll(app.PVC, []byte("ak3s.io/managed-by: ak3s"), []byte("ak3s.io/managed-by: other"))
				app.Observed = applicationObservation(t, app.Inventory, app.PVC)
			case "missing-inventory":
				app.Observed = applicationObservation(t, app.PVC)
			case "release-without-inventory":
				app.Observed = applicationObservation(t)
			case "size":
				app.Spec.Settings.Storage = "20Gi"
			case "class":
				app.Spec.Settings.StorageClass = "other"
			case "database":
				app.Spec.Settings.Database = "other"
			case "secret":
				app.Spec.Settings.ExistingSecret = "other-auth"
			case "equivalent-size":
				app.Spec.Settings.Storage, wantError = "10240Mi", false
			case "update":
				app.Recipe.Image += "changed"
			case "explicit-update":
				app.Recipe.Image += "changed"
				upgrade, wantError = true, false
			case "major":
				major, err := strconv.Atoi(strings.Split(app.Recipe.PostgresVersion, ".")[0])
				if err != nil {
					t.Fatal(err)
				}
				app.Recipe.PostgresVersion, upgrade = strconv.Itoa(major+1)+".1", true
			case "downgrade":
				parts := strings.Split(app.Recipe.PostgresVersion, ".")
				minor, err := strconv.Atoi(parts[1])
				if err != nil || minor < 1 {
					t.Fatal("downgrade fixture requires a stable patch release", err)
				}
				app.Recipe.PostgresVersion, upgrade = parts[0]+"."+strconv.Itoa(minor-1), true
			}
			if err := checkApplicationResources(app, upgrade, installed); (err != nil) != wantError {
				t.Fatalf("unexpected safety check result: %v", err)
			}
		})
	}
}

func TestApplicationPreparationBoundaries(t *testing.T) {
	for _, mode := range []string{"offline", "new", "existing", "missing-secret", "render-failure", "validation-failure"} {
		t.Run(mode, func(t *testing.T) {
			c, app := testApplication(t)
			s := managedSnapshot(t, testConfig(t))
			if mode == "offline" {
				s = freshSnapshot()
			}
			var commands []Command
			n := NativePreparer{Downloads: downloadFunc(func(_ context.Context, url, sum string) ([]byte, error) {
				if !strings.Contains(url, "get.helm.sh") || !validSHA(sum) {
					t.Fatal("application preparation downloaded platform artifacts")
				}
				return helmArchive(t, "linux-amd64/helm", tar.TypeReg), nil
			}), Runner: runFunc(func(_ context.Context, cmd Command) ([]byte, error) {
				commands = append(commands, cmd)
				args := strings.Join(cmd.Args, " ")
				if cmd.Args[0] == "list" {
					if mode == "existing" {
						return []byte(`[{"name":"ak3s-app-app-db","namespace":"applications"}]`), nil
					}
					return []byte("[]"), nil
				}
				if cmd.Args[0] == "template" {
					if mode == "render-failure" {
						return nil, errors.New("render failed")
					}
					if !strings.Contains(args, "--kube-version 1.") {
						t.Fatal("missing Kubernetes version", args)
					}
					return []byte("apiVersion: v1\nkind: Service\nmetadata: {name: test}\n"), nil
				}
				if strings.Contains(args, "get secret") {
					if mode == "missing-secret" {
						return nil, nil
					}
					return []byte("12"), nil
				}
				if strings.Contains(args, "get configmap/") {
					if mode == "existing" {
						return applicationObservation(t, app.Inventory, app.PVC), nil
					}
					return applicationObservation(t), nil
				}
				if !strings.Contains(args, "--dry-run=server") {
					t.Fatal("preparation attempted a mutation", args)
				}
				if mode == "validation-failure" {
					return nil, errors.New("validation failed")
				}
				return nil, nil
			})}
			b, err := n.PrepareApplications(context.Background(), c, testPins(t), s, false)
			wantError := mode == "missing-secret" || mode == "render-failure" || mode == "validation-failure"
			if (err != nil) != wantError {
				t.Fatal(err)
			}
			if err != nil {
				return
			}
			defer b.Close()
			if len(b.Applications) != 1 || len(b.Rendered) != 1 || len(b.Manifests) != 2 {
				t.Fatal("incomplete application preparation")
			}
			if mode == "offline" && len(commands) != 1 {
				t.Fatal("offline rendering accessed cluster")
			}
		})
	}
}

func TestApplicationFailedInstallRetainsPVCAndCanRetry(t *testing.T) {
	_, app := testApplication(t)
	app.Observed = applicationObservation(t)
	b := &ApplicationBundle{Bundle: &Bundle{Dir: t.TempDir(), Helm: "helm"}, Applications: []preparedApplication{app}}
	var stored [][]byte
	fail := true
	runner := runFunc(func(_ context.Context, cmd Command) ([]byte, error) {
		args := strings.Join(cmd.Args, " ")
		if strings.Contains(args, "get configmap/") {
			return applicationObservation(t, stored...), nil
		}
		if cmd.Args[0] == "upgrade" {
			if fail {
				return nil, errors.New("installation interrupted")
			}
			return nil, nil
		}
		if strings.Contains(args, "delete") || strings.Contains(args, "uninstall") {
			t.Fatal("failure deleted application storage")
		}
		if len(cmd.Input) > 0 {
			if len(stored) < 2 {
				stored = append(stored, cmd.Input)
			}
		}
		return nil, nil
	})
	if err := applyApplications(context.Background(), runner, b); err == nil || !strings.Contains(err.Error(), "PVC is retained") {
		t.Fatal("interrupted installation not reported", err)
	}
	if len(stored) != 2 || !bytes.Contains(stored[1], []byte("kind: PersistentVolumeClaim")) {
		t.Fatal("PVC or inventory checkpoint missing")
	}
	b.Applications[0].Observed, b.Applications[0].HasInventory = applicationObservation(t, stored...), true
	if err := checkApplicationResources(b.Applications[0], false, false); err != nil {
		t.Fatal("retry rejected retained resources", err)
	}
	fail = false
	if err := applyApplications(context.Background(), runner, b); err != nil {
		t.Fatal("retry failed", err)
	}
}

func TestApplicationStalePlanAndFailedUpgrade(t *testing.T) {
	_, app := testApplication(t)
	app.Observed = applicationObservation(t, app.Inventory, app.PVC)
	app.HasInventory = true
	b := &ApplicationBundle{Bundle: &Bundle{Dir: t.TempDir(), Helm: "helm"}, Applications: []preparedApplication{app}}
	writes := 0
	runner := runFunc(func(_ context.Context, cmd Command) ([]byte, error) {
		if strings.Contains(strings.Join(cmd.Args, " "), "get configmap/") {
			return applicationObservation(t), nil
		}
		writes++
		return nil, nil
	})
	if err := applyApplications(context.Background(), runner, b); err == nil || writes != 0 {
		t.Fatal("stale application plan mutated resources", err)
	}
	oldInventory := app.Inventory
	app.Recipe.Image += "updated"
	app.Inventory, _ = applicationInventory(app)
	b.Applications[0] = app
	runner = runFunc(func(_ context.Context, cmd Command) ([]byte, error) {
		if strings.Contains(strings.Join(cmd.Args, " "), "get configmap/") {
			return app.Observed, nil
		}
		if bytes.Contains(cmd.Input, []byte("kind: ConfigMap")) {
			t.Fatal("failed upgrade advanced the installed inventory")
		}
		if cmd.Args[0] == "upgrade" {
			return nil, errors.New("upgrade failed")
		}
		return nil, nil
	})
	if err := applyApplications(context.Background(), runner, b); err == nil {
		t.Fatal("failed application upgrade ignored")
	}
	if err := checkApplicationResources(app, true, true); err != nil {
		t.Fatal("same upgrade cannot retry", err)
	}
	app.Observed = applicationObservation(t, oldInventory, app.PVC)
	if err := checkApplicationResources(app, false, true); err == nil {
		t.Fatal("apply accepted versions from a failed upgrade")
	}
}

func TestApplicationPreparationMultipleInstancesAndCleanup(t *testing.T) {
	root := t.TempDir()
	t.Setenv("TMPDIR", root)
	c, _ := testApplication(t)
	c.Apps["another-db"] = c.Apps["app-db"]
	n := NativePreparer{Downloads: downloadFunc(func(context.Context, string, string) ([]byte, error) {
		return helmArchive(t, "linux-amd64/helm", tar.TypeReg), nil
	}), Runner: runFunc(func(context.Context, Command) ([]byte, error) {
		return []byte("apiVersion: v1\nkind: Service\nmetadata: {name: test}\n"), nil
	})}
	b, err := n.PrepareApplications(context.Background(), c, testPins(t), freshSnapshot(), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Applications) != 2 || len(b.Rendered) != 2 || b.Applications[0].Name != "another-db" {
		t.Fatal("missing applications or unstable ordering")
	}
	b.Close()
	n.Runner = runFunc(func(context.Context, Command) ([]byte, error) { return nil, errors.New("render failure") })
	if _, err := n.PrepareApplications(context.Background(), c, testPins(t), freshSnapshot(), false); err == nil {
		t.Fatal("render failure ignored")
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatal("application temporary artifacts leaked", entries, err)
	}
}

type applicationPrepareFunc func(context.Context, ApplicationsConfig, Pins, Snapshot, bool) (*ApplicationBundle, error)

func (f applicationPrepareFunc) Prepare(context.Context, Plan) (*Bundle, error) {
	return nil, errors.New("application command crossed platform boundary")
}

func (f applicationPrepareFunc) PrepareApplications(ctx context.Context, c ApplicationsConfig, p Pins, s Snapshot, upgrade bool) (*ApplicationBundle, error) {
	return f(ctx, c, p, s, upgrade)
}

type applicationTestHost struct {
	*fakeHost
	applicationCalls int
}

func (h *applicationTestHost) ApplyApplications(context.Context, Snapshot, *ApplicationBundle) error {
	h.applicationCalls++
	return h.applyErr
}

func TestApplicationCLIBoundaries(t *testing.T) {
	for _, mode := range []string{"apply", "dry-run", "upgrade", "render", "unmanaged", "agent", "pending", "nonroot", "prepare-failure"} {
		t.Run(mode, func(t *testing.T) {
			a, h, out, platformCalls := appFixture(t)
			h.s = managedSnapshot(t, testConfig(t))
			host := &applicationTestHost{fakeHost: h}
			a.Host = host
			a.ReadFile = func(string) ([]byte, error) { return []byte(testApplicationsYAML), nil }
			prepared := 0
			a.Preparer = applicationPrepareFunc(func(_ context.Context, c ApplicationsConfig, _ Pins, _ Snapshot, upgrade bool) (*ApplicationBundle, error) {
				prepared++
				if upgrade != (mode == "upgrade") || len(c.Apps) != 1 {
					t.Fatal("incorrect application plan")
				}
				if mode == "prepare-failure" {
					return nil, errors.New("preparation failed")
				}
				return &ApplicationBundle{Bundle: &Bundle{Dir: t.TempDir()}}, nil
			})
			args := []string{"apps", "apply"}
			switch mode {
			case "dry-run":
				args = append(args, "--dry-run")
			case "upgrade", "render":
				args[1] = mode
			case "unmanaged":
				h.s.Managed = false
			case "agent":
				h.s.Role = "agent"
			case "pending":
				h.s.State.PendingUninstall = true
			case "nonroot":
				h.s.Root = false
			}
			err := a.Run(context.Background(), args)
			wantError := mode == "unmanaged" || mode == "agent" || mode == "pending" || mode == "nonroot" || mode == "prepare-failure"
			if (err != nil) != wantError {
				t.Fatal(err)
			}
			wantCalls := 0
			if mode == "apply" || mode == "upgrade" {
				wantCalls = 1
			}
			if host.applicationCalls != wantCalls || h.applies != 0 || *platformCalls != 0 {
				t.Fatal("incorrect application execution boundary")
			}
			if mode == "render" && h.inspections != 0 || mode == "dry-run" && !strings.Contains(out.String(), "no persistent changes") {
				t.Fatal("read-only command crossed execution boundary")
			}
			if wantError && mode != "prepare-failure" && prepared != 0 {
				t.Fatal("prepared after host check failed")
			}
		})
	}
}

func TestApplicationAddAndStatus(t *testing.T) {
	a, h, _, calls := appFixture(t)
	var saved []byte
	a.ReadFile = func(string) ([]byte, error) {
		if saved == nil {
			return nil, fs.ErrNotExist
		}
		return saved, nil
	}
	a.WriteFile = func(path string, data []byte, mode fs.FileMode) error {
		if path != "/etc/ak3s/apps.yaml" || mode != 0600 {
			t.Fatal("incorrect configuration destination or permissions")
		}
		saved = data
		return nil
	}
	if err := a.Run(context.Background(), []string{"apps", "add", "postgres", "--existing-secret", "db-auth"}); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadApplicationsConfig(saved); err != nil || h.inspections != 0 || *calls != 0 {
		t.Fatal("add did not save valid configuration independently", err)
	}
	if err := a.Run(context.Background(), []string{"apps", "add", "postgres", "--existing-secret", "db-auth"}); err == nil {
		t.Fatal("add overwrote existing application")
	}
	h.s = managedSnapshot(t, testConfig(t))
	a.Runner = runFunc(func(_ context.Context, cmd Command) ([]byte, error) {
		args := strings.Join(cmd.Args, " ")
		if !strings.Contains(args, "--kubeconfig "+KubeconfigPath) || !strings.Contains(args, "persistentvolumeclaims") || !strings.Contains(args, "--selector=ak3s.io/managed-by=ak3s") {
			t.Fatal("status used wrong destination")
		}
		return []byte("observed"), nil
	})
	if err := a.Run(context.Background(), []string{"apps", "status"}); err != nil || h.applies != 0 {
		t.Fatal(err)
	}
}

func TestApplicationExportIsPrivateAndExcludesHostConfig(t *testing.T) {
	_, app := testApplication(t)
	b := &ApplicationBundle{Bundle: &Bundle{Values: map[string][]byte{app.Release: []byte("values")}, Rendered: map[string][]byte{app.Release: []byte("rendered")}}, Applications: []preparedApplication{app}}
	dir := t.TempDir()
	if err := b.ExportApplications(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "k3s-config.yaml")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("application render exported cluster settings")
	}
	if err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		info, err := entry.Info()
		if err == nil && info.Mode().Perm() != 0600 {
			t.Fatal("application export is not private", path)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
