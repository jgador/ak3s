# Optional applications

Install optional workloads with standard Kubernetes manifests or Helm, independently
of the AK3S platform. Application deployment is an operator task on an existing
cluster.

## PostgreSQL

The repository includes a standalone PostgreSQL Kustomize base for the shared
`psql-server`, private Services, and a `20Gi` PVC using an expandable CSI storage
class. The default class is `hcloud-volumes`; install the Hetzner CSI driver
separately, or select a suitable class for your provider. K3s's default
`local-path` class does not support PVC expansion. Follow the complete
[manual PostgreSQL deployment guide](../applications/postgres/README.md) for
Hetzner Cloud or another VPS provider.

This deployment creates only PostgreSQL's built-in maintenance database
`postgres`. Each application repository owns the creation of its
`psqldb-<application>` database, application roles and credentials, migrations,
and database recovery instructions. Application database provisioning is separate
from installing or updating this server.

The guide covers local kubeconfig setup, API access through SSH or a direct
connection, configuration, namespace and credential creation, preview and API
validation, deployment, application connections, local port-forwarding, backups
and restoration, updates, volume expansion, retained data, and troubleshooting.
All command examples use Bash, including on WSL, and Kubernetes commands use
local `kubectl`.

Keep `applications/postgres` on your workstation. `kubectl` reads the local
manifests and sends resources to the VPS cluster; no manifest transfer is needed.
PostgreSQL and its persistent data run on the VPS.

Operator deployment examples use `--kubeconfig "$HOME/.kube/ak3s-hetzner.yaml"`
to select production explicitly. Agents must follow the production access rules
in [AGENTS.md](../AGENTS.md); documentation and test requests do not authorize
production access. Use a separate disposable-cluster kubeconfig for testing.

## Runtime validation

Follow the PostgreSQL guide's
[runtime validation steps](../applications/postgres/README.md#runtime-validation)
on a disposable cluster. Test authenticated queries, persistence after pod
replacement and repeated applies, CSI volume and filesystem expansion, and
restoration from an off-server backup. Expansion requires a compatible CSI driver;
WSL persistence checks with `local-path` do not validate expansion.
Rendered manifests and API validation do not prove database recovery.

Use the broader [runtime testing guide](runtime-testing.md) when validating the
AK3S platform itself.
