#!/usr/bin/env bash
# Installs into a new disposable K3s container; never uses the current context.
set -euo pipefail
repo_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
smoke_dir="$(mktemp -d)"
cluster="ak3s-smoke-$$"
python_bin="${AK3S_PYTHON:-$repo_dir/.venv/bin/python}"
forward_pids=()

cleanup() {
  for pid in "${forward_pids[@]}"; do kill "$pid" 2>/dev/null || true; done
  docker rm -f "$cluster" >/dev/null 2>&1 || true
  rm -rf "$smoke_dir"
}
trap cleanup EXIT
for tool in docker helm kubectl curl rg; do command -v "$tool" >/dev/null; done
image="$("$python_bin" - "$repo_dir/platform/versions.yaml" <<'PY'
import sys, yaml
with open(sys.argv[1]) as stream:
    print(yaml.safe_load(stream)['k3s'].replace('+', '-'))
PY
)"
docker run -d --name "$cluster" --privileged --tmpfs /run --tmpfs /var/run \
  -p 127.0.0.1::6443 -p 127.0.0.1::80 -p 127.0.0.1::443 \
  "rancher/k3s:$image" server --disable=traefik --secrets-encryption --tls-san=127.0.0.1 >/dev/null
api_port="$(docker port "$cluster" 6443/tcp | cut -d: -f2)"
http_port="$(docker port "$cluster" 80/tcp | cut -d: -f2)"
https_port="$(docker port "$cluster" 443/tcp | cut -d: -f2)"
for attempt in $(seq 1 90); do
  if docker cp "$cluster:/etc/rancher/k3s/k3s.yaml" "$smoke_dir/kubeconfig" 2>/dev/null; then break; fi
  sleep 2
done
"$python_bin" - "$smoke_dir/kubeconfig" "$api_port" <<'PY'
import sys, yaml
from pathlib import Path
p = Path(sys.argv[1])
kube = yaml.safe_load(p.read_text())
kube['clusters'][0]['cluster']['server'] = 'https://127.0.0.1:' + sys.argv[2]
p.write_text(yaml.safe_dump(kube))
p.chmod(0o600)
PY
kube=(kubectl --kubeconfig "$smoke_dir/kubeconfig")
for attempt in $(seq 1 90); do
  if [ "$(docker inspect -f '{{.State.Running}}' "$cluster")" != true ]; then
    docker logs --tail=30 "$cluster"
    exit 1
  fi
  if "${kube[@]}" get nodes --no-headers 2>/dev/null | rg -q ' Ready '; then break; fi
  sleep 2
done
"${kube[@]}" wait --for=condition=Ready nodes --all --timeout=180s
"$python_bin" "$repo_dir/scripts/ak3s.py" platform --config "$repo_dir/examples/cluster.yaml" \
  --kubeconfig "$smoke_dir/kubeconfig" --state-dir "$smoke_dir/state"

"${kube[@]}" get deployment -A -o name | (! rg 'traefik')
test "$("${kube[@]}" get ingressclass nginx -o jsonpath='{.spec.controller}')" = 'nginx.org/ingress-controller'
test "$("${kube[@]}" auth can-i get secrets --as=system:serviceaccount:headlamp:headlamp-viewer 2>/dev/null || true)" = no
test "$("${kube[@]}" auth can-i create deployments --as=system:serviceaccount:headlamp:headlamp-viewer 2>/dev/null || true)" = no
test "$("${kube[@]}" auth can-i list pods --as=system:serviceaccount:headlamp:headlamp-viewer)" = yes
"${kube[@]}" -n headlamp create token headlamp-viewer --duration=10m >/dev/null

# Local certificate issuance proves cert-manager -> Secret -> NGINX TLS without
# requiring a public DNS zone or spending Let's Encrypt rate limits.
"$python_bin" - "$repo_dir/examples/app.yaml" "$smoke_dir/app.yaml" <<'PY'
import sys, yaml
docs=list(yaml.safe_load_all(open(sys.argv[1])))
ingress=docs[-1]
ingress['metadata']['annotations']={'cert-manager.io/issuer':'smoke-selfsigned', 'nginx.org/ssl-redirect':'false'}
docs.insert(1, {'apiVersion':'cert-manager.io/v1','kind':'Issuer',
 'metadata':{'name':'smoke-selfsigned','namespace':'demo'},'spec':{'selfSigned':{}}})
with open(sys.argv[2], 'w') as f: yaml.safe_dump_all(docs,f)
PY
"${kube[@]}" apply -f "$smoke_dir/app.yaml"
"${kube[@]}" -n demo rollout status deployment/hello --timeout=180s
"${kube[@]}" -n demo wait --for=condition=Ready certificate/hello-tls --timeout=180s
curl --fail --silent --show-error --retry 20 --retry-all-errors --retry-delay 2 \
  -H 'Host: hello.example.com' "http://127.0.0.1:$http_port/" | rg -q 'Welcome to nginx'
curl --fail --silent --show-error --insecure --retry 20 --retry-all-errors --retry-delay 2 \
  --resolve "hello.example.com:$https_port:127.0.0.1" --noproxy '*' \
  "https://hello.example.com:$https_port/" | rg -q 'Welcome to nginx'

"${kube[@]}" -n observability port-forward service/victoria-metrics 18428:8428 >"$smoke_dir/metrics-forward.log" 2>&1 &
forward_pids+=("$!")
"${kube[@]}" -n observability port-forward service/victoria-logs 19428:9428 >"$smoke_dir/logs-forward.log" 2>&1 &
forward_pids+=("$!")
"${kube[@]}" -n headlamp port-forward service/headlamp 18081:80 >"$smoke_dir/headlamp-forward.log" 2>&1 &
forward_pids+=("$!")
curl --fail --silent --show-error --retry 20 --retry-all-errors --retry-delay 2 http://127.0.0.1:18081/ >/dev/null

for attempt in $(seq 1 60); do
  curl --fail --silent --get --data-urlencode 'query=up' http://127.0.0.1:18428/api/v1/query >"$smoke_dir/up.json" || true
  if "$python_bin" - "$smoke_dir/up.json" <<'PY'
import json, sys
try:
    rows=json.load(open(sys.argv[1]))['data']['result']
    healthy={r['metric']['job'] for r in rows if r['value'][1]=='1'}
    required={'victoria-metrics','victoria-logs','nginx-ingress','kubernetes-apiserver','kubernetes-kubelet','kubernetes-cadvisor'}
    sys.exit(0 if required <= healthy else 1)
except (ValueError, KeyError): sys.exit(1)
PY
  then break; fi
  if [ "$attempt" -eq 60 ]; then cat "$smoke_dir/up.json"; exit 1; fi
  sleep 2
done
for attempt in $(seq 1 60); do
  curl --fail --silent --get --data-urlencode 'query=kubernetes.pod_namespace:demo' \
    http://127.0.0.1:19428/select/logsql/query >"$smoke_dir/logs.jsonl" || true
  if rg -q 'hello' "$smoke_dir/logs.jsonl"; then break; fi
  if [ "$attempt" -eq 60 ]; then "${kube[@]}" -n observability logs daemonset/victoria-logs-collector --tail=30; exit 1; fi
  sleep 2
done

# Reconcile a second time to catch Helm upgrade and state persistence failures.
"$python_bin" "$repo_dir/scripts/ak3s.py" platform --config "$repo_dir/examples/cluster.yaml" \
  --kubeconfig "$smoke_dir/kubeconfig" --state-dir "$smoke_dir/state"
printf '%s\n' 'AK3S smoke test passed: routing, TLS, RBAC, metrics, logs, and reconciliation.'
