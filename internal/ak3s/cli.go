// Package ak3s implements local K3s installation and platform reconciliation.
package ak3s

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"runtime"
	"strings"

	"github.com/jgador/ak3s/platform"
	"go.yaml.in/yaml/v3"
)

// App connects CLI commands to host inspection, artifact preparation, and execution.
// These dependencies can be replaced in tests without modifying a real host.
type App struct {
	Host     Host
	Preparer Preparer
	Runner   Runner
	ReadFile func(string) ([]byte, error)
	Out, Err io.Writer
}

// NewApp constructs the CLI with real host operations and the supplied output streams.
func NewApp(out, err io.Writer) *App {
	return &App{Host: NewNativeHost(), Preparer: NativePreparer{Downloads: HTTPDownloader{}, Runner: ExecRunner{}}, Runner: ExecRunner{}, ReadFile: os.ReadFile, Out: out, Err: err}
}

const usage = `Usage: ak3s <command> [options]

Commands:
  install   Install or reconcile this node and its platform
  apply     Reconcile the desired state (same safety checks as install)
  upgrade   Apply this AK3S release's pinned versions; allow a safe K3s upgrade
  status    Read this node's state and cluster resources
  config    Print merged configuration, or --defaults for embedded defaults
  render    Validate and render K3s, Helm values and Kubernetes resources
  version   Print AK3S and managed component versions

Options (after command):
  --config PATH    YAML overrides (otherwise /etc/ak3s/values.yaml, if present)
  --dry-run        Plan install/apply/upgrade without persistent changes
  --output DIR     Write render results to a directory (render only)
  --defaults       Print embedded defaults (config only)

No cluster is selected from KUBECONFIG or the current kubectl context.
`

