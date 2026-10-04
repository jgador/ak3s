# Operations

## Management access

The administrator kubeconfig is `/etc/rancher/k3s/k3s.yaml`, with mode 0600. K3s provides kubectl as a subcommand; no separate kubectl installation is needed on a server.

```bash
sudo k3s kubectl -n headlamp create token headlamp-viewer --duration=1h
sudo k3s kubectl -n headlamp port-forward service/headlamp 8080:80
```

Open `http://127.0.0.1:8080` on the VM, or forward that loopback port through SSH from your workstation. Enter the token. The viewer can inspect workloads and resource usage but cannot write or read Secrets. Tokens should stay out of files, transcripts and Git. Give named users explicit RBAC for management, or integrate an existing OIDC provider separately.

Set `headlamp_hostname` in your overrides to opt into HTTPS ingress. Start with `acme_environment: staging`, validate issuance, then switch to `production` and `sudo ak3s apply`. Remove the hostname to remove the generated ingress. Authentication still requires a Kubernetes token.

```bash
sudo k3s kubectl -n observability port-forward service/victoria-metrics 8428:8428
# http://127.0.0.1:8428/vmui/
sudo k3s kubectl -n observability port-forward service/victoria-logs 9428:9428
# http://127.0.0.1:9428/select/vmui/
```

Metrics queries include `up`, `container_memory_working_set_bytes` and `rate(container_cpu_usage_seconds_total[5m])`. LogsQL `kubernetes.pod_namespace:demo` selects application logs. No Grafana is installed. To opt a Service into application metrics scraping:

```yaml
metadata:
  annotations:
    prometheus.io/scrape: "true"
    prometheus.io/port: "8080"
    prometheus.io/path: /metrics
    prometheus.io/scheme: http
```

The port must match the declared metrics container port. Discovery also includes declared pod ports not exposed by the Service. Pod annotations are not used. Custom TLS/authentication requires an explicit scrape override under `helm_values.victoria-metrics.server.scrape`.

## Reconciliation and upgrades

The CLI follows a fixed sequence: load/validate settings, inspect host and ownership, plan changes, download verified artifacts, render/validate charts, recheck the host under an exclusive lock, prepare prerequisites, checkpoint identity, write configuration, install the binary if needed, enable/start or restart K3s, wait for server readiness, then reconcile platform releases and shared resources.

K3s uses a Go-generated systemd unit based on upstream service settings, including delegated cgroups, `KillMode=process`, restart policy and resource limits. No upstream installer shell script is executed. AK3S ensures CA certificates, iptables and kmod, loads overlay/br_netfilter and persists forwarding settings. It does not disable swap or reconfigure your firewall.

Host configuration and the binary use atomic file replacement. The ownership marker is `/etc/rancher/k3s/ak3s-managed`; identity/checkpoint state is `/var/lib/ak3s/state.json`. Keep these with the host. A pending-restart checkpoint makes interrupted runs retry readiness rather than incorrectly treating written files as applied. The state records the last attempted release, not a guarantee of platform health. Never remove ownership/state to force a downgrade or topology conversion.

Each release pins K3s, Helm, chart versions and SHA-256 checksums for Linux amd64 and arm64. Downloads are verified before use. Tools and charts use private temporary directories, with no persistent chart cache to migrate. Checksums protect artifact integrity under the trust of the release source; they are not a separate publisher signature.

For an upgrade, back up first, install a specific newer **CLI** binary using the bootstrap, then run `sudo ak3s upgrade --dry-run` and `sudo ak3s upgrade`. No overrides are copied into the binary or overwritten by an upgrade. A K3s version change requires `upgrade`; a downgrade or skipped Kubernetes minor version is refused. Older numbered AK3S releases are also rejected once a newer release has been recorded, preventing accidental chart downgrades. Development builds are not ordered release versions. Review chart/image changes between AK3S releases too. `upgrade` applies that binary's pins; it does not fetch the latest AK3S release or modify itself.

Servers must be upgraded sequentially before agents. Check quorum and node readiness after every node. Drain workloads deliberately if maintenance disruption is unacceptable; AK3S is not an unattended rolling-upgrade controller. Helm uses `--reset-values --atomic --wait --timeout 10m`, so persistent customization belongs in your `values.yaml`, not manual Helm arguments. A failure rolls back the failing release; prior releases, K3s changes and applied resources remain. Fix the failure and rerun. Reconciliation reapplies resources and can create Helm revisions even when desired values are unchanged.

