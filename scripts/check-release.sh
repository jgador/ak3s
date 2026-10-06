#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Release check failed: %s\n' "$1" >&2
  exit 1
}

[[ ${GITHUB_EVENT_NAME:-} == workflow_dispatch ]] || fail 'Use the manual release workflow.'
branch=${GITHUB_REF#refs/heads/}
[[ $GITHUB_REF == refs/heads/* && $branch =~ ^release/(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]] ||
  fail 'Select a release/<major>.<minor> branch, not master or a tag.'
line=${branch#release/}
[[ ${VERSION:-} =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]] ||
  fail 'Version must use vMAJOR.MINOR.PATCH without leading zeroes.'
[[ ${VERSION%.*} == "v$line" ]] || fail 'Version must match the selected release branch.'
[[ ${GITHUB_SHA:-} =~ ^[0-9a-f]{40}$ ]] || fail 'The source must be a full commit SHA.'

repo=${GITHUB_REPOSITORY:?GITHUB_REPOSITORY is required}
branch_path=${branch//\//%2F}
branch_info=$(gh api "repos/$repo/branches/$branch_path")
jq -e --arg sha "$GITHUB_SHA" '.protected == true and .commit.sha == $sha' \
  <<< "$branch_info" > /dev/null ||
  fail 'The release branch must be protected and still point to this workflow commit. Start a new run if it moved.'

# A tag or draft reserves a version, including after a failed publication.
tags=$(gh api "repos/$repo/git/matching-refs/tags/$VERSION")
jq -e --arg ref "refs/tags/$VERSION" 'all(.[]; .ref != $ref)' \
  <<< "$tags" > /dev/null || fail 'This version already has a tag. Choose a new version.'
releases=$(gh api --paginate --slurp "repos/$repo/releases?per_page=100")
jq -e --arg version "$VERSION" 'all(.[][]; .tag_name != $version)' \
  <<< "$releases" > /dev/null || fail 'This version already has a release or draft. Choose a new version.'

printf 'Release source: %s at %s; version: %s\n' "$branch" "$GITHUB_SHA" "$VERSION"
