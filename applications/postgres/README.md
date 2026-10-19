# Manually deploy PostgreSQL on a VPS

Use these manifests to install PostgreSQL into an **already running AK3S or K3s
cluster** on Hetzner Cloud or another VPS provider. Run Kubernetes commands yourself
over SSH on the server. This procedure uses the K3s installation you already have.

Your workstation needs SSH and SCP to transfer the files. The server needs K3s,
Bash, and root or sudo access. Make, Go, Helm, and application CLI commands are
not required. Kustomize, which combines the YAML files, is included in
`k3s kubectl`.

## What will be installed

| Resource or setting | Default |
| --- | --- |
| Namespace | `applications`, created separately |
| PostgreSQL image | Official PostgreSQL `18.6-bookworm`, pinned by digest |
| Database | `app` |
| Initial administrator | `postgres` |
| Credential Secret | `postgres-auth`, with a `password` key, created separately |
| StatefulSet and pod | `postgres` and `postgres-0`, one replica |
| Persistent volume claim (PVC) | `postgres-data`, requesting `10Gi` on `local-path` |
| Client Service | `postgres`, private ClusterIP, port `5432` |
| StatefulSet Service | `postgres-headless`, headless Service, port `5432` |
| CPU request | `100m`, one tenth of a CPU core |
| Memory | `256Mi` requested, `1Gi` limit |

The image supports Linux amd64 and arm64. Data mounts at `/var/lib/postgresql`,
with database files under `/var/lib/postgresql/18/docker`, following the official
PostgreSQL 18 image layout.

This is a single database instance using the server's local disk. It has no
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

## 1. Copy the directory to your server

Run on your **workstation**, from the repository root. Replace `USER@VPS_ADDRESS`
with your SSH account and VPS address. Use your usual SSH key; add `-i PATH` to
SSH and SCP if you select a key explicitly.

```bash
ssh USER@VPS_ADDRESS 'mkdir -p ~/deployments'
scp -r applications/postgres USER@VPS_ADDRESS:deployments/
ssh USER@VPS_ADDRESS
```

Run the remaining deployment commands on the **VPS**, unless a section explicitly
says otherwise:

```bash
cd ~/deployments/postgres
ls
```

Expect this README and the four YAML files listed above. The server does not need
the rest of the repository. `apply -k .` reads the files listed in
`kustomization.yaml`; it does not apply the README.

For later updates, compare new files with your server copies before replacing
them. Preserve your database settings and operator changes.

## 2. Check the existing cluster and available capacity

```bash
sudo systemctl is-active k3s
sudo k3s kubectl --kubeconfig /etc/rancher/k3s/k3s.yaml get nodes -o wide
sudo k3s kubectl --kubeconfig /etc/rancher/k3s/k3s.yaml get storageclass local-path
free -h
df -h /var/lib/rancher/k3s
```

Expect K3s to be active and the server node to be Ready. Resolve cluster failures
before deploying the database. The commands explicitly use the local K3s
kubeconfig instead of a workstation's current Kubernetes context.

Check that the server has enough disk and memory for PostgreSQL alongside its
existing workloads. The storage request does not reserve VPS disk space in
advance, and local-path volumes generally do not enforce the requested size as a
disk quota. Monitor actual disk usage.

If `local-path` is missing, inspect `get storageclass` and choose an installed
storage class before applying. Do not create an empty storage-class definition
and expect it to provision storage.

For this SSH-based procedure, no new public database port or Kubernetes API access
from your workstation is needed. Keep TCP `5432` and the example local forward
port `15432` private. Review both your provider firewall and host firewall;
existing cluster networking and outbound image downloads must continue to work.

## 3. Review the configuration

Edit the **copied files on the server** using your preferred editor. Keep these
operator changes outside the repository's tracked defaults.

| Change | Location |
| --- | --- |
| Storage capacity and class | `pvc.yaml`: `resources.requests.storage` and `storageClassName` |
| Initial database name | `statefulset.yaml`: `POSTGRES_DB` |
| Initial administrator name | `statefulset.yaml`: `POSTGRES_USER` |
| Credential Secret name | `statefulset.yaml`: `secretKeyRef.name` |
| CPU and memory | `statefulset.yaml`: `resources` |
| Namespace | `kustomization.yaml`: `namespace` |

The instructions below use the defaults. If you change the namespace, database,
administrator, or Secret name, use the same values in subsequent commands.

Choose storage settings before installation. Do not assume that `local-path`
supports resizing an existing volume. A storage-class change can require moving
data into another PVC.

Keep `replicas: 1`. Increasing this StatefulSet's replica count does not configure
PostgreSQL replication and can create unsafe access to the shared data directory.

