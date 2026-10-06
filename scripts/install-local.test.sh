#!/usr/bin/env bash
# Check configuration preservation and failure recovery without Docker or K3s.
set +x
set -Eeuo pipefail
umask 077

repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)
suite_tmp=$(mktemp -d "$repo_root/.tmp/ak3s-local-tests-XXXXXX")
fixture="$suite_tmp/checkout with spaces"
mkdir -p "$fixture/scripts" "$fixture/bin" "$fixture/.tmp"
cp "$repo_root/scripts/install-local.sh" "$fixture/scripts/"
trap 'rm -rf -- "$suite_tmp"' EXIT

# Configuration is parsed by the real CLI. All installation commands are fakes.
cat > "$fixture/bin/ak3s" <<'MOCK'
#!/usr/bin/env bash
set -euo pipefail
if [[ "$1" == config ]]; then
  exec "$LOCAL_TEST_CLI" "$@"
fi
action=$1
[[ "$2" == --config ]]
config=$3
phase=$action
[[ "$action" != install ]] || phase=bootstrap
[[ ${4:-} != --dry-run ]] || phase=preflight
printf '%s\n' "$phase" >> "$LOCAL_TEST_FIXTURE/trace"
[[ "$phase" != "$LOCAL_TEST_FAILURE" ]] || exit 1
if [[ "$phase" == bootstrap && "$LOCAL_TEST_FAILURE" == interrupt ]]; then
  kill -TERM "$PPID"
  exit 143
fi
if [[ "$phase" == bootstrap ]]; then
  [[ "$(stat -c %a "$config")" == 600 ]]
  "$LOCAL_TEST_CLI" config --config "$config" > "$LOCAL_TEST_FIXTURE/bootstrap-actual"
  cmp "$LOCAL_TEST_FIXTURE/bootstrap-expected" "$LOCAL_TEST_FIXTURE/bootstrap-actual"
  touch "$LOCAL_TEST_FIXTURE/bootstrapped"
else
  [[ "$config" == "$LOCAL_TEST_FIXTURE/operator.yaml" ]]
fi
if [[ "$phase" == apply ]]; then
  [[ -f "$LOCAL_TEST_FIXTURE/imported" ]]
  [[ ! -f "$LOCAL_TEST_FIXTURE/existing" || -f "$LOCAL_TEST_FIXTURE/restarted" ]]
  touch "$LOCAL_TEST_FIXTURE/existing"
fi
MOCK

cat > "$fixture/bin/docker" <<'MOCK'
#!/usr/bin/env bash
set -euo pipefail
case "$1" in
  build)
    printf 'build\n' >> "$LOCAL_TEST_FIXTURE/trace"
    [[ "$LOCAL_TEST_FAILURE" != build ]] || exit 1
    [[ "$2" == --platform && "$3" == linux/* && "$4" == -f ]]
    [[ "$5" == "$LOCAL_TEST_FIXTURE/dashboard/Dockerfile" && "$6" == -t ]]
    [[ "$7" == "$LOCAL_TEST_IMAGE" && "$8" == "$LOCAL_TEST_FIXTURE" && $# == 8 ]]
    ;;
  image)
    [[ "$2" == save && "$3" == "$LOCAL_TEST_IMAGE" && $# == 3 ]]
    [[ "$LOCAL_TEST_FAILURE" != save ]] || exit 1
    printf 'fictional image stream'
    ;;
  *) exit 1 ;;
esac
MOCK

cat > "$fixture/bin/k3s" <<'MOCK'
#!/usr/bin/env bash
set -euo pipefail
if [[ "$1" == ctr ]]; then
  [[ "$*" == 'ctr --address /run/k3s/containerd/containerd.sock --namespace k8s.io images import -' ]]
  [[ -f "$LOCAL_TEST_FIXTURE/bootstrapped" ]]
  printf 'import\n' >> "$LOCAL_TEST_FIXTURE/trace"
  stream=$(cat)
  [[ "$LOCAL_TEST_FAILURE" != import && "$stream" == 'fictional image stream' ]] || exit 1
  touch "$LOCAL_TEST_FIXTURE/imported"
  exit
fi
[[ "$1" == kubectl && "$2" == --kubeconfig && "$3" == /etc/rancher/k3s/k3s.yaml ]]
[[ "$4" == --cache-dir && "$5" == "$LOCAL_TEST_FIXTURE/.tmp/"*/kube-cache ]]
shift 5
[[ "$1" == -n && "$2" == ak3s ]]
shift 2
case "$1" in
  get)
    [[ "$*" == 'get deployment dashboard --ignore-not-found -o name --request-timeout=30s' ]]
    [[ "$LOCAL_TEST_FAILURE" != lookup ]] || exit 1
    if [[ -f "$LOCAL_TEST_FIXTURE/existing" ]]; then printf 'deployment.apps/dashboard\n'; fi
    ;;
  rollout)
    [[ "$*" == 'rollout restart deployment/dashboard --request-timeout=30s' ]]
    printf 'restart\n' >> "$LOCAL_TEST_FIXTURE/trace"
    [[ "$LOCAL_TEST_FAILURE" != restart && -f "$LOCAL_TEST_FIXTURE/imported" ]] || exit 1
    touch "$LOCAL_TEST_FIXTURE/restarted"
    ;;
  *) exit 1 ;;
