# Update the existing PostgreSQL server

Run these commands yourself in WSL Bash from the repository root. They select
production explicitly. Keep the existing `psql-server-auth` Secret and
`psql-server-data` PVC; this update retains the database image, data, and passwords.
It adds public TCP `5432`, mandatory TLS and SCRAM authentication for TCP clients,
and automatic certificate reloads. The single database replica restarts briefly.

## 1. Check the current server and prepare access

Back up the database before updating. Confirm the intended cluster, readiness,
and login with your existing administrator password:

```bash
kubectl --kubeconfig "$HOME/.kube/ak3s-hetzner.yaml" get nodes
kubectl --kubeconfig "$HOME/.kube/ak3s-hetzner.yaml" -n infrastructure \
  rollout status statefulset/psql-server --timeout=300s
kubectl --kubeconfig "$HOME/.kube/ak3s-hetzner.yaml" -n infrastructure \
  exec -it psql-server-0 -- psql -h 127.0.0.1 -U postgres -d postgres -W \
    -c 'SELECT current_user, current_database();'
```

Resolve any failure before continuing. Create a DNS A record for your chosen
hostname, such as `psql.ak3s.example.com`, pointing at your VPS's IPv4. With
Cloudflare, select **DNS only**. Keep port `80` available for certificate
validation. Allow TCP `5432` only from your client IPs in the provider firewall
and host Kubernetes forwarding rules; a host INPUT rule alone may not filter
ServiceLB traffic. Port `5432` must be free on the VPS.

All TCP clients, including cluster applications, must use TLS after this update.
Existing SCRAM passwords work unchanged; reset older MD5 passwords to SCRAM
before updating. Changing the Kubernetes password Secret does not rotate an
initialized database's password. The certificate covers your public hostname;
clients using `verify-full` must use that hostname. On a multi-node cluster,
point DNS at the node running PostgreSQL because the Service uses
`externalTrafficPolicy: Local`.

## 2. Set your hostname and apply

Copy the manifests into the ignored `.postgres/deployment/` directory. Replace
`REPLACE_WITH_POSTGRES_HOSTNAME` in its `certificate.yaml` with your chosen DNS
name. Keep real DNS names and operator settings out of tracked files:

```bash
mkdir -p .postgres/deployment
cp applications/postgres/*.yaml applications/postgres/*.conf \
  applications/postgres/*.sh .postgres/deployment/
```

After editing the local certificate file, issue the certificate first. This uses
AK3S's existing `letsencrypt-production` ClusterIssuer and NGINX HTTP validation:

```bash
kubectl --kubeconfig "$HOME/.kube/ak3s-hetzner.yaml" \
  apply -f .postgres/deployment/certificate.yaml
kubectl --kubeconfig "$HOME/.kube/ak3s-hetzner.yaml" -n infrastructure \
  wait --for=condition=Ready certificate/psql-server-tls --timeout=300s
```

Continue only when the certificate is Ready:

```bash
kubectl --kubeconfig "$HOME/.kube/ak3s-hetzner.yaml" \
  apply --dry-run=server -k .postgres/deployment
kubectl --kubeconfig "$HOME/.kube/ak3s-hetzner.yaml" apply -k .postgres/deployment
kubectl --kubeconfig "$HOME/.kube/ak3s-hetzner.yaml" -n infrastructure \
  rollout status statefulset/psql-server --timeout=300s
```

K3s ServiceLB exposes the VPS's port `5432`. Keep NodePort allocation enabled:
ServiceLB uses the allocated NodePort with `externalTrafficPolicy: Local`.
Clients connect on `5432`; keep other public ports restricted in the firewall.
The existing private Services remain available. Certificate renewal reloads
PostgreSQL without a restart after Kubernetes projects the updated Secret.

In pgAdmin, use your hostname, port `5432`, maintenance database `postgres`, your
existing database account and password, and SSL mode **verify-full** with a trusted
CA bundle for the issuer. Enter the hostname without `https://`. Verify your
connection in the Query Tool:

```sql
SELECT ssl FROM pg_stat_ssl WHERE pid = pg_backend_pid();
```

Expect `true`. Also verify that plaintext connections fail and clients outside
the firewall allowlist cannot reach the port.

## Later manifest changes

Refresh the private copy from `applications/postgres/`, restore your hostname in
`certificate.yaml`, and repeat step 2. `kubectl apply` reconciles resources with the same name
and namespace; Pod template changes restart PostgreSQL. It does not delete
resources removed from YAML, migrate data for PostgreSQL major upgrades, or
rotate database passwords. Keep the current PVC and Secret, and avoid changing
immutable identity or storage fields. If interrupted, fix the error and reapply
the same configuration.

To close public access, close its firewall rule and delete
`service/psql-server-external` in `infrastructure`. Remove that Service document
from your private copy before later applies, which would recreate it. TLS stays
enabled for the remaining private Services.
