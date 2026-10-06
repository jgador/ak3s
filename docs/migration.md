# Migrate an existing AK3S installation

AK3S now uses the official K3s installer for binary installation, upgrades,
systemd services, and generation of uninstall scripts. AK3S keeps its existing
configuration, ownership, identity, token, and version checks. The K3s and chart
versions in this change are unchanged.

Migration runs the installer in place. It preserves `/etc/rancher/k3s/config.yaml`
settings supported by AK3S, the datastore, server token, node identity, and local
volumes. It replaces the old service definition and may restart K3s. It never
uses an uninstall or killall script during installation, migration, or upgrade.

## Before changing the node

1. Back up the datastore, server token, local volumes, operator overrides, K3s
   configuration, ownership marker, and AK3S state. Test restoration on another
   disposable host. See [backups](operations.md#backups).
2. Keep the same `cluster_name`, `node.name`, role, join settings, datastore,
   network settings, and token file in `/etc/ak3s/values.yaml`. Keep token files
   private (`0600`). For older installations without a state file, explicitly
   set the existing node name and network settings rather than relying on defaults.
3. Review service changes locally with `sudo systemctl cat k3s` (or `k3s-agent`).
   Keep this output private. AK3S refuses nonempty service environment files and
   configuration drop-ins. Resolve those settings explicitly; do not delete
   custom settings merely to pass a check. The official installer replaces the
   base unit. Settings outside AK3S's supported YAML require a separate migration.
4. Install the new AK3S CLI. Replacing the CLI does not change the cluster.

## Apply the migration

```bash
ak3s version
sudo ak3s apply --dry-run
sudo ak3s apply
sudo ak3s status
```

Use `--config PATH` on every command if the overrides are stored elsewhere.
The plan identifies the pinned official installer. Dry-run downloads and verifies
artifacts and renders charts; it never executes the installer or changes the host.
If the installed K3s version differs from the release pin, use `upgrade --dry-run`
and `upgrade` after reviewing the [upgrade guidance](operations.md#apply-and-upgrade).
Downgrades and skipped Kubernetes minor versions remain blocked.

The same path supports the previous Go-based AK3S release (state schema 1) and
older AK3S installations with the ownership marker but no state file. Inline
legacy server tokens are retained. A joining node's inline token can move to a
matching private token file; a different token is refused. An installation without
the AK3S ownership marker is not automatically adopted. Do not manufacture a
marker or remove state to bypass these checks.

AK3S records state schema 2 before installation, then fingerprints the
upstream-generated service and removal scripts. It clears the pending restart
after service readiness. The scripts are:

- Server: `/usr/local/bin/k3s-uninstall.sh`
- Agent: `/usr/local/bin/k3s-agent-uninstall.sh`
- Shared helper: `/usr/local/bin/k3s-killall.sh`

An unchanged active, enabled node skips the installer on later runs. A changed
K3s configuration, version, installer revision, missing or changed generated
files, disabled service, or interrupted installation triggers reconciliation.
The server API and local node must become ready before platform installation.
Agents check their local service; verify their Node Ready condition from a server.

If migration is interrupted, keep the configuration and ownership/state files,
fix the reported issue, and repeat the same command. The pending checkpoint
forces the necessary restart even if the binary was already replaced. Helm
failures after K3s readiness can be retried without restarting an unchanged K3s
service. There is no automatic K3s downgrade or datastore rollback. The older CLI
does not understand schema 2; restore a compatible backup to return to it.

For multiple servers, migrate and upgrade one server at a time, check readiness
and etcd quorum, then handle agents. For sensitive workloads, plan cordon/drain
and uncordon steps using [upstream maintenance guidance](https://docs.k3s.io/upgrades/manual).
AK3S does not coordinate a cluster-wide maintenance operation.

Validate a restored disposable copy with the [runtime checklist](runtime-testing.md)
before changing an important installation. Automated tests verify ordering and
data-preservation rules; they do not prove recovery of a real datastore.
