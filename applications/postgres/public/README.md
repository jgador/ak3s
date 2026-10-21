# Public PostgreSQL access

This optional Kustomize overlay exposes the existing `psql-server` on TCP `5432`
through K3s ServiceLB. It keeps the same StatefulSet, password Secret, PVC, and
private Services. The default `applications/postgres` deployment stays private.

The overlay enables PostgreSQL TLS, requires SCRAM password authentication for
every TCP connection, and reloads certificates when cert-manager updates its
Secret. Local Unix socket access remains available for `kubectl exec` and the
official image's database initialization. The external Service selects only pods
configured by this overlay, so an old pod without TLS is not exposed during the
switch.

Run the commands below yourself from **WSL Bash at the repository root**. They
explicitly select the production kubeconfig. A repository change does not deploy
to the cluster or change DNS or firewall rules.

## 1. Prepare DNS, firewall rules, and database clients

Back up the database before changing its configuration. Switching an existing
installation restarts its single replica and briefly interrupts connections.
First complete the [private deployment](../README.md), including VPA and the
administrator password Secret, if PostgreSQL is not installed yet.

Create an A record for `psql.ak3s.ayumlabs.com` pointing to your VPS's public IPv4.
For an `ayumlabs.com` DNS zone, the record name is `psql.ak3s`. With Cloudflare,
select **DNS only**. Remove stale AAAA records if IPv6 is unavailable. Keep TCP
`80` available for Let's Encrypt HTTP-01 validation through the existing NGINX
controller. PostgreSQL traffic uses its own TCP Service; no HTTP Ingress is needed.

Before applying the overlay, allow inbound TCP `5432` only from your client IPs
in the provider firewall. Apply the equivalent restrictions to the host's
Kubernetes forwarding rules. A simple host INPUT rule may not filter traffic
forwarded by ServiceLB; verify the effective rules. Port `5432` must be free on
the VPS. This Service disables automatic NodePort allocation, so opening the
Kubernetes NodePort range is unnecessary.

The Service uses `externalTrafficPolicy: Local`. On a multi-node cluster, its DNS
must point at the node running PostgreSQL; another node has no local database
endpoint. ServiceLB does not provision a cloud load balancer or a public address.
Any cluster NetworkPolicies must also allow the intended PostgreSQL clients.

All TCP clients, including applications inside Kubernetes, must now use TLS.
Existing SCRAM passwords continue working. Older MD5 password verifiers must be
replaced with SCRAM verifiers before enabling this overlay. Changing
`psql-server-auth` alone does not rotate an initialized database's password.

For hostname verification, clients must connect using a name on the certificate.
The example certificate covers the public hostname. If applications verify
`psql-server.infrastructure.svc.cluster.local`, add that name to a certificate
issued by a private CA and distribute its trust root; Let's Encrypt cannot issue
certificates for Kubernetes-internal DNS names. Keep application credentials
separate from the administrator account.

## 2. Create an operator overlay and issue the certificate

Keep your real hostname and overrides outside tracked defaults. Create an
operator overlay under the repository's ignored `.tmp/` directory, retaining a
private copy elsewhere for future deployments. It contains no password:

```bash
mkdir -p .tmp/postgres-public
cat > .tmp/postgres-public/kustomization.yaml <<'YAML'
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
namespace: infrastructure
resources:
  - ../../applications/postgres/public
patches:
  - path: certificate.yaml
YAML

cat > .tmp/postgres-public/certificate.yaml <<'YAML'
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: psql-server-tls
  namespace: infrastructure
spec:
  secretName: psql-server-tls
  dnsNames:
    - psql.ak3s.ayumlabs.com
  issuerRef:
    name: letsencrypt-production
    kind: ClusterIssuer
  privateKey:
    rotationPolicy: Always
YAML
```

The tracked example uses `psql.ak3s.example.com`. The operator patch above sets
your real hostname. Select a different issuer in `certificate.yaml` if your
cluster does not have `letsencrypt-production`.

Confirm the intended cluster and issuer, then apply only the Certificate so its
Secret exists before the StatefulSet changes:

