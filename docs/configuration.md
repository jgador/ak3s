# Configuration

Put only your overrides in `/etc/ak3s/values.yaml`, or select another file with `--config PATH`. AK3S embeds defaults and component versions and never rewrites your YAML.

```yaml
acme_email: operator@example.com
api_endpoint: api.example.com
metrics_retention: 14d
logs_storage: 20Gi
logs_max_disk: 16GiB
# Optional HTTPS access to Headlamp:
headlamp_hostname: dashboard.example.com
```

## Essential settings

| Setting | Default / purpose |
| --- | --- |
| `cluster_name` | `ak3s` |
| `node.name` | Empty uses the host's lowercase hostname during installation |
| `acme_email` | Required for certificate issuers |
| `api_endpoint` | `127.0.0.1`; use a reachable IPv4 or DNS name without a scheme or port |
| `headlamp_hostname` | Empty keeps Headlamp private |
| `dashboard_image` | Empty selects this AK3S release's dashboard image; overrides need an explicit tag or SHA-256 digest |
| `acme_environment` | `staging`; select `production` for trusted Headlamp certificates |
| `metrics_retention`, `logs_retention` | `7d` |
| `metrics_storage`, `logs_storage` | `10Gi` each on local disk |
| `logs_max_disk` | `8GiB` log retention size cap |
| `helm_values` | Advanced overrides for bundled charts |

```bash
ak3s config --defaults
ak3s config --config ./values.yaml
sudo ak3s apply --config ./values.yaml --dry-run
sudo ak3s apply --config ./values.yaml
```

YAML mappings (key-value groups) merge; lists replace defaults. Omit keys to inherit defaults. Unknown keys, nulls, and YAML aliases are rejected. Overrides under `helm_values.<release>` take precedence over the corresponding AK3S settings; see [values.yaml](../examples/values.yaml).

For a new cluster, leaving `node.name` unset is usually sufficient. If you set it explicitly, use a unique, stable name such as `k3s-server-01`. For an existing cluster, preserve its installed node name; AK3S refuses implicit identity changes.

`ak3s render --output DIR` exports configuration and manifests that may contain secrets. Keep those files private.

## Control-plane redundancy

For a new cluster using etcd, a distributed datastore, use `node.datastore: etcd` and an odd `node.server_count` of at least `3`. Run the same AK3S release locally on each server. Joining servers need a reachable `api_endpoint`, `node.join: true`, and `node.token_file` pointing to a private copy of the first server's `/var/lib/rancher/k3s/server/token` (mode `0600`).

Use `platform: false` on additional servers so one server manages shared add-ons. Configure the [private network and API endpoint](vps.md#redundant-control-plane) first. AK3S refuses implicit datastore, node identity, and token changes.
