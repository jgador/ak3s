import type { DashboardSnapshot } from "./types";

// Fictional cluster and AK3S releases. Chart pins and settings match platform/.
// Loopback links require the port-forwards documented in docs/operations.md.
export const demoSnapshot: DashboardSnapshot = {
  cluster: {
    name: "cedar",
    apiEndpoint: "https://203.0.113.10:6443",
    k3sVersion: "v1.37.1+k3s1",
    kubernetesVersion: "v1.37.1",
    ak3sVersion: "v0.8.0",
    datastore: "SQLite",
    health: "healthy",
    nodes: [
      {
        name: "cedar-01",
        role: "Control plane",
        address: "203.0.113.10",
        operatingSystem: "Ubuntu 24.04 LTS",
        architecture: "amd64",
        health: "healthy",
      },
    ],
  },
  components: [
    {
      name: "NGINX Ingress",
      purpose: "Ingress controller",
      namespace: "ingress-nginx",
      chartVersion: "2.7.3",
      health: "healthy",
      icon: "network",
    },
    {
      name: "cert-manager",
      purpose: "Certificate management",
      namespace: "cert-manager",
      chartVersion: "v1.21.2",
      health: "healthy",
      icon: "shield",
    },
    {
      name: "Headlamp",
      purpose: "Kubernetes management",
      namespace: "headlamp",
      chartVersion: "0.45.0",
      health: "healthy",
      icon: "headlamp",
    },
    {
      name: "VictoriaMetrics",
      purpose: "Metrics storage",
      namespace: "observability",
      chartVersion: "0.48.0",
      health: "healthy",
      icon: "metrics",
    },
    {
      name: "VictoriaLogs",
      purpose: "Log storage",
      namespace: "observability",
      chartVersion: "0.13.10",
      health: "healthy",
      icon: "logs",
    },
    {
      name: "VictoriaLogs Collector",
      purpose: "Log collection",
      namespace: "observability",
      chartVersion: "0.3.8",
      health: "healthy",
      icon: "collector",
    },
  ],
  tools: [
    {
      id: "headlamp",
      name: "Headlamp",
      category: "KUBERNETES",
      description:
        "Browse and manage your Kubernetes cluster resources.",
      url: "http://127.0.0.1:8080",
      portForward:
        "sudo k3s kubectl -n headlamp port-forward service/headlamp 8080:80",
      health: "healthy",
    },
    {
      id: "metrics",
      name: "VictoriaMetrics",
      category: "MONITORING",
      description:
        "Explore metrics and monitor your cluster's performance.",
      url: "http://127.0.0.1:8428/vmui/",
      portForward:
        "sudo k3s kubectl -n observability port-forward service/victoria-metrics 8428:8428",
      health: "healthy",
    },
    {
      id: "logs",
      name: "VictoriaLogs",
      category: "LOGS & SEARCH",
      description:
        "Search and explore your application logs in one place.",
      url: "http://127.0.0.1:9428/select/vmui/",
      portForward:
        "sudo k3s kubectl -n observability port-forward service/victoria-logs 9428:9428",
      health: "healthy",
    },
  ],
  configuration: {
    path: "/etc/ak3s/values.yaml",
    overrideCount: 5,
    effectiveYaml: `cluster_name: cedar
api_endpoint: 203.0.113.10
tls_sans: []
acme_email: operator@example.com
acme_environment: staging
storage_class: local-path
metrics_retention: 14d
metrics_storage: 10Gi
logs_retention: 7d
logs_storage: 10Gi
logs_max_disk: 8GiB
headlamp_hostname: ""
platform: true
node:
  role: server
  name: cedar-01
  ip: ""
  external_ip: ""
  flannel_iface: ""
  datastore: sqlite
  server_count: 1
  join: false
  token_file: ""
helm_values: {}
`,
    overridesYaml: `cluster_name: cedar
api_endpoint: 203.0.113.10
acme_email: operator@example.com
metrics_retention: 14d
node:
  name: cedar-01
`,
  },
  upgrade: { availableVersion: "v0.9.0" },
};

export const docsUrl = "https://github.com/jgador/ak3s/blob/master/docs";
export const releasesUrl = "https://github.com/jgador/ak3s/releases";

export const upgradeCommands = `sudo ak3s upgrade --dry-run
sudo ak3s upgrade
sudo ak3s status`;
