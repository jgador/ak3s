#!/usr/bin/env bash
# Exercise real background processes and sockets without a cluster or credentials.
set +x
set -Eeuo pipefail
umask 077

repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)
suite_tmp=$(mktemp -d "$repo_root/.tmp/ak3s-forward-tests-XXXXXX")
fixture="$suite_tmp/checkout with spaces"
mkdir -p "$fixture/scripts" "$fixture/.tmp" "$fixture/bin"
cp "$repo_root/scripts/port-forward.sh" "$fixture/scripts/"
extra_pid=''
test_name=initialization
passed=0

run_helper() {
  env -i PATH="$fixture/bin:$PATH" LC_ALL=C PF_FIXTURE="$fixture" \
    bash "$fixture/scripts/port-forward.sh" "$@"
}

cleanup() {
  run_helper stop >/dev/null 2>&1 || true
  if [[ -n "$extra_pid" ]]; then
    kill "$extra_pid" 2>/dev/null || true
    wait "$extra_pid" 2>/dev/null || true
  fi
  rm -rf -- "$suite_tmp"
}
trap cleanup EXIT
trap 'printf "FAIL: %s\n" "$test_name" >&2' ERR

fail() {
  printf 'FAIL: %s: %s\n' "$test_name" "$1" >&2
  exit 1
}

capture() {
  status=0
  output=$(run_helper "$@" 2>&1) || status=$?
}

expect_status() {
  (( status == $1 )) || fail "Expected exit $1, got $status: $output"
}

pass() {
  printf 'PASS: %s\n' "$test_name"
  passed=$((passed + 1))
}

assert_stopped() {
  local port
  for port in 8080 8428 9428 5173; do
    [[ -z $(ss -H -ltn "sport = :$port") ]] || fail "Port $port is still occupied."
  done
}

# These neutral stand-ins check the command contract and expose real HTTP sockets.
cat > "$fixture/bin/listener" <<'PYTHON'
#!/usr/bin/env python3
import http.server
import os
from pathlib import Path
import sys
import time

fixture = Path(os.environ["PF_FIXTURE"])
args = sys.argv[1:]
name = args[-2].removeprefix("service/")
port, remote = {
    "headlamp": (8080, 80),
    "victoria-metrics": (8428, 8428),
    "victoria-logs": (9428, 9428),
    "dashboard": (5173, 80),
}[name]
namespace = {"headlamp": "headlamp", "dashboard": "ak3s"}.get(name, "observability")
assert args == ["kubectl", "--kubeconfig", "/etc/rancher/k3s/k3s.yaml",
                "-n", namespace, "port-forward", "--address", "127.0.0.1",
                "--pod-running-timeout=15s", f"service/{name}", f"{port}:{remote}"]

if (fixture / "fail-service").exists() and (fixture / "fail-service").read_text() == name:
    sys.exit("Simulated service failure")
if (fixture / "stall-service").exists() and (fixture / "stall-service").read_text() == name:
    time.sleep(60)
    sys.exit(1)

