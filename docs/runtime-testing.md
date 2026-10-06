# Runtime testing on WSL2 or a VPS

Run this checklist after a real AK3S installation in the [WSL2 test environment](wsl-testing.md) or on a disposable [VPS](vps.md). It applies to both published releases and local builds before release. When comparing WSL2 and VPS results, use the same AK3S release or source revision and platform defaults in both. Run all Bash commands on the server being tested; they change the local cluster and deploy a disposable demo.

Use the local kubeconfig (Kubernetes client configuration) explicitly so a workstation context cannot select another cluster. Define this helper again in each testing terminal:

```bash
k() { sudo k3s kubectl --kubeconfig /etc/rancher/k3s/k3s.yaml "$@"; }
```

Record the AK3S version, operating system and kernel, resources, and which checks passed. Keep any logs and backup files private. A successful `ak3s status` reports resources; it does not prove every workload, ingress path, or backup works.

## 1. Confirm service and workload readiness

```bash
ak3s version
sudo systemctl is-enabled k3s
sudo systemctl is-active k3s
k get --raw=/readyz
k wait --for=condition=Ready nodes --all --timeout=300s
k get nodes -o wide
k get pods -A
k get pvc -A
k get svc -A
k get clusterissuers
k get ingressclass
```

Expect an enabled, active service, API response `ok`, nodes in the Ready state, an `nginx` IngressClass, and both Let's Encrypt ClusterIssuers. Their account readiness requires outbound connectivity to the ACME (Automated Certificate Management Environment) certificate service and a valid contact email. Confirm the persistent volume claims (PVCs) for VictoriaMetrics and VictoriaLogs are Bound, and their Services and Headlamp are `ClusterIP`.

Wait for all installed Deployments, DaemonSets, and StatefulSets to become ready:

```bash
for namespace in kube-system ingress-nginx cert-manager headlamp observability ak3s; do
  for resource in $(k -n "$namespace" get deployment,daemonset,statefulset -o name); do
    k -n "$namespace" rollout status "$resource" --timeout=300s || break 2
  done
done
```

