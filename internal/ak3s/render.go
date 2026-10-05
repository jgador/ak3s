package ak3s

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	"github.com/jgador/ak3s/platform"
	"go.yaml.in/yaml/v3"
)

// K3sConfig renders settings for this node, preserving an existing inline token if needed.
func K3sConfig(c Config, legacyToken string) ([]byte, error) {
	n := c.Node
	v := map[string]any{}
	for k, s := range map[string]string{"node-name": n.Name, "node-ip": n.IP, "node-external-ip": n.ExternalIP, "flannel-iface": n.FlannelIface, "token-file": n.TokenFile} {
		if s != "" {
			v[k] = s
		}
	}
	if legacyToken != "" {
		v["token"] = legacyToken
	}
	if n.Role == "server" {
		v["disable"] = []string{"traefik"}
		v["write-kubeconfig-mode"] = "0600"
		v["secrets-encryption"] = true
		sans := []string{c.APIEndpoint}
		for _, s := range c.TLSSANs {
			found := false
			for _, x := range sans {
				if x == s {
					found = true
				}
			}
			if !found {
				sans = append(sans, s)
			}
		}
		v["tls-san"] = sans

		// Only the first etcd server initializes the datastore; other servers join it.
		if n.Datastore == "etcd" && !n.Join {
			v["cluster-init"] = true
		}
	}
	if n.Join {
		v["server"] = "https://" + c.APIEndpoint + ":6443"
	}
	return yaml.Marshal(v)
}

// ValuesFor merges chart defaults, generated AK3S settings, and operator overrides in that order.
func ValuesFor(chart Chart, c Config) ([]byte, error) {
	raw, err := platform.Files.ReadFile("values/" + chart.Values)
	if err != nil {
		return nil, err
	}
	base, err := mapping(raw)
	if err != nil {
		return nil, err
	}
	generated := map[string]any{}
	switch chart.Release {
	case "victoria-metrics", "victoria-logs":
		retention, storage := c.MetricsRetention, c.MetricsStorage
		if chart.Release == "victoria-logs" {
			retention, storage = c.LogsRetention, c.LogsStorage
		}
		server := map[string]any{"retentionPeriod": retention, "persistentVolume": map[string]any{"storageClassName": c.StorageClass, "size": storage}}
		if chart.Release == "victoria-logs" {
			server["retentionDiskSpaceUsage"] = c.LogsMaxDisk
		} else {
			server["scrape"] = map[string]any{"config": map[string]any{"global": map[string]any{"external_labels": map[string]any{"cluster": c.ClusterName}}}}
		}
		generated["server"] = server
	case "headlamp":
		if c.HeadlampHostname != "" {
			host := c.HeadlampHostname
			generated["ingress"] = map[string]any{"enabled": true, "ingressClassName": "nginx", "annotations": map[string]any{"cert-manager.io/cluster-issuer": "letsencrypt-" + c.ACMEEnvironment, "nginx.org/ssl-redirect": "true"}, "hosts": []any{map[string]any{"host": host, "paths": []any{map[string]any{"path": "/", "type": "Prefix"}}}}, "tls": []any{map[string]any{"secretName": "headlamp-tls", "hosts": []string{host}}}}
		}
	}
	return yaml.Marshal(Merge(Merge(base, generated), c.HelmValues[chart.Release]))
}

// Issuers creates staging and production Let's Encrypt issuers using NGINX HTTP-01.
func Issuers(c Config) ([]byte, error) {
	var out bytes.Buffer
	e := yaml.NewEncoder(&out)
	defer e.Close()
	for _, env := range []string{"staging", "production"} {
		url := "https://acme-v02.api.letsencrypt.org/directory"
		if env == "staging" {
			url = "https://acme-staging-v02.api.letsencrypt.org/directory"
		}
		doc := map[string]any{"apiVersion": "cert-manager.io/v1", "kind": "ClusterIssuer", "metadata": map[string]any{"name": "letsencrypt-" + env}, "spec": map[string]any{"acme": map[string]any{"email": c.ACMEEmail, "server": url, "privateKeySecretRef": map[string]any{"name": "letsencrypt-" + env + "-account"}, "solvers": []any{map[string]any{"http01": map[string]any{"ingress": map[string]any{"ingressClassName": "nginx"}}}}}}}
		if err := e.Encode(doc); err != nil {
			return nil, err
		}
	}
	return out.Bytes(), nil
}

// ValidateManifests checks basic Kubernetes document fields, not API schema validity.
func ValidateManifests(data []byte) error {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	count := 0
	for {
		var v map[string]any
		err := dec.Decode(&v)
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if len(v) == 0 {
			continue
		}
		count++
		if v["apiVersion"] == nil || v["kind"] == nil || v["metadata"] == nil {
			return fmt.Errorf("rendered document %d lacks Kubernetes resource fields", count)
		}
	}
	if count == 0 {
		return fmt.Errorf("no Kubernetes resources rendered")
	}
	return nil
}

// serviceUnit runs K3s with the managed configuration and a service type suited to its role.
func serviceUnit(role string) []byte {
	serviceType := "notify"
	if role == "agent" {
		serviceType = "exec"
	}
	return []byte(fmt.Sprintf(`[Unit]
Description=AK3S Kubernetes %s
Wants=network-online.target
After=network-online.target

[Service]
Type=%s
EnvironmentFile=-/etc/default/%%N
EnvironmentFile=-/etc/sysconfig/%%N
ExecStart=/usr/local/bin/k3s %s --config /etc/rancher/k3s/config.yaml
KillMode=process
Delegate=yes
LimitNOFILE=1048576
LimitNPROC=infinity
LimitCORE=infinity
TasksMax=infinity
TimeoutStartSec=0
Restart=always
RestartSec=5s

[Install]
WantedBy=multi-user.target
`, role, serviceType, role))
}
func kubeVersion(p Pins) string { return strings.Split(strings.TrimPrefix(p.K3s, "v"), "+")[0] }
