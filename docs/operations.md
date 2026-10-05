# Operations

Run AK3S on the control-plane VPS (or inside the WSL2 test distribution). Commands use its local K3s kubeconfig, the Kubernetes client configuration file, at `/etc/rancher/k3s/k3s.yaml`.

## Access

### Start all UIs for local testing

From a repository checkout inside the WSL distribution running your AK3S test
cluster, install the dashboard dependencies once, then start all four UIs:

```bash
npm --prefix dashboard ci
make port-forward
```

Use Bash 4+, Make, Node.js 22.12+ (24 recommended), npm, curl, and the standard
Linux tools `flock`, `nohup`, and `ss`. On Ubuntu, these tools come from
`util-linux`, `coreutils`, and `iproute2`. K3s must already be installed and the
three platform Services must be ready.

The command starts these listeners in the background, waits for startup, prints
their URLs, and returns to your shell. It does not tail logs. Run it as your normal
WSL user; it asks for sudo access if needed for the local K3s kubeconfig at
`/etc/rancher/k3s/k3s.yaml`. It does not select a cluster from your default kubectl
context. The dashboard runs as the user invoking the helper.

| UI | Windows browser URL |
| --- | --- |
| AK3S dashboard mockup | `http://127.0.0.1:5173` |
| Headlamp | `http://127.0.0.1:8080` |
| VictoriaMetrics | `http://127.0.0.1:8428/vmui/` |
| VictoriaLogs | `http://127.0.0.1:9428/select/vmui/` |

The AK3S dashboard is currently a local Vite server with sample data. The other
three listeners forward to Kubernetes Services. All four bind to `127.0.0.1`.
With WSL2 NAT networking and `localhostForwarding=true`, open these addresses
directly in Windows; no SSH tunnel is needed between Windows and its local WSL
distribution. See the [WSL guide](wsl-testing.md#4-run-the-shared-runtime-checks)
if Windows cannot reach them.

```bash
make port-forward-status  # show managed listeners; nonzero if one is down
make port-forward-stop    # stop managed listeners and remove their logs
make port-forward         # reuse running listeners and restart missing ones
```

Use the same checkout and Linux user for these commands. For only the three
Kubernetes tools, run `make port-forward DASHBOARD=0`; Node.js and npm are then
unnecessary. This leaves an already running dashboard alone. The direct Bash
equivalents are `bash scripts/port-forward.sh start`, `status`, and `stop`;
`start --no-dashboard` skips the mockup.

Private logs and process records are stored under the ignored
`.tmp/port-forward/` directory. To inspect a log without following it, use, for
example, `tail -n 50 .tmp/port-forward/headlamp.log`. Startup failures return a
nonzero status, retain logs, and stop listeners created by that call. Existing
managed listeners are preserved. Occupied ports produce an error; the helper
does not stop their owners. Stop any manual forwards before using it.

Forwarding stops when its selected pod exits or WSL shuts down. It does not
reconnect automatically; rerun `make port-forward` after the cluster recovers.
Keep the WSL test environment running while browsing. The status command checks
local processes and listening ports, not cluster health.

Headlamp still needs a viewer token. Generate it separately and keep it private:

```bash
sudo k3s kubectl --kubeconfig /etc/rancher/k3s/k3s.yaml -n headlamp create token headlamp-viewer --duration=1h
```

### Manual access and remote hosts

Headlamp provides a read-only viewer login:

```bash
sudo k3s kubectl -n headlamp create token headlamp-viewer --duration=1h
sudo k3s kubectl -n headlamp port-forward service/headlamp 8080:80
```

Open `http://127.0.0.1:8080` and enter the token. When connecting to a separate remote host, use an SSH tunnel in addition to the port-forwards on that host. Local Windows-to-WSL access uses localhost forwarding as described above. Keep tokens private. Set `headlamp_hostname` for optional HTTPS ingress; Kubernetes authentication still applies.

For metrics and logs, run the corresponding port-forward command:

```bash
sudo k3s kubectl -n observability port-forward service/victoria-metrics 8428:8428
# http://127.0.0.1:8428/vmui/
sudo k3s kubectl -n observability port-forward service/victoria-logs 9428:9428
# http://127.0.0.1:9428/select/vmui/
```

VictoriaMetrics scrapes metrics from infrastructure and Services with `prometheus.io/scrape: "true"`, `prometheus.io/port`, and `prometheus.io/path` annotations. VictoriaLogs collects container standard output and standard error (stdout/stderr). Keep both services private.

## Apply and upgrade

Use `sudo ak3s apply --dry-run`, then `sudo ak3s apply` after changing your YAML.

To upgrade, back up first, install a specific newer [AK3S release](../README.md#install), then run:

```bash
sudo ak3s upgrade --dry-run
sudo ak3s upgrade
sudo ak3s status
```

Upgrade applies the installed CLI's pinned versions; it does not update the CLI itself. Downgrades and skipped Kubernetes minor versions are refused. Upgrade redundant control-plane servers one at a time and check readiness and etcd quorum (a majority of servers available) after each.

Dry-run validates and previews changes; it cannot prove runtime readiness or certificate issuance. Failed Helm releases roll back individually, so fix failures and rerun.

## Backups

Store backups of these files and data outside the server:

- K3s datastore and server token under `/var/lib/rancher/k3s/server/`.
- Application volumes, normally under `/var/lib/rancher/k3s/storage/`.
- Overrides, K3s configuration, `/etc/rancher/k3s/ak3s-managed`, and `/var/lib/ak3s/state.json`.

Stop K3s for a consistent SQLite backup. For etcd, use `sudo k3s etcd-snapshot save`; snapshots exclude application volumes. Test the pinned K3s version's restore procedure on a separate disposable server. The [WSL2 guide](wsl-testing.md#back-up-and-restore-the-test-environment) includes a recovery test using an export of the whole distribution; use a separate VPS to validate your production backup method.

## Troubleshooting

```bash
sudo ak3s status
sudo journalctl -u k3s -n 100
sudo k3s kubectl get nodes
sudo k3s kubectl get pods -A
sudo k3s kubectl get pvc -A
sudo k3s kubectl top nodes
```

Pending ServiceLB pods: check occupied ports 80 and 443. Failed certificates: check public DNS and port 80. Pending volumes: check storage class and disk space. Use `kubectl logs` and `kubectl describe` for failing workloads.

## Older installations

Before migrating an Ansible-managed AK3S server, back up and preserve its node identity, datastore, and token. Move settings into `/etc/ak3s/values.yaml` and review `install --dry-run`; use `upgrade` if K3s versions differ. Resolve custom systemd settings explicitly. AK3S refuses unmanaged K3s installations; do not remove ownership or state files to bypass checks.
