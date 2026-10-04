package ak3s

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/jgador/ak3s/platform"
)

type Bundle struct {
	Dir, Helm                           string
	K3s                                 []byte
	Values, Charts, Rendered, Manifests map[string][]byte
	Releases                            map[string]string
}

func (b *Bundle) Close() {
	if b != nil && b.Dir != "" {
		os.RemoveAll(b.Dir)
	}
}
func (b *Bundle) HelmEnv() []string {
	return []string{"HOME=" + b.Dir, "HELM_CACHE_HOME=" + filepath.Join(b.Dir, "cache"), "HELM_CONFIG_HOME=" + filepath.Join(b.Dir, "config"), "HELM_DATA_HOME=" + filepath.Join(b.Dir, "data"), "HELM_PLUGINS=" + filepath.Join(b.Dir, "plugins"), "KUBECONFIG=" + KubeconfigPath}
}
func (b *Bundle) ChartPath(c Chart) string  { return filepath.Join(b.Dir, c.Release+".tgz") }
func (b *Bundle) ValuesPath(c Chart) string { return filepath.Join(b.Dir, c.Release+"-values.yaml") }

type Preparer interface {
	Prepare(context.Context, Plan) (*Bundle, error)
}
type NativePreparer struct {
	Downloads Downloader
	Runner    Runner
}

func (n NativePreparer) Prepare(ctx context.Context, p Plan) (bundle *Bundle, err error) {
	dir, err := os.MkdirTemp("", "ak3s-")
	if err != nil {
		return nil, err
	}
	b := &Bundle{Dir: dir, Values: map[string][]byte{}, Charts: map[string][]byte{}, Rendered: map[string][]byte{}, Manifests: map[string][]byte{}, Releases: map[string]string{}}
	defer func() {
		if err != nil {
			b.Close()
		}
	}()
	arch := p.Snapshot.Arch
	if arch != "amd64" && arch != "arm64" {
		return nil, fmt.Errorf("unsupported architecture %s", arch)
	}
	if p.InstallBinary {
		b.K3s, err = n.Downloads.Get(ctx, p.Pins.K3sURL(arch), p.Pins.K3sSHA256[arch])
		if err != nil {
			return nil, err
		}
	}
	if !p.Config.Platform {
		return b, nil
	}
	archive, err := n.Downloads.Get(ctx, "https://get.helm.sh/helm-"+p.Pins.Helm+"-linux-"+arch+".tar.gz", p.Pins.HelmSHA256[arch])
	if err != nil {
		return nil, err
	}
	binary, err := helmBinary(archive, arch)
	if err != nil {
		return nil, err
	}
	b.Helm = filepath.Join(dir, "helm")
	if err = os.WriteFile(b.Helm, binary, 0700); err != nil {
		return nil, err
	}
	if p.Snapshot.Kubeconfig && p.Snapshot.Managed && p.Snapshot.ServiceActive {
		out, err := n.Runner.Run(ctx, Command{Name: b.Helm, Args: []string{"list", "--all-namespaces", "--all", "--output", "json", "--kubeconfig", KubeconfigPath}, Env: b.HelmEnv()})
		if err != nil {
			return nil, fmt.Errorf("inspect Helm releases: %w", err)
		}
		var releases []struct {
			Name      string `json:"name"`
			Namespace string `json:"namespace"`
			Chart     string `json:"chart"`
			Status    string `json:"status"`
		}
		if err = json.Unmarshal(out, &releases); err != nil {
			return nil, err
		}
		for _, r := range releases {
			b.Releases[r.Namespace+"/"+r.Name] = r.Chart + " (" + r.Status + ")"
		}
	}
	for _, chart := range p.Pins.Charts {
		values, err := ValuesFor(chart, p.Config)
		if err != nil {
			return nil, err
		}
		b.Values[chart.Release] = values
		data, err := n.Downloads.Get(ctx, chart.URL, chart.SHA256)
		if err != nil {
			return nil, err
		}
		b.Charts[chart.Release] = data
		if err = os.WriteFile(b.ChartPath(chart), data, 0600); err != nil {
			return nil, err
		}
		if err = os.WriteFile(b.ValuesPath(chart), values, 0600); err != nil {
			return nil, err
		}
		args := []string{"template", chart.Release, b.ChartPath(chart), "--namespace", chart.Namespace, "--kube-version", kubeVersion(p.Pins), "--values", b.ValuesPath(chart)}
		if !chart.SkipCRDs {
			args = append(args, "--include-crds")
		}
		if _, exists := b.Releases[chart.Namespace+"/"+chart.Release]; exists {
			args = append(args, "--is-upgrade")
		}
		rendered, err := n.Runner.Run(ctx, Command{Name: b.Helm, Args: args, Env: b.HelmEnv()})
		if err != nil {
			return nil, err
		}
		if err = ValidateManifests(rendered); err != nil {
			return nil, fmt.Errorf("render %s: %w", chart.Release, err)
		}
		b.Rendered[chart.Release] = rendered
	}
	b.Manifests["issuers.yaml"], err = Issuers(p.Config)
	if err != nil {
		return nil, err
	}
	for _, name := range []string{"headlamp-rbac.yaml", "metrics-rbac.yaml"} {
		data, err := platform.Files.ReadFile("manifests/" + name)
		if err != nil {
			return nil, err
		}
		b.Manifests[name] = data
	}
	for name, data := range b.Manifests {
		if err = ValidateManifests(data); err != nil {
			return nil, fmt.Errorf("validate %s: %w", name, err)
		}
	}
	// Use Helm's own server dry-run for installed releases. No hooks are executed,
	// no releases are saved, and output is discarded rather than leaking Secrets.
	for _, chart := range p.Pins.Charts {
		if _, exists := b.Releases[chart.Namespace+"/"+chart.Release]; !exists {
			continue
		}
		args := helmUpgradeArgs(chart, b)
		args = append(args, "--dry-run=server")
		if _, err = n.Runner.Run(ctx, Command{Name: b.Helm, Args: args, Env: b.HelmEnv()}); err != nil {
			return nil, fmt.Errorf("validate installed release %s: %w", chart.Release, err)
		}
	}
	return b, nil
}
func helmUpgradeArgs(chart Chart, b *Bundle) []string {
	args := []string{"upgrade", "--install", chart.Release, b.ChartPath(chart), "--namespace", chart.Namespace, "--create-namespace", "--kubeconfig", KubeconfigPath, "--values", b.ValuesPath(chart), "--reset-values", "--atomic", "--wait", "--timeout", "10m"}
	if chart.SkipCRDs {
		args = append(args, "--skip-crds")
	}
	return args
}
func (b *Bundle) Export(dir string, k3s []byte) error {
	if err := AtomicWrite(filepath.Join(dir, "k3s-config.yaml"), k3s, 0600); err != nil {
		return err
	}
	for _, group := range []struct {
		prefix string
		data   map[string][]byte
	}{{"values", b.Values}, {"rendered", b.Rendered}, {"manifests", b.Manifests}} {
		names := make([]string, 0, len(group.data))
		for name := range group.data {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			if filepath.Ext(name) != ".yaml" {
				name += ".yaml"
			}
			key := name
			if group.prefix != "manifests" {
				key = name[:len(name)-5]
			}
			if err := AtomicWrite(filepath.Join(dir, group.prefix, name), group.data[key], 0600); err != nil {
				return err
			}
		}
	}
	return nil
}
