#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)
suite_tmp=$(mktemp -d "$repo_root/.tmp/ak3s-release-tests-XXXXXX")
trap 'rm -rf -- "$suite_tmp"' EXIT
mkdir -p "$suite_tmp/bin"

# Only the GitHub API is simulated; the real guard and jq validate its responses.
cat > "$suite_tmp/bin/gh" <<'MOCK'
#!/usr/bin/env bash
set -euo pipefail
[[ ${API_FAILURE:-false} == false ]] || { echo 'API unavailable' >&2; exit 1; }
case "$*" in
  'api repos/example/ak3s/branches/release%2F0.1')
    printf '{"protected":%s,"commit":{"sha":"%s"}}\n' \
      "${PROTECTED:-true}" "${BRANCH_SHA:-$GITHUB_SHA}"
    ;;
  'api repos/example/ak3s/git/matching-refs/tags/v0.1.2')
    printf '[{"ref":"refs/tags/%s"}]\n' "${EXISTING_TAG:-v0.1.20}"
    ;;
  'api --paginate --slurp repos/example/ak3s/releases?per_page=100')
    printf '[[{"tag_name":"v0.1.0"}],[{"tag_name":"%s","draft":true}]]\n' \
      "${EXISTING_RELEASE:-v0.1.1}"
    ;;
  *) echo 'Unexpected GitHub call' >&2; exit 1 ;;
esac
MOCK
chmod +x "$suite_tmp/bin/gh"

passed=0
check() {
  local name=$1 expected_status=$2 expected_text=$3 output status=0
  shift 3
  output=$(env -i PATH="$suite_tmp/bin:$PATH" \
    GITHUB_EVENT_NAME=workflow_dispatch GITHUB_REF=refs/heads/release/0.1 \
    GITHUB_SHA=1111111111111111111111111111111111111111 \
    GITHUB_REPOSITORY=example/ak3s VERSION=v0.1.2 \
    "$@" bash "$repo_root/scripts/check-release.sh" 2>&1) || status=$?
  if [[ $status != "$expected_status" || $output != *"$expected_text"* ]]; then
    printf 'FAIL: %s (exit %s): %s\n' "$name" "$status" "$output" >&2
    exit 1
  fi
  passed=$((passed + 1))
  printf 'PASS: %s\n' "$name"
}

check 'protected branch and unused patch version' 0 'version: v0.1.2'
check 'push event refused' 1 'manual release workflow' GITHUB_EVENT_NAME=push
check 'master refused' 1 'Select a release/' GITHUB_REF=refs/heads/master
check 'tag refused' 1 'Select a release/' GITHUB_REF=refs/tags/release/0.1
check 'nested release branch refused' 1 'Select a release/' GITHUB_REF=refs/heads/release/0.1/fix
check 'branch leading zero refused' 1 'Select a release/' GITHUB_REF=refs/heads/release/00.1
check 'wrong version line refused' 1 'Version must match' VERSION=v0.2.0
check 'prerelease refused' 1 'Version must use' VERSION=v0.1.2-rc.1
check 'version leading zero refused' 1 'Version must use' VERSION=v0.1.02
check 'invalid source refused' 1 'full commit SHA' GITHUB_SHA=short
check 'unprotected branch refused' 1 'must be protected' PROTECTED=false
check 'moved branch refused' 1 'Start a new run' BRANCH_SHA=2222222222222222222222222222222222222222
check 'existing tag refused' 1 'already has a tag' EXISTING_TAG=v0.1.2
check 'draft on a later page refused' 1 'release or draft' EXISTING_RELEASE=v0.1.2
check 'API failure stops release' 1 'API unavailable' API_FAILURE=true

printf '%s release guard tests passed.\n' "$passed"
