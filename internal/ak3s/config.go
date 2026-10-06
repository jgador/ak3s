package ak3s

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/jgador/ak3s/platform"
	"go.yaml.in/yaml/v3"
)

// Config holds operator settings after overrides have been merged with defaults.
// YAML tags define the names accepted in values.yaml.
type Config struct {
	ClusterName           string                    `yaml:"cluster_name"`
	KubernetesAPIEndpoint string                    `yaml:"kubernetes_api_endpoint"`
	TLSSANs               []string                  `yaml:"tls_sans"`
	ACMEEmail             string                    `yaml:"acme_email"`
	ACMEEnvironment       string                    `yaml:"acme_environment"`
	StorageClass          string                    `yaml:"storage_class"`
	MetricsRetention      string                    `yaml:"metrics_retention"`
	MetricsStorage        string                    `yaml:"metrics_storage"`
	LogsRetention         string                    `yaml:"logs_retention"`
	LogsStorage           string                    `yaml:"logs_storage"`
	LogsMaxDisk           string                    `yaml:"logs_max_disk"`
	DashboardImage        string                    `yaml:"dashboard_image"`
	DashboardHostname     string                    `yaml:"dashboard_hostname"`
	DashboardSharedPaths  bool                      `yaml:"dashboard_shared_paths"`
	DashboardAuthSecret   string                    `yaml:"dashboard_auth_secret"`
	HeadlampHostname      string                    `yaml:"headlamp_hostname"`
	Platform              bool                      `yaml:"platform"`
	Node                  NodeConfig                `yaml:"node"`
	HelmValues            map[string]map[string]any `yaml:"helm_values"`
	sourcePath            string
	displayOverrides      string
	overrideCount         int
}

// NodeConfig describes this node's identity, networking, and cluster membership.
// JSON tags also persist these settings in the installation checkpoint.
type NodeConfig struct {
	Role         string `yaml:"role" json:"role"`
	Name         string `yaml:"name" json:"name"`
	IP           string `yaml:"ip" json:"ip"`
	ExternalIP   string `yaml:"external_ip" json:"external_ip"`
	FlannelIface string `yaml:"flannel_iface" json:"flannel_iface"`
	Datastore    string `yaml:"datastore" json:"datastore"`
	ServerCount  int    `yaml:"server_count" json:"server_count"`
	Join         bool   `yaml:"join" json:"join"`
	TokenFile    string `yaml:"token_file" json:"token_file"`
}

// Merge recursively merges maps. Lists and scalars replace their defaults,
// including false, zero and empty lists. Inputs are never modified.
func Merge(base, overrides map[string]any) map[string]any {
	result := make(map[string]any, len(base))
	for k, v := range base {
		result[k] = clone(v)
	}
	for k, v := range overrides {
		a, aok := result[k].(map[string]any)
		b, bok := v.(map[string]any)
		if aok && bok {
			result[k] = Merge(a, b)
		} else {
			result[k] = clone(v)
		}
	}
	return result
}
func clone(v any) any {
	switch x := v.(type) {
	case map[string]any:
		return Merge(x, nil)
	case []any:
		out := make([]any, len(x))
		for i, v := range x {
			out[i] = clone(v)
		}
		return out
	default:
		return v
	}
}

// mapping accepts one YAML mapping; empty input means there are no overrides.
func mapping(data []byte) (map[string]any, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return map[string]any{}, nil
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var root yaml.Node
	if err := dec.Decode(&root); err != nil {
		return nil, err
	}
	if len(root.Content) != 1 || root.Content[0].Kind != yaml.MappingNode {
		return nil, errors.New("configuration must be a YAML mapping")
	}
	if err := checkYAML(root.Content[0]); err != nil {
		return nil, err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return nil, errors.New("only one YAML document is allowed")
	}
	var result map[string]any
	if err := root.Decode(&result); err != nil {
		return nil, err
	}
	return result, nil
}