Inspect `k get pods -A` again. Investigate missing platform workloads, pods in Pending or CrashLoopBackOff (waiting between repeated container failures), failed rollouts, or unbound PVCs before continuing. Completed K3s Jobs are normal. See [operations troubleshooting](operations.md#troubleshooting) for `logs` and `describe` commands.

## 2. Check ingress, cert-manager, and persistent storage

Review the [local demo](../examples/local-app.yaml) from the same release or checkout you installed. Use a fresh `demo` namespace; if it already holds another workload, use a new disposable server rather than overwriting it. For a published tag that includes the example:

```bash
mkdir -p ~/ak3s-test
cd ~/ak3s-test
VERSION=vX.Y.Z # replace with the installed release
curl -fL "https://raw.githubusercontent.com/jgador/ak3s/$VERSION/examples/local-app.yaml" -o local-app.yaml
export AK3S_LOCAL_APP_FILE="$PWD/local-app.yaml"
```

If testing from a checkout instead, run `export AK3S_LOCAL_APP_FILE="$PWD/examples/local-app.yaml"` from the repository root. Then review and deploy it:

```bash
less "$AK3S_LOCAL_APP_FILE"
k apply -f "$AK3S_LOCAL_APP_FILE"
k -n demo rollout status deployment/hello --timeout=300s
k -n demo wait --for=jsonpath='{.status.phase}'=Bound pvc/hello-data --timeout=180s
k -n demo wait --for=condition=Ready certificate/hello-local --timeout=180s
k -n demo get ingress hello-local
```

The manifest uses `hello.test`, a namespace-scoped self-signed issuer, a `1Gi` local-path volume, and a small NGINX workload. The hostname has no public DNS requirement. The certificate check tests real cert-manager issuance and NGINX TLS; it does not test ACME validation or browser trust.

Check cluster and upstream DNS from an actual pod:

```bash
k -n demo run dns-check --image=busybox:1.37.0 --restart=Never --command -- sh -c 'nslookup kubernetes.default.svc.cluster.local && nslookup example.com'
k -n demo wait --for=jsonpath='{.status.phase}'=Succeeded pod/dns-check --timeout=180s
k -n demo logs dns-check
k -n demo delete pod dns-check
```

Both names must resolve and the pod must succeed. If it fails, inspect its logs and CoreDNS before deleting it and continuing. This tests pod networking and DNS forwarding in addition to downloads from the host.

Set the primary IPv4 of the server being tested. In WSL2 this command selects the network address translation (NAT) address; on a VPS use the address reachable from that server for the local check:

```bash
export AK3S_TEST_ADDRESS=$(ip -4 route get 192.0.2.1 | awk '{for (i=1;i<=NF;i++) if ($i=="src") {print $(i+1); exit}}')
test -n "$AK3S_TEST_ADDRESS"
curl --noproxy '*' -sS -D - -o /dev/null --resolve "hello.test:80:$AK3S_TEST_ADDRESS" http://hello.test/
curl --noproxy '*' -fk --resolve "hello.test:443:$AK3S_TEST_ADDRESS" https://hello.test/
```

Expect an HTTP redirect to HTTPS and the page `ak3s test: persistent storage is working` over HTTPS. `--resolve` supplies the hostname for HTTP routing and TLS without changing DNS. TLS Server Name Indication (SNI) sends that hostname during the TLS handshake. `-k` is only for this local self-signed certificate. Host-port rules used by ServiceLB may not appear as listening sockets in `ss`; the request tests whether routing works.

Write a unique marker into the volume, replace the pod, then read it through ingress:

```bash
AK3S_STORAGE_MARKER="test-$(date +%s)"
k -n demo exec deployment/hello -- sh -c 'printf "%s\n" "$1" > /usr/share/nginx/html/marker.txt' sh "$AK3S_STORAGE_MARKER"
curl --noproxy '*' -fk --resolve "hello.test:443:$AK3S_TEST_ADDRESS" https://hello.test/marker.txt
k -n demo rollout restart deployment/hello
k -n demo rollout status deployment/hello --timeout=300s
curl --noproxy '*' -fk --resolve "hello.test:443:$AK3S_TEST_ADDRESS" https://hello.test/marker.txt
```

Both responses must match the recorded marker. Keep its value for service restart, WSL distribution or VPS restart, and restore checks. Local-path storage survives pod replacement on the same node; this check does not show replication or recovery after losing the server disk.

## 3. Check dashboard access and permissions

Check the deployed AK3S dashboard first:

```bash
k -n ak3s rollout status deployment/dashboard --timeout=180s
k -n ak3s get service/dashboard
k auth can-i list nodes --as=system:serviceaccount:ak3s:dashboard
k auth can-i list deployments.apps --all-namespaces --as=system:serviceaccount:ak3s:dashboard
k auth can-i get secrets -n demo --as=system:serviceaccount:ak3s:dashboard
k auth can-i create deployments.apps -n demo --as=system:serviceaccount:ak3s:dashboard
k -n ak3s port-forward --address 127.0.0.1 service/dashboard 5173:80
```

Expect a ready Deployment, a ClusterIP Service, `yes` for the two list checks,
and `no` for Secret access and writes. Open `http://127.0.0.1:5173`; check live
nodes, the configuration views, and refresh. From another terminal, confirm
`curl --noproxy '*' -fsS http://127.0.0.1:5173/api/snapshot` succeeds and
`curl --noproxy '*' -o /dev/null -s -w '%{http_code}\n' -H 'Origin: http://untrusted.example.com' http://127.0.0.1:5173/api/snapshot`
returns `403`. Keep snapshots private. Repeat after `ak3s apply` and pod
replacement to check configuration updates and recovery. Source builds require
[importing the local dashboard image](../dashboard/README.md#build-and-deploy-local-source).

For optional shared HTTPS access, follow [the setup guide](operations.md#shared-hostname)
on a disposable VPS with a real DNS hostname. After production certificate issuance:

- Confirm HTTP redirects to HTTPS and the HTTPS certificate is trusted without `-k`.
- Verify `/`, `/api/snapshot`, `/metrics/vmui/`, and `/logs/select/vmui/` return
  `401` without credentials and with an incorrect password. Use `curl --user USER`
  to prompt for a password without storing it in command history.
- Sign in through a browser. Verify live dashboard data, metrics queries, and log
  searches. Follow `/metrics` and `/logs` redirects and confirm their JavaScript,
  styles, and API requests remain under the correct prefixes.
- Open `/headlamp`, sign in with a temporary viewer token, and browse resources.
  Verify pod log streaming or a resource watch works through the proxy. Confirm
  the viewer still cannot read Secrets or change workloads.
- Rotate the login Secret and confirm the old password stops working after the
  mounted Secret updates. Check hostile Origin requests still return `403`.
- Repeat after `ak3s apply` and pod replacement. Then remove `dashboard_hostname`
  and apply again: the managed ingress must disappear, ordinary pods must be
  unable to reach the dashboard, and local port-forwards must still work.

Unit tests and Helm rendering do not validate public DNS, certificate issuance,
browser integration, or the cluster's NetworkPolicy enforcement.

Then check Headlamp:

```bash
k -n headlamp create token headlamp-viewer --duration=1h
k auth can-i get pods --all-namespaces --as=system:serviceaccount:headlamp:headlamp-viewer
k auth can-i get secrets -n demo --as=system:serviceaccount:headlamp:headlamp-viewer
k auth can-i create deployments.apps -n demo --as=system:serviceaccount:headlamp:headlamp-viewer
k auth can-i delete pods -n demo --as=system:serviceaccount:headlamp:headlamp-viewer
k -n headlamp port-forward service/headlamp 8080:80
```

Expect `yes` for reading pods and `no` for each Secret access or write check. The denied checks exit nonzero, so run them interactively rather than in a script that stops on the first denial. Open `http://127.0.0.1:8080`, sign in with the short-lived token, and confirm you can view workloads. Do not put the token in saved test notes.

In WSL2, use the Windows browser's localhost forwarding. For a remote VPS, use an SSH tunnel from your workstation while keeping the port-forward running on the server: `ssh -N -L 8080:127.0.0.1:8080 USER@VPS_ADDRESS`. Replace the placeholders with your own account and address. Do not publish Headlamp or change the port-forward bind address for this check.

## 4. Check resource metrics and stored metrics and logs

```bash
k top nodes
k -n demo top pods
```

Allow a few minutes after installation for metrics-server to collect samples. Persistent failure is a failed check, not a reason to skip it.

In a separate server terminal, define `k` and run:

```bash
k -n observability port-forward service/victoria-metrics 8428:8428
```

In the testing terminal:

```bash
curl --noproxy '*' -fsS --get http://127.0.0.1:8428/api/v1/query --data-urlencode 'query=up{job="victoria-metrics"}'
curl --noproxy '*' -fsS --get http://127.0.0.1:8428/api/v1/query --data-urlencode 'query=up{job="kubernetes-kubelet"}'
```

Expect JSON status `success`, a nonempty result, and sample value `1` for the current node's target. An empty result is not a pass. After collection starts, inspect `http://127.0.0.1:8428/vmui/` for current samples.

In another server terminal, define `k` and run:

```bash
k -n observability port-forward service/victoria-logs 9428:9428
```

Generate an identifiable access-log entry through ingress, then search for it:

```bash
AK3S_LOG_MARKER="test-log-$(date +%s)"
curl --noproxy '*' -fk --resolve "hello.test:443:$AK3S_TEST_ADDRESS" "https://hello.test/?marker=$AK3S_LOG_MARKER"
k -n demo logs deployment/hello --since=5m
curl --noproxy '*' -fsS http://127.0.0.1:9428/select/logsql/query --data-urlencode "query=_time:5m \"$AK3S_LOG_MARKER\"" --data-urlencode 'limit=10'
```

Allow up to a few minutes for ingestion and repeat the query with the same marker. Pass only when the access line appears both in container logs and in VictoriaLogs. A successful query with no rows is not a pass. Inspect `http://127.0.0.1:9428/select/vmui/` if needed. For remote browser access, use the same SSH-tunnel pattern with ports 8428 and 9428. Keep these Services private.

## 5. Check reruns and service recovery

```bash
sudo ak3s install --dry-run
sudo ak3s install
sudo ak3s apply --dry-run
sudo ak3s apply
sudo ak3s status
```

Both reruns must succeed without creating duplicate nodes or losing the storage marker. An unchanged plan should not propose a K3s configuration restart. Repeat the readiness, ingress, metrics, log, and marker checks after reconciliation.

Restart the service to exercise recovery:

```bash
sudo systemctl restart k3s
k get --raw=/readyz
k wait --for=condition=Ready nodes --all --timeout=300s
k -n demo rollout status deployment/hello --timeout=300s
curl --noproxy '*' -fk --resolve "hello.test:443:$AK3S_TEST_ADDRESS" https://hello.test/marker.txt
```

The API may briefly be unavailable; retry the readiness probe until it responds and rerun the full workload readiness check. The original storage marker must remain. Then perform the [WSL stop, start, and restore tests](wsl-testing.md#test-a-distribution-stop-and-start), or a disposable VPS reboot and restoration using your [backups stored outside the server](operations.md#backups).

## 6. Test interruption and upgrades

Use a disposable test environment or fresh VPS for intentional failures. Keep a verified backup and run only one restored copy at a time.

To test interrupted-run recovery, run `sudo ak3s apply`, press Ctrl+C during reconciliation, then rerun `sudo ak3s apply --dry-run` and `sudo ak3s apply`. If the command finished before interruption, repeat the test. Confirm readiness and the storage marker after recovery. To test interruption of a **first install**, use another clean disposable environment and interrupt `sudo ak3s install` there; rerun it with the same configuration. Do not delete ownership or state files to bypass a failure. Individual Helm releases can roll back; there is no whole-platform rollback.

For an actual upgrade test, start a fresh test environment or server on the published version you intend to upgrade from. Run this checklist and back up. Install the specific newer AK3S CLI using the release installer, or build and install the local source using [WSL2 path B](wsl-testing.md#b-test-local-source-before-release) on the same test environment. Replacing the CLI alone does not upgrade the cluster. Then:

```bash
ak3s version
sudo ak3s upgrade --dry-run
sudo ak3s upgrade
sudo ak3s status
```

Check that the intended pinned component versions changed, then repeat this checklist and backup restoration. Running `upgrade` with unchanged pinned component versions only tests command behavior; it does not validate a component version transition. If no previous release exists, record the upgrade transition as untested and run the first-install and recovery checks. AK3S refuses downgrades and skipped Kubernetes minor versions. For redundant control planes, upgrade one server at a time and verify etcd quorum (a majority of servers available); a single WSL distribution cannot prove those properties.

## Public VPS checks

Repeat checks 1–6 on a disposable VPS with its actual configuration. The local demo can test the same workload path there, but public networking and trusted certificates need the public-domain [app.yaml](../examples/app.yaml).

1. Apply the [provider and host firewall rules](vps.md#firewall-requirements). From your administrative workstation, verify SSH and authenticated Kubernetes API access. Confirm TCP 6443 is blocked from sources outside your administrative addresses. Keep dashboard, metrics, and log Services private.
2. Point a real hostname's DNS A record at the VPS's public IPv4 and remove stale AAAA records. Review `app.yaml`, replace every `hello.example.com` with that hostname, and apply it with `k apply -f app.yaml` after removing the local demo: `k delete -f "$AK3S_LOCAL_APP_FILE"`. This deletes the demo namespace and its PVC; save any demo results you need first.
3. Wait for the public deployment and `certificate/hello-tls` to become Ready in namespace `demo`. Use `k -n demo describe certificate hello-tls`, `k -n demo get orders,challenges`, and cert-manager logs to investigate failures. Verify HTTP and HTTPS from a client outside the VPS. HTTP-01 requires public TCP 80; the local self-signed check cannot validate it.
4. With staging working, change the ingress issuer annotation to `letsencrypt-production`. Wait for reissuance and confirm HTTPS with `curl -f https://YOUR_HOSTNAME/` **without `-k`**. Then enable `nginx.org/ssl-redirect: "true"` and confirm HTTP redirects. A staging certificate is intentionally untrusted and is not a production pass.
5. Reboot the VPS and repeat readiness and application checks. Restore your actual datastore, token, configuration, state, and application volumes into a separate disposable VPS using the pinned K3s restore procedure. A WSL export does not validate provider snapshots or restoration of volumes from backups stored outside the server.

After validation, remove the public demo and test data and deploy your own workloads. Passing the local and VPS checks validates this single-server test setup; it does not establish capacity, high availability, or backup retention for a production workload.

## Migration and removal checks

Use a disposable copy of a previous AK3S installation. Save node UIDs, a
persistent-volume marker, and private fingerprints of the datastore/token before
following [migration](migration.md). Confirm the node identity and volume marker
survive, the service uses the official unit, and the role-specific uninstall
script and `k3s-killall.sh` exist. Check token continuity; a running datastore
naturally changes, so test its contents and restore behavior rather than expecting
an unchanged database hash. Repeat `apply` and confirm the service's process ID
and start time remain unchanged. Stop or disable the service, rerun `apply`, and
confirm it becomes active and enabled again. Repeat with an interrupted installer.

After completing the storage and backup checks, test removal on this disposable
node only:

```bash
sudo ak3s uninstall --dry-run
sudo ak3s uninstall --yes
sudo ak3s uninstall --yes
```

Verify that K3s services, processes, mounts, networking, local datastore, and
local-path volumes are removed. Check that the AK3S CLI and operator overrides
remain. Test interrupted removal on another disposable copy, then repeat the
same removal command. Do not accept a successful exit alone as evidence of
network or mount cleanup. These live checks remain necessary before release;
simulated tests and shell syntax checks do not prove runtime cleanup.
