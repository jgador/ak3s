# Repository Guidelines

## Project Structure & Module Organization

- `cmd/ak3s/main.go` starts the command-line interface.
- `internal/ak3s/` handles configuration, planning, downloads, rendering, host inspection, and reconciliation. Tests sit beside source files as `*_test.go`.
- `platform/` embeds defaults, pinned versions, Helm values, and Kubernetes manifests.
- `examples/` contains configuration and application examples; `docs/` covers deployment, operations, and testing.
- `.github/workflows/` validates changes and manually publishes from protected release branches; `.codex/` contains the Go-formatting hook.

## Build, Test, and Development Commands

Use Go 1.25+, Make, Bash, and jq 1.6+.

- `go mod download`: download dependencies.
- `make build`: build `bin/ak3s` for the current machine.
- `bin/ak3s help`: inspect commands without installing a cluster.
- `make test`: run Go tests with race detection and coverage.
- `make verify`: run tests, `go vet`, formatting checks, and installer syntax checks.
- `make secrets-setup`: enable the local Gitleaks pre-commit hook (Bash 4+, jq 1.6+, and Gitleaks 8.30.1 or newer 8.x).
- `make secrets-scan`: scan the index and non-ignored working files; `make secrets-history` scans local Git history.
- `make test-secrets`: test secret scanning and the Git hook in isolated repositories.
- `make release`: build Linux amd64/arm64 binaries and checksums in `dist/`.
- `make test-release`: test release branch and version guards without GitHub access.
- `go test -tags integration -run TestPinnedCharts -v ./internal/ak3s`: verify downloads and Helm rendering; requires outbound HTTPS.

## Coding Style & Naming Conventions

Use `gofmt` and tabs for Go, two-space indentation for YAML and Bash, and tabs for Make recipes. Use exported CamelCase and unexported mixedCaps Go names; retain established AK3S YAML keys such as `kubernetes_api_endpoint`. Document exported declarations.

## Testing Guidelines

Keep one current set of PostgreSQL manifests in `applications/postgres/`, with
concise update instructions and placeholders for operator DNS names. Test updates
with existing data and credentials. Applying manifests does not establish
database readiness or successful authentication.

Use Go's `testing` package, `Test…` names, and `Fuzz…` names for fuzz tests. Test failures, interrupted operations, and repeated runs using simulated host and process dependencies. Normal tests need no root or cluster. Coverage is reported without an enforced minimum. Follow [runtime testing](docs/runtime-testing.md) on disposable environments for installation changes; distinguish automated checks from live validation.

## Security, Configuration & Agent Instructions

Keep credentials and rendered secrets private. Use fictional examples. Preserve ownership, identity, token, checksum, and upgrade checks. Store operator overrides outside tracked defaults.

`~/.kube/ak3s-hetzner.yaml` identifies the live production cluster. Deployment
examples using this path are instructions for the human operator, not permission
for an agent to execute them. Do not read production credentials, contact the
production API, or access the production VPS through SSH or provider tools unless
the user explicitly authorizes the specific production operation. General coding,
documentation, verification, and testing requests do not grant that authorization.
For cluster tests, use a dedicated disposable-cluster kubeconfig, pass
`--kubeconfig` explicitly to every Kubernetes or Helm command, and verify its
target before making changes. Never rely on inherited `KUBECONFIG` or the current
context, and never use production as a fallback when a test cluster is unavailable.

Develop on `master`. Backport selected fixes through pull requests into protected
`release/<major>.<minor>` branches. Publish manually from a release branch; see
[releases and backports](docs/releases.md).

Treat employer information as confidential. Do not read employer files or copy employer information from other projects, conversations, configuration, or inherited environment variables. If encountered, stop affected work without reproducing it.

Use `.tmp/` for temporary scripts, screenshots, and logs; remove them when finished and keep `.gitkeep`.

## Final Wording Review

During technical work, focus on architecture, correctness, tests, and implementation. After implementation and verification, review this task's changed text and final response once:

Use plain international English. Avoid regional idioms, unnecessary jargon, and fashionable terminology. Keep established technical terms when they are precise and widely used in the industry.

Limit corrections to wording; preserve technical decisions and program behavior.