// checkYAML rejects ambiguous keys, aliases, and nulls before converting to maps.
func checkYAML(n *yaml.Node) error {
	if n.Kind == yaml.AliasNode || n.Tag == "!!null" {
		return errors.New("YAML aliases and null values are not supported; omit keys to inherit defaults")
	}
	if n.Kind == yaml.MappingNode {
		seen := map[string]bool{}
		for i := 0; i < len(n.Content); i += 2 {
			k := n.Content[i]
			if k.Tag != "!!str" || seen[k.Value] {
				return errors.New("YAML keys must be unique strings")
			}
			seen[k.Value] = true
		}
	}
	for _, child := range n.Content {
		if err := checkYAML(child); err != nil {
			return err
		}
	}
	return nil
}

// LoadConfig merges YAML overrides with embedded defaults and validates them.
func LoadConfig(overrides []byte) (Config, error) {
	var c Config
	defaults, err := platform.Files.ReadFile("defaults.yaml")
	if err != nil {
		return c, err
	}
	base, err := mapping(defaults)
	if err != nil {
		return c, err
	}
	given, err := mapping(overrides)
	if err != nil {
		return c, err
	}
	data, err := yaml.Marshal(Merge(base, given))
	if err != nil {
		return c, err
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))

	// Decode into the typed settings so misspelled AK3S keys are errors.
	dec.KnownFields(true)
	if err := dec.Decode(&c); err != nil {
		return c, err
	}
	c.displayOverrides = dashboardConfigYAML(given)
	c.overrideCount = dashboardOverrideCount(given)
	return c, c.Validate()
}

var labelRE = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

func hostname(s string) bool {
	if len(s) == 0 || len(s) > 253 {
		return false
	}
	for _, part := range strings.Split(s, ".") {
		if !labelRE.MatchString(part) {
			return false
		}
	}
	return true
}
func endpoint(s string) bool {
	if ip := net.ParseIP(s); ip != nil {
		return ip.To4() != nil
	}
	return hostname(s) && !regexp.MustCompile(`^[0-9.]+$`).MatchString(s)
}

func (c Config) sharedDashboardPaths() bool {
	return c.DashboardSharedPaths || c.DashboardHostname != ""
}

