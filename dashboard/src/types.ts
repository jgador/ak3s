export type Page = "overview" | "configuration" | "upgrade";
export type Health = "healthy" | "degraded" | "unknown";
export type ToolId = "headlamp" | "metrics" | "logs";

export interface ClusterNode {
  name: string;
  role: string;
  address: string;
  operatingSystem: string;
  architecture: string;
  health: Health;
}

export interface InstalledComponent {
  name: string;
  purpose: string;
  namespace: string;
  chartVersion: string;
  health: Health;
  icon: "network" | "shield" | "headlamp" | "metrics" | "logs" | "collector";
}

export interface ClusterTool {
  id: ToolId;
  name: string;
  category: string;
  description: string;
  url: string;
  portForward: string;
  health: Health;
}

/** Display data only; a future server must collect and redact cluster data. */
export interface DashboardSnapshot {
  cluster: {
    name: string;
    apiEndpoint: string;
    k3sVersion: string;
    kubernetesVersion: string;
    ak3sVersion: string;
    datastore: string;
    health: Health;
    nodes: ClusterNode[];
  };
  components: InstalledComponent[];
  tools: ClusterTool[];
  configuration: {
    path: string;
    overrideCount: number;
    effectiveYaml: string;
    overridesYaml: string;
  };
  upgrade: { availableVersion: string };
}
