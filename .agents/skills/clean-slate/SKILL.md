---
name: clean-slate
description: Remove the local AK3S test cluster, persistent data, and AK3S configuration for a fresh installation in the current WSL distribution. Run only when explicitly invoked as $clean-slate; creating, editing, explaining, or previewing this skill does not authorize cleanup.
---

# AK3S clean slate

Return the current WSL distribution's verified AK3S test installation to an
uninstalled state. Remove the cluster's persistent data and credentials, K3s
configuration and services, AK3S ownership/state, and operator overrides. Leave
installation stopped so the next test starts from the beginning.

This skill belongs only to this repository. Resolve the checkout with
`git rev-parse --show-toplevel` and confirm it contains AK3S's `go.mod` and
`internal/ak3s/plan.go`. Do not assume an absolute checkout path or install this
skill in a user/global skills directory.

## Authorization and scope

- Execute only on an explicit `$clean-slate` invocation for cleanup. That
  invocation authorizes stopping verified local AK3S processes and deleting the
  verified cluster and test configuration below. A request to create, edit,
  explain, or preview the skill does not authorize execution. For a preview,
  perform read-only inventory and stop before mutation.
- Before mutation, state which local installation, configuration, credentials,
  and persistent volumes will be deleted. Deleted data requires an existing
  backup to recover. Do not make new secret-bearing backups automatically.
- Preserve source files, Git history, uncommitted work, tracked placeholders,
  this skill, the installed AK3S CLI, build tools, Docker, shared image/download
  caches, host PostgreSQL, and unrelated application data. Honor narrower user
  requests, such as retaining overrides, and report those exceptions.
- Operate in the current, confirmed WSL distribution. Do not unregister or
  restart WSL, edit Windows `.wslconfig`, change `/etc/wsl.conf` or the hostname,
  unload shared kernel modules, or touch Windows processes or other distributions.
  Remote clusters, external databases/storage, Git commits, pushes, and a new
  installation require separate authorization.
- AK3S and other installers use the same host K3s paths. Do not uninstall a
  cluster owned by another project. If the user explicitly includes another
  project's installation, inspect its own cleanup workflow and ownership first;
  the AK3S marker or a familiar port is not ownership evidence for that project.

## Inspect the current implementation and installation

Read these repository files before using the inventory below:

- `internal/ak3s/plan.go`: fixed paths, ownership/state, and generated host settings.
- `internal/ak3s/reconcile.go`, `internal/ak3s/installer.go`, and
  `internal/ak3s/uninstall.go`: upstream lifecycle, node roles, and recovery checkpoints.
- `internal/ak3s/host.go`: existing-installation and configuration checks.
- `docs/wsl-testing.md`, `docs/runtime-testing.md`, and `docs/operations.md`:
  runtime tests, ports, storage, and recovery limits.

Record Git status. Put temporary scripts and private logs in this checkout's
`.tmp/`, remove them when finished, and preserve `.gitkeep`. Never print full
kubeconfigs, tokens, keys, rendered Secrets, process environments, or credentials.
Follow the repository's confidentiality rules; stop affected work if confidential
information is encountered without reproducing it.

1. Confirm Linux is running on WSL and that this is the intended local host.
   Inspect `/etc/rancher/k3s/ak3s-managed` privately: the expected prefix is
   `Managed by AK3S`. Correlate it with `/var/lib/ak3s/state.json`, the installed
   service, binary, and desired node role. A state file records an attempted
   installation, not successful completion or association with a checkout.
2. Inventory both `k3s.service` and `k3s-agent.service`, their drop-ins and
   environment files, actual processes, data directories, and mounts. Check
   privately for custom `--data-dir`, `K3S_DATA_DIR`, `token-file`, datastore,
   kubelet, CNI, or storage settings. Do not inherit deletion paths from the shell.
   Verify a local kubeconfig's API endpoint before making a cluster request.
