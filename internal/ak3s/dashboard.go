package ak3s

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

// Dashboard data contains an explicit display projection, never raw Kubernetes objects.
type dashboardNode struct {
	Name            string `json:"name"`
	Role            string `json:"role"`
	Address         string `json:"address"`
	OperatingSystem string `json:"operatingSystem"`
	Architecture    string `json:"architecture"`
	Health          string `json:"health"`
}
type dashboardComponent struct {
	Name         string `json:"name"`
	Purpose      string `json:"purpose"`
	Namespace    string `json:"namespace"`
	ChartVersion string `json:"chartVersion"`
	Health       string `json:"health"`
	Icon         string `json:"icon"`
}
type dashboardTool struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Category    string `json:"category"`
	Description string `json:"description"`
	URL         string `json:"url"`
	PortForward string `json:"portForward"`
	Health      string `json:"health"`
}
type dashboardData struct {
	CollectedAt string `json:"collectedAt"`
	Access      string `json:"access"`
	Cluster     struct {
		Name                  string          `json:"name"`
		KubernetesAPIEndpoint string          `json:"kubernetesApiEndpoint"`
		K3sVersion            string          `json:"k3sVersion"`
		KubernetesVersion     string          `json:"kubernetesVersion"`
		AK3SVersion           string          `json:"ak3sVersion"`
		Datastore             string          `json:"datastore"`
		Health                string          `json:"health"`
		Nodes                 []dashboardNode `json:"nodes"`
	} `json:"cluster"`
	Components    []dashboardComponent   `json:"components"`
	Tools         []dashboardTool        `json:"tools"`
	Configuration dashboardConfiguration `json:"configuration"`
	Upgrade       struct {
		AvailableVersion *string `json:"availableVersion"`
	} `json:"upgrade"`
}

type dashboardVersion struct{ GitVersion string }
type dashboardResources struct {
	Items    []dashboardResource
	Metadata struct{ Continue string }
}

type dashboardResource struct {
	Metadata struct {
		Name       string
		Namespace  string
		Generation int64
		Labels     map[string]string
	}
	Spec   struct{ Replicas *int }
	Status struct {
		ObservedGeneration     int64
		ReadyReplicas          int
		UpdatedReplicas        int
		DesiredNumberScheduled int
		NumberReady            int
		UpdatedNumberScheduled int
		Conditions             []struct{ Type, Status string }
		Addresses              []struct{ Type, Address string }
		NodeInfo               struct{ OSImage, Architecture, KubeletVersion string }
	}
	Kind string
}

func (a *App) dashboardSnapshot(ctx context.Context, path string) error {
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	explicit := path != ""
	if path == "" {
		path = "/etc/ak3s/values.yaml"
	}
	raw, err := a.ReadFile(path)
	if err != nil && (explicit || !errors.Is(err, fs.ErrNotExist)) {
		return errors.New("cannot read AK3S configuration")
	}
	c, err := LoadConfig(raw)
	if err != nil {
		return errors.New("invalid AK3S configuration; check it with ak3s config")
	}
	marker, err := a.ReadFile(MarkerPath)
	if err != nil || !strings.HasPrefix(string(marker), "Managed by AK3S") {
		return errors.New("no local AK3S-managed cluster found")
	}
	stateRaw, err := a.ReadFile(StatePath)
	if err != nil {
		return errors.New("cannot read AK3S installation state")
	}
	var state State
	if json.Unmarshal(stateRaw, &state) != nil || (state.Schema != 1 && state.Schema != 2) || state.ClusterName == "" || state.Node.Name == "" || state.Node.Role != "server" || state.PendingUninstall {
		return errors.New("dashboard requires a local AK3S server with valid installation state")
	}
	// Validate the selected local API without returning any authentication material.
	kubeRaw, err := a.ReadFile(KubeconfigPath)
	if err != nil {
		return errors.New("cannot read local K3s kubeconfig; run with permission to read it")
	}
	var kube struct {
		CurrentContext string `yaml:"current-context"`
		Contexts       []struct {
			Name    string
			Context struct{ Cluster string }
		}
		Clusters []struct {
			Name    string
			Cluster struct{ Server string }
		}
	}
	if yaml.Unmarshal(kubeRaw, &kube) != nil {
		return errors.New("invalid local K3s kubeconfig")
	}
	clusterName, endpoint := "", ""
	for _, entry := range kube.Contexts {
		if entry.Name == kube.CurrentContext {
			clusterName = entry.Context.Cluster
		}
	}
	for _, entry := range kube.Clusters {
		if entry.Name == clusterName {
			endpoint = entry.Cluster.Server
		}
	}
	u, err := url.Parse(endpoint)
	if err != nil || clusterName == "" || u.Scheme != "https" || u.User != nil || u.Port() != "6443" || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || (u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost" && u.Hostname() != "::1") {
		return errors.New("dashboard only reads the loopback API in the local K3s kubeconfig")
	}
	dir, err := os.MkdirTemp("", "ak3s-dashboard-")
	if err != nil {
		return errors.New("cannot create private dashboard cache")
	}
	defer os.RemoveAll(dir)
	query := func(args []string, target any) error {
		out, err := a.Runner.Run(ctx, Command{Name: K3sPath, Args: append([]string{"kubectl", "--kubeconfig", KubeconfigPath, "--cache-dir", filepath.Join(dir, "cache"), "--request-timeout=8s"}, args...), IsolatedEnv: true})
		if err != nil {
			return errors.New("cannot read local Kubernetes API")
		}
		if json.Unmarshal(out, target) != nil {
			return errors.New("invalid Kubernetes API response")
		}
		return nil
	}
	var nodes, workloads dashboardResources
	var version dashboardVersion
	if err := query([]string{"get", "--raw=/version"}, &version); err != nil {
		return err
	}
	if err := query([]string{"get", "nodes", "-o", "json"}, &nodes); err != nil {
		return err
	}
	if err := query([]string{"get", "deployments.apps,statefulsets.apps,daemonsets.apps", "-A", "-o", "json"}, &workloads); err != nil {
		return err
	}
	c.sourcePath = path
	if c.Node.Name == "" {
		c.Node.Name = state.Node.Name
	}
	metadata, err := dashboardMetadataFor(c)
	if err != nil {
		return err
	}
	metadata.ClusterName, metadata.KubernetesAPIEndpoint = state.ClusterName, endpoint
	metadata.AK3SVersion, metadata.Datastore = state.AK3SVersion, state.Node.Datastore
	metadata.NodeName = state.Node.Name
	return json.NewEncoder(a.Out).Encode(dashboardProjection(metadata, version, nodes, workloads))
}

