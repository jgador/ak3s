# Optional applications

Install optional workloads with standard Kubernetes manifests or Helm, independently
of the AK3S platform. Application deployment is an operator task on an existing
cluster.

## PostgreSQL

The repository includes a standalone PostgreSQL Kustomize base with persistent
storage and private Services. Follow the complete
[manual PostgreSQL deployment guide](../applications/postgres/README.md) for
Hetzner Cloud or another VPS provider.

The guide covers copying the directory to a running server, configuration,
namespace and credential creation, preview and API validation, deployment,
application connections, SSH access, backups and restoration, updates, retained
data, and troubleshooting. It uses direct `k3s kubectl` commands.

The detailed README is included when you copy `applications/postgres`; the server
needs only that directory and its existing K3s installation.

## Runtime validation

Follow the PostgreSQL guide's
[runtime validation steps](../applications/postgres/README.md#runtime-validation)
on a disposable cluster. Test authenticated queries, persistence after pod
replacement and repeated applies, and restoration from an off-server backup.
Rendered manifests and API validation do not prove database recovery.

Use the broader [runtime testing guide](runtime-testing.md) when validating the
AK3S platform itself.
