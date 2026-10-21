# Optional applications

Install optional workloads with standard Kubernetes manifests or Helm, independently
of the AK3S platform. Application deployment is an operator task on an existing
cluster.

## PostgreSQL

The repository includes a standalone PostgreSQL Kustomize base for the shared
`psql-server`, private Services, and a `20Gi` PVC using K3s's `local-path` storage
class on the node's existing disk. The request is not a quota or reserved space;
database files can grow beyond it while the filesystem has free space.
Local-path does not support PVC expansion. Follow the
[manual PostgreSQL deployment guide](../applications/postgres/README.md) for
Hetzner Cloud or another VPS provider.

This deployment creates only PostgreSQL's built-in maintenance database
`postgres`. Each application repository owns the creation of its
`psqldb-<application>` database, application roles and credentials, migrations,
and database recovery instructions. Application database provisioning is separate
from installing or updating this server.

For direct external clients, the optional [public PostgreSQL overlay](../applications/postgres/public/README.md)
adds K3s ServiceLB on TCP `5432`, a dedicated certificate, mandatory TLS and SCRAM
authentication, and certificate reloads. DNS and firewall rules restricted to
client addresses are operator tasks. The private base remains the default, and
switching access keeps the existing database and storage.

The guide covers creating the namespace and password Secret from WSL, deploying
all manifests with `kubectl apply -k`, and checking readiness and login. It assumes
your local kubeconfig already has access to the cluster.

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

`make test-postgres` renders the private and public deployments and runs the pinned
PostgreSQL image in a disposable local Docker container. It checks verified TLS,
rejected plaintext and invalid credentials, certificate rotation without a server
restart, clean shutdown, and data persistence after another startup. It uses
fictional credentials and temporary files under `.tmp/`, with no cluster access.

Before enabling public access, also validate the overlay on a disposable K3s
cluster: check Secret volume permissions, certificate projection and reloads,
ServiceLB connectivity, and switching back to private access. Repeat DNS,
certificate issuance, and effective firewall allowlist checks on the intended
VPS as an operator; local container tests do not validate public networking.

Use the broader [runtime testing guide](runtime-testing.md) when validating the
AK3S platform itself.
