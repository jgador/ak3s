# Local secret scanning

AK3S uses the [Gitleaks CLI](https://github.com/gitleaks/gitleaks) with a local
pre-commit hook. Scanning runs on your machine without an account, license key,
or source upload. It uses the same local scanning approach as Goblin, adapted
for AK3S.

The configuration keeps Gitleaks' built-in rules and adds checks for K3s join
tokens, OpenAI project/service/admin keys, and private authentication files.

## Setup

Use Bash 4+, Make, Git, jq 1.6+, standard GNU utilities, and
**Gitleaks 8.30.1 or a newer 8.x release** on your `PATH`.
Download Gitleaks from its [official releases](https://github.com/gitleaks/gitleaks/releases)
and verify the archive against the release's checksums file. On WSL, install and
run the Linux version inside WSL. On Ubuntu or Debian, install jq with
`sudo apt install jq` if it is missing.

From the repository root:

```bash
gitleaks version
make secrets-setup
make secrets-scan
```

Setup enables `.githooks/pre-commit` using this checkout's local
`core.hooksPath`. Run it once per clone. It preserves existing hook
configurations and refuses to replace other Git hooks. If you already manage
hooks, add `bash scripts/check-secrets.sh staged` to your pre-commit hook and
propagate its exit status. Bash, jq, and Gitleaks must be available to the Git
client performing the commit.

The scanner, hook setup, and tests are Bash scripts intended for Linux and WSL.
They call the installed Gitleaks binary and use jq to validate reports and select
finding metadata. They need no compilation, Node.js, npm dependencies, or
dashboard build. Normal Go builds and `make verify` do not require jq or
Gitleaks; `make verify` checks the shell syntax.

## Commands

| Command                | Coverage                                                                                                                                                   |
| ---------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `make secrets-scan`    | Full current index, tracked working files, and non-ignored untracked files, including hidden files.                                                        |
| `make secrets-staged`  | Full index contents of staged additions and modifications, including renames and type changes. The pre-commit hook runs this check.                        |
| `make secrets-history` | All commits reachable from local refs, including secrets removed in later commits. It does not fetch; shallow checkouts must fetch complete history first. |
| `make test-secrets`    | Isolated tests using the installed Gitleaks CLI and Git hook.                                                                                              |

You can also run the scanner directly with
`bash scripts/check-secrets.sh [scan|staged|history]`.

The scanner reads exact index blobs without checkout filters. Cleaning a working
file without restaging it does not hide a staged secret. Staged deletions can
remove a secret without blocking that cleanup commit.

Ignored, untracked files such as operator overrides, `.env`, `.ak3s/`, `.kube/`,
dependencies, and build output are not opened. Tracked files are checked even if
they match an ignore rule. Symlinks are scanned as their link text without
following their targets. Unmerged entries and submodules stop the scan and
require separate review.

Findings show only the snapshot, file path, line, rule, and history commit.
Secret values and source excerpts are withheld. Temporary snapshots and redacted
reports are stored under a private `.tmp/ak3s-secrets-*` directory and removed
when the process finishes. Exit codes are `0` for no findings, `1` for findings,
and `2` for an incomplete scan or tool error.

Private-file rules reject force-added runtime files, including kubeconfigs,
K3s token files, `.env` files, and authentication stores. Template files named
`.env.example`, `.env.sample`, or `.env.template` and `.gitkeep` files in private
directories have narrow path exceptions; credential content rules still apply.

## Findings and limits

Review the reported location locally. Remove private files from the index or
replace credentials with runtime configuration, then restage and rescan. If a
real credential entered Git history, revoke or rotate it; deleting its current
file does not remove old commits.

The wrapper ignores `gitleaks:allow` comments and `.gitleaksignore` files. Do not
add blanket exclusions or a baseline to make a scan pass. Any justified
false-positive exception should match a specific synthetic value or a narrowly
defined field and path, with its reason documented.

Gitleaks performs pattern matching, decoding, and archive inspection up to two
levels. Its upstream rules exclude some formats and paths, including images,
fonts, lockfiles, and Gitleaks configuration files. It cannot identify every
credential format or detect all confidential information. Keep configuration
and scanner changes subject to review. Local hooks can be bypassed and must be
enabled separately in each checkout; this setup does not add a CI scan.
