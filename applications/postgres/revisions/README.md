# PostgreSQL deployment history

Choose a numbered revision explicitly. These numbers version the deployment
configuration; they do not replace PostgreSQL version numbers or application SQL
migrations. Applying Kubernetes manifests is not proof of database readiness or
successful login.

| Revision | Configuration | Status | Installation or upgrade |
| --- | --- | --- | --- |
| [001](001-private/README.md) | Private PostgreSQL 18.6, local-path PVC, VPA | Merged in `master` by PR #3, commit `e21cd1e` | [First installation](../README.md) |
| [002](002-public-tls/README.md) | Same database and storage, public TCP 5432, mandatory TLS, certificate reloads | Proposed by `feat/postgres-public-tls`; not included in the original merge | [001 to 002](../upgrades/001-to-002.md) |

An operator who already applied the original manifests starts at revision `001`.
Verify readiness and an authenticated query before upgrading. Revision `001`
has no revision annotation; do not infer its readiness from that absence.
Revision `002` adds `ak3s.io/postgres-revision: "002"` to the StatefulSet's
metadata. This records the declared configuration, not a completed deployment
or a successful connection test.

The existing `applications/postgres` path continues to select revision `001`.
The `applications/postgres/public` path selects revision `002`. Use the numbered
paths in operator configuration and deployment records for reproducible updates.
Apply only the target revision's complete configuration; these directories are
not scripts to run repeatedly in order. Do not apply both revisions together.

## Adding another revision

Preserve previously merged revisions. Add the next numbered directory, reusing a
previous revision as a Kustomize base where suitable, and document the supported
upgrade in `upgrades/`. Never change an old revision's image, storage, identity,
or authentication behavior as part of a new update. Wording corrections may
clarify existing instructions without changing their meaning.
Revision `001` has checksums verified by `make verify`; preserve those digests.

Each upgrade guide must identify the source and target revisions, prerequisites,
expected downtime, affected clients, resources kept or replaced, verification,
retry behavior, and rollback limits. Append an entry to this history. Keep
operator hostnames, credentials, backups, and records outside tracked defaults.

Test an upgrade against data and credentials initialized by the source revision,
then test repeat applies and the documented rollback. Preserve the StatefulSet
name, selectors, password Secret, PVC, and data directory unless the guide
explicitly explains a migration. A PostgreSQL major-version change requires a
supported database migration; changing the image and applying manifests alone
is insufficient.

After an operator verifies a deployment, record the revision, exact Git commit,
UTC date, and checks performed in a private operations log. A Git merge or an
applied revision annotation does not establish which configuration a live
database is successfully running.
