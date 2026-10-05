# Operations

Run AK3S on the control-plane VM. Commands use its local K3s kubeconfig at `/etc/rancher/k3s/k3s.yaml`.

## Access

Headlamp provides a read-only viewer login:

```bash
sudo k3s kubectl -n headlamp create token headlamp-viewer --duration=1h
sudo k3s kubectl -n headlamp port-forward service/headlamp 8080:80
```

Open `http://127.0.0.1:8080` and enter the token. When connecting from a workstation, use an SSH tunnel. Keep tokens private. Set `headlamp_hostname` for optional HTTPS ingress; Kubernetes authentication still applies.

For metrics and logs, run the corresponding port-forward:

```bash
sudo k3s kubectl -n observability port-forward service/victoria-metrics 8428:8428
# http://127.0.0.1:8428/vmui/
sudo k3s kubectl -n observability port-forward service/victoria-logs 9428:9428
# http://127.0.0.1:9428/select/vmui/
```

VictoriaMetrics scrapes infrastructure and Services with `prometheus.io/scrape: "true"`, `prometheus.io/port`, and `prometheus.io/path` annotations. VictoriaLogs collects container stdout/stderr. Keep both services private.

## Apply and upgrade

Use `sudo ak3s apply --dry-run`, then `sudo ak3s apply` after changing your YAML.

To upgrade, back up first, install a specific newer [AK3S release](../README.md#install), then run:

```bash
sudo ak3s upgrade --dry-run
sudo ak3s upgrade
sudo ak3s status
```

Upgrade applies the installed CLI's pinned versions; it does not update the CLI itself. Downgrades and skipped Kubernetes minor versions are refused. Upgrade redundant control-plane servers one at a time and check readiness and quorum after each.

Dry-run validates and previews changes; it cannot prove runtime readiness or certificate issuance. Failed Helm releases roll back individually, so fix failures and rerun.

## Backups

Back up these off-host:

- K3s datastore and server token under `/var/lib/rancher/k3s/server/`.
- Application volumes, normally under `/var/lib/rancher/k3s/storage/`.
- Overrides, K3s configuration, `/etc/rancher/k3s/ak3s-managed`, and `/var/lib/ak3s/state.json`.

Stop K3s for a consistent SQLite backup. For etcd, use `sudo k3s etcd-snapshot save`; snapshots exclude application volumes. Test the pinned K3s version's restore procedure on a separate VM.

## Troubleshooting

```bash
sudo ak3s status
sudo journalctl -u k3s -n 100
sudo k3s kubectl get nodes
sudo k3s kubectl get pods -A
sudo k3s kubectl get pvc -A
sudo k3s kubectl top nodes
```

Pending ServiceLB pods: check occupied ports 80/443. Failed certificates: check public DNS and port 80. Pending volumes: check storage class and disk space. Use kubectl logs/describe for failing workloads.

## Older installations

Before migrating an Ansible-managed AK3S server, back up and preserve its node identity, datastore, and token. Move settings into `/etc/ak3s/values.yaml` and review `install --dry-run`; use `upgrade` if K3s versions differ. Resolve custom systemd settings explicitly. AK3S refuses unmanaged K3s installations; do not remove ownership/state files to bypass checks.
