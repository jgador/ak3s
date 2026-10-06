package ak3s

import (
	"reflect"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func testConfig(t *testing.T) Config {
	t.Helper()
	c, err := LoadConfig([]byte("acme_email: operator@example.com\n"))
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func testPins(t *testing.T) Pins {
	t.Helper()
	p, err := LoadPins()
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// TestSparseDefaults checks that omitted settings keep their defaults and platform installs require an email.
func TestSparseDefaults(t *testing.T) {
	c, err := LoadConfig([]byte("acme_email: ops@example.com\nkubernetes_api_endpoint: api.example.com\nnode:\n  ip: 10.0.0.2\nmetrics_retention: 14d\n"))
	if err != nil {
		t.Fatal(err)
	}
	if c.Node.Role != "server" || c.Node.Datastore != "sqlite" || c.Node.ServerCount != 1 || c.Node.IP != "10.0.0.2" || c.LogsRetention != "7d" || c.MetricsRetention != "14d" || c.ACMEEnvironment != "staging" || c.KubernetesAPIEndpoint != "api.example.com" {
		t.Fatalf("lost defaults: %+v", c)
	}
	empty, err := LoadConfig(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = empty.ValidateInstall(); err == nil {
		t.Fatal("missing email accepted for install")
	}
	empty.Platform = false
	if err = empty.ValidateInstall(); err != nil {
		t.Fatal(err)
	}
}

// TestMergeSemanticsAndIsolation checks map merging, scalar and list replacement, and isolation from the inputs.
func TestMergeSemanticsAndIsolation(t *testing.T) {
	base := map[string]any{"nested": map[string]any{"enabled": true, "count": 3, "list": []any{"a", "b"}}, "other": "kept"}
	got := Merge(base, map[string]any{"nested": map[string]any{"enabled": false, "count": 0, "list": []any{}}})
	nested := got["nested"].(map[string]any)
	if nested["enabled"] != false || nested["count"] != 0 || len(nested["list"].([]any)) != 0 || got["other"] != "kept" {
		t.Fatal(got)
	}
	nested["enabled"] = "changed"
	if base["nested"].(map[string]any)["enabled"] != true {
		t.Fatal("mutated defaults")
	}
	second := Merge(base, nil)
	second["nested"].(map[string]any)["list"].([]any)[0] = "different"
	if base["nested"].(map[string]any)["list"].([]any)[0] != "a" {
		t.Fatal("aliased default list")
	}
}

// TestRejectInvalidConfiguration checks that malformed or unsupported settings are rejected.
func TestRejectInvalidConfiguration(t *testing.T) {
	cases := []string{
		"foo: true", "node:\n  unknown: true", "node: null", "tls_sans: null", "[]", "a: b\n---\nc: d", "platform: false\nplatform: true", "node: &x {}\nhelm_values: *x", "true: 1", "kubernetes_api_endpoint: https://host:6443", "kubernetes_api_endpoint: 999.1.1.1", "kubernetes_api_endpoint: ::1", "tls_sans: [bad;host]", "tls_sans: host", "acme_email: invalid", "acme_environment: prod", "cluster_name: bad.name", "storage_class: /tmp/x", "metrics_retention: 0d", "logs_retention: -7d", "metrics_storage: 0Gi", "logs_storage: -1Gi", "logs_max_disk: 8Gi", "headlamp_hostname: https://example.com", "platform: nope", "node: {role: worker}", "node: {name: BAD}", "node: {ip: example.com}", "node: {external_ip: 2001:db8::1}", "node: {flannel_iface: 'eth0;touch'}", "node: {datastore: postgres}", "node: {datastore: sqlite, server_count: 3}", "node: {datastore: etcd, server_count: 2}", "node: {datastore: etcd, server_count: 4}", "node: {role: agent}", "node: {join: true}", "node: {token_file: relative}", "node: {token_file: /etc/rancher/k3s/config.yaml}", "helm_values: {typo: {enabled: true}}",
	}
	for _, input := range cases {
		t.Run(input, func(t *testing.T) {
			if _, err := LoadConfig([]byte(input)); err == nil {
				t.Fatal("accepted invalid configuration")
			}
		})
	}
}

// TestValidTopologies checks the supported server and agent configurations.
func TestValidTopologies(t *testing.T) {
	for _, s := range []string{"node: {datastore: etcd, server_count: 3}", "node: {datastore: etcd, server_count: 5, join: true, token_file: /root/token}\nkubernetes_api_endpoint: api.example.com", "platform: false\nnode: {role: agent, join: true, token_file: /root/token}\nkubernetes_api_endpoint: 10.0.0.1"} {
		if _, err := LoadConfig([]byte(s)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSharedDashboardConfiguration(t *testing.T) {
	for _, input := range []string{
		"dashboard_hostname: https://ak3s.example.com",
		"dashboard_hostname: ak3s.example.com/metrics",
		"dashboard_hostname: ak3s.example.com:443",
		"dashboard_hostname: 192.0.2.10",
		"dashboard_hostname: ak3s.example.com\nheadlamp_hostname: headlamp.example.com",
		"dashboard_hostname: ak3s.example.com\ndashboard_auth_secret: ''",
		"dashboard_hostname: ak3s.example.com\ndashboard_auth_secret: ../private",
		"dashboard_shared_paths: nope",
		"dashboard_shared_paths: true\nheadlamp_hostname: headlamp.example.com",
	} {
		if _, err := LoadConfig([]byte(input)); err == nil {
			t.Errorf("invalid shared dashboard configuration accepted: %s", input)
		}
	}
	c, err := LoadConfig([]byte("dashboard_hostname: ak3s.example.com"))
	if err != nil {
		t.Fatal(err)
	}
	if c.DashboardAuthSecret != "dashboard-auth" {
		t.Fatal("missing default authentication Secret")
	}
	for _, chart := range testPins(t).Charts {
		data, err := ValuesFor(chart, c)
		if err != nil {
			t.Fatal(err)
		}
		local := c
		local.DashboardHostname, local.DashboardSharedPaths = "", true
		localData, err := ValuesFor(chart, local)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(data, localData) {
			t.Fatal("local shared paths do not use the same chart settings as HTTPS access")
		}
		if chart.Release == "headlamp" {
			var values struct {
				Config struct {
					BaseURL string `yaml:"baseURL"`
					Unsafe  bool   `yaml:"unsafeUseServiceAccountToken"`
				}
				Ingress struct{ Enabled bool }
			}
			if err := yaml.Unmarshal(data, &values); err != nil {
				t.Fatal(err)
			}
			if values.Config.BaseURL != "/headlamp" || values.Config.Unsafe || values.Ingress.Enabled {
				t.Fatal("Headlamp prefix or authentication changed incorrectly")
			}
		} else if chart.Release == "victoria-metrics" || chart.Release == "victoria-logs" || chart.Release == "victoria-logs-collector" {
			private, _ := ValuesFor(chart, testConfig(t))
			if !reflect.DeepEqual(data, private) {
				t.Fatal("shared UI access changed internal collection settings")
			}
		}
	}
}

// TestValuesPreserveArchitecture checks generated chart settings, operator overrides and repeatable rendering.
func TestValuesPreserveArchitecture(t *testing.T) {
	c := testConfig(t)
	p := testPins(t)
	c.HeadlampHostname = "dashboard.example.com"
	c.ACMEEnvironment = "production"
	c.StorageClass = "custom"
	c.MetricsRetention = "14d"
	c.HelmValues = map[string]map[string]any{"nginx-ingress": {"controller": map[string]any{"replicaCount": 2}}}
	for _, chart := range p.Charts {
		t.Run(chart.Release, func(t *testing.T) {
			data, err := ValuesFor(chart, c)
			if err != nil {
				t.Fatal(err)
			}
			var v map[string]any
			if err = yaml.Unmarshal(data, &v); err != nil {
				t.Fatal(err)
			}
			switch chart.Release {
			case "nginx-ingress":
				ctrl := v["controller"].(map[string]any)
				if ctrl["replicaCount"] != 2 || ctrl["enableCustomResources"] != false || ctrl["enableSnippets"] != false || ctrl["nginxplus"] != false {
					t.Fatal(ctrl)
				}
			case "headlamp":
				if v["clusterRoleBinding"].(map[string]any)["create"] != false || !strings.Contains(string(data), "letsencrypt-production") || !strings.Contains(string(data), "dashboard.example.com") {
					t.Fatal(string(data))
				}
			case "victoria-metrics":
				if !strings.Contains(string(data), "retentionPeriod: 14d") || !strings.Contains(string(data), "storageClassName: custom") || !strings.Contains(string(data), "nodes/$1/proxy/metrics") {
					t.Fatal(string(data))
				}
			case "victoria-logs":
				if !strings.Contains(string(data), "retentionDiskSpaceUsage: 8GiB") || v["vector"].(map[string]any)["enabled"] != false {
					t.Fatal(string(data))
				}
			case "victoria-logs-collector":
				if strings.Contains(string(data), "/var/lib") || !strings.Contains(string(data), "maxDiskUsagePerURL: 1GiB") {
					t.Fatal(string(data))
				}
			}
		})
	}

	// Loading the defaults again cannot inherit an earlier render's edits.
	clean := testConfig(t)
	a, _ := ValuesFor(p.Charts[0], clean)
	b, _ := ValuesFor(p.Charts[0], clean)
	if !reflect.DeepEqual(a, b) {
		t.Fatal("nondeterministic values")
	}
}

// TestK3sRendering checks role-specific K3s settings, issuer generation and basic manifest validation.
func TestK3sRendering(t *testing.T) {
	c := testConfig(t)
	c.KubernetesAPIEndpoint = "control.example.com"
	c.TLSSANs = []string{c.KubernetesAPIEndpoint, "api.example.com"}
	data, _ := K3sConfig(c, "")
	var v map[string]any
	yaml.Unmarshal(data, &v)
	if v["secrets-encryption"] != true || v["write-kubeconfig-mode"] != "0600" || !reflect.DeepEqual(v["disable"], []any{"traefik"}) || !reflect.DeepEqual(v["tls-san"], []any{"control.example.com", "api.example.com"}) || v["cluster-init"] != nil {
		t.Fatal(v)
	}
	c.Node.Datastore = "etcd"
	c.Node.ServerCount = 3
	data, _ = K3sConfig(c, "")
	if !strings.Contains(string(data), "cluster-init: true") {
		t.Fatal(string(data))
	}
	c.Node.Role = "agent"
	c.Node.Join = true
	c.Node.TokenFile = "/root/token"
	data, _ = K3sConfig(c, "")
	if strings.Contains(string(data), "disable:") || strings.Contains(string(data), "cluster-init:") || !strings.Contains(string(data), "token-file: /root/token") {
		t.Fatal(string(data))
	}
	if !strings.Contains(string(data), "server: https://control.example.com:6443") {
		t.Fatal("joining node did not use the configured Kubernetes API endpoint")
	}
	docs, _ := Issuers(c)
	if err := ValidateManifests(docs); err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(docs), "kind: ClusterIssuer") != 2 || !strings.Contains(string(docs), "ingressClassName: nginx") {
		t.Fatal(string(docs))
	}
	for _, bad := range []string{"", "foo: bar", "[invalid", "apiVersion: v1\nkind: Service"} {
		if ValidateManifests([]byte(bad)) == nil {
			t.Fatal("accepted invalid manifests")
		}
	}
}

// FuzzLoadConfig checks that arbitrary bounded YAML input does not panic during configuration loading.
func FuzzLoadConfig(f *testing.F) {
	for _, s := range []string{"", "platform: false", "node: {role: agent}", "a: &a [*a]"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if len(s) > 16384 {
			t.Skip()
		}
		_, _ = LoadConfig([]byte(s))
	})
}