// Validate checks setting formats, supported cluster topology, and chart override names.
// Host prerequisites and installation-only requirements are checked separately.
func (c Config) Validate() error {
	if !labelRE.MatchString(c.ClusterName) {
		return errors.New("cluster_name must be a DNS label")
	}
	if !endpoint(c.KubernetesAPIEndpoint) {
		return errors.New("kubernetes_api_endpoint must be an IPv4 address or DNS hostname without a port")
	}
	for _, s := range c.TLSSANs {
		if !endpoint(s) {
			return errors.New("tls_sans must contain IPv4 addresses or DNS hostnames")
		}
	}
	if c.ACMEEmail != "" && !regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`).MatchString(c.ACMEEmail) {
		return errors.New("acme_email must be a contact email address")
	}
	if c.ACMEEnvironment != "staging" && c.ACMEEnvironment != "production" {
		return errors.New("acme_environment must be staging or production")
	}
	if !hostname(c.StorageClass) {
		return errors.New("storage_class must be a Kubernetes resource name")
	}
	for name, value := range map[string]string{"metrics_retention": c.MetricsRetention, "logs_retention": c.LogsRetention} {
		if !regexp.MustCompile(`^[1-9][0-9]*[dwy]$`).MatchString(value) {
			return fmt.Errorf("%s must be a duration such as 7d", name)
		}
	}
	for name, value := range map[string]string{"metrics_storage": c.MetricsStorage, "logs_storage": c.LogsStorage} {
		if !regexp.MustCompile(`^[1-9][0-9]*(Mi|Gi|Ti)$`).MatchString(value) {
			return fmt.Errorf("%s must be a quantity such as 10Gi", name)
		}
	}
	if !regexp.MustCompile(`^[1-9][0-9]*(MiB|GiB|TiB)$`).MatchString(c.LogsMaxDisk) {
		return errors.New("logs_max_disk must be a size such as 8GiB")
	}
	if c.HeadlampHostname != "" && !hostname(c.HeadlampHostname) {
		return errors.New("headlamp_hostname must be a DNS hostname")
	}
	if c.sharedDashboardPaths() && c.HeadlampHostname != "" {
		return errors.New("use dashboard shared paths or headlamp_hostname for standalone Headlamp access")
	}
	if c.DashboardHostname != "" {
		if !hostname(c.DashboardHostname) || net.ParseIP(c.DashboardHostname) != nil {
			return errors.New("dashboard_hostname must be a DNS hostname without a scheme, port, or path")
		}
		if !hostname(c.DashboardAuthSecret) {
			return errors.New("dashboard_auth_secret must name a Kubernetes Secret containing username and password")
		}
	}
	if c.DashboardImage != "" && !dashboardImageRE.MatchString(c.DashboardImage) {
		return errors.New("dashboard_image must be a container image with an explicit tag or SHA-256 digest")
	}
	n := c.Node
	if n.Role != "server" && n.Role != "agent" {
		return errors.New("node.role must be server or agent")
	}
	if n.Name != "" && !labelRE.MatchString(n.Name) {
		return errors.New("node.name must be a DNS label")
	}
	for _, s := range []string{n.IP, n.ExternalIP} {
		if s != "" && (net.ParseIP(s) == nil || net.ParseIP(s).To4() == nil) {
			return errors.New("node addresses must be IPv4 addresses")
		}
	}
	if n.FlannelIface != "" && !regexp.MustCompile(`^[a-zA-Z0-9_.:-]{1,15}$`).MatchString(n.FlannelIface) {
		return errors.New("invalid node.flannel_iface")
	}
	if n.Datastore != "sqlite" && n.Datastore != "etcd" {
		return errors.New("node.datastore must be sqlite or etcd")
	}
	if (n.Datastore == "sqlite" && n.ServerCount != 1) || (n.Datastore == "etcd" && (n.ServerCount < 3 || n.ServerCount%2 == 0)) {
		return errors.New("use one SQLite server or an odd number of at least three etcd servers")
	}
	if n.Role == "agent" && (!n.Join || c.Platform) {
		return errors.New("agents require node.join: true and platform: false")
	}
	if n.Role == "server" && n.Join && n.Datastore != "etcd" {
		return errors.New("joining servers require etcd")
	}
	if n.Join && (n.TokenFile == "" || c.KubernetesAPIEndpoint == "127.0.0.1" || c.KubernetesAPIEndpoint == "localhost") {
		return errors.New("joining requires node.token_file and a reachable kubernetes_api_endpoint")
	}
	if n.TokenFile != "" && (!filepath.IsAbs(n.TokenFile) || strings.ContainsAny(n.TokenFile, "\n\r\x00")) {
		return errors.New("node.token_file must be an absolute path")
	}

	// Reconciliation must never overwrite the file that supplies the join token.
	for _, path := range []string{ConfigPath, MarkerPath, StatePath, KubeconfigPath, K3sPath, ModulesPath, SysctlPath, unitPath("server"), unitPath("agent"), unitPath("server") + ".env", unitPath("agent") + ".env", uninstallPath("server"), uninstallPath("agent"), KillallPath, RemovalScriptPath, RemovalKillallPath} {
		if n.TokenFile != "" && filepath.Clean(n.TokenFile) == path {
			return errors.New("node.token_file must not overlap an AK3S-managed file")
		}
	}
	pins, err := LoadPins()
	if err != nil {
		return err
	}
	names := map[string]bool{}
	for _, chart := range pins.Charts {
		names[chart.Release] = true
	}
	for name := range c.HelmValues {
		if !names[name] {
			return fmt.Errorf("unknown helm_values release %q", name)
		}
	}
	return nil
}

// ValidateInstall requires an ACME contact email only when platform services are managed.
func (c Config) ValidateInstall() error {
	if c.Platform && c.ACMEEmail == "" {
		return errors.New("set acme_email in values.yaml before installing the platform")
	}
	return nil
}
