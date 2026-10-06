# AK3S dashboard

The React and TypeScript dashboard is installed with the AK3S platform as a
Deployment and private Service in namespace `ak3s`. A small Go server serves the
built frontend and a read-only API. Node.js is needed only to build or develop
the frontend.

- **Overview:** node readiness, versions, platform add-ons, and links to Headlamp, VictoriaMetrics, and VictoriaLogs.
- **Configuration:** sanitized effective YAML and operator overrides, with a copy control.
- **Upgrade:** last attempted AK3S release and manual upgrade steps.

Data refreshes every 30 seconds and on manual refresh. Connection failures show
an error and mark the last successful snapshot as stale. Configuration and
upgrade commands are displayed for copying; the dashboard never executes them.
Available AK3S releases are not checked automatically.

## Access

`ak3s install`, `apply`, and `upgrade` deploy the dashboard when `platform: true`.
For an existing cluster, install the updated CLI and run `sudo ak3s apply`.
Then start all four UI port-forwards from the checkout:

```bash
make port-forward
```

Open `http://127.0.0.1:5173`. Use `make port-forward-status` and
`make port-forward-stop` to manage the background forwards. If a Vite development
server already occupies port 5173, stop it first. See the
[access guide](../docs/operations.md#access) for Windows, WSL, and remote hosts.

Without a checkout, forward the dashboard Service directly:

```bash
sudo k3s kubectl --kubeconfig /etc/rancher/k3s/k3s.yaml -n ak3s port-forward --address 127.0.0.1 service/dashboard 5173:80
```

The server accepts local, same-origin requests. Kubernetes port-forward access
provides the access boundary; there is no separate dashboard login. A
NetworkPolicy blocks pod-network ingress. Keep the Service private. For a remote
host, also use an SSH tunnel from your workstation.

## Build and deploy local source

Use Docker to build the frontend and Go server together. From the repository
root on the local K3s server:

```bash
make dashboard-image
# Transfer the local image into K3s's separate container runtime.
docker save ghcr.io/jgador/ak3s-dashboard:dev -o .tmp/dashboard-image.tar
sudo k3s ctr images import .tmp/dashboard-image.tar
rm .tmp/dashboard-image.tar
make build
sudo bin/ak3s apply --dry-run
sudo bin/ak3s apply
```

These commands use the `dev` image for a default source build. Import the image
on every node where the dashboard may run, or push it to a registry the cluster
can reach. For a custom image, set `dashboard_image` in your operator values to
an explicit tag or SHA-256 digest. Use a new tag for each build; if reusing the
local `dev` tag, import it again and run
`sudo k3s kubectl -n ak3s rollout restart deployment/dashboard` after applying.

Published release builds select an image by immutable digest. An empty
`dashboard_image` uses that release default. The release workflow publishes
Linux amd64 and arm64 images before creating the CLI release. The GHCR package
must have public visibility for anonymous pulls.

## Frontend development

Use Node.js 22.12+ (24 recommended), npm, and the local AK3S server. Build the CLI
with `make build` first, stop the dashboard port-forward if it occupies port 5173,
then:

```bash
cd dashboard
npm ci
sudo -v
npm run dev
```

The Vite development endpoint runs `ak3s dashboard-snapshot`, preferring
`bin/ak3s` and falling back to `/usr/local/bin/ak3s`. It uses non-interactive sudo
when needed to read the fixed local K3s kubeconfig, AK3S state, and operator
configuration. Renew sudo authorization in that terminal if it expires. Keep
these files private.

```bash
npm run check   # TypeScript checks
npm test        # development endpoint access and failure checks
npm run build   # type-check and build static files
npm run preview # serve the build and local endpoint on port 4173
```

Navigation uses URL hashes. Fonts and icons are bundled locally. The layout
supports narrow screens, keyboard navigation, visible focus, and reduced motion.

## Data and permissions

The deployed server authenticates directly to the in-cluster Kubernetes API
using its rotating service-account token and CA. Its role can list nodes,
Deployments, StatefulSets, and DaemonSets, and read the API version. It has no
Secret access or write permissions, and mounts no host files. It runs as a
non-root user with a read-only filesystem and no Linux capabilities.

Reconciliation places only display settings in a ConfigMap and restarts the
Deployment when they change. Cluster name, datastore, configuration, and the
last attempted AK3S release reflect that reconciliation; run `ak3s apply` after
editing the operator file. Node status and chart labels are collected live.
Missing components and incomplete rollouts need attention; workload readiness
comes from Kubernetes status, not application health probes.

The deployed API and the local CLI share the same display projection.
Contact email, token paths, and nonempty Helm overrides are redacted before
configuration reaches the ConfigMap or browser. Kubeconfig credentials,
Kubernetes Secrets, workload environment variables, and installation
fingerprints are never included. Copied YAML is a sanitized display, not a
configuration backup. The override count counts leaf settings explicitly
present in the operator file.

`/api/snapshot` accepts read-only requests without parameters, caches successful
collections for five seconds, and suppresses private errors. The readiness
probe checks live data collection; the liveness probe checks the HTTP server.
The static frontend alone does not provide live data.

The tool cards use separate loopback forwards:

| Tool | URL |
| --- | --- |
| Headlamp | `http://127.0.0.1:8080` |
| VictoriaMetrics | `http://127.0.0.1:8428/vmui/` |
| VictoriaLogs | `http://127.0.0.1:9428/select/vmui/` |

Headlamp also requires a Kubernetes login token. Keep workload management in
Headlamp, metrics in VictoriaMetrics, and log search in VictoriaLogs.