Keep the image pinned by version and digest. The image and `PGDATA` directory must
match the PostgreSQL major version; a major upgrade requires a planned database
migration.

`POSTGRES_DB`, `POSTGRES_USER`, and `POSTGRES_PASSWORD` initialize an **empty** data
directory. Editing these values later does not rename existing databases or users
or change the password stored by PostgreSQL.

## 4. Create the namespace and credentials

Create the namespace, or retain it if it already exists:

```bash
sudo k3s kubectl --kubeconfig /etc/rancher/k3s/k3s.yaml create namespace applications \
  --dry-run=client -o yaml | sudo k3s kubectl --kubeconfig /etc/rancher/k3s/k3s.yaml apply -f -
```

Check for existing workloads before using these resource names:

```bash
sudo k3s kubectl --kubeconfig /etc/rancher/k3s/k3s.yaml -n applications \
  get statefulsets,pods,pvc,services
```

If `postgres`, `postgres-data`, or either PostgreSQL Service already belongs to
another deployment, stop and review that deployment. These manifests do not adopt
an existing Helm release. The earlier AK3S Helm recipe used different names;
applying this directory creates a separate instance. Preserve existing data and
plan a migration before removing an old instance.

For a first installation, run this block in **Bash on the server**. It prompts for
a strong administrator password without placing it in shell history or command
arguments, creates the Secret, and removes the private temporary password file.

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
  sudo k3s kubectl --kubeconfig /etc/rancher/k3s/k3s.yaml -n applications \
    create secret generic postgres-auth --from-file=password="$postgres_password_file"
)
```

Save the password in your password manager. If `postgres-auth` already exists for
this database, reuse it. Do not delete or overwrite credentials to resolve an
installation error. Never commit a Secret manifest containing password values;
base64 encoding does not make a password confidential.

Verify that the Secret exists without printing its contents:

```bash
sudo k3s kubectl --kubeconfig /etc/rancher/k3s/k3s.yaml -n applications \
  get secret postgres-auth
```

## 5. Preview and validate

From `~/deployments/postgres` on the server:

```bash
k3s kubectl kustomize .
sudo k3s kubectl --kubeconfig /etc/rancher/k3s/k3s.yaml apply --dry-run=server -k .
```

The first command renders the four resources without contacting the cluster.
Check the namespace, image, credential reference, and storage settings. The second
validates the resources against the Kubernetes API without saving them. It does
not prove that the database can start or that the disk has sufficient capacity.

For an update to an existing deployment, inspect differences before applying:

```bash
sudo k3s kubectl --kubeconfig /etc/rancher/k3s/k3s.yaml diff -k .
```

`diff` returns exit status `1` when differences exist, `0` when there are none, and
a higher status for an error. Review the changes before proceeding. Before
reapplying to an existing database, confirm its PVC still contains the intended
data. Reapplying after storage was deleted can create an empty database.

## 6. Apply and wait for readiness

Run each command separately. Stop and investigate if one fails:

```bash
sudo k3s kubectl --kubeconfig /etc/rancher/k3s/k3s.yaml apply -k .
sudo k3s kubectl --kubeconfig /etc/rancher/k3s/k3s.yaml -n applications \
  rollout status statefulset/postgres --timeout=300s
sudo k3s kubectl --kubeconfig /etc/rancher/k3s/k3s.yaml -n applications \
  wait '--for=jsonpath={.status.phase}=Bound' pvc/postgres-data --timeout=180s
sudo k3s kubectl --kubeconfig /etc/rancher/k3s/k3s.yaml -n applications \
  get statefulset/postgres pod/postgres-0 pvc/postgres-data service/postgres service/postgres-headless
```

Expect the StatefulSet and pod to be ready, the PVC to be Bound, and both Services
to have no external IP. The headless Service has `CLUSTER-IP` set to `None`.

A local-path PVC may be Pending until a pod requests it, because its storage class
can use `WaitForFirstConsumer`. Apply the complete directory before judging volume
binding. The first startup can take longer while the image downloads and the
database initializes.

Kubernetes applies resources individually. A failed apply or rollout can leave
some resources created; investigate and rerun after fixing the cause. Reapplying
the same manifests updates the same named resources.

## 7. Test authentication and connect applications

Use the PostgreSQL client already in the database container:

```bash
sudo k3s kubectl --kubeconfig /etc/rancher/k3s/k3s.yaml -n applications \
  exec -it postgres-0 -- psql -h 127.0.0.1 -U postgres -d app -W