// Run handles arguments after the executable name and returns command failures.
func (a *App) Run(ctx context.Context, args []string) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		_, err := io.WriteString(a.Out, usage)
		return err
	}
	command := args[0]
	switch command {
	case "install", "apply", "upgrade", "status", "config", "render", "version":
	default:
		return fmt.Errorf("unknown command %q; run ak3s help", command)
	}
	f := flag.NewFlagSet(command, flag.ContinueOnError)
	f.SetOutput(a.Err)
	var path, output string
	var dry, defaults bool
	f.StringVar(&path, "config", "", "values.yaml overrides; omitted settings use defaults")
	if command == "install" || command == "apply" || command == "upgrade" {
		f.BoolVar(&dry, "dry-run", false, "make no persistent changes")
	}
	if command == "render" {
		f.StringVar(&output, "output", "", "output directory; stdout if omitted")
	}
	if command == "config" {
		f.BoolVar(&defaults, "defaults", false, "print embedded defaults")
	}
	if err := f.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if f.NArg() > 0 {
		return errors.New("unexpected positional arguments")
	}
	pins, err := LoadPins()
	if err != nil {
		return err
	}
	if command == "version" {
		fmt.Fprintf(a.Out, "ak3s %s (%s)\nk3s %s\nhelm %s\n", Version, Commit, pins.K3s, pins.Helm)
		for _, c := range pins.Charts {
			fmt.Fprintf(a.Out, "%s %s\n", c.Release, c.Version)
		}
		return nil
	}
	if defaults {
		if path != "" {
			return errors.New("--defaults cannot be combined with --config")
		}
		data, err := platform.Files.ReadFile("defaults.yaml")
		if err != nil {
			return err
		}
		_, err = a.Out.Write(data)
		return err
	}

	// The default file is optional; an explicitly selected file must exist.
	explicit := path != ""
	if !explicit {
		path = "/etc/ak3s/values.yaml"
	}
	data, err := a.ReadFile(path)
	if err != nil && (explicit || !errors.Is(err, fs.ErrNotExist)) {
		return fmt.Errorf("read configuration: %w", err)
	}
	c, err := LoadConfig(data)
	if err != nil {
		return err
	}
	if command == "config" {
		data, err := yaml.Marshal(c)
		if err != nil {
			return err
		}
		_, err = a.Out.Write(data)
		return err
	}

	// Rendering needs validated artifacts but does not inspect or change the host.
	if command == "render" {
		if err = c.ValidateInstall(); err != nil {
			return err
		}
		p := Plan{Config: c, Pins: pins, Snapshot: Snapshot{OS: runtime.GOOS, Arch: runtime.GOARCH}}
		b, err := a.Preparer.Prepare(ctx, p)
		if err != nil {
			return err
		}
		defer b.Close()
		k3s, err := K3sConfig(c, "")
		if err != nil {
			return err
		}
		if output != "" {
			if err = b.Export(output, k3s); err != nil {
				return err
			}
			fmt.Fprintln(a.Out, "Rendered configuration and resources into", output)
			return nil
		}
		fmt.Fprintf(a.Out, "# k3s-config.yaml\n%s\n", k3s)
		for _, chart := range pins.Charts {
			if rendered, ok := b.Rendered[chart.Release]; ok {
				fmt.Fprintf(a.Out, "---\n# %s\n%s\n", chart.Release, rendered)
			}
		}
		for _, name := range []string{"issuers.yaml", "headlamp-rbac.yaml", "metrics-rbac.yaml"} {
			if raw, ok := b.Manifests[name]; ok {
				fmt.Fprintf(a.Out, "---\n# %s\n%s\n", name, raw)
			}
		}
		return nil
	}
	snapshot, err := a.Host.Inspect(ctx, c)
	if err != nil {
		return err
	}
	if command == "status" {
		return a.status(ctx, c, pins, snapshot)
	}
	plan, err := BuildPlan(c, pins, snapshot, command)
	if err != nil {
		return err
	}
	fmt.Fprintf(a.Out, "AK3S %s | %s/%s | %s %s\n", Version, snapshot.OS, snapshot.Arch, snapshot.Distribution, snapshot.DistributionVersion)
	for _, action := range plan.Actions {
		fmt.Fprintf(a.Out, "%-10s %s: %s\n", action.Kind, action.Target, strings.TrimSpace(action.Detail))
	}
	for _, warning := range plan.Warnings {
		fmt.Fprintln(a.Out, "NOTE:", warning)
	}
	for _, problem := range plan.Problems {
		fmt.Fprintln(a.Out, "BLOCKED:", problem)
	}

	// Dry-run renders charts even when host checks fail. Real execution stops
	// before downloading assets for an unsupported host.
	if !dry {
		if err = plan.Check(); err != nil {
			return err
		}
		if !snapshot.Root {
			return errors.New("run as root to apply this plan")
		}
	}
	b, err := a.Preparer.Prepare(ctx, plan)
	if err != nil {
		return err
	}
	defer b.Close()
	for _, chart := range pins.Charts {
		if !c.Platform {
			break
		}
		state := "not observed; Helm will install or reconcile"
		if v, ok := b.Releases[chart.Namespace+"/"+chart.Release]; ok {
			state = "installed " + v + "; server dry-run passed"
		}
		fmt.Fprintf(a.Out, "validated  %s: %s\n", chart.Release, state)
	}
	if dry {
		fmt.Fprintf(a.Out, "Dry-run complete: K3s configuration and %d chart templates validated; no persistent changes.\n", len(b.Rendered))
		return plan.Check()
	}
	if err = a.Host.Apply(ctx, plan, b); err != nil {
		return err
	}
	fmt.Fprintln(a.Out, "AK3S reconciled. Run ak3s status.")
	return nil
}

// status reports local service state and, on servers, queries the managed cluster.
func (a *App) status(ctx context.Context, c Config, p Pins, s Snapshot) error {
	fmt.Fprintf(a.Out, "AK3S-managed: %t\nK3s: %s (desired %s)\nService active: %t\n", s.Managed, s.K3sVersion, p.K3s, s.ServiceActive)
	if s.State != nil {
		fmt.Fprintf(a.Out, "Last attempted AK3S release: %s\n", s.State.AK3SVersion)
	}
	if !s.Managed {
		return errors.New("no AK3S-managed installation found")
	}
	if !s.ServiceActive {
		return errors.New("K3s service is not active")
	}
	if c.Node.Role == "agent" {
		fmt.Fprintln(a.Out, "Agent service is active; check the node's Ready condition from a server.")
		return nil
	}
	if !s.Kubeconfig {
		return errors.New("K3s kubeconfig is unavailable")
	}

	// Put kubectl's discovery cache in a disposable directory, never under ~/.kube.
	dir, err := os.MkdirTemp("", "ak3s-status-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	for _, resource := range []string{"nodes", "pods", "svc", "ingress", "clusterissuers"} {
		out, err := a.Runner.Run(ctx, Command{Name: K3sPath, Args: []string{"kubectl", "--kubeconfig", KubeconfigPath, "--cache-dir", dir, "get", resource, "-A", "--request-timeout=30s"}})
		if err != nil {
			return err
		}
		fmt.Fprintln(a.Out, string(out))
	}
	return nil
}
