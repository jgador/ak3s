# Operations

Run AK3S on the control-plane VPS (or inside the WSL2 test distribution). Commands use its local K3s kubeconfig, the Kubernetes client configuration file, at `/etc/rancher/k3s/k3s.yaml`.

## Access

### Start all UIs for local testing

From a repository checkout inside the WSL distribution running your AK3S test
cluster, start all four UIs:

```bash
make port-forward
```

Use Bash 4+, Make, curl, and the standard
Linux tools `flock`, `nohup`, and `ss`. On Ubuntu, these tools come from
`util-linux`, `coreutils`, and `iproute2`. K3s must already be installed and the
four platform Services must be ready. The AK3S dashboard is installed by
`ak3s install`, `apply`, and `upgrade` when `platform: true`.

The command starts these listeners in the background, waits for startup, prints
their URLs, and returns to your shell. It does not tail logs. Run it as your normal
WSL user; it asks for sudo access if needed for the local K3s kubeconfig at
`/etc/rancher/k3s/k3s.yaml`. It does not select a cluster from your default kubectl
context. Node.js and npm are only needed for frontend development.

| UI | Windows browser URL |
| --- | --- |
| AK3S dashboard | `http://127.0.0.1:5173` |
| Headlamp | `http://127.0.0.1:8080` |
| VictoriaMetrics | `http://127.0.0.1:8428/vmui/` |
| VictoriaLogs | `http://127.0.0.1:9428/select/vmui/` |

All four listeners forward to Kubernetes Services and bind to `127.0.0.1`.
The AK3S dashboard runs in namespace `ak3s` with a read-only service account.
It serves the built frontend and live cluster data from one container.
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
Kubernetes tools, run `make port-forward DASHBOARD=0`. This leaves an already
running dashboard forward alone. The direct Bash
equivalents are `bash scripts/port-forward.sh start`, `status`, and `stop`;
`start --no-dashboard` skips the dashboard.

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

For the AK3S dashboard:

```bash
sudo k3s kubectl --kubeconfig /etc/rancher/k3s/k3s.yaml -n ak3s port-forward --address 127.0.0.1 service/dashboard 5173:80
```

Open `http://127.0.0.1:5173`. For a remote server, also run
`ssh -N -L 5173:127.0.0.1:5173 USER@VPS_ADDRESS` from your workstation.
The dashboard requires Kubernetes port-forward access; it has no separate login.
Its NetworkPolicy blocks pod-network ingress, and the server checks loopback peers,
local Host headers, and browser origins. Keep it private.

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

Upgrade applies the installed CLI's pinned versions; it does not update the CLI itself. Downgrades and skipped Kubernetes minor versions are refused. Upgrade servers first, one at a time, and check readiness and etcd quorum (a majority of servers available) after each; upgrade agents afterwards. The official installer replaces the binary and updates the service using the same role and configuration file. It does not drain or cordon the node. For workloads sensitive to brief API outages, plan drain/cordon and uncordon steps as described in [upstream manual upgrades](https://docs.k3s.io/upgrades/manual).

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

Follow the [migration guide](migration.md) for the previous Go-based release or older Ansible-managed nodes. Migration replaces the service in place and preserves cluster data, identity, and tokens. AK3S refuses unmanaged installations and unsupported custom settings; do not remove ownership or state files to bypass checks.

## Removal

Uninstalling stops local workloads and deletes the local K3s datastore, tokens,
configuration, and local-path persistent volumes. Back up anything you need
first. External datastores and external volumes are outside this operation.

```bash
sudo ak3s uninstall --dry-run
sudo ak3s uninstall --yes
```

AK3S checks ownership and the fingerprints of the upstream-generated scripts,
then runs `/usr/local/bin/k3s-uninstall.sh` for a server or
`/usr/local/bin/k3s-agent-uninstall.sh` for an agent. These call the generated
`k3s-killall.sh` helper. See [upstream removal guidance](https://docs.k3s.io/installation/uninstall).
AK3S does not implement its own process, mount, firewall, or K3s data deletion.
Use this command for AK3S nodes so its lock, recovery state, and host settings
are also handled.

The AK3S CLI, `/etc/ak3s/values.yaml`, operator token files outside K3s's data
and configuration directories, and backups are retained. Removing AK3S's sysctl
file does not reset current kernel settings; no previous baseline is assumed.
An already removed installation is a successful repeated run.

If removal fails or leaves local data, AK3S retains its state and private copies
of the unmodified generated scripts under `/var/lib/ak3s/`. Resolve the reported
problem and rerun `uninstall --yes`; it can recover after the upstream script
deletes itself. Apply and upgrade are blocked while removal is pending. Older
installations must complete [migration](migration.md) before this removal command
can verify their generated scripts. Do not run an installer just to obtain
removal scripts during a cleanup operation.

For a node that will rejoin another running cluster, delete its Node from a
remaining server so the node-password secret is removed, following
[upstream node registration](https://docs.k3s.io/architecture#how-agent-node-registration-works).
AK3S removal acts on the local host and does not make that cluster-wide change.