func dashboardProjection(metadata dashboardMetadata, version dashboardVersion, nodes, workloads dashboardResources) dashboardData {
	d := dashboardData{CollectedAt: time.Now().UTC().Format(time.RFC3339), Components: []dashboardComponent{}, Tools: []dashboardTool{}}
	d.Cluster.Name, d.Cluster.KubernetesAPIEndpoint = metadata.ClusterName, metadata.KubernetesAPIEndpoint
	d.Cluster.AK3SVersion, d.Cluster.Datastore = metadata.AK3SVersion, metadata.Datastore
	d.Cluster.KubernetesVersion, d.Cluster.Health = version.GitVersion, "healthy"
	d.Cluster.Nodes = []dashboardNode{}
	for _, n := range nodes.Items {
		node := dashboardNode{Name: n.Metadata.Name, Role: "Worker", OperatingSystem: n.Status.NodeInfo.OSImage, Architecture: n.Status.NodeInfo.Architecture, Health: "unknown"}
		if _, ok := n.Metadata.Labels["node-role.kubernetes.io/control-plane"]; ok {
			node.Role = "Control plane"
		}
		for _, address := range n.Status.Addresses {
			if address.Type == "InternalIP" {
				node.Address = address.Address
				break
			}
		}
		for _, condition := range n.Status.Conditions {
			if condition.Type == "Ready" {
				node.Health = "degraded"
				if condition.Status == "True" {
					node.Health = "healthy"
				}
			}
		}
		if node.Health != "healthy" {
			d.Cluster.Health = "degraded"
		}
		if n.Metadata.Name == metadata.NodeName {
			d.Cluster.K3sVersion = n.Status.NodeInfo.KubeletVersion
		}
		d.Cluster.Nodes = append(d.Cluster.Nodes, node)
	}
	if len(d.Cluster.Nodes) == 0 {
		d.Cluster.Health = "unknown"
	}
	definitions := []struct{ release, namespace, name, purpose, icon string }{
		{"nginx-ingress", "ingress-nginx", "NGINX Ingress", "Ingress controller", "network"},
		{"cert-manager", "cert-manager", "cert-manager", "Certificate management", "shield"},
		{"headlamp", "headlamp", "Headlamp", "Kubernetes management", "headlamp"},
		{"victoria-metrics", "observability", "VictoriaMetrics", "Metrics storage", "metrics"},
		{"victoria-logs", "observability", "VictoriaLogs", "Log storage", "logs"},
		{"victoria-logs-collector", "observability", "VictoriaLogs Collector", "Log collection", "collector"},
	}
	health := map[string]string{}
	for _, definition := range definitions {
		component := dashboardComponent{Name: definition.name, Purpose: definition.purpose, Namespace: definition.namespace, Icon: definition.icon, Health: "healthy", ChartVersion: "Not available"}
		found := false
		for _, workload := range workloads.Items {
			if workload.Metadata.Namespace != definition.namespace || workload.Metadata.Labels["app.kubernetes.io/instance"] != definition.release {
				continue
			}
			found = true
			chart := workload.Metadata.Labels["helm.sh/chart"]
			if index := strings.LastIndex(chart, "-"); index >= 0 {
				component.ChartVersion = chart[index+1:]
			}
			if !dashboardWorkloadReady(workload) {
				component.Health = "degraded"
			}
		}
		if !found {
			component.Health = "degraded"
			component.ChartVersion = "Not installed"
		}
		if found || metadata.Platform {
			d.Components = append(d.Components, component)
			if component.Health != "healthy" {
				d.Cluster.Health = "degraded"
			}
		}
		health[definition.release] = component.Health
	}
	d.Tools = []dashboardTool{
		{ID: "headlamp", Name: "Headlamp", Category: "KUBERNETES", Description: "Browse Kubernetes cluster resources.", URL: "http://127.0.0.1:8080", PortForward: "sudo k3s kubectl -n headlamp port-forward service/headlamp 8080:80", Health: health["headlamp"]},
		{ID: "metrics", Name: "VictoriaMetrics", Category: "MONITORING", Description: "Explore cluster metrics.", URL: "http://127.0.0.1:8428/vmui/", PortForward: "sudo k3s kubectl -n observability port-forward service/victoria-metrics 8428:8428", Health: health["victoria-metrics"]},
		{ID: "logs", Name: "VictoriaLogs", Category: "LOGS & SEARCH", Description: "Search application logs.", URL: "http://127.0.0.1:9428/select/vmui/", PortForward: "sudo k3s kubectl -n observability port-forward service/victoria-logs 9428:9428", Health: health["victoria-logs"]},
	}
	d.Access = "local"
	if metadata.DashboardSharedPaths {
		// CLI snapshots also serve Vite, where tools still use direct forwards.
		d.Tools[0].URL += "/headlamp/"
	}
	if metadata.DashboardHostname != "" {
		d.Access = "https"
		for i := range d.Tools {
			d.Tools[i].URL = "https://" + metadata.DashboardHostname + "/" + d.Tools[i].ID
			d.Tools[i].PortForward = ""
		}
	}
	d.Configuration = metadata.Configuration
	return d
}

