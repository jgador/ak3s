package ak3s

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"go.yaml.in/yaml/v3"
)

const applicationsUsage = `Usage: ak3s apps <command> [options]

Commands:
  list             List available recipes and their pinned versions
  add postgres     Save a PostgreSQL application; does not deploy it
  render           Render application resources without accessing a cluster
  apply            Reconcile applications without upgrading installed versions
  upgrade          Apply recipe updates; refuses PostgreSQL major version changes
  status           Read installed application resources, including PVCs

Options:
  --config PATH           Application YAML (default /etc/ak3s/apps.yaml; except status)
  --dry-run               Validate without persistent changes (apply/upgrade only)
  --output DIR            Save rendered resources (render only)
  --name NAME             Application name (add only; default postgres)
  --namespace NAME        Application namespace (add only; default applications)
  --storage SIZE          Required PVC size (add only; default 10Gi)
  --storage-class NAME    Storage class (add only; default local-path)
  --existing-secret NAME  Existing Secret with a password key (required for add)
  --database NAME         Initial database (add only; default app)
  --username NAME         Initial administrator (add only; default postgres)

Application commands use only the local AK3S-managed server kubeconfig.
Removing configuration entries does not remove applications or their PVCs.
`

// ApplicationHost executes optional workloads without changing the node or platform.
type ApplicationHost interface {
	ApplyApplications(context.Context, Snapshot, *ApplicationBundle) error
}

func checkApplicationHost(s Snapshot, write bool) error {
	if !s.Managed || !s.Existing || s.Role != "server" {
		return errors.New("application commands require an existing AK3S-managed server")
	}
	if !s.ServiceActive || !s.Kubeconfig {
		return errors.New("application commands require active K3s and its local kubeconfig")
	}
	if s.State != nil && (s.State.PendingUninstall || s.State.PendingRestart) {
		return errors.New("finish the pending cluster operation before managing applications")
	}
	if write && !s.Root {
		return errors.New("run application apply or upgrade as root")
	}
	return nil
}

// ApplyApplications locks against cluster operations and rechecks the managed server before writes.
func (h *NativeHost) ApplyApplications(ctx context.Context, s Snapshot, b *ApplicationBundle) error {
	if err := checkApplicationHost(s, true); err != nil {
		return err
	}
	unlock, err := lock(filepath.Dir(StatePath))
	if err != nil {
		return err
	}
	defer unlock()
	c, err := LoadConfig(nil)
	if err != nil {
		return err
	}
	fresh, err := h.Inspect(ctx, c)
	if err != nil {
		return err
	}
	if err = checkApplicationHost(fresh, true); err != nil {
		return err
	}
	if !sameSnapshot(s, fresh) {
		return errors.New("host changed while planning; rerun AK3S")
	}
	return applyApplications(ctx, h.Runner, b)
}

