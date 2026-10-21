# Revision 002: public PostgreSQL with TLS

This revision builds on [revision 001](../001-private/README.md) to add public
TCP access on port `5432`, a dedicated cert-manager Certificate, mandatory TLS
and SCRAM authentication for all TCP clients, and automatic certificate reloads.
It retains the database image, StatefulSet identity, password Secret, data
directory, PVC, private Services, and VPA settings.

Follow the [001 to 002 upgrade guide](../../upgrades/001-to-002.md), including
readiness, login, backup, certificate, client, and firewall checks. A new
installation first completes revision `001`, then follows the same upgrade.
Switching an existing installation restarts its single database replica.

Use `applications/postgres/revisions/002-public-tls` as your operator overlay's
base. The tracked certificate hostname is fictional; the upgrade guide shows
how to set your hostname outside tracked defaults. Continue applying that same
operator overlay for later reconciliation of this revision.

The StatefulSet's `ak3s.io/postgres-revision` annotation identifies the declared
revision as `002`. Verify rollout and database access separately before recording
the upgrade as completed. See the [history](../README.md) for revision policy and
the upgrade guide for retry and rollback instructions.
