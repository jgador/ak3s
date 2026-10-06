import {
  Activity,
  ArrowDownToLine,
  ArrowRight,
  ArrowUpRight,
  Box,
  Boxes,
  Check,
  CircleCheck,
  Database,
  FileText,
  Info,
  Layers3,
  LockKeyhole,
  Network,
  Server,
  ShieldCheck,
  SquareTerminal,
  Terminal,
} from "lucide-react";
import { useState } from "react";
import {
  CheckItem,
  CodeBlock,
  CopyButton,
  ExternalLinkButton,
  SectionHeading,
  Status,
} from "./components";
import type { DashboardSnapshot, InstalledComponent, ToolId } from "./types";

const toolIcons = { headlamp: Box, metrics: Activity, logs: SquareTerminal };
const componentIcons: Record<InstalledComponent["icon"], typeof Box> = {
  network: Network,
  shield: ShieldCheck,
  headlamp: Box,
  metrics: Activity,
  logs: FileText,
  collector: ArrowDownToLine,
};

export function Overview({ snapshot }: { snapshot: DashboardSnapshot }) {
  const { cluster, components, tools } = snapshot;
  const [guideOpen, setGuideOpen] = useState(false);
  const [guideTool, setGuideTool] = useState<ToolId>("headlamp");
  const readyNodes = cluster.nodes.filter(
    (node) => node.health === "healthy",
  ).length;
  const healthyComponents = components.filter(
    (component) => component.health === "healthy",
  ).length;
  const selectedTool = tools.find((tool) => tool.id === guideTool) ?? tools[0];

  return (
    <>
      <div className="summary-grid" aria-label="Cluster summary">
        <article className="summary-card">
          <div className="summary-label">
            <span>Kubernetes</span>
            <Boxes size={17} />
          </div>
          <div className="summary-value">{cluster.kubernetesVersion}</div>
          <p>
            K3s <span className="mono">{cluster.k3sVersion}</span>
          </p>
        </article>
        <article className="summary-card">
          <div className="summary-label">
            <span>Nodes</span>
            <Server size={17} />
          </div>
          <div className="summary-value">
            {readyNodes}
            <span className="value-caption">
              / {cluster.nodes.length} ready
            </span>
            {cluster.nodes.length > 0 && readyNodes === cluster.nodes.length && (
              <span className="mini-check">
                <Check size={12} />
              </span>
            )}
          </div>
          <p>
            {cluster.nodes.length === 1
              ? "Single-server cluster"
              : `${cluster.nodes.length} cluster nodes`}
          </p>
        </article>
        <article className="summary-card">
          <div className="summary-label">
            <span>AK3S version</span>
            <Layers3 size={17} />
          </div>
          <div className="summary-value">{cluster.ak3sVersion}</div>
          <a className="summary-update" href="#/upgrade">
            {snapshot.upgrade.availableVersion ? "Update available" : "Review upgrades"} <ArrowRight size={13} />
          </a>
        </article>
        <article className="summary-card">
          <div className="summary-label">
            <span>Platform components</span>
            <CircleCheck size={17} />
          </div>
          <div className="summary-value">
            {healthyComponents}
            <span className="value-caption">/ {components.length} healthy</span>
          </div>
          <p>Managed by AK3S</p>
        </article>
      </div>

      <section className="tools-section" aria-labelledby="tools-title">
        <div className="section-heading">
          <div>
            <h2 id="tools-title">Your cluster toolkit</h2>
            <p>The right tool for every task, one click away.</p>
          </div>
          <span className="section-meta">
            <ArrowUpRight size={14} /> Open in a new tab
          </span>
        </div>
        <div className="tools-grid">
          {tools.map((tool) => {
            const Icon = toolIcons[tool.id];
            return (
              <a
                className={`tool-card tool-${tool.id}`}
                key={tool.id}
                href={tool.url}
                target="_blank"
                rel="noopener noreferrer"
              >
                <div className="tool-card-top">
                  <span className="tool-icon">
                    <Icon size={25} strokeWidth={1.7} />
                  </span>
                  <span className="tool-category">{tool.category}</span>
                  <ArrowUpRight className="tool-arrow" size={19} />
                </div>
                <h3>{tool.name}</h3>
                <p>{tool.description}</p>
                <div className="tool-card-bottom">
                  <Status health={tool.health} subtle />
                  <span className="tool-open">
                    Open {tool.name}
                    <ArrowRight size={14} />
                  </span>
                </div>
                <span className="sr-only">
                  {" "}
                  (opens in a new tab; requires a local port-forward or SSH
                  tunnel)
                </span>
              </a>
            );
          })}
        </div>
        <div className="connection-note">
          <span>
            <LockKeyhole size={13} />
            Private by default. Connect through a local port-forward or SSH
            tunnel.
          </span>
          <button
            aria-expanded={guideOpen}
            aria-controls="connection-guide"
            onClick={() => setGuideOpen(!guideOpen)}
          >
            Connection guide
            <ArrowRight className={guideOpen ? "rotate-down" : ""} size={13} />
          </button>
        </div>
        {guideOpen && selectedTool && (
          <div className="connection-guide panel" id="connection-guide">
            <SectionHeading
              title="Connect to your tools"
              description="Run the command on the cluster host. For a remote server, also tunnel the matching local port over SSH."
            />
            <div className="guide-controls">
              <div className="segmented-control" aria-label="Connection tool">
                {tools.map((tool) => (
                  <button
                    key={tool.id}
                    aria-pressed={guideTool === tool.id}
                    onClick={() => setGuideTool(tool.id)}
                  >
                    {tool.name}
                  </button>
                ))}
              </div>
              <CopyButton
                text={selectedTool.portForward}
                label="Copy command"
              />
            </div>
            <CodeBlock code={selectedTool.portForward} />
            <p className="guide-help">
              Then open{" "}
              <ExternalLinkButton href={selectedTool.url}>
                {selectedTool.url}
              </ExternalLinkButton>
              .
            </p>
            {guideTool === "headlamp" && (
              <div className="headlamp-access">
                <p>
                  Headlamp requires a Kubernetes login token. Create a temporary
                  viewer token on the cluster host:
                </p>
                <CodeBlock code="sudo k3s kubectl -n headlamp create token headlamp-viewer --duration=1h" />
                <p>Enter the token in Headlamp. Keep it private.</p>
              </div>
            )}
          </div>
        )}
      </section>

      <div className="overview-details">
        <section
          className="panel components-panel"
          aria-labelledby="components-title"
        >
          <div className="panel-heading">
            <h2 id="components-title">
              Installed components
              <span className="count-badge">{components.length}</span>
            </h2>
            <span className="all-healthy">
              <span className="status-dot" />
              {components.length > 0 && healthyComponents === components.length
                ? "All healthy"
                : `${healthyComponents} healthy`}
            </span>
          </div>
          <div
            className="table-scroll"
            role="region"
            aria-label="Installed component health"
            tabIndex={0}
          >
            <table className="components-table">
              <thead>
                <tr>
                  <th>Component</th>
                  <th>Chart version</th>
                  <th>Status</th>
                </tr>
              </thead>
              <tbody>
                {components.map((component) => {
                  const Icon = componentIcons[component.icon];
                  return (
                    <tr key={component.name}>
                      <td>
                        <div className="component-cell">
                          <span
                            className={`component-icon component-${component.icon}`}
                          >
                            <Icon size={17} strokeWidth={1.7} />
                          </span>
                          <div>
                            <span className="component-name">
                              {component.name}
                            </span>
                            <span className="component-purpose">
                              {component.purpose}
                            </span>
                          </div>
                        </div>
                      </td>
                      <td className="mono version-cell">
                        {component.chartVersion}
                      </td>
                      <td>
                        <Status health={component.health} />
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
          <div className="panel-footer">
            <Info size={13} />
            <span>Platform add-ons installed and managed by AK3S.</span>
          </div>
        </section>
        <div className="details-column">
          <section
            className="panel cluster-panel"
            aria-labelledby="cluster-title"
          >
            <div className="panel-heading">
              <h2 id="cluster-title">Cluster details</h2>
              <Server size={17} />
            </div>
            <dl className="cluster-properties">
              <div>
                <dt>Cluster name</dt>
                <dd>{cluster.name?.trim() || "Not available"}</dd>
              </div>
              <div>
                <dt>Datastore</dt>
                <dd>
                  <Database size={13} />
                  {cluster.datastore}
                </dd>
              </div>
              <div>
                <dt>Configuration</dt>
                <dd>
                  <a href="#/configuration">
                    {snapshot.configuration.overrideCount} overrides
                    <ArrowUpRight size={13} />
                  </a>
                </dd>
              </div>
            </dl>
            <div className="endpoint-block">
              <span>API endpoint</span>
              <div>
                <code>{cluster.apiEndpoint}</code>
                <CopyButton
                  text={cluster.apiEndpoint}
                  label="Copy API endpoint"
                  compact
                />
              </div>
            </div>
          </section>
          <section className="panel nodes-panel" aria-labelledby="nodes-title">
            <div className="panel-heading">
              <h2 id="nodes-title">
                Nodes<span className="count-badge">{cluster.nodes.length}</span>
              </h2>
              <ExternalLinkButton
                href={tools.find((tool) => tool.id === "headlamp")?.url}
                className="text-link"
              >
                Headlamp
              </ExternalLinkButton>
            </div>
            {cluster.nodes.map((node) => (
              <article className="node" key={node.address}>
                <div className="node-heading">
                  <span className="node-icon">
                    <Server size={18} />
                  </span>
                  <div>
                    <h3>{node.name?.trim() || "Not available"}</h3>
                    <p>{node.role}</p>
                  </div>
                  <Status
                    health={node.health}
                    label={node.health === "healthy" ? "Ready" : undefined}
                    subtle
                  />
                </div>
                <div className="node-meta">
                  <code>{node.address}</code>
                  <span>{node.architecture}</span>
                </div>
                <p className="node-os">{node.operatingSystem}</p>
              </article>
            ))}
          </section>
          <div className="ownership-note">
            <Terminal size={16} />
            <p>
              AK3S keeps the platform together.
              <br />
              Your tools take it from here.
            </p>
          </div>
        </div>
      </div>
      <div className="overview-footer">
        <CheckItem>Small footprint. Full control.</CheckItem>
        <span>Live cluster · Read only</span>
      </div>
    </>
  );
}
