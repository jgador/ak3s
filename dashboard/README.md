# AK3S dashboard mockup

A small React and TypeScript dashboard built with Vite. It provides three views:

- **Overview:** cluster health, Kubernetes/K3s and AK3S versions, nodes, installed platform add-ons, and links to Headlamp, VictoriaMetrics, and VictoriaLogs.
- **Configuration:** read-only effective YAML and operator overrides, with a copy control.
- **Upgrade:** current and available example AK3S releases, with the documented manual upgrade steps.

The dashboard uses fictional data and has no backend or live cluster connection. Refresh only updates the sample display timestamp. Configuration and upgrade commands are displayed for copying; the dashboard never executes them.

## Run locally

Use Node.js 22.12+ (Node.js 24 recommended) and npm:

```bash
cd dashboard
npm ci
npm run dev
```

Open `http://127.0.0.1:5173`. The development server binds to loopback. No cluster, Go build, credentials, or environment file is needed.

To browse all four UIs with a local AK3S test cluster, run `make port-forward`
from the repository root after installing the dashboard dependencies. It starts
this development server and the Headlamp, VictoriaMetrics, and VictoriaLogs
port-forwards in the background, then returns to your shell. Use
`make port-forward-status` and `make port-forward-stop` to manage them. Stop any
existing `npm run dev` instance first so port 5173 is free. See the
[access guide](../docs/operations.md#start-all-uis-for-local-testing) for Windows
and WSL access. The dashboard continues to use fictional data.

```bash
npm run check   # strict TypeScript checks
npm run build   # type-check and build static files into dist/
npm run preview # serve the production build at http://127.0.0.1:4173
```

Navigation uses URL hashes, so the static build can be served without route rewrites. Fonts and icons are bundled locally. The layout supports narrow screens, keyboard navigation, visible focus, and reduced motion.

## Sample data and external tools

[`src/data.ts`](src/data.ts) contains the typed sample snapshot. Component chart versions and configuration keys reflect [`platform/versions.yaml`](../platform/versions.yaml) and [`platform/defaults.yaml`](../platform/defaults.yaml) when the mockup was created; they are not synchronized automatically. AK3S release numbers and cluster health are fictional. Chart versions are explicitly labelled because they may differ from application versions. The components table lists the six AK3S-managed Helm add-ons; K3s-bundled components are represented by the cluster summary.

The tool cards use the loopback URLs from the [operations guide](../docs/operations.md#access):

| Tool            | URL                                  |
| --------------- | ------------------------------------ |
| Headlamp        | `http://127.0.0.1:8080`              |
| VictoriaMetrics | `http://127.0.0.1:8428/vmui/`        |
| VictoriaLogs    | `http://127.0.0.1:9428/select/vmui/` |

These links need the corresponding port-forward or SSH tunnel on the operator's machine. The connection guide below the cards shows the commands. Headlamp also requires a Kubernetes login token. For a real installation, supply that installation's private URLs instead of making the services public.

## Connecting a backend later

[`src/types.ts`](src/types.ts) defines the display contract. Replace the sample snapshot passed by `App` with an authenticated server response and handle loading, stale data, and errors. Collect node readiness, component health, and installed versions on the server; the browser should not receive kubeconfigs, tokens, or credentials. Redact sensitive configuration before it reaches the browser or clipboard.

Keep workload management in Headlamp, metrics in VictoriaMetrics, and log search in VictoriaLogs. Any future configuration or upgrade action must use the CLI's existing validation and ownership, identity, token, checksum, and version checks.
