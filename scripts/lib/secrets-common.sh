#!/usr/bin/env bash
# Shared prerequisites and safe error handling for local secret scanning.
# Diagnostics from Git, jq, and Gitleaks may contain private data.
set +x
set -Eeuo pipefail
umask 077
exec 3>&2
exec 2>/dev/null

secrets_fail() {
  trap - ERR
  printf '%s\n' "$1" >&3
  exit 2
}

trap 'secrets_fail "Secret-scanning command failed; raw output is withheld to protect private data."' ERR

secrets_require_tools() {
  local tool version minor patch
  (( BASH_VERSINFO[0] >= 4 )) || secrets_fail 'Secret scanning requires Bash 4 or newer.'
  for tool in git gitleaks jq mktemp mkdir rm cat readlink chmod; do
    command -v "$tool" >/dev/null || secrets_fail "Required tool is missing: $tool. See docs/secret-scanning.md."
  done
  version=$(gitleaks version) || secrets_fail 'Cannot read the Gitleaks version.'
  [[ "$version" =~ ^v?8\.([0-9]{1,4})\.([0-9]{1,4})$ ]] || secrets_fail 'Install Gitleaks 8.30.1 or a newer 8.x release.'
  minor=${BASH_REMATCH[1]}
  patch=${BASH_REMATCH[2]}
  (( 10#$minor > 30 || (10#$minor == 30 && 10#$patch >= 1) )) || secrets_fail 'Install Gitleaks 8.30.1 or a newer 8.x release.'
}

secrets_find_root() {
  repo_root=$(git rev-parse --show-toplevel) || secrets_fail 'Run this command inside the AK3S Git checkout.'
  cd -- "$repo_root"
  repo_root=$PWD
  # Resolve the checkout itself; snapshot paths must stay underneath it.
  repo_root=$(pwd -P)
  cd -- "$repo_root"
}
