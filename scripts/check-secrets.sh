#!/usr/bin/env bash
set +x
# shellcheck source=scripts/lib/secrets-common.sh
source "$(dirname -- "${BASH_SOURCE[0]}")/lib/secrets-common.sh"

mode=${1:-scan}
case "$mode" in scan|staged|history) ;; *) secrets_fail 'Usage: bash scripts/check-secrets.sh [scan|staged|history]' ;; esac
(( $# <= 1 )) || secrets_fail 'Usage: bash scripts/check-secrets.sh [scan|staged|history]'
secrets_require_tools
secrets_find_root

scan_tmp=''
# Invoked indirectly by the EXIT trap, including error and signal exits.
# shellcheck disable=SC2317
cleanup() {
  local status=$?
  trap - EXIT
  if [[ -n "$scan_tmp" ]] && ! rm -rf -- "$scan_tmp"; then
    printf '%s\n' 'Could not remove private scanner temporary files.' >&3
    status=2
  fi
  exit "$status"
}
trap 'cleanup' EXIT
trap 'secrets_fail "Secret scan interrupted; scan incomplete."' HUP INT TERM
[[ ! -L "$repo_root/.tmp" ]] || secrets_fail 'The .tmp directory must not be a symlink.'
mkdir -p -- "$repo_root/.tmp"
scan_tmp=$(mktemp -d "$repo_root/.tmp/ak3s-secrets-XXXXXX")
mkdir -- "$scan_tmp/ignore"
found=0

# Paths are read from NUL-delimited Git inventories, never from shell word splitting.
validate_path() {
  case "$1" in
    ''|/*|.|..|../*|*/../*|*/..|./*|*/./*|*/.) secrets_fail 'Unsafe snapshot path; scan incomplete.' ;;
  esac
}

prepare_destination() {
  local directory=$1 path=$2 parent
  validate_path "$path"
  parent=${path%/*}
  [[ "$parent" != "$path" ]] || parent=.
  mkdir -p -- "$directory/$parent"
}

snapshot_index() {
  local directory=$1 entry metadata path file_mode object stage extra
  local count=0
  declare -A changed=()
  git ls-files --stage -z > "$scan_tmp/index.list"
  if [[ "$mode" == staged ]]; then
    git diff --cached --name-only --no-renames --diff-filter=ACMT -z > "$scan_tmp/changed.list"
    while IFS= read -r -d '' path; do
      changed["$path"]=1
    done < "$scan_tmp/changed.list"
  fi
  while IFS= read -r -d '' entry; do
    [[ "$entry" == *$'\t'* ]] || secrets_fail 'Invalid Git index entry; scan incomplete.'
    metadata=${entry%%$'\t'*}
    path=${entry#*$'\t'}
    read -r file_mode object stage extra <<< "$metadata"
    [[ "$stage" == 0 ]] || secrets_fail 'Resolve merge conflicts before scanning.'
    case "$file_mode" in
      100644|100755|120000) ;;
      *) secrets_fail 'Unsupported Git entry; scan submodules separately.' ;;
    esac
    [[ -z "$extra" && "$object" =~ ^([a-f0-9]{40}|[a-f0-9]{64})$ ]] || secrets_fail 'Invalid Git object; scan incomplete.'
    if [[ "$mode" == staged && -z "${changed["$path"]+present}" ]]; then
      continue
    fi
    prepare_destination "$directory" "$path"
    # Exact index blobs preserve partial staging and avoid checkout filters.
    # Symlink blobs contain link text, so their targets are never opened.
    git cat-file blob "$object" > "$directory/$path"
    ((count += 1))
  done < "$scan_tmp/index.list"
  snapshot_count=$count
}

snapshot_worktree() {
  local directory=$1 path source rest parent part count=0
  declare -A seen=()
  git ls-files --cached --others --exclude-standard -z > "$scan_tmp/worktree.list"
  while IFS= read -r -d '' path; do
    [[ -z "${seen["$path"]+present}" ]] || continue
    seen["$path"]=1
    validate_path "$path"
    source="$repo_root/$path"
    # Reject symlink ancestors before reading any target outside the checkout.
    rest=$path
    parent=$repo_root
    while [[ "$rest" == */* ]]; do
      part=${rest%%/*}
      rest=${rest#*/}
      parent="$parent/$part"
      [[ ! -L "$parent" ]] || secrets_fail 'Cannot scan through a symlink directory; review it separately.'
      if [[ -e "$parent" ]]; then
        [[ -d "$parent" && -x "$parent" ]] || secrets_fail 'Cannot access a working directory; scan incomplete.'
      fi
    done
    if [[ ! -e "$source" && ! -L "$source" ]]; then
      continue # Tracked working-tree deletion.
    fi
    prepare_destination "$directory" "$path"
    if [[ -L "$source" ]]; then
      readlink -n -- "$source" > "$directory/$path"
    elif [[ -f "$source" ]]; then
      cat -- "$source" > "$directory/$path"
    else
      secrets_fail 'Cannot scan a working entry; expected a file or symlink.'
    fi
    ((count += 1))
  done < "$scan_tmp/worktree.list"
  snapshot_count=$count
}