```

Enter the password from step 4. In the SQL session:

```sql
SELECT current_database(), current_user, version();
```

Expect database `app`, user `postgres`, and the pinned PostgreSQL version. Exit
with `\q`.

Applications running inside Kubernetes use:

| Connection field | Value |
| --- | --- |
| Host | `postgres.applications.svc.cluster.local` |
| Port | `5432` |
| Database | `app` |
| User and password | A separate application account |

The initial `postgres` account is a superuser. Create a separate login and grant
only the schema, table, and sequence permissions your application requires. Use
psql's `\password app_user` command to set its password interactively. Database
schema migrations may need a separate account with broader permissions. Store
application credentials in that application's own Secret, outside tracked YAML.

## 8. Connect from your workstation through SSH

In one **server terminal**, start a loopback-only Kubernetes port-forward:

```bash
sudo k3s kubectl --kubeconfig /etc/rancher/k3s/k3s.yaml -n applications \
  port-forward --address 127.0.0.1 service/postgres 15432:5432
```

Keep it running. In a separate **workstation terminal**, start an SSH tunnel:

```bash
ssh -o ExitOnForwardFailure=yes -N -L 15432:127.0.0.1:15432 USER@VPS_ADDRESS
```

Connect psql, pgAdmin, DBeaver, or another database tool to `127.0.0.1:15432`, with
database `app` and the appropriate account. If psql is installed on your
workstation:

```bash
psql -h 127.0.0.1 -p 15432 -U postgres -d app -W
```

Stop the tunnel and port-forward with Ctrl+C when finished. The port-forward can
exit after pod replacement; start it again when the replacement pod is ready.
If local port `15432` is occupied, choose another workstation port, such as
`25432`, in the left side of SSH's `-L` option and in your database tool.

The forwarding connection is encrypted by SSH. This recipe does not configure
PostgreSQL TLS for traffic between pods.

## 9. Make backups and test restoration

Local-path storage survives pod replacement on the same node. It does not protect
against disk failure, VPS loss, namespace deletion, or PVC deletion. A K3s datastore
backup does not include PostgreSQL volume contents.

For a manual logical backup of database `app`, run on the **server**:

```bash
umask 077
mkdir -p ~/backups/postgres
chmod 700 ~/backups/postgres
postgres_backup_file="$HOME/backups/postgres/app-$(date -u +%Y%m%dT%H%M%SZ).dump"
sudo k3s kubectl --kubeconfig /etc/rancher/k3s/k3s.yaml -n applications \
  exec postgres-0 -- pg_dump -U postgres -d app --format=custom > "$postgres_backup_file"
```

Check that `pg_dump` exited successfully before treating the file as a backup.
Do not add `-t` to this command; the output is a binary archive. It uses the local
database connection inside the pod under the official image's default local
authentication configuration.

Check that PostgreSQL can read the archive:

```bash
sudo k3s kubectl --kubeconfig /etc/rancher/k3s/k3s.yaml -n applications \
  exec -i postgres-0 -- pg_restore --list < "$postgres_backup_file"
```

Archive listing is a format check, not a restoration test. The archive contains
one database; it does not include cluster-wide roles. If your restore plan needs
roles and their settings, also save globals privately:

```bash
postgres_roles_file="${postgres_backup_file%.dump}-roles.sql"
sudo k3s kubectl --kubeconfig /etc/rancher/k3s/k3s.yaml -n applications \
  exec postgres-0 -- pg_dumpall -U postgres --globals-only > "$postgres_roles_file"
```

The globals file can contain password hashes. Keep both files private, protect
their transfer and storage, and retain the credential recovery information needed
by your restore plan.

Copy backups **outside the server**. For example, on your **workstation**, replace
the example timestamp with the actual filenames:

```bash
mkdir -p ~/private-backups/postgres
chmod 700 ~/private-backups/postgres
scp USER@VPS_ADDRESS:backups/postgres/app-YYYYMMDDTHHMMSSZ.dump ~/private-backups/postgres/
scp USER@VPS_ADDRESS:backups/postgres/app-YYYYMMDDTHHMMSSZ-roles.sql ~/private-backups/postgres/
```

Test restoration on a **separate disposable server or database instance**. Deploy
the recipe there, transfer the archive privately, and create an empty test database:

```bash
sudo k3s kubectl --kubeconfig /etc/rancher/k3s/k3s.yaml -n applications \
  exec postgres-0 -- createdb -U postgres restore_check
