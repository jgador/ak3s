# Configuration

Every AK3S binary contains its own defaults, K3s/Helm versions, chart versions, checksums, Helm values and shared RBAC. `/etc/ak3s/values.yaml` holds only operator overrides. Use `--config PATH` to select another file explicitly. A missing explicit path is an error; a missing default path means use embedded defaults. All nodes should run the same AK3S release.

```yaml
acme_email: operator@example.com
api_endpoint: api.example.com
metrics_retention: 14d
logs_storage: 20Gi
logs_max_disk: 16GiB
headlamp_hostname: dashboard.example.com
helm_values:
  nginx-ingress:
    controller:
      replicaCount: 2
  victoria-metrics:
    server:
      resources:
        limits:
          memory: 1Gi
```

This inherits every other default. No full configuration copy is needed. Merge order is embedded chart values, named AK3S settings (retention, storage, ingress), then `helm_values`. Advanced Helm values take precedence over named settings. A list replaces the whole default list; an empty mapping leaves inherited keys intact. Nulls and YAML aliases are deliberately rejected, so omit a key to inherit it. An empty `headlamp_hostname` disables the generated Headlamp ingress on the next reconciliation.

`ak3s config` shows merged AK3S settings, including sparse advanced overrides. `ak3s render --output DIR` additionally writes fully merged Helm values, rendered chart resources, K3s configuration and shared manifests. Files are private (0600), since advanced chart values/templates can contain secrets. `render` without `--output` prints rendered configuration/resources to stdout; treat that output as potentially sensitive. Dry-run never prints join tokens or rendered Secrets.

## Settings

Run `ak3s config --defaults` for the complete shipped defaults. Component pins cannot be replaced through YAML; select an AK3S release instead. Advanced chart overrides can alter images and security settings, so their compatibility is the operator's responsibility.

| Setting | Default / meaning |
| --- | --- |
| `cluster_name` | `ak3s`, also used as the metrics external label |
| `api_endpoint` | `127.0.0.1`; IPv4 or DNS name, without scheme/port |
| `tls_sans` | Additional IPv4/DNS API certificate names |
| `acme_email` | Empty; required when `platform: true` |
| `acme_environment` | `staging`; `production` selects the Headlamp issuer |
| `storage_class` | `local-path` |
| `metrics_retention`, `logs_retention` | `7d` |
| `metrics_storage`, `logs_storage` | `10Gi` |
| `logs_max_disk` | `8GiB`, separate from the PVC request |
| `headlamp_hostname` | Empty, private by default |
| `platform` | `true`; install/reconcile add-ons from this server |
| `node.role` | `server` or `agent` |
| `node.name` | Empty uses the host name; set it before first installation to override |
| `node.ip`, `node.external_ip` | Empty uses K3s detection; explicit values must be IPv4 |
| `node.flannel_iface` | Empty uses K3s detection |
| `node.datastore` | `sqlite` or `etcd` |
| `node.server_count` | `1` for SQLite; odd and at least `3` for etcd |
| `node.join` | `false`; true joins through `api_endpoint` |
| `node.token_file` | Absolute path to an owner-only file containing the join token |
| `helm_values` | Map from a pinned Helm release name to sparse chart overrides |

`platform: false` stops managing add-ons from that node; it does not uninstall existing releases. Designate one server to manage platform releases and set this false on other servers. AK3S serializes operations on one node, not concurrent Helm changes from different nodes.

## Additional nodes

Run AK3S locally on each VM, via your own SSH session if desired. Configure the private network and firewall first. `server_count` validates the intended topology; it is not a scheduler or proof of live quorum.

For a **new** three-server etcd cluster, the first server uses:

```yaml
acme_email: operator@example.com
api_endpoint: api.example.com
node:
  name: server-1
  ip: 10.0.0.11
  datastore: etcd
  server_count: 3
```

The API endpoint must initially reach that server and remain reachable by all nodes. After it is ready, securely transfer `/var/lib/rancher/k3s/server/token` to `/etc/ak3s/join-token` on joining nodes, with owner-only permissions (`chmod 600`). Do not put its contents on a command line, into chat, or in Git. Each additional server uses:

```yaml
api_endpoint: api.example.com
platform: false
node:
  name: server-2 # server-3 on the third VM
  ip: 10.0.0.12
  datastore: etcd
  server_count: 3
  join: true
  token_file: /etc/ak3s/join-token
```

An agent uses:

```yaml
api_endpoint: api.example.com
platform: false
node:
  role: agent
  name: worker-1
  ip: 10.0.0.20
  join: true
  token_file: /etc/ak3s/join-token
```

Run `sudo ak3s install --dry-run`, then `sudo ak3s install` on each node, one at a time. Confirm every Node is Ready from a server with `sudo k3s kubectl get nodes`. Agent installation checks service activity; server-side Node readiness is a separate verification. For API availability, supply your own TCP load balancer or managed virtual IP. AK3S does not create cloud load balancers.

A SQLite-to-etcd conversion, node rename, role change or token rotation is an explicit maintenance procedure outside ordinary reconciliation. AK3S refuses implicit identity/datastore changes and refuses a changed token file after ownership has been recorded.
