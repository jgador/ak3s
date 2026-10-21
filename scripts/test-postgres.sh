#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
cd "$repo_root"
for tool in docker kubectl openssl python3 rg; do
  command -v "$tool" >/dev/null || { printf 'Missing tool: %s\n' "$tool" >&2; exit 1; }
done
python3 -c 'import yaml' || { printf 'Install PyYAML for manifest checks.\n' >&2; exit 1; }

# Only use the local Docker socket. Never select a saved remote Docker context.
docker_local() {
  env -u DOCKER_HOST -u DOCKER_CONTEXT -u DOCKER_TLS \
    -u DOCKER_TLS_VERIFY -u DOCKER_CERT_PATH \
    docker --host=unix:///var/run/docker.sock "$@"
}
docker_local info >/dev/null
umask 077
mkdir -p .tmp
test_dir=$(mktemp -d "$repo_root/.tmp/postgres-test.XXXXXX")
container="ak3s-postgres-test-${test_dir##*.}"
cleanup() {
  docker_local rm -f "$container" >/dev/null 2>&1 || true
  rm -rf -- "$test_dir"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

# Rendering is a local operation; these commands never open a kubeconfig.
(cd applications/postgres/revisions/001-private && sha256sum -c checksums.sha256)
kubectl kustomize applications/postgres/revisions/001-private > "$test_dir/private.yaml"
kubectl kustomize applications/postgres/revisions/002-public-tls > "$test_dir/public.yaml"
kubectl kustomize applications/postgres > "$test_dir/private-alias.yaml"
kubectl kustomize applications/postgres/public > "$test_dir/public-alias.yaml"
image=$(python3 - "$test_dir" <<'PY'
import pathlib
import sys
import yaml

root = pathlib.Path(sys.argv[1])
private = list(yaml.safe_load_all((root / 'private.yaml').read_text()))
public = list(yaml.safe_load_all((root / 'public.yaml').read_text()))
assert private == list(yaml.safe_load_all((root / 'private-alias.yaml').read_text()))
assert public == list(yaml.safe_load_all((root / 'public-alias.yaml').read_text()))
assert all(r['spec']['type'] == 'ClusterIP' if 'type' in r['spec'] else True
           for r in private if r['kind'] == 'Service')
assert not any(r['kind'] == 'Certificate' for r in private)
assert all(r['metadata']['namespace'] == 'infrastructure' for r in public)
private_ids = {(r['kind'], r['metadata']['name']) for r in private}
assert private_ids <= {(r['kind'], r['metadata']['name']) for r in public}
for kind in ('PersistentVolumeClaim', 'StatefulSet', 'VerticalPodAutoscaler'):
    before = next(r for r in private if r['kind'] == kind)
    after = next(r for r in public if r['kind'] == kind)
    if kind == 'StatefulSet':
        # Switching access must retain the same database identity and storage.
        assert before['spec']['serviceName'] == after['spec']['serviceName']
        assert before['spec']['selector'] == after['spec']['selector']
        assert before['spec']['template']['spec']['containers'][0]['env'] == after['spec']['template']['spec']['containers'][0]['env']
        assert before['spec']['template']['spec']['volumes'][0] == next(v for v in after['spec']['template']['spec']['volumes'] if v['name'] == 'data')
    else:
        assert before == after
statefulset = next(r for r in public if r['kind'] == 'StatefulSet')
assert statefulset['metadata']['annotations']['ak3s.io/postgres-revision'] == '002'
pod = statefulset['spec']['template']['spec']
server = pod['containers'][0]
external = next(r for r in public if r['kind'] == 'Service' and r['metadata']['name'] == 'psql-server-external')
assert external['spec']['selector'].items() <= statefulset['spec']['template']['metadata']['labels'].items()
assert external['spec']['ports'][0]['targetPort'] == server['ports'][0]['name']
assert external['spec']['ports'][0]['port'] == 5432
assert external['spec']['externalTrafficPolicy'] == 'Local'
assert external['spec']['allocateLoadBalancerNodePorts'] is False
tls = next(v for v in pod['volumes'] if v['name'] == 'postgres-tls')['secret']
cert = next(r for r in public if r['kind'] == 'Certificate')
assert tls['secretName'] == cert['spec']['secretName']
assert tls['defaultMode'] == 0o440
config_name = next(v for v in pod['volumes'] if v['name'] == 'public-config')['configMap']['name']
config = next(r for r in public if r['kind'] == 'ConfigMap' and r['metadata']['name'] == config_name)
(root / 'config').mkdir()
for name, data in config['data'].items():
    (root / 'config' / name).write_text(data)
print(server['image'])
PY
)

mkdir -p "$test_dir/tls" "$test_dir/data"
openssl req -x509 -newkey rsa:2048 -nodes -days 1 \
  -subj /CN=AK3S-Test-CA \
  -keyout "$test_dir/ca.key" -out "$test_dir/ca.crt" >/dev/null 2>&1
new_certificate() {
  local generation=$1
  mkdir "$test_dir/tls/$generation"
  openssl req -new -newkey rsa:2048 -nodes -subj /CN=psql.ak3s.example.com \
    -keyout "$test_dir/tls/$generation/tls.key" \
    -out "$test_dir/server.csr" >/dev/null 2>&1
  printf 'subjectAltName=DNS:psql.ak3s.example.com\nextendedKeyUsage=serverAuth\n' > "$test_dir/cert.ext"
  openssl x509 -req -days 1 -in "$test_dir/server.csr" \
    -CA "$test_dir/ca.crt" -CAkey "$test_dir/ca.key" -CAcreateserial \
    -extfile "$test_dir/cert.ext" -out "$test_dir/tls/$generation/tls.crt" >/dev/null 2>&1
  # Match the Secret volume's root owner and fsGroup permissions.
  chgrp 999 "$test_dir/tls/$generation"/*
  chmod 0440 "$test_dir/tls/$generation"/*
  chmod 0755 "$test_dir/tls/$generation"
}
new_certificate generation-1
ln -s generation-1 "$test_dir/tls/..data"
ln -s ..data/tls.crt "$test_dir/tls/tls.crt"
ln -s ..data/tls.key "$test_dir/tls/tls.key"
chmod 0755 "$test_dir" "$test_dir/config" "$test_dir/tls"
chmod 0644 "$test_dir/config"/* "$test_dir/ca.crt"
chown 999:999 "$test_dir/data"
data_dir="$test_dir/data"

# All credentials below are fictional and restricted to this disposable server.
start_server() {
  local mode=$1 initialization_password=fictional-test-password connection
  local -a launch=("$image" postgres)
  connection='host=127.0.0.1 user=postgres dbname=postgres sslmode=disable'
  if [[ "$mode" == public ]]; then
    launch=(--entrypoint /bin/bash "$image" /etc/postgresql/public/start-postgres.sh)
    connection='host=psql.ak3s.example.com hostaddr=127.0.0.1 user=postgres dbname=postgres sslmode=verify-full sslrootcert=/test-ca.crt'
    # Existing data must retain its password even if initialization settings differ.
    if [[ -f "$data_dir/18/docker/PG_VERSION" ]]; then
      initialization_password=fictional-unused-init-password
    fi
  fi
  docker_local run -d --name "$container" \
    --user 999:999 --cap-drop ALL --security-opt no-new-privileges \
    --publish 127.0.0.1::5432 --stop-timeout 15 \
    --env POSTGRES_USER=postgres --env POSTGRES_DB=postgres \
    --env "POSTGRES_PASSWORD=$initialization_password" \
    --env PGDATA=/var/lib/postgresql/18/docker \
    --env POSTGRES_TLS_RELOAD_INTERVAL=1 \
    --mount "type=bind,src=$data_dir,dst=/var/lib/postgresql" \
    --mount "type=bind,src=$test_dir/config,dst=/etc/postgresql/public,readonly" \
    --mount "type=bind,src=$test_dir/tls,dst=/etc/postgresql/tls,readonly" \
    --mount "type=bind,src=$test_dir/ca.crt,dst=/test-ca.crt,readonly" \
    "${launch[@]}" >/dev/null
  local attempt
  for ((attempt = 0; attempt < 60; attempt++)); do
    if docker_local exec "$container" psql -U postgres -d postgres -Atc 'SELECT 1' >/dev/null 2>&1; then
      # Initialization's temporary server also has a local socket. Wait for TCP.
      if docker_local exec --env PGPASSWORD=fictional-test-password "$container" \
        psql "$connection" \
        -Atc 'SELECT 1' >/dev/null 2>&1; then
        return
      fi
    fi
    sleep 1
  done
  docker_local logs "$container" >&2
  printf 'PostgreSQL did not become ready.\n' >&2
  exit 1
}
private_query() {
  docker_local exec --env PGPASSWORD=fictional-test-password "$container" \
    psql 'host=127.0.0.1 user=postgres dbname=postgres sslmode=disable' \
    -v ON_ERROR_STOP=1 -Atc "$1"
}
stop_server() {
  docker_local stop "$container" >/dev/null
  [[ "$(docker_local inspect --format '{{.State.ExitCode}}' "$container")" == 0 ]]
  docker_local rm "$container" >/dev/null
}
query() {
  docker_local exec --env PGPASSWORD=fictional-test-password "$container" \
    psql 'host=psql.ak3s.example.com hostaddr=127.0.0.1 user=postgres dbname=postgres sslmode=verify-full sslrootcert=/test-ca.crt' \
    -v ON_ERROR_STOP=1 -Atc "$1"
}
reject_connection() {
  local password=$1 connection=$2 expected=$3
  if docker_local exec --env "PGPASSWORD=$password" "$container" \
    psql "$connection" -Atc 'SELECT 1' > "$test_dir/rejected.log" 2>&1; then
    printf 'An invalid connection was accepted.\n' >&2
    exit 1
  fi
  if ! rg -q -- "$expected" "$test_dir/rejected.log"; then
    cat "$test_dir/rejected.log" >&2
    exit 1
  fi
}

start_server private
[[ "$(private_query 'SELECT ssl FROM pg_stat_ssl WHERE pid = pg_backend_pid()')" == f ]]
private_query "CREATE TABLE tls_persistence (value text); INSERT INTO tls_persistence VALUES ('preserved');" >/dev/null
private_query "CREATE ROLE sample_client LOGIN PASSWORD 'fictional-client-password'; GRANT SELECT ON tls_persistence TO sample_client;" >/dev/null
identity=$(private_query 'SELECT system_identifier FROM pg_control_system()')
verifier=$(private_query "SELECT rolpassword FROM pg_authid WHERE rolname = 'postgres'")
stop_server
start_server public
[[ "$(query 'SELECT system_identifier FROM pg_control_system()')" == "$identity" ]]
[[ "$(query "SELECT rolpassword FROM pg_authid WHERE rolname = 'postgres'")" == "$verifier" ]]
[[ "$(query 'SELECT value FROM tls_persistence')" == preserved ]]
[[ "$(docker_local exec --env PGPASSWORD=fictional-client-password "$container" \
  psql 'host=psql.ak3s.example.com hostaddr=127.0.0.1 user=sample_client dbname=postgres sslmode=verify-full sslrootcert=/test-ca.crt' \
  -Atc 'SELECT value FROM tls_persistence')" == preserved ]]
printf 'Revision 001 to 002 preserved database identity, data, roles, and passwords.\n'
[[ "$(query 'SELECT ssl FROM pg_stat_ssl WHERE pid = pg_backend_pid()')" == t ]]
[[ "$(query "SELECT rolpassword LIKE 'SCRAM-SHA-256%' FROM pg_authid WHERE rolname = 'postgres'")" == t ]]
reject_connection fictional-test-password \
  'host=127.0.0.1 user=postgres dbname=postgres sslmode=disable' 'no encryption'
reject_connection incorrect-password \
  'host=psql.ak3s.example.com hostaddr=127.0.0.1 user=postgres dbname=postgres sslmode=verify-full sslrootcert=/test-ca.crt' 'password authentication failed'
reject_connection fictional-test-password \
  'host=wrong.example.com hostaddr=127.0.0.1 user=postgres dbname=postgres sslmode=verify-full sslrootcert=/test-ca.crt' 'does not match host name'
printf 'TLS, hostname verification, SCRAM, and rejected connections passed.\n'

started=$(query 'SELECT pg_postmaster_start_time()')
port=$(docker_local port "$container" 5432/tcp)
served_serial() {
  openssl s_client -starttls postgres -connect "$port" \
    -servername psql.ak3s.example.com </dev/null 2>/dev/null \
    | openssl x509 -noout -serial
}
before_serial=$(served_serial)
new_certificate generation-2
ln -s generation-2 "$test_dir/tls/..data-new"
mv -Tf "$test_dir/tls/..data-new" "$test_dir/tls/..data"
after_serial=$(openssl x509 -in "$test_dir/tls/generation-2/tls.crt" -noout -serial)
[[ "$before_serial" != "$after_serial" ]]
for ((attempt = 0; attempt < 30; attempt++)); do
  if [[ "$(served_serial)" == "$after_serial" ]]; then
    break
  fi
  sleep 1
done
[[ "$(served_serial)" == "$after_serial" ]]
[[ "$(query 'SELECT pg_postmaster_start_time()')" == "$started" ]]
printf 'Certificate rotation passed without restarting PostgreSQL.\n'

stop_server
start_server public
[[ "$(query 'SELECT value FROM tls_persistence')" == preserved ]]
stop_server
printf 'Clean shutdown, repeated startup, and persistence passed.\n'

start_server private
[[ "$(private_query 'SELECT value FROM tls_persistence')" == preserved ]]
[[ "$(private_query 'SELECT system_identifier FROM pg_control_system()')" == "$identity" ]]
[[ "$(private_query "SELECT rolpassword FROM pg_authid WHERE rolname = 'postgres'")" == "$verifier" ]]
[[ "$(private_query 'SHOW ssl')" == off ]]
stop_server
printf 'Rollback to revision 001 preserved database identity, data, and passwords.\n'

# Retain coverage of the TLS launcher's first startup with an empty data volume.
data_dir="$test_dir/fresh-data"
mkdir "$data_dir"
chown 999:999 "$data_dir"
start_server public
[[ "$(query 'SELECT ssl FROM pg_stat_ssl WHERE pid = pg_backend_pid()')" == t ]]
stop_server
printf 'Revision 002 initialization with an empty data volume passed.\n'