## Migrating an existing Ansible-managed AK3S node

1. Keep backups of the old `cluster.yaml`, inventory, `.ak3s/bootstrap.yaml`, server data, token and application volumes. Preserve the original node name and datastore topology.
2. Install the Go CLI binary. Create `/etc/ak3s/values.yaml` using your existing cluster settings plus `node.name`, `node.ip`, `node.external_ip` and `node.flannel_iface` from inventory. Use `node.datastore: etcd` and the original odd `node.server_count` for a multi-server cluster; set `node.join: true` on joining servers/agents.
3. The original first server can retain its existing inline token automatically. On a joining node, securely copy that same token into an owner-only `node.token_file`; the CLI checks that it matches the old inline token. New installations let K3s create the first-server token automatically. Never generate a replacement token for an existing cluster.
4. Review custom K3s service environment files and systemd/K3s drop-ins. Nonempty custom environment and drop-ins are blocked because they can override rendered settings. Migrate intentional settings explicitly before proceeding; do not delete them blindly.
5. Run `sudo ak3s install --dry-run`. The CLI accepts the previous AK3S marker, preserves identity, and replaces the old service unit with its managed unit on apply. It refuses unmanaged K3s installations. If the existing K3s pin differs, use `upgrade` after backup instead.
6. Apply on one node at a time and verify readiness, application routing and data. Once migrated, the old repository checkout and Python/Ansible virtual environment are no longer needed for operation. Keep historical backup material securely.

Old SSH inventories and `platform --kubeconfig` are not CLI inputs. Execute the binary on the intended node rather than selecting an arbitrary workstation context. Never copy another node's ownership or state files.

## Verification and troubleshooting

```bash
sudo ak3s status
sudo systemctl status k3s
sudo journalctl -u k3s -n 100
sudo k3s kubectl get nodes
sudo k3s kubectl get pods -A
sudo k3s kubectl top nodes
sudo k3s kubectl -n ingress-nginx get service nginx-ingress-controller
sudo k3s kubectl get clusterissuer
sudo k3s kubectl -n demo describe certificate hello-tls
sudo k3s kubectl -n demo get order,challenge
sudo k3s kubectl -n observability logs statefulset/victoria-metrics --tail=50
sudo k3s kubectl -n observability logs daemonset/victoria-logs-collector --tail=50
```

Use `k3s-agent` for agent service/journal commands. Failed subprocess output is suppressed by the CLI because Helm and Kubernetes validation errors can echo Secrets. Diagnose services through systemd/journald and workloads through kubectl. Review private `ak3s render --output DIR` files when needed.

A healthy install has no Traefik workload or HelmChart, a default `nginx` IngressClass, ready platform pods and bound metrics/log PVCs. The six built-in `up` metrics jobs should be 1. Test routing before certificate issuance, then confirm Certificate Ready and the served certificate.

Pending ServiceLB pods often indicate occupied 80/443 host ports. Pending PVCs may indicate missing storage classes or disk capacity. HTTP-01 needs public DNS, inbound port 80 and reachability from both Let's Encrypt and cert-manager; remove stale AAAA records. Logs collect container stdout/stderr under `/var/log/pods`, not OS journals. ClusterIP is not isolation from other pods; add network policies for tenant boundaries.

## Persistence, backup and recovery

SQLite data is under `/var/lib/rancher/k3s/server/db`. Stop K3s for a consistent SQLite backup and include `/var/lib/rancher/k3s/server/token` plus configuration. For embedded etcd, use `sudo k3s etcd-snapshot save` and copy snapshots and the server token off-host. Follow the pinned K3s release's restore procedure and test recovery on a separate VM.

Local-path volumes, typically under `/var/lib/rancher/k3s/storage`, are node-local. They need separate application-consistent backups; etcd snapshots do not contain volume data. Retention and requested PVC size are not replacements for disk monitoring or backup. Back up the user overrides, K3s configuration, AK3S marker/state, token files and any custom RBAC.

AK3S has no uninstall command or automatic restore/rollback of the whole platform. Do not use old uninstall scripts without reviewing their destructive behavior. Disabling platform management does not delete workloads or volumes.