3. If a verified local server is already reachable, inventory namespaces,
   workloads, PVCs/PVs, backing paths, and Helm resources using explicitly selected
   local credentials. For example:

   ```bash
   sudo k3s kubectl --kubeconfig /etc/rancher/k3s/k3s.yaml get namespaces,pods,pvc -A --request-timeout=10s
   sudo k3s kubectl --kubeconfig /etc/rancher/k3s/k3s.yaml get pv --request-timeout=10s
   ```

   Expected platform namespaces include `ingress-nginx`, `cert-manager`,
   `headlamp`, and `observability`; runtime testing adds `demo`. Inspect unexpected
   workloads before deleting a whole cluster. A stopped or unreachable cluster
   does not need to be started for inventory. On a joined node, remove only its
   local installation; cluster-wide resources and remote data are outside scope.
4. Identify listeners and supervisors using `sudo ss -lntup`, covering 80, 443,
   6443, 8080, 8428, 9428, and actual custom ports. K3s ServiceLB uses host-port
   rules that may not appear as listening sockets. Correlate each process with
   its cgroup, executable, working directory, parentage, and runtime endpoint.
   Never use a port number or process name alone to establish ownership.
5. Inventory Docker test containers, their published ports, volume/bind mounts,
   networks, and restart policies if present. Confirm the daemon is local to the
   intended WSL host. A name such as `ak3s-local` is only a candidate; correlate
   its K3s image, labels, mounts, and test provenance. Check other consumers before
   deleting a volume or network.
6. Inspect candidate filesystem targets without exposing contents. Validate exact
   absolute paths, resolved ancestors, symlinks, mounts, and tracked-file status.
   Verify custom storage individually; do not traverse into unrelated or external
   data. Do not use broad `git clean`, recursive deletion globs, or global prunes.

If the AK3S marker is absent and K3s machine-level artifacts remain, or ownership
records conflict, stop that deletion and explain the specific ambiguity. Never
forge ownership or remove state to bypass an ownership check. Session evidence
that identifies an already interrupted cleanup can establish ownership of its
remaining artifacts. An already clean host is a successful repeat-cleanup case.
If no cluster remains but AK3S overrides or generated test artifacts exist,
remove the verified leftovers within the requested scope without starting K3s
or creating a new ownership record.

## Stop writers, containers, and cluster networking

AK3S delegates native installation and removal to upstream K3s. The official
installer generates the role-specific uninstall script and `k3s-killall.sh`.
Use `ak3s uninstall` for a migrated native installation; it verifies ownership
and generated-script fingerprints, locks the host, and retains recovery copies
if upstream cleanup is interrupted. Its removal command does not remove operator
overrides, so handle those separately below within this skill's authorized scope.

1. Stop verified AK3S CLI operations and private interface port-forwards. Let
   `ak3s uninstall` acquire its own lock; do not hold `apply.lock` while invoking
   it. If another reconciliation holds the lock, identify that operation and
   resolve it within the authorized cleanup scope before retrying.
2. For verified AK3S Docker test clusters, stop and remove the exact containers
   and their exclusive test volumes/networks using Docker. Preserve the shared
   daemon, bridge, unrelated containers, images, and caches.
3. For a native installation, review `sudo ak3s uninstall --dry-run`, then run
   `sudo ak3s uninstall --yes`. The explicit invocation of this cleanup skill
   already authorizes deletion of the verified local test cluster. The generated
   upstream scripts own service, process, mount, network, and K3s data removal.
4. For interrupted removal, preserve `/var/lib/ak3s/state.json`, `uninstall.sh`,
   and `killall.sh` and repeat the removal command after resolving the reported
   issue. The marker and original script may already be gone; the pending state
   and verified recovery scripts retain evidence for the remaining operation.
5. If a legacy installation lacks verified generated scripts, stop native cleanup
   and explain that it needs a separately authorized migration or an existing
   upstream recovery procedure. Do not run an installer merely to generate
   removal scripts, and do not substitute manual process, firewall, mount, or
   recursive K3s data deletion. See `docs/migration.md` and
   `docs/operations.md#removal`.

If upstream cleanup fails, keep the ownership/state and recovery scripts for
retry. Report completed deletions accurately. Do not delete persistent files
beneath running containers or live mounts.

## Remove configuration and persistent data

After upstream removal succeeds, inspect the targets below. K3s-owned targets
should already be removed by the generated scripts; report leftovers and retry
the upstream workflow instead of recreating its cleanup logic. Remove only the
remaining verified AK3S test configuration and artifacts within scope:

| Target | Scope |
| --- | --- |
| `/var/lib/rancher/k3s/` | Local datastore, certificates/tokens, images, snapshots, and local-path application volumes. |
| `/etc/rancher/k3s/` | K3s config, kubeconfig credentials, owned drop-ins, and the AK3S ownership marker. |
| `/etc/rancher/node/` | Local node registration credentials, when owned by the removed cluster. |
| `/var/lib/ak3s/` | AK3S removes completed checkpoints and recovery scripts. Keep `apply.lock` in place so concurrent operations use the same lock file. |
| `/etc/ak3s/values.yaml` | Operator overrides, including node identity and ACME contact settings. Remove the directory only if empty or all remaining files are verified AK3S test configuration. |
| `/etc/systemd/system/k3s.service` or `k3s-agent.service` | Verified installation unit, enablement links, and owned drop-ins/environment files. |
| `/etc/modules-load.d/ak3s.conf` | AK3S's persistent module-loading settings. |
| `/etc/sysctl.d/90-ak3s.conf` | AK3S's persistent forwarding/bridge settings. |
| `/usr/local/bin/k3s` | Verified cluster executable; remove owned helper scripts/symlinks only when proven installation artifacts. |
| `/var/lib/kubelet/`, `/var/lib/cni/`, `/run/k3s/`, `/run/flannel/` | Verified runtime state after their processes and mounts are gone. |

Report remaining CNI configuration under `/etc/cni/net.d/` or custom volume
paths for explicit review. Do not delete all of `/etc/rancher`,
`/var/lib/rancher`, `/etc/cni`, or `/etc/kubernetes`. Retained/external PVs and
datastores require separate authorization and are not erased by local cleanup.

Keep the installed `/usr/local/bin/ak3s` CLI, `bin/`, `dist/`, source examples,
and user source/configuration edits in the checkout unless removal was explicitly
requested. Remove individually verified generated render outputs, copied test
kubeconfigs, logs, and abandoned private temporary bundles only when in scope;
preserve tracked files and active agent work. From a shared kubeconfig, remove
only entries proven to belong to the removed cluster; never erase all contexts.

Removing the sysctl file does not restore previous runtime sysctl values. Restore
them only with a known pre-install baseline and no other consumers; otherwise
report them as retained. Do not guess that forwarding used to be zero, unload
`overlay`/`br_netfilter`, change swap, or reset shared WSL settings. Keep existing
backup exports unless the user explicitly includes them. Do not vacuum the system
journal to erase installation logs.

The upstream scripts handle service-manager cleanup. Keep failure output private
if it could contain credentials.

## Verify and hand off

Use read-only checks; do not install, start a cluster, or run runtime tests to
verify cleanup. Distinguish absent resources from query/permission failures.

- Verified K3s services are inactive and their owned unit files are absent.
  No owned server/agent, containerd, shim, pod, or port-forward processes remain.
- Owned data, configuration, credentials, runtime mounts, interfaces, firewall
  rules, and Docker test containers/volumes are absent. Verify both IPv4 and IPv6.
- Inventoried AK3S ports are released. If an unrelated listener occupies one,
  preserve it and report the conflict; never declare the host ready to install
  on that port or silently substitute another port.
- AK3S's existing-installation checks no longer find the removed binary, config,
  service, datastore, or state. Other installers' leftover K3s artifacts remain
  a blocker to switching installers; do not remove them without ownership/scope.
- Git status shows no unintended source changes. Remove task-created temporary
  scripts/logs while preserving `.tmp/.gitkeep`. Shared services remain healthy.

Summarize deleted configuration and data, released ports, any forced stops,
verification, preserved files/shared settings, and unresolved blockers. State that
`/etc/ak3s/values.yaml` must be recreated for a fresh AK3S test. Give the next step
without running it unless the user separately requested installation:

```bash
sudo install -d -m 0755 /etc/ak3s
sudo nano /etc/ak3s/values.yaml
```

Then follow `docs/wsl-testing.md`: dry-run, install, and the runtime checklist.
The distribution can then be reused for either installer. Install one native
cluster at a time; shared image/download caches remain available.
