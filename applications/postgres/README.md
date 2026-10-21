# Set up PostgreSQL from WSL

This is the first-installation guide for **revision 001**, the private deployment
already merged into `master`. See the [deployment history](revisions/README.md)
for numbered revisions. If the original manifests are already applied, retain
the existing Secret and PVC and start with the checks in the
[001 to 002 upgrade guide](upgrades/001-to-002.md) to add public TLS access.

Run these commands in **WSL Bash from the repository root**. You need `kubectl`
and a working `~/.kube/ak3s-hetzner.yaml` with access to the live production cluster.

The cluster needs the `local-path` storage provisioner. Before deploying, follow
[Install VPA from WSL](../../docs/vpa.md) once per cluster. PostgreSQL uses VPA
`InPlace`; its manifest does not install the required CRDs and controllers.

## 1. Check access and create the namespace

Confirm that the nodes belong to your intended production cluster:

```bash
kubectl --kubeconfig "$HOME/.kube/ak3s-hetzner.yaml" get nodes
```

Create `infrastructure`, or retain it if it already exists:

```bash
kubectl --kubeconfig "$HOME/.kube/ak3s-hetzner.yaml" create namespace infrastructure \
  --dry-run=client -o yaml | kubectl --kubeconfig "$HOME/.kube/ak3s-hetzner.yaml" apply -f -
```

## 2. Set the administrator password

For the first installation, replace `REPLACE_WITH_YOUR_PASSWORD` in the block below
with your chosen password, then run it. Keep the single quotes; if the password
contains a single quote, use `'\''` for that character inside the quoted value.
The block creates the `psql-server-auth` Secret and removes the private temporary
file. Pasting a password into a command can save it in shell history; keep the
edited block private and do not save it in this repository.

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
  postgres_password='REPLACE_WITH_YOUR_PASSWORD'
  if [[ -z "$postgres_password" || "$postgres_password" == 'REPLACE_WITH_YOUR_PASSWORD' ]]; then
    printf 'Replace the password placeholder before running this block.\n' >&2
    exit 1
  fi
  printf '%s' "$postgres_password" > "$postgres_password_file"
  unset postgres_password
  kubectl --kubeconfig "$HOME/.kube/ak3s-hetzner.yaml" -n infrastructure \
    create secret generic psql-server-auth --from-file=password="$postgres_password_file"
)
```

Save the password in your password manager. If this server's Secret already
exists, reuse it and skip this step. PostgreSQL reads this password when
initializing an empty data directory; changing the Secret later does not rotate
the database password.

## 3. Deploy PostgreSQL

Apply all the manifests with one command:

```bash
kubectl --kubeconfig "$HOME/.kube/ak3s-hetzner.yaml" apply -k applications/postgres
```

`-k` uses `kustomization.yaml` to apply `revisions/001-private/` to
`infrastructure`, including both Services and the VPA.
You do not need to apply each file separately.

Wait for PostgreSQL to become ready:

```bash
kubectl --kubeconfig "$HOME/.kube/ak3s-hetzner.yaml" -n infrastructure \
  rollout status statefulset/psql-server --timeout=300s
```

Verify login using the password from step 2:

```bash
kubectl --kubeconfig "$HOME/.kube/ak3s-hetzner.yaml" -n infrastructure \
  exec -it psql-server-0 -- psql -h 127.0.0.1 -U postgres -d postgres -W
```

Exit with `\q`. Applications inside Kubernetes connect to
`psql-server.infrastructure.svc.cluster.local:5432` using their own database accounts.

## Optional public access

For direct pgAdmin access through a hostname such as `psql.ak3s.ayumlabs.com`,
follow the [001 to 002 upgrade guide](upgrades/001-to-002.md). Revision 002 adds
a ServiceLB Service on port `5432`, a separate cert-manager certificate, mandatory
TLS and SCRAM authentication for TCP clients, and certificate reloads. Public
access also requires DNS and firewall rules limited to your client IPs. The
private deployment above does not expose PostgreSQL publicly.
