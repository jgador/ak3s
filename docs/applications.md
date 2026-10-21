# Optional applications

Install optional workloads with standard Kubernetes manifests or Helm, independently
of the AK3S platform. Application deployment is an operator task on an existing
cluster.

## PostgreSQL

The repository includes PostgreSQL manifests for the shared `psql-server`,
private Services, public TLS access, and a `20Gi` PVC using K3s's `local-path` storage
class on the node's existing disk. The request is not a quota or reserved space;
database files can grow beyond it while the filesystem has free space.
Local-path does not support PVC expansion. Follow the
[PostgreSQL update guide](../applications/postgres/README.md) for
Hetzner Cloud or another VPS provider.

This deployment creates only PostgreSQL's built-in maintenance database
`postgres`. Each application repository owns the creation of its
`psqldb-<application>` database, application roles and credentials, migrations,
and database recovery instructions. Application database provisioning is separate
from installing or updating this server.

The update retains the existing database identity, image, password Secret, and
PVC. It adds K3s ServiceLB on TCP `5432`, a dedicated certificate, mandatory TLS
and SCRAM authentication, and certificate reloads. The guide uses a placeholder
for DNS and a private local copy for operator settings. DNS and firewall rules
restricted to client addresses are operator tasks.

Keep `applications/postgres` on your workstation. `kubectl` reads the local
manifests and sends resources to the VPS cluster; no manifest transfer is needed.
PostgreSQL and its persistent data run on the VPS.

Operator deployment examples use `--kubeconfig "$HOME/.kube/ak3s-hetzner.yaml"`
to select production explicitly. Agents must follow the production access rules
in [AGENTS.md](../AGENTS.md); documentation and test requests do not authorize
production access. Use a separate disposable-cluster kubeconfig for testing.

## Runtime validation

Validate PostgreSQL on a disposable cluster. Test authenticated queries,
persistence after pod replacement and repeated applies, cluster shutdown and
restart, and restoration from an off-server backup. Check disk monitoring and
the volume's node affinity.
These local-path checks do not validate Kubernetes PVC expansion or disk migration.
Rendered manifests and API validation do not prove database recovery.

`make test-postgres` renders the manifests and runs the pinned PostgreSQL image
in a disposable local Docker container. It checks the TLS update with existing
data, roles, and passwords, verified TLS, rejected plaintext and invalid credentials, certificate
rotation without a server restart, clean shutdown, another startup, and rollback
to the previous launch configuration. It uses fictional credentials and temporary
files under `.tmp/`, with no cluster access.

Before enabling public access, also validate the manifests on a disposable K3s
cluster: check Secret volume permissions, certificate projection and reloads,
ServiceLB connectivity, and switching back to private access. Repeat DNS,
certificate issuance, and effective firewall allowlist checks on the intended
VPS as an operator; local container tests do not validate public networking.

Use the broader [runtime testing guide](runtime-testing.md) when validating the
AK3S platform itself.
