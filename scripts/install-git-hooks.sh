#!/usr/bin/env bash
set +x
# shellcheck source=scripts/lib/secrets-common.sh
source "$(dirname -- "${BASH_SOURCE[0]}")/lib/secrets-common.sh"

(( $# == 0 )) || secrets_fail 'Usage: bash scripts/install-git-hooks.sh'
secrets_require_tools
secrets_find_root
status=0
configured=$(git config --get core.hooksPath) || status=$?
(( status == 0 || status == 1 )) || secrets_fail 'Cannot check the existing Git hook configuration.'
if [[ -n "$configured" && "$configured" != .githooks ]]; then
  secrets_fail "An existing core.hooksPath is configured. Add 'bash scripts/check-secrets.sh staged' to that pre-commit hook and propagate its exit status."
fi
if [[ -z "$configured" ]]; then
  hooks=$(git rev-parse --git-path hooks)
  shopt -s nullglob dotglob
  for hook in "$hooks"/*; do
    [[ "$hook" == *.sample ]] || secrets_fail "Existing Git hooks found. Add 'bash scripts/check-secrets.sh staged' to your pre-commit hook and propagate its exit status."
  done
fi
chmod 755 .githooks/pre-commit
git config --local core.hooksPath .githooks
printf '%s\n' 'Enabled Gitleaks pre-commit checks for this checkout.'
