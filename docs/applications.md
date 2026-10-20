# Optional applications

Install optional workloads with standard Kubernetes manifests or Helm, independently
of the AK3S platform. Application deployment is an operator task on an existing
cluster.

## PostgreSQL

The repository includes a standalone PostgreSQL Kustomize base for the shared
`psql-server`, private Services, and a `20Gi` PVC using K3s's `local-path` storage
class on the node's existing disk. The request is not a quota or reserved space;
database files can grow beyond it while the filesystem has free space. No CSI
driver or separate cloud volume is required. Local-path does not support PVC
expansion. Follow the complete
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
and restoration, updates, disk monitoring and future growth, retained data, and
troubleshooting.
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
replacement and repeated applies, cluster shutdown and restart, and restoration
from an off-server backup. Check disk monitoring and the volume's node affinity.
These local-path checks do not validate Kubernetes PVC expansion or disk migration.
Rendered manifests and API validation do not prove database recovery.

Use the broader [runtime testing guide](runtime-testing.md) when validating the
AK3S platform itself.