func (a *App) runApplications(ctx context.Context, args []string) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		_, err := io.WriteString(a.Out, applicationsUsage)
		return err
	}
	command := args[0]
	if command == "list" {
		if len(args) != 1 {
			return errors.New("apps list does not accept options")
		}
		catalog, err := loadApplicationCatalog()
		if err != nil {
			return err
		}
		p := catalog["postgres"]
		fmt.Fprintf(a.Out, "postgres  recipe %s  PostgreSQL %s  persistent storage required\n", p.Version, p.PostgresVersion)
		return nil
	}
	switch command {
	case "add", "render", "apply", "upgrade", "status":
	default:
		return errors.New("unknown application command; run ak3s apps help")
	}
	parseArgs := args[1:]
	if command == "add" {
		if len(parseArgs) == 0 || parseArgs[0] != "postgres" {
			return errors.New("use ak3s apps add postgres with --existing-secret")
		}
		parseArgs = parseArgs[1:]
	}
	f := flag.NewFlagSet("apps "+command, flag.ContinueOnError)
	f.SetOutput(a.Err)
	var path, output, name, namespace string
	var dry bool
	var settings ApplicationSettings
	if command != "status" {
		f.StringVar(&path, "config", "/etc/ak3s/apps.yaml", "application configuration")
	}
	if command == "apply" || command == "upgrade" {
		f.BoolVar(&dry, "dry-run", false, "make no persistent changes")
	}
	if command == "render" {
		f.StringVar(&output, "output", "", "output directory")
	}
	if command == "add" {
		f.StringVar(&name, "name", "postgres", "application name")
		f.StringVar(&namespace, "namespace", "applications", "application namespace")
		f.StringVar(&settings.Storage, "storage", "10Gi", "persistent storage size")
		f.StringVar(&settings.StorageClass, "storage-class", "local-path", "storage class")
		f.StringVar(&settings.ExistingSecret, "existing-secret", "", "Secret with a password key")
		f.StringVar(&settings.Database, "database", "app", "initial database")
		f.StringVar(&settings.Username, "username", "postgres", "initial administrator")
	}
	if err := f.Parse(parseArgs); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if f.NArg() != 0 || (path == "" && command != "status") {
		return errors.New("unexpected application arguments or empty configuration path")
	}
	if command == "status" {
		return a.applicationsStatus(ctx)
	}
	data, err := a.ReadFile(path)
	if err != nil && !(command == "add" && errors.Is(err, fs.ErrNotExist)) {
		return fmt.Errorf("read application configuration: %w", err)
	}
	c, err := LoadApplicationsConfig(data)
	if err != nil {
		return err
	}
	if command == "add" {
		if _, exists := c.Apps[name]; exists {
			return errors.New("application already exists; edit its configuration to change settings")
		}
		if c.Apps == nil {
			c.Apps = map[string]ApplicationSpec{}
		}
		c.Apps[name] = ApplicationSpec{Recipe: "postgres", Namespace: namespace, Settings: settings}
		if err = c.Validate(); err != nil {
			return err
		}
		var encoded strings.Builder
		encoder := yaml.NewEncoder(&encoded)
		encoder.SetIndent(2)
		if err = encoder.Encode(c); err != nil {
			return err
		}
		if err = encoder.Close(); err != nil {
			return err
		}
		data = []byte(encoded.String())
		if a.WriteFile == nil {
			return errors.New("application configuration writer is unavailable")
		}
		if err = a.WriteFile(path, data, 0600); err != nil {
			return err
		}
		fmt.Fprintf(a.Out, "Saved PostgreSQL application %s in %s. Create its credential Secret, then run ak3s apps apply --config %s.\n", name, path, path)
		return nil
	}
	pins, err := LoadPins()
	if err != nil {
		return err
	}
	s := Snapshot{OS: runtime.GOOS, Arch: runtime.GOARCH}
	if command != "render" {
		config, err := LoadConfig(nil)
		if err != nil {
			return err
		}
		s, err = a.Host.Inspect(ctx, config)
		if err != nil {
			return err
		}
		if err = checkApplicationHost(s, !dry); err != nil {
			return err
		}
	}
	preparer, ok := a.Preparer.(ApplicationPreparer)
	if !ok {
		return errors.New("application preparer is unavailable")
	}
	b, err := preparer.PrepareApplications(ctx, c, pins, s, command == "upgrade")
	if err != nil {
		return err
	}
	defer b.Close()
	if command == "render" {
		if output != "" {
			// Application exports contain only application artifacts, without node configuration.
			if err = b.ExportApplications(output); err != nil {
				return err
			}
			fmt.Fprintln(a.Out, "Rendered application resources into", output)
			return nil
		}
		for _, app := range b.Applications {
			fmt.Fprintf(a.Out, "---\n%s---\n%s---\n%s", app.Inventory, app.PVC, b.Rendered[app.Release])
		}
		return nil
	}
	for _, app := range b.Applications {
		fmt.Fprintf(a.Out, "validated  %s: PostgreSQL %s; %s/%s; PVC %s (%s)\n", app.Name, app.Recipe.PostgresVersion, app.Namespace,
			app.Release, app.Spec.Settings.Storage, app.Spec.Settings.StorageClass)
	}
	if dry {
		fmt.Fprintln(a.Out, "Application dry-run complete: no persistent changes.")
		return nil
	}
	host, ok := a.Host.(ApplicationHost)
	if !ok {
		return errors.New("host does not support application reconciliation")
	}
	if err = host.ApplyApplications(ctx, s, b); err != nil {
		return err
	}
	fmt.Fprintln(a.Out, "Applications reconciled. Run ak3s apps status.")
	return nil
}

// ExportApplications writes private application artifacts without exporting host configuration.
func (b *ApplicationBundle) ExportApplications(dir string) error {
	for _, app := range b.Applications {
		for path, data := range map[string][]byte{
			filepath.Join("rendered", app.Release+".yaml"):            b.Rendered[app.Release],
			filepath.Join("values", app.Release+".yaml"):              b.Values[app.Release],
			filepath.Join("manifests", app.Release+"-pvc.yaml"):       app.PVC,
			filepath.Join("manifests", app.Release+"-inventory.yaml"): app.Inventory,
		} {
			if err := AtomicWrite(filepath.Join(dir, path), data, 0600); err != nil {
				return err
			}
		}
	}
	return nil
}

func (a *App) applicationsStatus(ctx context.Context) error {
	c, err := LoadConfig(nil)
	if err != nil {
		return err
	}
	s, err := a.Host.Inspect(ctx, c)
	if err != nil {
		return err
	}
	if err = checkApplicationHost(s, false); err != nil {
		return err
	}
	dir, err := os.MkdirTemp("", "ak3s-apps-status-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	out, err := applicationKube(ctx, a.Runner, &Bundle{Dir: dir}, nil, "get", "configmaps,statefulsets,services,persistentvolumeclaims",
		"--all-namespaces", "--selector="+applicationManagedLabel+"=ak3s", "--request-timeout=30s")
	if err != nil {
		return fmt.Errorf("read application status: %w", err)
	}
	_, err = a.Out.Write(out)
	return err
}
