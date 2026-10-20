# Optional applications

Install optional workloads with standard Kubernetes manifests or Helm, independently
of the AK3S platform. Application deployment is an operator task on an existing
cluster.

## PostgreSQL

The repository includes a standalone PostgreSQL Kustomize base with persistent
storage and private Services. Follow the complete
[manual PostgreSQL deployment guide](../applications/postgres/README.md) for
Hetzner Cloud or another VPS provider.

The guide covers local kubeconfig setup, API access through SSH or a direct
connection, configuration, namespace and credential creation, preview and API
validation, deployment, application connections, local port-forwarding, backups
and restoration, updates, retained data, and troubleshooting. All command examples
use Bash, including on WSL, and Kubernetes commands use local `kubectl`.

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
replacement and repeated applies, and restoration from an off-server backup.
Rendered manifests and API validation do not prove database recovery.

Use the broader [runtime testing guide](runtime-testing.md) when validating the
AK3S platform itself.
