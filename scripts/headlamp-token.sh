#!/usr/bin/env bash
# Run the source checkout's CLI to print a temporary viewer token.
set +x
set -Eeuo pipefail

fail() {
  printf 'headlamp-token: %s\n' "$*" >&2
  exit 1
}

(( $# == 0 )) || fail 'Usage: make headlamp-token'
repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)
cli="$repo_root/bin/ak3s"
[[ -x "$cli" ]] || fail 'Build the CLI with make build first.'
command=("$cli" headlamp-token)
if (( EUID != 0 )); then
  command -v sudo >/dev/null || fail 'sudo is required to access the local K3s cluster.'
  command=(sudo -- "${command[@]}")
fi
exec "${command[@]}"