esac
MOCK

cat > "$fixture/bin/install" <<'MOCK'
#!/usr/bin/env bash
set -euo pipefail
[[ "$*" == "-m 0755 $LOCAL_TEST_FIXTURE/bin/ak3s /usr/local/bin/ak3s" ]]
printf 'install-cli\n' >> "$LOCAL_TEST_FIXTURE/trace"
MOCK
cat > "$fixture/bin/sudo" <<'MOCK'
#!/usr/bin/env bash
set -euo pipefail
[[ "$1" != -v ]] || exit 0
[[ "$1" == -- ]]
shift
exec "$@"
MOCK
chmod 755 "$fixture/bin/"*

passed=0
check() {
  local name=$1 failure=$2 existing=$3 configured_image=$4 platform=$5 expected=$6
  local status=0 image=${configured_image:-ghcr.io/jgador/ak3s-dashboard:dev}
  rm -f "$fixture/bootstrapped" "$fixture/imported" "$fixture/restarted" "$fixture/existing"
  : > "$fixture/trace"
  [[ "$existing" != true ]] || touch "$fixture/existing"
  cat > "$fixture/operator.yaml" <<YAML
# Preserve this comment and the operator's formatting.
acme_email: tester@example.com
kubernetes_api_endpoint: localhost
dashboard_shared_paths: true
dashboard_image: "$configured_image"
platform: $platform
node:
  name: test-node
helm_values:
  headlamp:
    test:
      platform: true
      text: |
        platform: true
YAML
  cp "$fixture/operator.yaml" "$fixture/operator-before"
  # Build an independently parsed expected configuration with one override.
  sed 's/^platform: true$/platform: false/' "$fixture/operator.yaml" > "$fixture/bootstrap-input"
  "$repo_root/bin/ak3s" config --config "$fixture/bootstrap-input" > "$fixture/bootstrap-expected"
  env -i PATH="$fixture/bin:$PATH" LC_ALL=C \
    LOCAL_TEST_CLI="$repo_root/bin/ak3s" LOCAL_TEST_FIXTURE="$fixture" \
    LOCAL_TEST_FAILURE="$failure" LOCAL_TEST_IMAGE="$image" \
    bash "$fixture/scripts/install-local.sh" ghcr.io/jgador/ak3s-dashboard:dev "$fixture/operator.yaml" \
    > "$fixture/output" 2>&1 || status=$?
  if [[ ( "$expected" == success && $status != 0 ) || ( "$expected" != success && $status == 0 ) ]]; then
    printf 'FAIL: %s (exit %s)\n' "$name" "$status" >&2
    cat "$fixture/output" >&2
    exit 1
  fi
  cmp "$fixture/operator-before" "$fixture/operator.yaml"
  [[ -z $(find "$fixture/.tmp" -mindepth 1 -print -quit) ]] || { printf 'FAIL: temporary files remain\n' >&2; exit 1; }
  if [[ "$expected" == success ]]; then
    local expected_trace=$'preflight\nbuild\nbootstrap\nimport\n'
    [[ "$existing" != true ]] || expected_trace+=$'restart\n'
    expected_trace+=$'apply\ninstall-cli'
    [[ "$(cat "$fixture/trace")" == "$expected_trace" ]]
  else
    local trace
    trace=$(cat "$fixture/trace")
    [[ "$trace" != *install-cli* ]]
    [[ "$failure" == apply || "$trace" != *apply* ]]
    if [[ "$failure" == preflight || "$expected" == config-error ]]; then
      [[ "$trace" != *build* && "$trace" != *bootstrap* ]]
    fi
  fi
  printf 'PASS: %s\n' "$name"
  passed=$((passed + 1))
}

check 'first install with the real configuration parser' '' false '' true success
check 'rerun restarts the dashboard before applying' '' true '' true success
check 'custom tagged image uses the operator reference' '' false 'registry.example.com:5000/demo/dashboard:test' true success
check 'disabled platform rejected before host changes' '' false '' false config-error
check 'digest override rejected before host changes' '' false "registry.example.com/demo/dashboard@sha256:$(printf '1%.0s' {1..64})" true config-error
for phase in preflight build bootstrap save import lookup restart apply interrupt; do
  check "failure or interruption during $phase stops and cleans up" "$phase" true '' true failure
done
printf '%s local installation tests passed.\n' "$passed"
