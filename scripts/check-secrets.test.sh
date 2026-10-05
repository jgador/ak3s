#!/usr/bin/env bash
set +x
set -Eeuo pipefail
umask 077
shopt -s nullglob

repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)
suite_tmp=$(mktemp -d "$repo_root/.tmp/ak3s-secret-tests-XXXXXX")
trap 'rm -rf -- "$suite_tmp"' EXIT
trap 'printf "FAIL: %s\n" "$test_name" >&2' ERR
passed=0
test_name=initialization

fail() {
  printf 'FAIL: %s: %s\n' "$test_name" "$1" >&2
  exit 1
}

in_fixture() {
  (
    cd -- "$fixture"
    # Isolate fixtures from user identities, configuration, hooks, and Git indexes.
    env -i PATH="${fake_bin:+$fake_bin:}$PATH" LC_ALL=C \
      GIT_CONFIG_NOSYSTEM=1 GIT_CONFIG_GLOBAL="$fixture/.tmp/empty.gitconfig" \
      GIT_CONFIG_COUNT=0 GIT_AUTHOR_NAME='Scanner test' \
      GIT_AUTHOR_EMAIL=scanner@example.invalid GIT_COMMITTER_NAME='Scanner test' \
      GIT_COMMITTER_EMAIL=scanner@example.invalid "$@"
  )
}

git_fixture() {
  in_fixture git "$@" 2>/dev/null
}