scan_snapshot() {
  local label=$1 directory=$2 count=${3:-} status=0 findings
  local report="$scan_tmp/$label.json"
  local args=(dir "$directory")
  if [[ "$mode" == history ]]; then
    args=(git "$repo_root" '--log-opts=--all --full-history')
  fi
  # Neither raw diagnostics nor report contents are printed. jq selects metadata only.
  gitleaks "${args[@]}" --config "$repo_root/.gitleaks.toml" \
    --redact=100 --no-banner --no-color --log-level=error \
    --ignore-gitleaks-allow --gitleaks-ignore-path "$scan_tmp/ignore" \
    --max-archive-depth=2 --report-format=json --report-path "$report" \
    >/dev/null 2>&1 || status=$?
  (( status == 0 || status == 1 )) || secrets_fail 'Gitleaks failed; scan incomplete. Check its version and .gitleaks.toml.'
  jq -e 'type == "array" and all(.[];
    type == "object" and (.File | type == "string") and
    (.StartLine | type == "number" and . == floor and . >= 0) and
    (.RuleID | type == "string") and
    ((has("Commit") | not) or (.Commit | type == "string")))' "$report" >/dev/null \
    || secrets_fail 'Invalid Gitleaks report; scan incomplete. Raw output is withheld to protect secrets.'
  findings=$(jq 'length' "$report")
  if (( (status == 1) != (findings > 0) )); then
    secrets_fail 'Gitleaks exit status and report disagree; scan incomplete.'
  fi
  jq -r --arg label "$label" --arg prefix "$directory/" '
    .[] | (.Commit // "" | .[0:12] | tojson) as $commit |
    "[\($label)\(if $commit == "\"\"" then "" else " @ " + $commit end)] " +
    "\(.File | ltrimstr($prefix) | tojson):\(.StartLine) (\(.RuleID | tojson))"
  ' "$report" >&3
  if [[ -n "$count" ]]; then
    printf '%s: %s finding(s) in %s file(s).\n' "$label" "$findings" "$count"
  else
    printf '%s: %s finding(s).\n' "$label" "$findings"
  fi
  if (( findings > 0 )); then
    found=1
  fi
}

if [[ "$mode" == history ]]; then
  shallow=$(git rev-parse --is-shallow-repository)
  [[ "$shallow" == false ]] || secrets_fail 'Fetch complete Git history before scanning; this checkout is shallow.'
  scan_snapshot history "$repo_root"
else
  mkdir -- "$scan_tmp/index"
  snapshot_index "$scan_tmp/index"
  label=index
  [[ "$mode" != staged ]] || label=staged
  scan_snapshot "$label" "$scan_tmp/index" "$snapshot_count"
  if [[ "$mode" == scan ]]; then
    mkdir -- "$scan_tmp/worktree"
    snapshot_worktree "$scan_tmp/worktree"
    scan_snapshot worktree "$scan_tmp/worktree" "$snapshot_count"
  fi
fi
if (( found )); then
  printf '%s\n' 'Possible secrets found. Review the reported locations and restage corrected files.' >&3
fi
exit "$found"
