# Deploy PostgreSQL on a VPS from your workstation

Use these manifests to install PostgreSQL into an **already running AK3S or K3s
cluster** on Hetzner Cloud or another VPS provider. Run `kubectl` on your workstation
to deploy to the VPS. It reads the local YAML files and sends resources to the
Kubernetes API; PostgreSQL runs on the VPS and stores data on a persistent volume.

This recipe installs a shared PostgreSQL server. Each application repository owns
its database creation, roles, schema migrations, and application credentials.
There are no application databases or initialization scripts in these manifests.

Your workstation needs Bash, SSH, a local checkout of this repository, and
[`kubectl`](https://kubernetes.io/docs/tasks/tools/install-kubectl-linux/)
compatible with the cluster's Kubernetes version. On Windows, run the Bash examples
inside WSL. Kustomize, which combines the YAML files, is included in `kubectl`.
You do not need to copy the manifests to the VPS or install K3s, Helm, Make, Go,
or the AK3S CLI on your workstation. The VPS must already have K3s running and an
expandable CSI storage class. Installing a CSI driver separately may require Helm.

## What will be installed

| Resource or setting | Default |
| --- | --- |
| Namespace | `applications`, created separately |
| PostgreSQL image | Official PostgreSQL `18.6-bookworm`, pinned by digest |
| Built-in maintenance database | `postgres`; no application database is created |
| Initial administrator | `postgres` |
| Credential Secret | `psql-server-auth`, with a `password` key, created separately |
| StatefulSet and pod | `psql-server` and `psql-server-0`, one replica |
| Persistent volume claim (PVC) | `psql-server-data`, requesting `20Gi` on expandable `hcloud-volumes` |
| Client Service | `psql-server`, private ClusterIP, port `5432` |
| StatefulSet Service | `psql-server-headless`, headless Service, port `5432` |
| CPU request | `100m`, one tenth of a CPU core |
| Memory | `256Mi` requested, `1Gi` limit |

The image supports Linux amd64 and arm64. Data mounts at `/var/lib/postgresql`,
with database files under `/var/lib/postgresql/18/docker`, following the official
PostgreSQL 18 image layout.

Server resource names use the `psql-` prefix, with `psql-server` as the default.
Application database names use `psqldb-<application>`, for example
`psqldb-example`. These are this project's chosen prefixes. Microsoft's current
[resource abbreviation table](https://learn.microsoft.com/en-us/azure/cloud-adoption-framework/ready/azure-best-practices/resource-abbreviations)
uses `pgsql` for PostgreSQL flexible servers; this recipe keeps your requested
`psql-` and `psqldb-` convention.
PostgreSQL's built-in database and administrator remain named `postgres`.

This is a single PostgreSQL server using a CSI-managed persistent volume. It has no
replication, automatic failover, database TLS, or automated backups. A private
Service is reachable from other pods with network access; it does not itself
restrict access to selected applications. PostgreSQL authentication still applies.

| File | Purpose |
| --- | --- |
| [kustomization.yaml](kustomization.yaml) | Lists the manifests and assigns their namespace |
| [pvc.yaml](pvc.yaml) | Requests persistent storage |
| [service.yaml](service.yaml) | Defines the client and headless Services |
| [statefulset.yaml](statefulset.yaml) | Defines PostgreSQL, credentials, storage mounts, and health checks |

The namespace and password Secret are deliberately created separately. There are
no password values in these files.

## 1. Connect local kubectl to the VPS cluster

Run the commands in this guide in **Bash on your workstation**. Replace
`USER@VPS_ADDRESS` with your SSH account and VPS address. Use your usual SSH key;
add `-i PATH` if you select a key explicitly.

Deployment examples use `~/.kube/ak3s-hetzner.yaml` for the live production
cluster. Each command passes `--kubeconfig` explicitly instead of relying on
`KUBECONFIG` or the current context. These are instructions for the human operator;
agents must follow [AGENTS.md](../../AGENTS.md) and must not access production
without explicit authorization for the specific operation. This instruction is
a safeguard, not an access restriction enforced by Kubernetes.

Use this dedicated kubeconfig for production. If you already have it, skip the
retrieval block below. Otherwise, retrieve the K3s administrator kubeconfig over
SSH and save it privately on your workstation:

```bash
(
  set +x
  set -euo pipefail
  umask 077
  mkdir -p "$HOME/.kube"
  postgres_kubeconfig_file=$(mktemp "$HOME/.kube/ak3s-hetzner.XXXXXX")
  trap 'rm -f -- "$postgres_kubeconfig_file"' EXIT
  ssh USER@VPS_ADDRESS 'sudo -n cat /etc/rancher/k3s/k3s.yaml' > "$postgres_kubeconfig_file"
  test -s "$postgres_kubeconfig_file"
  mv -- "$postgres_kubeconfig_file" "$HOME/.kube/ak3s-hetzner.yaml"
)
```

This retrieval requires remote sudo access without a password prompt. If your
account does not have it, obtain the kubeconfig through your administrator or
use an authorized root SSH account with `cat` instead of `sudo -n cat`.
The default K3s kubeconfig grants cluster administrator access. Keep it private,
outside the repository, and refresh it from the server when its certificates
are renewed.

Protect the file and check its context. The same explicit path is included in
later deployment, port-forwarding, password rotation, and troubleshooting commands:

```bash
chmod 600 "$HOME/.kube/ak3s-hetzner.yaml"
kubectl --kubeconfig "$HOME/.kube/ak3s-hetzner.yaml" config current-context
```

For API access through SSH, open a **separate local terminal** and keep this tunnel
running:

```bash
ssh -o ExitOnForwardFailure=yes -N -L 127.0.0.1:16443:127.0.0.1:6443 USER@VPS_ADDRESS
```

In your deployment terminal, point this kubeconfig at the tunnel:

```bash
kubectl --kubeconfig "$HOME/.kube/ak3s-hetzner.yaml" config set-cluster \
  "$(kubectl --kubeconfig "$HOME/.kube/ak3s-hetzner.yaml" config view -o jsonpath='{.clusters[0].name}')" \
  --server=https://127.0.0.1:16443
kubectl --kubeconfig "$HOME/.kube/ak3s-hetzner.yaml" get nodes -o wide
```

The cluster-name expression assumes a dedicated kubeconfig with one cluster.
Confirm that the listed nodes belong to the intended VPS before proceeding.
The standard K3s API certificate includes `127.0.0.1`, so TLS verification remains
enabled through this tunnel. Keep the tunnel running for all remote `kubectl`
commands; no public access to TCP `6443` is needed for this connection.

If you already have direct API access through a private network, VPN, or restricted
firewall rules, you can use that connection instead of the tunnel. Replace
`YOUR_API_HOST` below with a host included in the API certificate:

```bash
kubectl --kubeconfig "$HOME/.kube/ak3s-hetzner.yaml" config set-cluster \
  "$(kubectl --kubeconfig "$HOME/.kube/ak3s-hetzner.yaml" config view -o jsonpath='{.clusters[0].name}')" \
  --server=https://YOUR_API_HOST:6443
kubectl --kubeconfig "$HOME/.kube/ak3s-hetzner.yaml" get nodes -o wide
```

See [API configuration](../../docs/configuration.md) and
[VPS firewall requirements](../../docs/vps.md#firewall-requirements).

From your **local repository root**, enter the manifest directory:

```bash
cd applications/postgres
ls
```

Expect this README and the four YAML files listed above. All subsequent manifest
commands run from this local directory, or a local working copy containing the
same files. `apply -k .` reads `kustomization.yaml`; it does not apply the README.

## 2. Check the existing cluster and available capacity

```bash
kubectl --kubeconfig "$HOME/.kube/ak3s-hetzner.yaml" get nodes -o wide
kubectl --kubeconfig "$HOME/.kube/ak3s-hetzner.yaml" get storageclass hcloud-volumes \
  -o custom-columns=NAME:.metadata.name,PROVISIONER:.provisioner,EXPANDABLE:.allowVolumeExpansion,BINDING:.volumeBindingMode,RECLAIM:.reclaimPolicy
kubectl --kubeconfig "$HOME/.kube/ak3s-hetzner.yaml" get csidriver csi.hetzner.cloud
ssh USER@VPS_ADDRESS 'systemctl is-active k3s'
ssh USER@VPS_ADDRESS 'free -h'
ssh -t USER@VPS_ADDRESS 'sudo df -h /var/lib/rancher/k3s'
```

Expect K3s to be active and the server node to be Ready. Resolve cluster failures
before deploying the server. `kubectl` uses the VPS kubeconfig selected in
step 1. The SSH commands inspect the VPS rather than your workstation.

Expect `hcloud-volumes` to use `csi.hetzner.cloud` with `EXPANDABLE` set to `true`.
The driver must be running and able to provision and expand volumes. AK3S installs
`local-path` for its platform workloads; it does not install the Hetzner CSI driver.
If the driver or class is missing, follow the
[Hetzner CSI installation guide](https://github.com/hetznercloud/csi-driver/blob/main/docs/kubernetes/guides/quickstart.md)
as a separate infrastructure setup step. Keep its provider API token private.
Do not apply this recipe until the class and driver are ready.

On another provider, select an installed CSI storage class that supports volume
and filesystem expansion, with `allowVolumeExpansion: true`, and change
`storageClassName` in your local `pvc.yaml`. Verify the class and its CSI driver
instead of the Hetzner names above. Creating a StorageClass alone does not install
a driver. Setting `allowVolumeExpansion` on K3s's `local-path` class does not add
resize support.

Check that the VPS has enough memory for PostgreSQL alongside its existing
workloads and that the storage provider can provision at least `20Gi`. `20Gi`
means 20 GiB, approximately 21.5 decimal GB. Hetzner Cloud Volumes are separately
billed block storage; the claim does not allocate 20 GiB on the VPS's root disk.
Monitor both volume usage and the node's root disk.

Keep TCP `5432` and the local forward ports `15432` and `16443` private. Review
both your provider firewall and host firewall; existing cluster networking and
outbound image downloads must continue to work. Direct API access must be
restricted to your administrative addresses or private network.

## 3. Review the configuration

Review the **local files** using your preferred editor. For custom settings, make
a local working copy of the manifest directory outside the repository's tracked
defaults and run subsequent manifest commands from that copy. Preserve your
operator changes when updating it.

| Change | Location |
| --- | --- |
| Storage capacity and class | `pvc.yaml`: `resources.requests.storage` and `storageClassName` |
| Initial administrator name | `statefulset.yaml`: `POSTGRES_USER` |
| Credential Secret name | `statefulset.yaml`: `secretKeyRef.name` |
| CPU and memory | `statefulset.yaml`: `resources` |
| Namespace | `kustomization.yaml`: `namespace` |

The instructions below use the defaults. If you change the namespace, server
resource names, administrator, or Secret name, use the same values in subsequent
commands. Keep the `psql-` prefix on server resource names and update Service
selectors, pod labels, `serviceName`, and the PVC reference together.

Choose storage settings before installation. The PVC requests `20Gi` initially;
you can later increase it using the [volume expansion steps](#expand-server-storage).
An existing PVC's storage class cannot be changed in place. Moving from
`local-path` requires a new PVC and a planned backup and restore.

Keep `replicas: 1`. Increasing this StatefulSet's replica count does not configure
PostgreSQL replication and can create unsafe access to the shared data directory.

Keep the image pinned by version and digest. The image and `PGDATA` directory must
match the PostgreSQL major version; a major upgrade requires a planned database
migration.

`POSTGRES_DB`, `POSTGRES_USER`, and `POSTGRES_PASSWORD` initialize an **empty** data
directory. `POSTGRES_DB: postgres` selects the built-in maintenance database and
does not create an application database. Keep application provisioning in its own
repository. Editing these values later does not rename existing databases or
users or change the password stored by PostgreSQL.

## 4. Create the namespace and credentials

Create the namespace, or retain it if it already exists:

```bash
kubectl --kubeconfig "$HOME/.kube/ak3s-hetzner.yaml" create namespace applications \
  --dry-run=client -o yaml | kubectl --kubeconfig "$HOME/.kube/ak3s-hetzner.yaml" apply -f -
```

Check for existing workloads before using these resource names:

```bash
kubectl --kubeconfig "$HOME/.kube/ak3s-hetzner.yaml" -n applications \
  get statefulsets,pods,pvc,services
```

If `psql-server`, `psql-server-data`, or either PostgreSQL Service already belongs to
another deployment, stop and review that deployment. These manifests do not adopt
an existing Helm release. Earlier recipes used names such as `postgres`,
`postgres-data`, and `postgres-auth`, a `10Gi` local-path PVC, and an initial `app`
database. Applying these new names creates a separate server with a new volume;
it does not rename or migrate the old server or its data. Preserve the old PVC and
credentials, take backups, and plan a restore and client cutover before removing
the old instance. An existing `app` database is not renamed or deleted.

For a first installation, run this block in **Bash on your workstation**. It
prompts for a strong administrator password without placing it in shell history
or command arguments, creates the Secret, and removes the private temporary
password file.

```bash
(
  set +x
  set -euo pipefail
  umask 077
  mkdir -p .tmp
  postgres_password_file=$(mktemp .tmp/postgres-password.XXXXXX)
  trap 'rm -f -- "$postgres_password_file"' EXIT
  trap 'exit 130' INT
  trap 'exit 143' TERM
  read -r -s -p 'PostgreSQL administrator password: ' postgres_password
  printf '\n'
  test -n "$postgres_password"
  printf '%s' "$postgres_password" > "$postgres_password_file"
  unset postgres_password
  kubectl --kubeconfig "$HOME/.kube/ak3s-hetzner.yaml" -n applications \
    create secret generic psql-server-auth --from-file=password="$postgres_password_file"
)
```

Save the password in your password manager. If `psql-server-auth` already exists for
this server, reuse it. Do not delete or overwrite credentials to resolve an
installation error. Never commit a Secret manifest containing password values;
base64 encoding does not make a password confidential.

Verify that the Secret exists without printing its contents:

```bash
kubectl --kubeconfig "$HOME/.kube/ak3s-hetzner.yaml" -n applications \
  get secret psql-server-auth
```

## 5. Preview and validate

From your local manifest directory:

```bash
kubectl kustomize .
kubectl --kubeconfig "$HOME/.kube/ak3s-hetzner.yaml" apply --dry-run=server -k .
```

The first command renders the four resources without contacting the cluster.
Check the namespace, image, credential reference, and storage settings. The second
validates the resources against the Kubernetes API without saving them. It does
not prove that the database can start or that the disk has sufficient capacity.

For an update to an existing deployment, inspect differences before applying:

```bash
kubectl --kubeconfig "$HOME/.kube/ak3s-hetzner.yaml" diff -k .
```

`diff` returns exit status `1` when differences exist, `0` when there are none, and
a higher status for an error. Review the changes before proceeding. Before
reapplying to an existing database, confirm its PVC still contains the intended
data. Reapplying after storage was deleted can create an empty database.

## 6. Apply and wait for readiness

Run each command separately. Stop and investigate if one fails:

```bash
kubectl --kubeconfig "$HOME/.kube/ak3s-hetzner.yaml" apply -k .
kubectl --kubeconfig "$HOME/.kube/ak3s-hetzner.yaml" -n applications \
  rollout status statefulset/psql-server --timeout=300s
kubectl --kubeconfig "$HOME/.kube/ak3s-hetzner.yaml" -n applications \
  wait '--for=jsonpath={.status.phase}=Bound' pvc/psql-server-data --timeout=180s
kubectl --kubeconfig "$HOME/.kube/ak3s-hetzner.yaml" -n applications \
  get statefulset/psql-server pod/psql-server-0 pvc/psql-server-data service/psql-server service/psql-server-headless
```

Expect the StatefulSet and pod to be ready, the PVC to be Bound, and both Services
to have no external IP. The headless Service has `CLUSTER-IP` set to `None`.

A CSI PVC may be Pending until a pod requests it, because its storage class can
use `WaitForFirstConsumer`. Apply the complete directory before judging volume
binding. The first startup can take longer while the image downloads and the
database initializes.

Kubernetes applies resources individually. A failed apply or rollout can leave
some resources created; investigate and rerun after fixing the cause. Reapplying
the same manifests updates the same named resources.

## 7. Test authentication and connect applications

Use the PostgreSQL client already in the database container:

```bash
kubectl --kubeconfig "$HOME/.kube/ak3s-hetzner.yaml" -n applications \
  exec -it psql-server-0 -- psql -h 127.0.0.1 -U postgres -d postgres -W
```

Enter the password from step 4. In the SQL session:

```sql
SELECT current_database(), current_user, version();
```

Expect database `postgres`, user `postgres`, and the pinned PostgreSQL version. Exit
with `\q`.

Applications running inside Kubernetes use:

| Connection field | Value |
| --- | --- |
| Host | `psql-server.applications.svc.cluster.local` |
| Port | `5432` |
| Database | `psqldb-<application>`, provisioned by the application repository |
| User and password | A separate application account |

The initial `postgres` account is a superuser for server administration. Each
application repository must provision its own `psqldb-<application>` database and
separate login, grant the required permissions, and run schema migrations.
Hyphenated database names must be quoted as SQL identifiers, for example
`"psqldb-example"`; pass them as ordinary strings to client `-d` options.
Store application credentials in that application's own Secret, outside tracked
YAML. Application workloads should use their own account. Keep the server
administrator Secret in this server deployment.

## 8. Connect from your workstation with port-forwarding

In a **local terminal**, start a loopback-only Kubernetes port-forward using the
explicit production kubeconfig:

```bash
kubectl --kubeconfig "$HOME/.kube/ak3s-hetzner.yaml" -n applications \
  port-forward --address 127.0.0.1 service/psql-server 15432:5432
```

Keep it running, along with the API tunnel if you use one. Connect psql, pgAdmin,
DBeaver, or another database tool to `127.0.0.1:15432`, with database `postgres`
and the administrator account for maintenance. Application connections use the
application's database and login. If psql is installed on your workstation, run
this in another local terminal:

```bash
psql -h 127.0.0.1 -p 15432 -U postgres -d postgres -W
```

Stop the database port-forward with Ctrl+C when finished. The port-forward can
exit after pod replacement; start it again when the replacement pod is ready.
If local port `15432` is occupied, choose another workstation port, such as
`25432`, in the left side of `25432:5432` and in your database tool.

`kubectl` carries the forwarding connection through the authenticated Kubernetes
API connection. This recipe does not configure PostgreSQL TLS for traffic
between pods.

## 9. Make backups and test restoration

The CSI volume survives pod replacement. It is not a database backup; deleting
the PVC can delete the backing volume, depending on the PV's reclaim policy.
A K3s datastore backup does not include PostgreSQL volume contents. Each
application repository should document its database backup and recovery plan;
the server operator must cover every database and the shared roles.

For a manual logical backup of an existing application database, enter its name
when prompted on your **workstation**. This does not create a database. The dump
streams from the pod directly into a private local file:

```bash
umask 077
mkdir -p "$HOME/private-backups/postgres"
chmod 700 "$HOME/private-backups/postgres"
unset postgres_backup_file
read -r -p 'Existing application database (for example, psqldb-example): ' postgres_database
if [[ -n "$postgres_database" ]]; then
  postgres_backup_file="$HOME/private-backups/postgres/psql-server-$(date -u +%Y%m%dT%H%M%SZ).dump"
  kubectl --kubeconfig "$HOME/.kube/ak3s-hetzner.yaml" -n applications \
    exec psql-server-0 -- pg_dump -U postgres -d "$postgres_database" --format=custom > "$postgres_backup_file"
else
  printf 'Enter the name of an existing database.\n' >&2
fi
```

Check that `pg_dump` exited successfully before treating the file as a backup.
For migration, you can also enter a legacy database name such as `app`.
Do not add `-t` to this command; the output is a binary archive. It uses the local
database connection inside the pod under the official image's default local
authentication configuration.

Check that PostgreSQL can read the archive:

```bash
kubectl --kubeconfig "$HOME/.kube/ak3s-hetzner.yaml" -n applications \
  exec -i psql-server-0 -- pg_restore --list < "$postgres_backup_file"
```

Archive listing is a format check, not a restoration test. The archive contains
one database; it does not include cluster-wide roles. If your restore plan needs
roles and their settings, also save globals privately:

```bash
postgres_roles_file="${postgres_backup_file%.dump}-roles.sql"
kubectl --kubeconfig "$HOME/.kube/ak3s-hetzner.yaml" -n applications \
  exec psql-server-0 -- pg_dumpall -U postgres --globals-only > "$postgres_roles_file"
```

The globals file can contain password hashes. Keep both files private, protect
their transfer and storage, and retain the credential recovery information needed
by your restore plan.

These backup files are already **outside the VPS** on your workstation. Retain
another protected copy according to your backup policy.

Test restoration on a **separate disposable cluster**. Deploy the recipe there.
In a separate local terminal, establish API access to that cluster and verify its
target nodes using `~/.kube/restore-test-k3s.yaml` before restoring.
Do not run the restore commands against your production cluster. Replace the
example archive path below with the actual local filename:

```bash
kubectl --kubeconfig "$HOME/.kube/restore-test-k3s.yaml" get nodes -o wide
```

Then create an empty disposable `psqldb-restore-check` database and stream the
local archive into it. This database exists only for the recovery test:

```bash
kubectl --kubeconfig "$HOME/.kube/restore-test-k3s.yaml" -n applications \
  exec psql-server-0 -- createdb -U postgres psqldb-restore-check
kubectl --kubeconfig "$HOME/.kube/restore-test-k3s.yaml" -n applications \
  exec -i psql-server-0 -- pg_restore -U postgres -d psqldb-restore-check \
  --no-owner --no-acl --exit-on-error < "$HOME/private-backups/postgres/psql-server-YYYYMMDDTHHMMSSZ.dump"
```

This test restores content under the test administrator and omits original
ownership and permissions. Verify expected rows and application queries. A full
recovery plan must also restore the required roles, ownership, permissions,
credentials, and application configuration. Test that complete plan separately.

## 10. Apply updates and rotate passwords

For manifest updates, keep your operator changes, review the rendered resources
and `diff`, back up, then repeat validation, apply, readiness, and authentication
checks. Image or pod configuration changes can restart the single database pod
and interrupt connections.

Minor PostgreSQL updates should be reviewed and tested before production. Major
version changes need a database migration, such as dump and restore into a new
instance. Kubernetes does not validate database version compatibility or perform
the migration. Changing the image back does not restore database contents.

To rotate the administrator password, connect using step 7 and run
`\password postgres`. Keep that maintenance session open while updating the
Kubernetes Secret. In a separate local terminal, enter the same new password using
the explicit production kubeconfig:

```bash
(
  set +x
  set -euo pipefail
  umask 077
  mkdir -p .tmp
  postgres_password_file=$(mktemp .tmp/postgres-password.XXXXXX)
  trap 'rm -f -- "$postgres_password_file"' EXIT
  trap 'exit 130' INT
  trap 'exit 143' TERM
  read -r -s -p 'New PostgreSQL administrator password: ' postgres_password
  printf '\n'
  test -n "$postgres_password"
  printf '%s' "$postgres_password" > "$postgres_password_file"
  unset postgres_password
  kubectl --kubeconfig "$HOME/.kube/ak3s-hetzner.yaml" -n applications \
    create secret generic psql-server-auth --from-file=password="$postgres_password_file" \
    --dry-run=client -o yaml | \
    kubectl --kubeconfig "$HOME/.kube/ak3s-hetzner.yaml" apply -f -
)
```

The Secret YAML passes directly between commands rather than being saved or
printed. Changing the Secret alone does not change an existing database password.
Running pods also retain their old Secret-derived environment values until
restart; PostgreSQL authentication uses the password stored in the database.
Update affected clients and test a new TCP connection before ending the
maintenance session.

### Expand server storage

Expansion is a deliberate operator action, not automatic growth. Back up and
test the resize on a disposable cluster first. Verify that the bound PVC uses
the intended expandable CSI class and record the current capacity and PVC UID:

```bash
kubectl --kubeconfig "$HOME/.kube/ak3s-hetzner.yaml" get storageclass hcloud-volumes \
  -o custom-columns=NAME:.metadata.name,PROVISIONER:.provisioner,EXPANDABLE:.allowVolumeExpansion
kubectl --kubeconfig "$HOME/.kube/ak3s-hetzner.yaml" -n applications \
  get pvc psql-server-data \
  -o custom-columns=NAME:.metadata.name,UID:.metadata.uid,CLASS:.spec.storageClassName,REQUEST:.spec.resources.requests.storage,CAPACITY:.status.capacity.storage
```

For example, to grow from `20Gi` to `40Gi`, change your local `pvc.yaml` to
`storage: 40Gi`, retain that override, and apply only the PVC:

```bash
kubectl --kubeconfig "$HOME/.kube/ak3s-hetzner.yaml" -n applications \
  apply --dry-run=server -f pvc.yaml
kubectl --kubeconfig "$HOME/.kube/ak3s-hetzner.yaml" -n applications apply -f pvc.yaml
kubectl --kubeconfig "$HOME/.kube/ak3s-hetzner.yaml" -n applications \
  wait '--for=jsonpath={.status.capacity.storage}=40Gi' pvc/psql-server-data --timeout=600s
kubectl --kubeconfig "$HOME/.kube/ak3s-hetzner.yaml" -n applications \
  describe pvc psql-server-data
kubectl --kubeconfig "$HOME/.kube/ak3s-hetzner.yaml" -n applications \
  exec psql-server-0 -- df -h /var/lib/postgresql
```

Use the actual storage-class name if you customized it. The CSI driver expands
the backing volume and filesystem; confirm the PVC capacity, unchanged UID,
mounted filesystem size, and database queries. Some drivers require a pod restart
for filesystem expansion. If `FileSystemResizePending` remains, follow that
driver's instructions and schedule downtime for the single server. Investigate
resize failures instead of deleting the PVC or recreating it. Kubernetes supports
growing a volume, not shrinking it. Keep the larger request in future applies;
reapplying the original `20Gi` default after expansion will fail.

## 11. Stop or remove PostgreSQL while retaining data

To temporarily stop the database:

```bash
kubectl --kubeconfig "$HOME/.kube/ak3s-hetzner.yaml" -n applications \
  scale statefulset/psql-server --replicas=0
```

To start it again:

```bash
kubectl --kubeconfig "$HOME/.kube/ak3s-hetzner.yaml" -n applications \
  scale statefulset/psql-server --replicas=1
kubectl --kubeconfig "$HOME/.kube/ak3s-hetzner.yaml" -n applications \
  rollout status statefulset/psql-server --timeout=300s
```

The manifest declares one replica, so reapplying it also restores that desired
replica count. To remove the workload and Services while retaining the PVC and
Secret:

```bash
kubectl --kubeconfig "$HOME/.kube/ak3s-hetzner.yaml" -n applications \
  delete statefulset/psql-server service/psql-server service/psql-server-headless
```

**Do not use `kubectl delete -k .` to stop PostgreSQL.** The directory includes
the PVC, and deleting it can delete its volume contents. Deleting namespace
`applications` removes all resources in that namespace, including its PVCs.
The backing volume may be deleted if its PV uses reclaim policy `Delete`; the
Hetzner CSI class defaults to that policy. Review PV retention before removing
the cluster and verify an independent backup before deleting data.

## Troubleshooting

Run these on your workstation with the explicit production kubeconfig:

```bash
kubectl --kubeconfig "$HOME/.kube/ak3s-hetzner.yaml" -n applications describe pod psql-server-0
kubectl --kubeconfig "$HOME/.kube/ak3s-hetzner.yaml" -n applications logs psql-server-0 --tail=100
kubectl --kubeconfig "$HOME/.kube/ak3s-hetzner.yaml" -n applications describe pvc psql-server-data
kubectl --kubeconfig "$HOME/.kube/ak3s-hetzner.yaml" -n applications \
  get events --sort-by=.metadata.creationTimestamp
```

Keep logs and diagnostic output private if they contain application information.

| Symptom | What to check |
| --- | --- |
| K3s API is unavailable | Selected kubeconfig, API tunnel or direct network access, node readiness, and remote service logs with `ssh -t USER@VPS_ADDRESS 'sudo journalctl -u k3s -n 100'` |
| `CreateContainerConfigError` | The `psql-server-auth` Secret exists in `applications` and has a nonempty `password` key |
| `ImagePullBackOff` | Pod events, outbound DNS and HTTPS, registry access, and the complete pinned image reference |
| Pod Pending | Scheduling events, CPU and memory capacity, node taints, and PVC binding |
| PVC Pending after apply | Installed CSI driver, storage class, provider capacity and authorization, and pod scheduling; `WaitForFirstConsumer` needs a schedulable pod |
| PVC expansion stalls | `allowVolumeExpansion`, driver resize support, provider capacity, PVC events, and filesystem resize conditions |
| `CrashLoopBackOff` | Current and previous logs, image/data-version compatibility, volume permissions, and available disk space |
| Pod is `OOMKilled` | Memory use and the `1Gi` limit; adjust capacity after checking server headroom |
| Password authentication fails | The database's actual user/password; changing the Secret does not rotate an initialized password |
| Application cannot connect | Service DNS, namespace, selected pod readiness, application credentials, and NetworkPolicies |
| Workstation cannot connect | Local `kubectl port-forward` is running with the correct kubeconfig, the API tunnel is running if used, and the database tool uses the local forwarded port |

For a restarted container, inspect its previous logs:

```bash
kubectl --kubeconfig "$HOME/.kube/ak3s-hetzner.yaml" -n applications \
  logs psql-server-0 --previous --tail=100
```

## Runtime validation

On a disposable cluster, test database persistence as well as readiness. The
examples below use `~/.kube/ak3s-wsl-test.yaml`; replace it with your dedicated
test kubeconfig if it has another name. Verify the test target first, and use this
path in place of the production kubeconfig when following the deployment steps:

```bash
kubectl --kubeconfig "$HOME/.kube/ak3s-wsl-test.yaml" get nodes -o wide
```

Hetzner's CSI driver requires Hetzner Cloud infrastructure; it cannot provision
Hetzner volumes for a WSL node. On WSL, install a test CSI driver with expansion
support and select its class in a local manifest copy. If only `local-path` is
available, you can use it in that disposable copy to test startup and persistence,
but this does not validate capacity limits or volume expansion. Repeat expansion
validation on the CSI driver you will use in production.

1. Deploy, authenticate against the maintenance database `postgres`, and verify
   that no application database was created.
2. Create `"psqldb-runtime-check"` only for this disposable test, write a test row,
   and record the PVC UID with
   `kubectl --kubeconfig "$HOME/.kube/ak3s-wsl-test.yaml" -n applications get pvc psql-server-data -o jsonpath='{.metadata.uid}'`.
3. Delete only the pod with
   `kubectl --kubeconfig "$HOME/.kube/ak3s-wsl-test.yaml" -n applications delete pod psql-server-0`.
4. Wait for the replacement to become ready, reconnect, and verify the row remains.
5. Reapply the directory and confirm the PVC UID and row are unchanged.
6. On expandable CSI storage, increase the request to `40Gi` using the test
   kubeconfig, wait for the PVC and filesystem to grow, and verify the PVC UID
   and test row remain unchanged. Reapply the updated manifests and check again.
7. Restore an off-server backup into a separate instance and verify its contents.

Rendered manifests and API validation do not prove database startup, filesystem
permissions, persistence, or recovery. Record automated checks separately from
live validation. In a repository checkout, see the broader
[AK3S runtime testing guide](../../docs/runtime-testing.md).
