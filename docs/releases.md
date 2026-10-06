# Releases and backports

Develop on `master`; stabilize and maintain each release line on
`release/<major>.<minor>`. Publication is a manual action after changes have
merged into a protected release branch.

| Branch | Purpose |
| --- | --- |
| `master` | Normal destination for development and fixes. |
| `release/0.1` | Stabilization and fixes for the `0.1.x` releases. |
| `backport/0.1/fix-description` | A selected fix, submitted by pull request to `release/0.1`. |

## Create a release line

Choose a tested commit on `master` when that line is ready for stabilization.
Start with a clean working tree and fetch the current source:

```bash
git fetch origin
```

Confirm that both `ak3s-checks` and `dashboard-checks` passed for the exact
`origin/master` commit, then create the branch. For the `0.1` line:

```bash
git switch --create release/0.1 origin/master
git push --set-upstream origin release/0.1
```

New development continues on `master`. Once the release branch exists, update it
through pull requests. Keep it focused on that release line; do not merge all of
`master` into an existing release branch. Creating a branch publishes nothing.

## Backport a fix

Normally merge the fix into `master` first, then create a separate backport for
each affected release line. Replace `MASTER_FIX_COMMIT` with the squash commit or
ordinary commit that landed on `master`:

```bash
git fetch origin
git switch --create backport/0.1/fix-description origin/release/0.1
git cherry-pick -x MASTER_FIX_COMMIT
```

The `-x` option records the original commit. Resolve conflicts for the older
source and run the checks relevant to the fix. Use `git cherry-pick --continue`
after resolving conflicts, or `git cherry-pick --abort` to abandon the attempt.
Do not blindly cherry-pick a merge commit. Include necessary prerequisite fixes
explicitly.

```bash
git push --set-upstream origin backport/0.1/fix-description
gh pr create --base release/0.1 --head backport/0.1/fix-description
```

Link the original fix in the PR and describe any conflict resolutions and
validation. Merge after both required checks pass and review conversations are
resolved. No additional reviewer is required, so a sole maintainer can merge.
A fix specific to an older line may start there; explain why it does not apply
to `master`, or track a corresponding forward fix.

## Publish

Complete the [runtime checks](runtime-testing.md) for the intended source first.
Then wait for `ak3s-checks` and `dashboard-checks` on the release branch to pass.

1. Open **Actions → Release AK3S → Run workflow**.
2. In **Use workflow from**, select the release branch, for example `release/0.1`.
3. Enter an unused version from that line, for example `v0.1.0`.
4. Select **Mark as the latest release** only when this should become GitHub's
   latest release. Leave it off when patching an older release line.

The equivalent command for a backport release is:

```bash
gh workflow run release.yaml --ref release/0.1 -f version=v0.1.1 -f latest=false
```

The workflow checks the protected branch, version, and captured commit. It
rejects `master`, tags, mismatched release lines, and existing tags or releases,
including drafts. If the branch moves before the initial check, start a new run.
All tests and builds use the captured commit, even if later backports merge while
the workflow runs.

After validation, the workflow reserves the version with a tag at that commit,
publishes the dashboard image for Linux amd64/arm64, embeds its immutable digest
in the CLI binaries, and creates the GitHub Release with both binaries, the
installer, and checksums. Releases run one at a time. The
`ghcr.io/jgador/ak3s-dashboard` package must be public for cluster nodes to pull it
without credentials.

Merging into either branch and pushing tags do not publish releases. There is no
additional approval stage after manually starting publication.

### Failed publication

If a run fails before creating its tag, fix the cause and start a new run with
the same unused version. If it already created a tag, that version is reserved:
use a new patch version after fixing the cause. A failed run may leave a tag,
image, or draft behind. Do not move tags or replace published assets. This
workflow deliberately uses a new version instead of recovering partial releases.

## Branch protection

[`.github/release-ruleset.json`](../.github/release-ruleset.json) defines the
repository ruleset for all `release/*` branches. It requires pull requests,
up-to-date passing `ak3s-checks` and `dashboard-checks` from GitHub Actions, and
resolved review conversations. It blocks deletion and force pushes, includes
administrators, and has no bypass actors or mandatory second reviewer.

Both checks run on every PR and on pushes to `master` and `release/**`, without
path filters that could leave required checks pending. Protection applies when
creating a release branch too, so create it from a commit with both checks passed.

For a new repository, install the workflows and wait for both checks before
applying the ruleset with repository administrator access:

```bash
gh api --method POST repos/jgador/ak3s/rulesets --input .github/release-ruleset.json
```

For an existing ruleset, use `--method PUT` and append its ID to the endpoint.
Verify that the live ruleset is active in **Settings → Rules → Rulesets**. The
file alone does not protect GitHub branches. `master` remains the default
development branch; this ruleset protects release branches only.
