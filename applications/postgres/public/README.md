# Public PostgreSQL access

This path selects [revision 002](../revisions/002-public-tls/README.md) for
compatibility. For an existing private deployment, follow the
[001 to 002 upgrade guide](../upgrades/001-to-002.md). Use the explicit revision
path in your operator overlay so future changes cannot silently select a different
revision.

The [deployment history](../revisions/README.md) records the original private
deployment and later changes. PostgreSQL deployment revisions are separate from
PostgreSQL versions and application database migrations.
