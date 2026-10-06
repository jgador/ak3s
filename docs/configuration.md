# Configuration

Put only your overrides in `/etc/ak3s/values.yaml`, or select another file with `--config PATH`. AK3S embeds defaults and component versions and never rewrites your YAML.

```yaml
acme_email: operator@example.com
kubernetes_api_endpoint: api.example.com
metrics_retention: 14d
logs_storage: 20Gi
logs_max_disk: 16GiB
# Optional shared HTTPS access to all four UIs:
dashboard_hostname: ak3s.example.com
```

## Essential settings

| Setting | Default / purpose |
| --- | --- |
| `cluster_name` | `ak3s` |
| `node.name` | Empty uses the host's lowercase hostname during installation |
| `acme_email` | Your certificate account contact email; AK3S requires it for both staging and production issuers |
| `kubernetes_api_endpoint` | `127.0.0.1`; Kubernetes API hostname or IPv4 address, without a scheme or port; AK3S uses HTTPS port `6443` |
| `dashboard_hostname` | Empty keeps the shared UI ingress disabled; otherwise serves the dashboard at `/`, metrics at `/metrics`, logs at `/logs`, and Headlamp at `/headlamp` |
| `dashboard_shared_paths` | `false`; enables the tool paths through a local dashboard port-forward; a nonempty `dashboard_hostname` enables them automatically |
| `dashboard_auth_secret` | `dashboard-auth`; Secret in namespace `ak3s` with `username` and `password` keys for shared HTTPS access |
| `headlamp_hostname` | Optional standalone Headlamp hostname; leave empty when using shared dashboard paths |
| `dashboard_image` | Empty selects this AK3S release's dashboard image; overrides need an explicit tag or SHA-256 digest |
| `acme_environment` | `staging`; select `production` for trusted UI certificates |
| `metrics_retention`, `logs_retention` | `7d` |
| `metrics_storage`, `logs_storage` | `10Gi` each on local disk |
| `logs_max_disk` | `8GiB` log retention size cap |
| `helm_values` | Advanced overrides for bundled charts |

ACME means Automatic Certificate Management Environment, the protocol Let's
Encrypt uses to issue and renew HTTPS certificates. Use an email address you
control for `acme_email`. The `acme_environment` setting selects the certificate
service: `staging` for testing, or `production` for live sites with trusted
certificates.

`kubernetes_api_endpoint` supplies the hostname or IP portion of the Kubernetes
API URL. AK3S includes it in the API certificate and uses it when nodes join;
it does not rewrite an existing kubeconfig's `clusters[].cluster.server` field.

```bash
ak3s config --defaults
ak3s config --config ./values.yaml
sudo ak3s apply --config ./values.yaml --dry-run
sudo ak3s apply --config ./values.yaml
```

YAML mappings (key-value groups) merge; lists replace defaults. Omit keys to inherit defaults. Unknown keys, nulls, and YAML aliases are rejected. Overrides under `helm_values.<release>` take precedence over the corresponding AK3S settings; see [values.yaml](../examples/values.yaml).

Follow [shared hostname access](operations.md#shared-hostname) to create the login
Secret and configure DNS before enabling `dashboard_hostname`. The dashboard,
metrics, and logs share a browser password prompt. Headlamp retains its Kubernetes
login. Keep the generated Headlamp base URL `/headlamp` and Service names when
using this mode; the dashboard proxy uses those routes and Services.

For [local WSL testing](wsl-testing.md#test-shared-ui-paths), set
`dashboard_shared_paths: true` and leave `dashboard_hostname` empty. One dashboard
port-forward then serves all four UIs without public DNS, a dashboard login Secret,
or browser certificates. Headlamp still requires its Kubernetes login token.

For a new cluster, leaving `node.name` unset is usually sufficient. If you set it explicitly, use a unique, stable name such as `k3s-server-01`. For an existing cluster, preserve its installed node name; AK3S refuses implicit identity changes.

`ak3s render --output DIR` exports configuration and manifests that may contain secrets. Keep those files private.

## Control-plane redundancy

For a new cluster using etcd, a distributed datastore, use `node.datastore: etcd` and an odd `node.server_count` of at least `3`. Run the same AK3S release locally on each server. Joining servers need a reachable `kubernetes_api_endpoint`, `node.join: true`, and `node.token_file` pointing to a private copy of the first server's `/var/lib/rancher/k3s/server/token` (mode `0600`).

Use `platform: false` on additional servers so one server manages shared add-ons. Configure the [private network and Kubernetes API endpoint](vps.md#redundant-control-plane) first. AK3S refuses implicit datastore, node identity, and token changes.