sudo k3s kubectl --kubeconfig /etc/rancher/k3s/k3s.yaml -n applications \
  exec -i postgres-0 -- pg_restore -U postgres -d restore_check \
  --no-owner --no-acl --exit-on-error < app-YYYYMMDDTHHMMSSZ.dump
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
Kubernetes Secret. In a separate server terminal, enter the same new password:

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
  sudo k3s kubectl --kubeconfig /etc/rancher/k3s/k3s.yaml -n applications \
    create secret generic postgres-auth --from-file=password="$postgres_password_file" \
    --dry-run=client -o yaml | \
    sudo k3s kubectl --kubeconfig /etc/rancher/k3s/k3s.yaml apply -f -
)
```

The Secret YAML passes directly between commands rather than being saved or
printed. Changing the Secret alone does not change an existing database password.
Running pods also retain their old Secret-derived environment values until
restart; PostgreSQL authentication uses the password stored in the database.
Update affected clients and test a new TCP connection before ending the
maintenance session.

## 11. Stop or remove PostgreSQL while retaining data

To temporarily stop the database:

```bash
sudo k3s kubectl --kubeconfig /etc/rancher/k3s/k3s.yaml -n applications \
  scale statefulset/postgres --replicas=0
```

To start it again:

```bash
sudo k3s kubectl --kubeconfig /etc/rancher/k3s/k3s.yaml -n applications \
  scale statefulset/postgres --replicas=1
sudo k3s kubectl --kubeconfig /etc/rancher/k3s/k3s.yaml -n applications \
  rollout status statefulset/postgres --timeout=300s
```

The manifest declares one replica, so reapplying it also restores that desired
replica count. To remove the workload and Services while retaining the PVC and
Secret:

```bash
sudo k3s kubectl --kubeconfig /etc/rancher/k3s/k3s.yaml -n applications \
  delete statefulset/postgres service/postgres service/postgres-headless
```

**Do not use `kubectl delete -k .` to stop PostgreSQL.** The directory includes
the PVC, and deleting it can delete its volume contents. Deleting namespace
`applications` removes all resources in that namespace. Full K3s removal also
removes local-path volumes. Verify an independent backup before deleting data.

## Troubleshooting

Run these on the server:

```bash
sudo k3s kubectl --kubeconfig /etc/rancher/k3s/k3s.yaml -n applications describe pod postgres-0
sudo k3s kubectl --kubeconfig /etc/rancher/k3s/k3s.yaml -n applications logs postgres-0 --tail=100
sudo k3s kubectl --kubeconfig /etc/rancher/k3s/k3s.yaml -n applications describe pvc postgres-data
sudo k3s kubectl --kubeconfig /etc/rancher/k3s/k3s.yaml -n applications \
  get events --sort-by=.metadata.creationTimestamp
```

Keep logs and diagnostic output private if they contain application information.

| Symptom | What to check |
| --- | --- |
| K3s API is unavailable | Service state, node readiness, local kubeconfig, and `sudo journalctl -u k3s -n 100` |
| `CreateContainerConfigError` | The `postgres-auth` Secret exists in `applications` and has a nonempty `password` key |
| `ImagePullBackOff` | Pod events, outbound DNS and HTTPS, registry access, and the complete pinned image reference |
| Pod Pending | Scheduling events, CPU and memory capacity, node taints, and PVC binding |
| PVC Pending after apply | Storage class, local-path provisioner, disk capacity, and pod scheduling; `WaitForFirstConsumer` needs a schedulable pod |
| `CrashLoopBackOff` | Current and previous logs, image/data-version compatibility, volume permissions, and available disk space |
| Pod is `OOMKilled` | Memory use and the `1Gi` limit; adjust capacity after checking server headroom |
| Password authentication fails | The database's actual user/password; changing the Secret does not rotate an initialized password |
| Application cannot connect | Service DNS, namespace, selected pod readiness, application credentials, and NetworkPolicies |
| Workstation cannot connect | Both forwarding commands are running and the database tool uses the local forwarded port |

For a restarted container, inspect its previous logs:

```bash
sudo k3s kubectl --kubeconfig /etc/rancher/k3s/k3s.yaml -n applications \
  logs postgres-0 --previous --tail=100
```

## Runtime validation

On a disposable cluster, test database persistence as well as readiness:

1. Deploy and run an authenticated query.
2. Write a test row and record the PVC UID with
   `sudo k3s kubectl --kubeconfig /etc/rancher/k3s/k3s.yaml -n applications get pvc postgres-data -o jsonpath='{.metadata.uid}'`.
3. Delete only the pod with
   `sudo k3s kubectl --kubeconfig /etc/rancher/k3s/k3s.yaml -n applications delete pod postgres-0`.
4. Wait for the replacement to become ready, reconnect, and verify the row remains.
5. Reapply the directory and confirm the PVC UID and row are unchanged.
6. Restore an off-server backup into a separate instance and verify its contents.

Rendered manifests and API validation do not prove database startup, filesystem
permissions, persistence, or recovery. Record automated checks separately from
live validation. In a repository checkout, see the broader
[AK3S runtime testing guide](../../docs/runtime-testing.md).
