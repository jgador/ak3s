# Operations

## Management access

Select the AK3S kubeconfig explicitly:

```bash
export KUBECONFIG="$PWD/.ak3s/kubeconfig"
kubectl -n headlamp create token headlamp-viewer --duration=1h
kubectl -n headlamp port-forward service/headlamp 8080:80
```

Open `http://127.0.0.1:8080` and enter that token. The viewer account can inspect workloads and usage but cannot modify them or read Secrets. The administrator kubeconfig can create RBAC for individual users when write access is required. Tokens should stay out of files, terminal transcripts, and Git. The requested one-hour lifetime may be shortened by API server policy.

For public Headlamp access, set `headlamp_hostname` in `cluster.yaml`, create the public DNS A record, and rerun `make platform`. Set `acme_environment: production` after validating staging issuance. Login still requires a Kubernetes token. Remove the hostname and rerun platform to remove the ingress.

Access the metrics and logs UIs in separate terminals:

```bash
kubectl -n observability port-forward service/victoria-metrics 8428:8428
# http://127.0.0.1:8428/vmui/
kubectl -n observability port-forward service/victoria-logs 9428:9428
# http://127.0.0.1:9428/select/vmui/
```

Metrics query examples: `up`, `container_memory_working_set_bytes`, and `rate(container_cpu_usage_seconds_total[5m])`. A LogsQL query such as `kubernetes.pod_namespace:demo` selects application container logs. There is no separate Grafana installation.

To expose application metrics to built-in scraping, annotate its Service:

```yaml
metadata:
  annotations:
    prometheus.io/scrape: "true"
    prometheus.io/port: "8080"
    prometheus.io/path: /metrics
    prometheus.io/scheme: http
```

The annotated port must match the metrics container port declared in the pod. Discovery filters to that port, including declared pod ports not present on the Service. Custom authentication or TLS for application metrics requires explicit scrape configuration in `platform/values/victoria-metrics.yaml`. Annotations on pods are not used by this baseline.

## Verification and troubleshooting

```bash
kubectl get nodes
kubectl get pods -A
kubectl top nodes
kubectl -n ingress-nginx get service nginx-ingress-controller
kubectl get clusterissuer
kubectl -n demo describe certificate hello-tls
kubectl -n demo get order,challenge
kubectl -n observability logs statefulset/victoria-metrics --tail=50
kubectl -n observability logs daemonset/victoria-logs-collector --tail=50
```

A successful fresh install has no Traefik workload or HelmChart, a default `nginx` IngressClass, ready platform pods, and bound metrics/log PVCs. `up` should be 1 for the built-in metrics jobs. Test application routing before certificate issuance, then confirm the Certificate Ready condition and the certificate served for its hostname.

Pending ServiceLB pods usually mean host ports 80/443 are occupied. Pending PVCs usually mean no usable storage class or disk. Certificate challenges require public DNS, inbound port 80, and successful reachability from both Let's Encrypt and cert-manager's self-check. Staging certificates are untrusted by design. Logs are collected from container stdout/stderr under `/var/log/pods`, not host journals. Metrics discovery uses Kubernetes endpoint resources; inspect VictoriaMetrics logs for TLS, permission, or scrape errors.

## Configuration, reconciliation, and upgrades

`platform/versions.yaml` pins K3s, Helm versions, and chart archive SHA-256 checksums. Archives are downloaded directly, verified, and cached under `.ak3s/charts`; cached archives are reverified before use. The installer is fetched from the matching K3s tag, verified against its committed SHA-256, and uses K3s release checksum verification for the binary. Render chart changes with `make render`, inspect the results in `.ak3s/rendered`, and run `make test` and `tests/smoke.sh` before applying. Chart values are fully controlled by this repository: `helm upgrade --reset-values` removes old release overrides, so make persistent customization here.

Host configuration changes require `make bootstrap`. Add agents to `inventory.yaml` and rerun bootstrap; the existing servers retain their configuration and version. Server configuration changes and upgrades restart affected services one at a time. Schedule maintenance, check quorum, and drain workload nodes deliberately before an upgrade when disruption is unacceptable. Bootstrap is not an unattended rolling-upgrade controller. Do not skip Kubernetes minor versions. Update the K3s installer checksum whenever its release pin changes.

Platform changes require `make platform`. Each Helm release uses `--atomic --wait`, with a ten-minute timeout. This rolls back a failing release; previously completed releases and applied resources remain. It is not a transaction for the entire platform. Fix the reported issue and rerun. Helm installs cert-manager CRDs but does not upgrade existing CRDs automatically; before changing cert-manager versions, apply the matching upstream CRDs as instructed by [cert-manager's upgrade documentation](https://cert-manager.io/docs/installation/upgrade/), then upgrade the chart. Back up certificate resources first.

Keep `.ak3s/bootstrap.yaml`, `.ak3s/kubeconfig`, inventory, and cluster settings in an encrypted, access-controlled backup. The state directory and kubeconfig are mode 0700/0600 and ignored by Git. The bootstrap settings contain the cluster join token and datastore topology. Do not delete them to work around a topology guard. Retrieve the existing server token securely if local bootstrap settings are lost; generating a different token cannot join an existing cluster safely.

Converting a single-server SQLite cluster to embedded etcd is a K3s datastore migration, not an inventory edit. Follow the upstream K3s procedure with a backup and maintenance window, update AK3S state only after the migration is confirmed, and keep the original first server first in inventory. AK3S blocks implicit conversion to prevent accidental split clusters. Removing nodes also requires deliberate drain, node deletion, and, for servers, etcd membership handling.

## Persistence and recovery

The default local-path provisioner stores data under `/var/lib/rancher/k3s/storage`. The two observability PVCs request 10 GiB each, but local-path does not enforce those sizes or reserve capacity. Logs retain seven days or at most 8 GiB; metrics retain seven days. Both ingestion services reserve at least 1 GiB free space. Tune retention and resource values to the real workload and monitor the node filesystem. Custom `storage_class` must exist before platform installation and support ReadWriteOnce PVCs.

K3s datastore backups protect Kubernetes objects, not application data. For SQLite, take a consistent offline backup of `/var/lib/rancher/k3s/server/db`, the server token, configuration, and secret-encryption material during maintenance. For embedded etcd, use `k3s etcd-snapshot save` and copy the snapshots and server token off the cluster. Follow [K3s backup and restore guidance](https://docs.k3s.io/datastore/backup-restore) for the chosen datastore and retain required secret-encryption configuration.

Back up application volumes independently. VictoriaMetrics supports [native snapshots and vmbackup](https://docs.victoriametrics.com/victoriametrics/vmbackup/); VictoriaLogs supports [backup and restore](https://docs.victoriametrics.com/victorialogs/#backup-and-restore). A simpler maintenance backup can stop ingestion and the stateful pods, then copy local data consistently off-node. Node-local volume data cannot recover from loss of the node disk without an external backup. Practice restore on disposable hosts; a successful snapshot command alone does not establish recoverability.

Bootstrap does not remove nodes, uninstall K3s, or destroy infrastructure. For deliberate teardown, first back up data, remove applications and platform releases as needed, run the K3s uninstall script on the intended hosts, and finally delete the intended VMs in the Hetzner Cloud Console. Deleting a VM destroys its local application, metrics, and log data. Avoid deleting observability PVCs during upgrades. No backup operator or storage replication system is installed automatically.