```bash
kubectl --kubeconfig "$HOME/.kube/ak3s-hetzner.yaml" get nodes
kubectl --kubeconfig "$HOME/.kube/ak3s-hetzner.yaml" \
  wait --for=condition=Ready clusterissuer/letsencrypt-production --timeout=120s

kubectl --kubeconfig "$HOME/.kube/ak3s-hetzner.yaml" \
  apply -f .tmp/postgres-public/certificate.yaml
kubectl --kubeconfig "$HOME/.kube/ak3s-hetzner.yaml" -n infrastructure \
  wait --for=condition=Ready certificate/psql-server-tls --timeout=300s
```

If certificate issuance fails, resolve DNS, port `80`, or issuer readiness before
continuing. Do not enable public access with a staging or untrusted certificate.

## 3. Apply the public deployment

Preview the rendered resources, validate them, and apply the operator overlay:

```bash
kubectl kustomize .tmp/postgres-public
kubectl --kubeconfig "$HOME/.kube/ak3s-hetzner.yaml" \
  apply --dry-run=server -k .tmp/postgres-public
kubectl --kubeconfig "$HOME/.kube/ak3s-hetzner.yaml" apply -k .tmp/postgres-public
kubectl --kubeconfig "$HOME/.kube/ak3s-hetzner.yaml" -n infrastructure \
  rollout status statefulset/psql-server --timeout=300s
kubectl --kubeconfig "$HOME/.kube/ak3s-hetzner.yaml" -n infrastructure \
  get service psql-server-external
```

After enabling this overlay, use `.tmp/postgres-public` for subsequent applies.
Applying the private base changes the StatefulSet back to its private
configuration. A plain `kubectl apply` does not delete extra resources.

The certificate Secret is mounted read-only without `subPath`, allowing Kubernetes
to project renewals. Its root-owned private key has mode `0440` and is readable by
the pod's group `999`. The launcher retains the official image's entrypoint and
sends PostgreSQL a reload when the mounted Secret generation changes. Detection
runs every 30 seconds, after Kubernetes projects the updated Secret. Existing
database sessions remain connected; new sessions use the renewed certificate.

## 4. Connect and verify

Register the server in pgAdmin:

| Setting | Value |
| --- | --- |
| Name | `AK3S PostgreSQL` |
| Host name/address | `psql.ak3s.ayumlabs.com` |
| Port | `5432` |
| Maintenance database | `postgres` |
| Username | Your database account; `postgres` for administration |
| Password | That account's existing password |
| SSL mode | `verify-full` |
| Root certificate | A trusted CA bundle covering the Let's Encrypt issuer |

Configure the root certificate on the machine running pgAdmin's server process.
Use a trusted CA bundle from your operating system or the issuing CA. Do not
disable verification to resolve a trust error. Enter the hostname without
`https://`; the dashboard's certificate is independent from PostgreSQL TLS.

In pgAdmin's Query Tool, verify the current connection:

```sql
SELECT ssl, version, cipher
FROM pg_stat_ssl
WHERE pid = pg_backend_pid();
```

The `ssl` column must be `true`. Also check that a connection with SSL disabled
fails and that a machine outside your firewall allowlist cannot reach the port.
Check this from the actual external clients; local tests do not prove the
provider firewall, public DNS, ServiceLB, or public certificate issuance works.

## Disable public access

Close the firewall allowance, delete the external Service, then reapply the
private deployment. This retains the database PVC and credentials but restarts
PostgreSQL:

```bash
kubectl --kubeconfig "$HOME/.kube/ak3s-hetzner.yaml" -n infrastructure \
  delete service psql-server-external
kubectl --kubeconfig "$HOME/.kube/ak3s-hetzner.yaml" apply -k applications/postgres
kubectl --kubeconfig "$HOME/.kube/ak3s-hetzner.yaml" -n infrastructure \
  rollout status statefulset/psql-server --timeout=300s
```

The Certificate and generated ConfigMaps remain unless you remove them separately.
If you previously created a NodePort Service for this database, remove it and
its firewall rule as well; do not leave a second public connection path.
