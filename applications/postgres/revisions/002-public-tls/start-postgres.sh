#!/usr/bin/env bash
set -euo pipefail

tls_dir=${POSTGRES_TLS_DIR:-/etc/postgresql/tls}
config_dir=${POSTGRES_PUBLIC_CONFIG_DIR:-/etc/postgresql/public}
reload_interval=${POSTGRES_TLS_RELOAD_INTERVAL:-30}
if [[ ! "$reload_interval" =~ ^[1-9][0-9]*$ ]]; then
  printf 'POSTGRES_TLS_RELOAD_INTERVAL must be a positive integer.\n' >&2
  exit 1
fi

# Keep the official entrypoint, including initialization of an empty data volume.
docker-entrypoint.sh postgres \
  -c ssl=on \
  -c "ssl_cert_file=$tls_dir/tls.crt" \
  -c "ssl_key_file=$tls_dir/tls.key" \
  -c "hba_file=$config_dir/pg_hba.conf" \
  -c password_encryption=scram-sha-256 "$@" &
postgres_pid=$!

forward_signal() {
  # Called by the TERM and INT traps while the parent is waiting.
  # shellcheck disable=SC2317
  kill -s "$1" "$postgres_pid" 2>/dev/null || true
}
trap 'forward_signal TERM' TERM
trap 'forward_signal INT' INT

watch_certificate() {
  local generation current_generation postmaster_pid
  generation=$(readlink "$tls_dir/..data")
  while sleep "$reload_interval"; do
    current_generation=$(readlink "$tls_dir/..data")
    if [[ "$current_generation" == "$generation" ]]; then
      continue
    fi
    # The entrypoint starts a temporary server during initialization. Signal
    # only the final server, after the entrypoint has replaced itself with it.
    postmaster_pid=
    if [[ -r "$PGDATA/postmaster.pid" ]]; then
      read -r postmaster_pid < "$PGDATA/postmaster.pid" || true
    fi
    if [[ "$postmaster_pid" == "$postgres_pid" ]]; then
      kill -HUP "$postgres_pid"
      generation=$current_generation
      printf 'Requested a PostgreSQL reload after the TLS Secret changed.\n'
    fi
  done
}
watch_certificate &
watcher_pid=$!

# A signal can interrupt Bash's wait before PostgreSQL finishes shutting down.
status=0
while true; do
  wait "$postgres_pid" && status=0 || status=$?
  if ! kill -0 "$postgres_pid" 2>/dev/null; then
    break
  fi
done
kill "$watcher_pid" 2>/dev/null || true
wait "$watcher_pid" 2>/dev/null || true
exit "$status"
