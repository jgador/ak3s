# Revision 001: private PostgreSQL

This is the original deployment merged into `master` by
[PR #3](https://github.com/jgador/ak3s/pull/3), commit `e21cd1e`. Its four YAML
files are preserved exactly from that merge. The directory move does not change
the rendered Kubernetes resources.
`checksums.sha256` records their original contents. `make verify` checks these
digests to detect accidental changes to this deployed revision.

It installs PostgreSQL 18.6 as `infrastructure/psql-server`, uses the existing
`psql-server-auth` password Secret, and stores data on `psql-server-data`, a
`20Gi` local-path PVC. Both Services remain private. The VPA requires the
previously documented VPA installation. Only the built-in maintenance database
`postgres` is initialized; application repositories own their own databases,
roles, and migrations.

For a first installation, follow the [installation guide](../../README.md).
The existing `kubectl apply -k applications/postgres` command still selects this
revision. You can also select it explicitly with
`-k applications/postgres/revisions/001-private`.

If these manifests are already applied, reuse the current Secret and PVC.
Applying them does not confirm that PostgreSQL started or that its password works.
The [001 to 002 upgrade guide](../../upgrades/001-to-002.md) begins with those
checks before adding public access. Do not rerun first-install credential setup
or delete persistent storage to perform that upgrade.
