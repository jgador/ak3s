#!/usr/bin/env bash

# Format pending Go changes without traversing ignored scratch files.
set -euo pipefail

repo_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$repo_root"

# Include staged edits, unstaged edits, and new non-ignored Go files.
mapfile -d '' -t candidates < <(
  git diff --name-only --diff-filter=ACMR -z HEAD -- '*.go'
  git ls-files --others --exclude-standard -z -- '*.go'
)

files=()
for file in "${candidates[@]}"; do

  # Deleted files and symlinks are not formatter targets.
  if [[ -f "$file" && ! -L "$file" ]]; then
    files+=("$file")
  fi
done
if [[ ${#files[@]} -eq 0 ]]; then
  exit 0
fi

if ! command -v gofmt >/dev/null 2>&1; then
  printf '%s\n' 'Go formatting requires gofmt on PATH; install Go first.' >&2
  exit 2
fi
if ! formatted="$(gofmt -l -w -- "${files[@]}")"; then
  printf '%s\n' 'Fix Go syntax errors and rerun the formatter.' >&2
  exit 2
fi
if [[ -n "$formatted" ]]; then
  printf '%s\n' '{"hookSpecificOutput":{"hookEventName":"PostToolUse","additionalContext":"Formatted changed Go files with gofmt."}}'
fi