func dashboardWorkloadReady(w dashboardResource) bool {
	if w.Status.ObservedGeneration < w.Metadata.Generation {
		return false
	}
	if w.Kind == "DaemonSet" {
		return w.Status.DesiredNumberScheduled > 0 && w.Status.NumberReady == w.Status.DesiredNumberScheduled && w.Status.UpdatedNumberScheduled == w.Status.DesiredNumberScheduled
	}
	replicas := 1
	if w.Spec.Replicas != nil {
		replicas = *w.Spec.Replicas
	}
	return w.Status.ReadyReplicas == replicas && w.Status.UpdatedReplicas == replicas
}

func dashboardOverrideCount(m map[string]any) int {
	count := 0
	for _, value := range m {
		if nested, ok := value.(map[string]any); ok && len(nested) > 0 {
			count += dashboardOverrideCount(nested)
		} else {
			count++
		}
	}
	return count
}

// Only known public settings are copied. Arbitrary Helm values and identity secrets
// are replaced as a whole so new nested credential keys cannot escape redaction.
func dashboardConfigYAML(m map[string]any) string {
	result := map[string]any{}
	for _, key := range []string{"cluster_name", "kubernetes_api_endpoint", "tls_sans", "acme_environment", "storage_class", "metrics_retention", "metrics_storage", "logs_retention", "logs_storage", "logs_max_disk", "headlamp_hostname", "dashboard_hostname", "dashboard_shared_paths", "dashboard_auth_secret", "dashboard_image", "platform"} {
		if value, ok := m[key]; ok {
			result[key] = value
		}
	}
	if _, ok := m["acme_email"]; ok {
		result["acme_email"] = "[redacted]"
	}
	if values, ok := m["helm_values"].(map[string]any); ok {
		if len(values) == 0 {
			result["helm_values"] = map[string]any{}
		} else {
			result["helm_values"] = "[redacted]"
		}
	}
	if node, ok := m["node"].(map[string]any); ok {
		public := map[string]any{}
		for _, key := range []string{"role", "name", "ip", "external_ip", "flannel_iface", "datastore", "server_count", "join"} {
			if value, ok := node[key]; ok {
				public[key] = value
			}
		}
		if value, ok := node["token_file"]; ok {
			if value == "" {
				public["token_file"] = ""
			} else {
				public["token_file"] = "[redacted]"
			}
		}
		result["node"] = public
	}
	out, _ := yaml.Marshal(result)
	return string(out)
}