class Handler(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        self.send_response(200)
        self.end_headers()
        self.wfile.write(name.encode())

    def log_message(self, *_):
        pass

server = http.server.HTTPServer(("127.0.0.1", port), Handler)
print(f"Forwarding from 127.0.0.1:{port} -> {remote}", flush=True)
server.serve_forever()
PYTHON
chmod 755 "$fixture/bin/listener"
ln -s listener "$fixture/bin/k3s"
cat > "$fixture/bin/sudo" <<'BASH'
#!/usr/bin/env bash
set -euo pipefail
[[ "$1" != -v ]] || exit 0
[[ "$1" == -n && "$2" == -- ]]
shift 2
exec "$@"
BASH
chmod 755 "$fixture/bin/sudo"

test_name='ports available before testing'
# Never stop or replace a listener that was running before the suite.
assert_stopped

test_name='all four listeners start, detach, and serve HTTP'
capture start
expect_status 0
for entry in '8080 headlamp' '8428 victoria-metrics' '9428 victoria-logs' '5173 dashboard'; do
  read -r port name <<< "$entry"
  response=$(curl --noproxy '*' --fail --silent --max-time 2 "http://127.0.0.1:$port/")
  [[ "$response" == "$name" ]] || fail "Unexpected response on $port."
done
[[ $(stat -c %a "$fixture/.tmp/port-forward") == 700 ]] || fail 'State directory is not private.'
[[ $(stat -c %a "$fixture/.tmp/port-forward/headlamp.log") == 600 ]] || fail 'Log is not private.'
pass

test_name='repeated start reuses listeners and releases its lock'
before=$(cat "$fixture/.tmp/port-forward/"*.pid)
capture start
expect_status 0
[[ $(cat "$fixture/.tmp/port-forward/"*.pid) == "$before" ]] || fail 'Running processes were replaced.'
capture status
expect_status 0
pass

test_name='a dead listener is reported and restarted'
read -r dead_pid _ < "$fixture/.tmp/port-forward/victoria-logs.pid"
kill -TERM -- "$dead_pid"
sleep 0.2
capture status
expect_status 1
capture start
expect_status 0
read -r restarted_pid _ < "$fixture/.tmp/port-forward/victoria-logs.pid"
[[ "$dead_pid" != "$restarted_pid" ]] || fail 'Failed to replace dead listener.'
pass

test_name='a failed restart preserves listeners from earlier calls'
read -r dead_pid _ < "$fixture/.tmp/port-forward/victoria-metrics.pid"
kill -TERM -- "$dead_pid"
sleep 0.2
printf '%s' victoria-metrics > "$fixture/fail-service"
capture start
expect_status 1
[[ $(curl --noproxy '*' -fsS --max-time 2 http://127.0.0.1:8080) == headlamp ]] || fail 'Existing forward was stopped.'
rm -- "$fixture/fail-service"
pass

test_name='stop is repeatable and removes managed logs and PID files'
capture stop
expect_status 0
capture stop
expect_status 0
assert_stopped
[[ ! -f "$fixture/.tmp/port-forward/headlamp.log" ]] || fail 'Logs were retained after stop.'
pass

test_name='dashboard startup failure rolls back all new forwards'
printf '%s' dashboard > "$fixture/fail-service"
capture start
expect_status 1
assert_stopped
[[ -f "$fixture/.tmp/port-forward/dashboard.log" ]] || fail 'Failure log is missing.'
rm -- "$fixture/fail-service"
pass

test_name='startup timeout rolls back its process'
printf '%s' headlamp > "$fixture/stall-service"
capture start
expect_status 1
[[ "$output" == *'within 20 seconds'* ]] || fail 'Expected readiness timeout.'
assert_stopped
rm -- "$fixture/stall-service"
pass

test_name='stale state never signals an unrelated process'
sleep 60 &
extra_pid=$!
sleep 0.1
stat_line=$(cat "/proc/$extra_pid/stat")
read -r -a process_fields <<< "${stat_line##*) }"
boot_id=$(cat /proc/sys/kernel/random/boot_id)
printf '%s %s %s\n' "$extra_pid" "$((process_fields[19] + 1))" "$boot_id" > "$fixture/.tmp/port-forward/headlamp.pid"
capture stop
expect_status 0
kill -0 "$extra_pid" || fail 'Process with a different start time was stopped.'
printf '%s %s stale-boot\n' "$extra_pid" "${process_fields[19]}" > "$fixture/.tmp/port-forward/headlamp.pid"
capture stop
expect_status 0
kill -0 "$extra_pid" || fail 'Unrelated process was stopped.'
kill "$extra_pid"
wait "$extra_pid" 2>/dev/null || true
extra_pid=''
pass

test_name='interrupted startup cleans up its children'
printf '%s' headlamp > "$fixture/stall-service"
env -i PATH="$fixture/bin:$PATH" LC_ALL=C PF_FIXTURE="$fixture" \
  bash "$fixture/scripts/port-forward.sh" start > "$suite_tmp/interrupted.log" 2>&1 &
extra_pid=$!
for ((attempt = 0; attempt < 50; attempt++)); do
  [[ -s "$fixture/.tmp/port-forward/headlamp.pid" ]] && break
  sleep 0.1
done
[[ -s "$fixture/.tmp/port-forward/headlamp.pid" ]] || fail 'Startup process was not recorded.'
kill -TERM "$extra_pid"
status=0
wait "$extra_pid" || status=$?
extra_pid=''
(( status == 143 )) || fail 'Interrupted startup returned an unexpected status.'
[[ ! -f "$fixture/.tmp/port-forward/headlamp.pid" ]] || fail 'Interrupted startup retained a PID file.'
assert_stopped
rm -- "$fixture/stall-service"
pass

test_name='occupied ports are preserved'
env -i PATH="$PATH" python3 -m http.server 8080 --bind 127.0.0.1 > "$suite_tmp/occupied.log" 2>&1 &
extra_pid=$!
for ((attempt = 0; attempt < 50; attempt++)); do
  [[ -n $(ss -H -ltn 'sport = :8080') ]] && break
  sleep 0.1
done
capture start
expect_status 1
[[ "$output" == *'8080 is already in use'* ]] || fail 'Expected port conflict message.'
capture stop
expect_status 0
kill -0 "$extra_pid" || fail 'Existing listener was stopped.'
kill "$extra_pid"
wait "$extra_pid" 2>/dev/null || true
extra_pid=''
pass

test_name='tools-only mode skips the deployed dashboard'
capture start --no-dashboard
expect_status 0
[[ -z $(ss -H -ltn 'sport = :5173') ]] || fail 'Dashboard started in tools-only mode.'
capture stop
expect_status 0
pass

test_name='concurrent operations are rejected'
exec 8>"$fixture/.tmp/port-forward/lock"
flock -n 8
capture status
expect_status 1
[[ "$output" == *'Another port-forward command'* ]] || fail 'Concurrent operation was not rejected.'
exec 8>&-
pass

test_name='state symlinks are rejected'
ln -s "$suite_tmp/untouched" "$fixture/.tmp/port-forward/headlamp.pid"
capture stop
expect_status 1
[[ ! -e "$suite_tmp/untouched" ]] || fail 'Symlink target was changed.'
rm -- "$fixture/.tmp/port-forward/headlamp.pid"
pass

assert_stopped
printf '\n%d port-forward tests passed.\n' "$passed"
