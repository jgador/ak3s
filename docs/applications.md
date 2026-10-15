# Optional applications

AK3S keeps optional workloads separate from the cluster and monitoring platform.
The first supported recipe is standalone PostgreSQL, with one StatefulSet replica,
private ClusterIP Services, and a persistent volume claim (PVC). No optional
application is installed by `ak3s install`, `apply`, or `upgrade`.

Application commands run on an existing AK3S-managed server. They use
`/etc/rancher/k3s/k3s.yaml` explicitly and do not install, restart, or upgrade K3s
or platform components. They also work on a server installed with `platform: false`.

## Configure PostgreSQL

List the available recipe and its pinned versions:

```bash
ak3s apps list
```

Save an application named `app-db` in `/etc/ak3s/apps.yaml`:

```bash
sudo ak3s apps add postgres --name app-db --existing-secret app-db-auth
```

This command saves configuration with mode `0600`; it does not access the cluster
or install PostgreSQL. If the application name already exists, edit its
configuration directly. Existing YAML comments are not preserved when adding an
application. Select another application file with `--config PATH`.

The saved configuration matches [apps.yaml](../examples/apps.yaml):

```yaml
apps:
  app-db:
    recipe: postgres
    namespace: applications
    settings:
      storage: 10Gi
      storage_class: local-path
      existing_secret: app-db-auth
      database: app
      username: postgres
```

Application names must be DNS labels of at most 40 characters. Each application
gets its own Helm release, Services, StatefulSet, PVC, and inventory ConfigMap.
You can add multiple PostgreSQL instances with different names. Application
namespaces must be outside the AK3S platform and Kubernetes system namespaces.
Unknown settings, nulls, YAML aliases, and multiple YAML documents are rejected.

Storage defaults to `10Gi` on `local-path`. The application file has its own
storage-class setting; it does not inherit overrides from the platform file.
PostgreSQL uses the latest stable release, currently **18.6**, with an official
image pinned to a version and multi-platform digest for Linux amd64 and arm64.
The PVC mounts at `/var/lib/postgresql`, with database files in
`/var/lib/postgresql/18/docker`, following the official PostgreSQL 18 image layout.
The image, chart, and recipe versions are embedded
in the AK3S binary, independently of the platform charts.

## Create credentials and deploy

Create the namespace and a Secret with a nonempty `password` key before applying
the application. If `applications` already exists, use that namespace rather than
creating it again. Keep the password outside the repository. For example, store
it without a trailing newline in a private file at
`/etc/ak3s/private/app-db-password`, with mode `0600`, then run:

```bash
sudo k3s kubectl --kubeconfig /etc/rancher/k3s/k3s.yaml create namespace applications
sudo k3s kubectl --kubeconfig /etc/rancher/k3s/k3s.yaml -n applications create secret generic app-db-auth \
  --from-file=password=/etc/ak3s/private/app-db-password
sudo ak3s apps apply --dry-run
sudo ak3s apps apply
sudo ak3s apps status
```

The Secret supplies the initial database administrator's password. `username`
defaults to `postgres`; a custom username also creates a PostgreSQL superuser.
Create separate users with limited permissions for your applications. Database
and username settings initialize an empty data directory; they do not rename
existing databases or users. Changing the Kubernetes Secret does not change an
existing database password. Rotate the database password and its Secret together.

Before changes, AK3S checks the existing application inventory, resource
ownership, PVC settings, and credential reference. It renders the chart using
the verified Helm binary, validates the PVC and inventory against the server,
and performs Helm server validation for an existing release. A first installation
receives local chart validation; Helm validates its resources during installation.
Dry-run creates no persistent resources.

If an installed application's PVC is missing, reconciliation stops so the
operator can restore its data before continuing. A new inventory checkpoint
in the `provisioning` phase, without an installed release or workload, can create
its first PVC during a retry. A completed application keeps the `ready` phase,
which prevents creating empty storage even if its Helm release was removed.

Application reconciliation shares the local lock used by cluster operations.
It records an inventory checkpoint, applies the separate PVC, installs the Helm
release with atomic rollback, and waits for StatefulSet readiness and PVC binding.
If an installation fails, the PVC and checkpoint remain for a retry. Stop at the
first failed application, fix the cause, and rerun the same command. Successful
earlier applications remain installed.

PostgreSQL listens on port `5432` at
`ak3s-app-app-db.applications.svc.cluster.local`. For local administration:

```bash
sudo k3s kubectl --kubeconfig /etc/rancher/k3s/k3s.yaml -n applications \
  port-forward service/ak3s-app-app-db 5432:5432
```

The Service has no public load balancer or ingress. Clients in the cluster can
connect to it. This first recipe does not add database TLS, backup automation,
an exporter, replication, or high availability. Platform container metrics and
logs remain available; PostgreSQL-specific metrics need a separate exporter.

## Preview without a cluster

Rendering only needs outbound HTTPS for the pinned Helm binary:

```bash
ak3s apps render --config ./examples/apps.yaml
ak3s apps render --config ./examples/apps.yaml --output .tmp/postgres-render
```

Exports contain application manifests and values, with mode `0600`. They do not
include K3s configuration or Secret values. Remove temporary exports after review.

## Upgrades and retained data

`ak3s apps apply` keeps installed recipe and image versions unchanged. After
backing up the database and installing a newer AK3S binary, explicitly apply a
recipe update with:

```bash
sudo ak3s apps upgrade --dry-run
sudo ak3s apps upgrade
```

PostgreSQL major version changes and downgrades are refused. A major upgrade
requires a separate database migration, such as dump and restore into a new
application instance. Storage size, storage class, initial database identity,
and credential reference cannot change implicitly; migrate these separately.
The installed inventory records the versions. An update records its new versions
only after readiness succeeds, so a failed update can retry the same upgrade.
Helm rollback does not restore database contents or undo a database migration.

Removing an entry from `apps.yaml` stops reconciliation of that entry. It does
not delete the release or PVC. Application removal is manual in this first
version. The PVC is managed separately from Helm and remains after an ordinary
Helm uninstall. Deleting the namespace or PVC can delete its data. Full
`ak3s uninstall --yes` removes local K3s volumes, including application data.

Local-path storage lives on the VPS disk. Retaining a PVC is not a backup and
does not protect against disk failure or VPS loss. Keep database backups outside
the server and test restoration on a disposable environment.

## Runtime validation

On a disposable AK3S server, apply the example and confirm the StatefulSet is
ready and `ak3s-app-app-db-data` is Bound. Run an authenticated SQL query, write
a test row, delete the PostgreSQL pod, and verify the row after the replacement
pod becomes ready. Repeat `apps apply` and verify the PVC UID and row remain.
Test an installation failure followed by a retry, resource ownership rejection,
and backup restoration into a separate application instance.

Unit tests and real Helm rendering verify configuration, manifests, and execution
boundaries without installing a cluster. They do not prove database startup,
filesystem permissions, data persistence, or recovery. Follow the general
[runtime testing guide](runtime-testing.md) before release.
