# Set up PostgreSQL from WSL

Run these commands in **WSL Bash from the repository root**. You need `kubectl`
and a working `~/.kube/ak3s-hetzner.yaml` with access to the live production cluster.

The cluster needs the `local-path` storage provisioner and compatible VPA CRDs
and controllers. The manifest uses VPA `InPlace`, requiring VPA 1.7 or later with
`--feature-gates=InPlace=true` on the updater and admission controller. These
manifests configure VPA; they do not install it.

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

For the first installation, run this block and enter your chosen password. It
creates the `psql-server-auth` Secret without putting the password in shell history
or command arguments, then removes the private temporary file.

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

`-k` uses `kustomization.yaml` to apply `pvc.yaml`, `service.yaml`, and
`statefulset.yaml` in `infrastructure`, including both Services and the VPA.
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
