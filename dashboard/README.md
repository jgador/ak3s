# AK3S local dashboard

A React and TypeScript dashboard built with Vite. It provides three views:

- **Overview:** node readiness, versions, platform add-ons, and links to Headlamp, VictoriaMetrics, and VictoriaLogs.
- **Configuration:** sanitized effective YAML and operator overrides, with a copy control.
- **Upgrade:** last attempted AK3S release and manual upgrade steps.

The dashboard reads the local AK3S-managed K3s server through a read-only endpoint. It loads data on startup, refreshes every 30 seconds, and supports manual refresh. Connection failures display an error and mark the last successful snapshot as stale. No sample data is substituted. Configuration and upgrade commands are displayed for copying; the dashboard never executes them.

Cluster name, datastore, and the last attempted AK3S release come from installation state. Node names, addresses, K3s version, readiness, and chart labels come from Kubernetes. Missing names display **Not available**. Available AK3S releases are not checked automatically.

## Run locally

Use Node.js 22.12+ (Node.js 24 recommended) and npm. First run `make build` from the repository root, then:

```bash
cd dashboard
npm ci
npm run dev
```

Run the dashboard inside the Linux or WSL distribution containing the cluster. It prefers `bin/ak3s`, falling back to `/usr/local/bin/ak3s`; the selected CLI must support `dashboard-snapshot`. Open `http://127.0.0.1:5173`. The server binds to loopback.

The collector needs permission to read `/etc/rancher/k3s/k3s.yaml`, `/var/lib/ak3s/state.json`, and `/etc/ak3s/values.yaml` if present. For an ordinary WSL user, `make port-forward` obtains sudo authorization for the local cluster. When starting Vite manually, run `sudo -v` in the same terminal first. The endpoint uses non-interactive sudo and reports an error if authorization expires; renew it in that terminal. Do not make the kubeconfig world-readable or copy credentials into dashboard files.

To start all four UIs, run `make port-forward` from the repository root after installing dashboard dependencies. It starts Vite and the Headlamp, VictoriaMetrics, and VictoriaLogs port-forwards in the background. Use `make port-forward-status` and `make port-forward-stop` to manage them. Stop an existing Vite instance first so port 5173 is free. See the [access guide](../docs/operations.md#access) for Windows and WSL access.

```bash
npm run check   # strict TypeScript checks
npm test        # endpoint access and failure checks
npm run build   # type-check and build static files
npm run preview # serve the build and live endpoint on port 4173
```

Navigation uses URL hashes. Fonts and icons are bundled locally. The layout supports narrow screens, keyboard navigation, visible focus, and reduced motion.

## Live data and external tools

[`server/live.mjs`](server/live.mjs) runs `ak3s dashboard-snapshot`. The CLI queries only the fixed local kubeconfig and refuses non-loopback API addresses. It projects node and workload fields into [`src/types.ts`](src/types.ts). Chart versions come from installed workload labels. Missing expected components or incomplete rollouts need attention; readiness is based on Kubernetes status, not an application health probe. The AK3S version records the last installation attempt, not a guarantee of successful reconciliation.

The configuration view merges the local operator file with embedded defaults and resolves an omitted node name from installation state. The override count counts leaf settings explicitly present in the operator file. Contact email, token paths, and all nonempty Helm overrides are redacted before sending data to the browser. Kubeconfig credentials, Kubernetes Secrets, workload environment variables, and installation fingerprints are never included. Copied YAML is a sanitized display, not a complete configuration backup.

The tool cards use these loopback addresses:

| Tool | URL |
| --- | --- |
| Headlamp | `http://127.0.0.1:8080` |
| VictoriaMetrics | `http://127.0.0.1:8428/vmui/` |
| VictoriaLogs | `http://127.0.0.1:9428/select/vmui/` |

These links require the corresponding port-forward or SSH tunnel on the operator's machine. Headlamp also requires a Kubernetes login token.

## Local access boundary

The endpoint accepts only `GET /api/snapshot`, with no parameters. It checks loopback peers, local Host headers, and same-origin requests, disables cross-origin access, and suppresses subprocess error output. Collection is cached for five seconds and concurrent requests share one collection. The Vite development and preview servers provide this endpoint; hosting only the static build does not.

This is a local operator tool. Keep it bound to loopback and use an SSH tunnel for a remote host. Public deployment would require a separate authenticated server and an explicit access policy.

Keep workload management in Headlamp, metrics in VictoriaMetrics, and log search in VictoriaLogs. Configuration and upgrade actions continue through the CLI's existing checks.
