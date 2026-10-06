#!/usr/bin/env bash
# Local testing only: manage loopback listeners without occupying a terminal.
set +x
set -Eeuo pipefail
set +m
umask 077

fail() {
  printf 'port-forward: %s\n' "$*" >&2
  exit 1
}

action=${1:-start}
option=${2:-}
[[ $# -le 2 && "$action" =~ ^(start|status|stop)$ ]] || fail 'Usage: port-forward.sh [start [--no-dashboard]|status|stop]'
[[ -z "$option" || ( "$action" == start && "$option" == --no-dashboard ) ]] || fail 'Only start accepts --no-dashboard.'
(( BASH_VERSINFO[0] >= 4 )) || fail 'Bash 4 or newer is required.'
for tool in flock nohup ss grep cat chmod mkdir rm sleep; do
  command -v "$tool" >/dev/null || fail "Required command is missing: $tool"
done
[[ -r /proc/sys/kernel/random/boot_id ]] || fail 'Linux or WSL2 is required.'

repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)
state_dir="$repo_root/.tmp/port-forward"
[[ ! -L "$repo_root/.tmp" && ! -L "$state_dir" ]] || fail 'The temporary directory must not be a symlink.'
mkdir -p -- "$state_dir"
[[ -O "$state_dir" ]] || fail 'Run as the same user who started the forwards; do not use sudo make.'
chmod 700 "$state_dir"
[[ ! -L "$state_dir/lock" ]] || fail 'The lock file must not be a symlink.'
exec 9>"$state_dir/lock"
flock -n 9 || fail 'Another port-forward command is running. Try again when it finishes.'
boot_id=$(cat /proc/sys/kernel/random/boot_id)

names=(headlamp victoria-metrics victoria-logs dashboard)
declare -A ports=([headlamp]=8080 [victoria-metrics]=8428 [victoria-logs]=9428 [dashboard]=5173)
declare -A urls=(
  [headlamp]=http://127.0.0.1:8080
  [victoria-metrics]=http://127.0.0.1:8428/vmui/
  [victoria-logs]=http://127.0.0.1:9428/select/vmui/
  [dashboard]=http://127.0.0.1:5173
)
for name in "${names[@]}"; do
  for suffix in pid log; do
    [[ ! -L "$state_dir/$name.$suffix" ]] || fail 'State and log files must not be symlinks.'
  done
done

# A PID alone can refer to an unrelated process after exit or a WSL restart.
# Check the boot and kernel start time before signalling.
process_info() {
  local stat
  [[ "$pid" =~ ^[1-9][0-9]*$ && -r /proc/$pid/stat ]] || return 1
  stat=$(cat "/proc/$pid/stat" 2>/dev/null) || return 1
  read -r -a fields <<< "${stat##*) }"
  [[ ${#fields[@]} -ge 20 && ${fields[0]} != Z && ${fields[0]} != X ]] || return 1
}

managed() {
  local recorded_boot extra
  [[ -f "$state_dir/$1.pid" ]] || return 1
  read -r pid started recorded_boot extra < "$state_dir/$1.pid" || return 1
  [[ -z "$extra" && "$recorded_boot" == "$boot_id" && "$started" =~ ^[0-9]+$ ]] || return 1
  process_info || return 1
  [[ ${fields[19]} == "$started" ]]
}

listening() {
  [[ -n $(ss -H -ltn "sport = :${ports[$1]}") ]]
}

ready() {
  managed "$1" && listening "$1" || return 1
  grep -Fq "Forwarding from 127.0.0.1:${ports[$1]} ->" "$state_dir/$1.log" || return 1
  if [[ "$1" == dashboard ]]; then
    curl --noproxy '*' --silent --fail --max-time 1 "${urls[$1]}/" >/dev/null 2>&1
  fi
}

stop_one() {
  local name=$1 attempt
  if managed "$name"; then
    # sudo relays TERM to its command, even when the command runs as root.
    kill -TERM -- "$pid" 2>/dev/null || return 1
    for ((attempt = 0; attempt < 50; attempt++)); do
      managed "$name" || break
      sleep 0.1
    done
    managed "$name" && return 1
  fi
  rm -f -- "$state_dir/$name.pid"
}

show_status() {
  local name result=0 status
  for name in "${names[@]}"; do
    status=stopped
    if managed "$name" && listening "$name"; then
      status=running
    elif [[ "$name" != dashboard || -f "$state_dir/$name.pid" ]]; then
      result=1
    fi
    printf '%-18s %-8s %s\n' "$name" "$status" "${urls[$name]}"
  done
  return "$result"
}

case "$action" in
  status)
    show_status
    exit
    ;;
  stop)
    result=0
    for name in "${names[@]}"; do
      if stop_one "$name"; then
        rm -f -- "$state_dir/$name.log"
      else
        printf 'Could not stop %s; its state and log were retained.\n' "$name" >&2
        result=1
      fi
    done
    (( result == 0 )) && printf 'Stopped all listeners managed by this checkout.\n'
    exit "$result"
    ;;
esac

selected=(headlamp victoria-metrics victoria-logs)
[[ "$option" == --no-dashboard ]] || selected+=(dashboard)
needs_k3s=false
for name in "${selected[@]}"; do
  if ! managed "$name"; then
    needs_k3s=true
  fi
done
kubectl=()
if "$needs_k3s"; then
  command -v k3s >/dev/null || fail 'K3s is not installed. Run this helper inside the WSL distribution with the AK3S test cluster.'
  kubectl=("$(command -v k3s)" kubectl --kubeconfig /etc/rancher/k3s/k3s.yaml)
  if (( EUID != 0 )); then
    command -v sudo >/dev/null || fail 'sudo is required to access the local K3s cluster.'
    sudo -v || fail 'Cannot obtain sudo access to the local K3s cluster.'
    kubectl=(sudo -n -- "${kubectl[@]}")
  fi
fi
if [[ "$option" != --no-dashboard ]]; then
  command -v curl >/dev/null || fail 'curl is required to check dashboard readiness.'
fi

newly_started=()
complete=false
rollback() {
  local name
  if ! "$complete"; then
    for name in "${newly_started[@]}"; do
      stop_one "$name" || printf 'Could not stop %s; run make port-forward-stop.\n' "$name" >&2
    done
  fi
}
trap rollback EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
trap 'exit 129' HUP

for name in "${selected[@]}"; do
  if managed "$name"; then
    ready "$name" || fail "$name is still running but is not ready. Inspect .tmp/port-forward/$name.log, then run make port-forward-stop and retry."
    continue
  fi
  listening "$name" && fail "Port ${ports[$name]} is already in use. Stop its owner or an earlier manual forward, then retry."
  rm -f -- "$state_dir/$name.pid"
  namespace=observability
  remote_port=${ports[$name]}
  if [[ "$name" == headlamp ]]; then
    namespace=headlamp
    remote_port=80
  elif [[ "$name" == dashboard ]]; then
    namespace=ak3s
    remote_port=80
  fi
  command=("${kubectl[@]}" -n "$namespace" port-forward --address 127.0.0.1 --pod-running-timeout=15s "service/$name" "${ports[$name]}:$remote_port")
  (
    # Close the lock descriptor in children; otherwise later commands cannot run.
    exec 9>&-
    cd -- "$repo_root"
    # Keep sudo in the same terminal session as sudo -v so its cached
    # authorization works. nohup and redirected streams free the terminal.
    exec nohup "${command[@]}"
  ) >"$state_dir/$name.log" 2>&1 < /dev/null &
  pid=$!
  # The kernel start time survives exec, including sudo and K3s command startup.
  process_info || fail "$name exited during startup. Inspect .tmp/port-forward/$name.log."
  printf '%s %s %s\n' "$pid" "${fields[19]}" "$boot_id" > "$state_dir/$name.pid"
  newly_started+=("$name")
  deadline=$((SECONDS + 20))
  until ready "$name"; do
    process_info || fail "$name exited during startup. Inspect .tmp/port-forward/$name.log."
    (( SECONDS < deadline )) || fail "$name did not become ready within 20 seconds. Inspect .tmp/port-forward/$name.log."
    sleep 0.1
  done
done
complete=true
show_status
printf '\nRunning in the background. Logs: .tmp/port-forward/*.log\nStop: make port-forward-stop\n'