write_file() {
  local path=$1 content=$2 parent
  parent=${path%/*}
  [[ "$parent" != "$path" ]] || parent=.
  mkdir -p -- "$fixture/$parent"
  printf '%s' "$content" > "$fixture/$path"
}

new_fixture() {
  fixture=$(mktemp -d "$suite_tmp/case-XXXXXX")
  fake_bin=''
  mkdir -p "$fixture/.tmp" "$fixture/scripts/lib" "$fixture/.githooks"
  : > "$fixture/.tmp/empty.gitconfig"
  cp "$repo_root/.gitleaks.toml" "$fixture/"
  cp "$repo_root/scripts/check-secrets.sh" "$repo_root/scripts/install-git-hooks.sh" "$fixture/scripts/"
  cp "$repo_root/scripts/lib/secrets-common.sh" "$fixture/scripts/lib/"
  cp "$repo_root/.githooks/pre-commit" "$fixture/.githooks/"
  write_file .gitignore $'.tmp/\n.env\n.ak3s/\n.kube/\n'
  write_file notes.txt $'Safe content.\n'
  git_fixture init --quiet
  git_fixture config user.name 'Scanner test'
  git_fixture config user.email scanner@example.invalid
  git_fixture config commit.gpgsign false
  git_fixture add .
  git_fixture commit --quiet -m 'Initial fixture'
}

capture() {
  status=0
  output=$(in_fixture "$@" 2>&1) || status=$?
}

scan() {
  capture bash scripts/check-secrets.sh "${1:-scan}"
  local leftovers=("$fixture/.tmp"/ak3s-secrets-*)
  (( ${#leftovers[@]} == 0 )) || fail 'Private scanner snapshots were not removed.'
}

assert_status() {
  (( status == $1 )) || fail "Expected exit $1, received $status."
}

assert_detected() {
  local secret=$1 snapshot=$2 rule=${3:-ak3s-openai-key}
  assert_status 1
  [[ "$output" == *"[$snapshot"* && "$output" == *"$rule"* ]] || fail 'Expected finding metadata is missing.'
  [[ "$output" != *"$secret"* ]] || fail 'Synthetic secret appeared in output.'
}

random_hex() {
  od -An -N32 -tx1 /dev/urandom | tr -d ' \n'
}

synthetic_key() {
  # Generated locally; this value has never been a valid credential.
  printf '%s-%s-%s' sk proj "$(random_hex)"
}

test_ignored_and_hidden() {
  local key
  key=$(synthetic_key)
  write_file .env "$key"
  write_file .ak3s/auth.json "$key"
  scan
  assert_status 0
  write_file .hidden-settings "$key"
  scan
  assert_detected "$key" worktree
}

test_partial_staging() {
  local key
  key=$(synthetic_key)
  write_file notes.txt "$key"
  git_fixture add notes.txt
  write_file notes.txt $'Safe again.\n'
  scan staged
  assert_detected "$key" staged
  scan
  assert_detected "$key" index
  git_fixture add notes.txt
  scan staged
  assert_status 0
}

test_unstaged_changes() {
  local key
  key=$(synthetic_key)
  write_file notes.txt "$key"
  scan staged
  assert_status 0
  scan
  assert_detected "$key" worktree
}

test_rename_and_deletion() {
  local key
  key=$(synthetic_key)
  write_file notes.txt "$key"
  git_fixture add notes.txt
  git_fixture commit --quiet -m 'Synthetic credential fixture'
  git_fixture mv notes.txt 'renamed notes.txt'
  scan staged
  assert_detected "$key" staged
  git_fixture rm --force --quiet 'renamed notes.txt'
  scan staged
  assert_status 0
}

test_history() {
  local key
  key=$(synthetic_key)
  write_file notes.txt "$key"
  git_fixture add notes.txt
  git_fixture commit --quiet -m 'Synthetic credential fixture'
  write_file notes.txt $'Safe again.\n'
  git_fixture add notes.txt
  git_fixture commit --quiet -m 'Remove synthetic credential'
  scan
  assert_status 0
  scan history
  assert_detected "$key" history
}

test_private_files() {
  local path
  for path in .env .ak3s/state.json .kube/config export/kubeconfig.yaml server-token; do
    write_file "$path" $'Unknown credential format.\n'
    git_fixture add --force "$path"
  done
  scan staged
  assert_status 1
  for path in .env .ak3s/state.json .kube/config export/kubeconfig.yaml server-token; do
    [[ "$output" == *"\"$path\""* ]] || fail 'A private file was not reported.'
  done
}

test_template_exceptions() {
  local key
  write_file .env.example $'API_URL=https://example.invalid\n'
  write_file .ak3s/.gitkeep ''
  git_fixture add --force .env.example .ak3s/.gitkeep
  scan staged
  assert_status 0
  key=$(synthetic_key)
  write_file .env.example "$key"
  git_fixture add .env.example
  scan staged
  assert_detected "$key" staged
}

test_join_tokens() {
  local token
  token="K10$(random_hex)::server:$(random_hex)"
  write_file join-settings.txt "$token"
  scan
  assert_detected "$token" worktree ak3s-join-token
}

test_upstream_rules() {
  local value
  value=$(printf -- '-----BEGIN %s-----\n%s\n-----END %s-----\n' 'RSA PRIVATE KEY' "$(random_hex)" 'RSA PRIVATE KEY')
  write_file private.pem "$value"
  scan
  assert_detected "$value" worktree private-key
}

test_suppression() {
  local key
  key=$(synthetic_key)
  write_file notes.txt "$key # gitleaks:allow"
  write_file .gitleaksignore $'notes.txt:ak3s-openai-key:1\n'
  scan
  assert_detected "$key" worktree
}

test_symlinks() {
  local key
  key=$(synthetic_key)
  write_file .tmp/private.txt "$key"
  ln -s .tmp/private.txt "$fixture/link.txt"
  git_fixture add link.txt
  scan
  assert_status 0
  ln -s "$key" "$fixture/secret-link"
  git_fixture add secret-link
  scan staged
  assert_detected "$key" staged
}

test_unmerged_index() {
  local blob zero
  blob=$(git_fixture hash-object notes.txt)
  zero=$(printf '%040d' 0)
  printf '0 %s\tnotes.txt\n100644 %s 1\tnotes.txt\n100644 %s 2\tnotes.txt\n' "$zero" "$blob" "$blob" | git_fixture update-index --index-info
  scan staged
  assert_status 2
}

test_submodules() {
  local commit
  commit=$(git_fixture rev-parse HEAD)
  git_fixture update-index --add --cacheinfo "160000,$commit,nested"
  scan
  assert_status 2
}

test_invalid_config() {
  local key
  key=$(synthetic_key)
  write_file .gitleaks.toml "broken = $key"
  scan
  assert_status 2
  [[ "$output" != *"$key"* ]] || fail 'Configuration contents appeared in output.'
}

test_shallow_history() {
  git_fixture rev-parse HEAD > "$fixture/.git/shallow"
  scan history
  assert_status 2
}

test_parent_symlink() {
  local key
  write_file nested/settings.txt $'Safe.\n'
  git_fixture add nested/settings.txt
  rm -rf "$fixture/nested"
  key=$(synthetic_key)
  write_file .tmp/elsewhere/settings.txt "$key"
  ln -s .tmp/elsewhere "$fixture/nested"
  scan
  assert_status 2
  [[ "$output" != *"$key"* ]] || fail 'Symlink target contents appeared in output.'
}

test_malformed_report() {
  local key
  key=$(synthetic_key)
  write_file .tmp/fake-secret "$key"
  fake_bin="$fixture/.tmp/fake-bin"
  mkdir -p "$fake_bin"
  cat > "$fake_bin/gitleaks" <<'FAKE'
#!/usr/bin/env bash
if [[ "$1" == version ]]; then
  printf '%s\n' 8.30.1
  exit 0
fi
while (( $# )); do
  if [[ "$1" == --report-path ]]; then
    cat .tmp/fake-secret > "$2"
    cat .tmp/fake-secret
    cat .tmp/fake-secret >&2
    exit 0
  fi
  shift
done
exit 2
FAKE
  chmod 755 "$fake_bin/gitleaks"
  scan
  assert_status 2
  [[ "$output" != *"$key"* ]] || fail 'Malformed report or diagnostics appeared in output.'
}

test_existing_hooks() {
  git_fixture config core.hooksPath custom-hooks
  capture bash scripts/install-git-hooks.sh
  assert_status 2
  [[ $(git_fixture config --get core.hooksPath) == custom-hooks ]] || fail 'Existing hook configuration was replaced.'
  git_fixture config --unset core.hooksPath
  write_file .git/hooks/pre-commit $'#!/bin/sh\nexit 0\n'
  capture bash scripts/install-git-hooks.sh
  assert_status 2
}

test_installed_hook() {
  local key
  capture bash scripts/install-git-hooks.sh
  assert_status 0
  capture bash scripts/install-git-hooks.sh
  assert_status 0
  key=$(synthetic_key)
  write_file notes.txt "$key"
  git_fixture add notes.txt
  capture git commit --quiet -m 'Synthetic credential fixture'
  (( status != 0 )) || fail 'The hook allowed a credential commit.'
  [[ "$output" != *"$key"* ]] || fail 'The hook printed a synthetic secret.'
  write_file notes.txt $'Corrected fixture.\n'
  git_fixture add notes.txt
  git_fixture commit --quiet -m 'Corrected fixture'
}

test_unusual_paths() {
  local key path
  key=$(synthetic_key)
  path=$'notes \tline\n$(touch unexpected).txt'
  write_file "$path" "$key"
  scan
  assert_detected "$key" worktree
  git_fixture add -- "$path"
  scan staged
  assert_detected "$key" staged
  [[ ! -e "$fixture/unexpected" ]] || fail 'A filename was interpreted as shell code.'
}

test_old_gitleaks() {
  fake_bin="$fixture/.tmp/fake-bin"
  mkdir -p "$fake_bin"
  printf '#!/bin/sh\nprintf "8.29.0\\n"\n' > "$fake_bin/gitleaks"
  chmod 755 "$fake_bin/gitleaks"
  scan
  assert_status 2
  capture bash scripts/install-git-hooks.sh
  assert_status 2
}

for test_name in \
  test_ignored_and_hidden test_partial_staging test_unstaged_changes \
  test_rename_and_deletion test_history test_private_files test_template_exceptions \
  test_join_tokens test_upstream_rules test_suppression test_symlinks \
  test_unmerged_index test_submodules test_invalid_config test_shallow_history \
  test_parent_symlink test_malformed_report test_existing_hooks test_installed_hook \
  test_unusual_paths test_old_gitleaks; do
  (
    new_fixture
    "$test_name"
  )
  ((passed += 1))
  printf 'ok %s - %s\n' "$passed" "$test_name"
done
printf '%s tests passed.\n' "$passed"
