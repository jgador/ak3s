# Repository Guidelines

## Project Structure & Module Organization

- `cmd/ak3s/main.go` starts the command-line interface.
- `internal/ak3s/` handles configuration, planning, downloads, rendering, host inspection, and reconciliation. Tests sit beside source files as `*_test.go`.
- `platform/` embeds defaults, pinned versions, Helm values, and Kubernetes manifests.
- `examples/` contains configuration and application examples; `docs/` covers deployment, operations, and testing.
- `.github/workflows/` validates changes and publishes tagged releases; `.codex/` contains the Go-formatting hook.

## Build, Test, and Development Commands

Use Go 1.25+, Make, and Bash.

- `go mod download`: download dependencies.
- `make build`: build `bin/ak3s` for the current machine.
- `bin/ak3s help`: inspect commands without installing a cluster.
- `make test`: run Go tests with race detection and coverage.
- `make verify`: run tests, `go vet`, formatting checks, and installer syntax checks.
- `make secrets-setup`: enable the local Gitleaks pre-commit hook (Bash 4+, jq 1.6+, and Gitleaks 8.30.1 or newer 8.x).
- `make secrets-scan`: scan the index and non-ignored working files; `make secrets-history` scans local Git history.
- `make test-secrets`: test secret scanning and the Git hook in isolated repositories.
- `make release`: build Linux amd64/arm64 binaries and checksums in `dist/`.
- `go test -tags integration -run TestPinnedCharts -v ./internal/ak3s`: verify downloads and Helm rendering; requires outbound HTTPS.

## Coding Style & Naming Conventions

Use `gofmt` and tabs for Go, two-space indentation for YAML and Bash, and tabs for Make recipes. Use exported CamelCase and unexported mixedCaps Go names; retain established AK3S YAML keys such as `api_endpoint`. Document exported declarations.

## Testing Guidelines

Use Go's `testing` package, `Test…` names, and `Fuzz…` names for fuzz tests. Test failures, interrupted operations, and repeated runs using simulated host and process dependencies. Normal tests need no root or cluster. Coverage is reported without an enforced minimum. Follow [runtime testing](docs/runtime-testing.md) on disposable environments for installation changes; distinguish automated checks from live validation.

## Security, Configuration & Agent Instructions

Keep credentials and rendered secrets private. Use fictional examples. Preserve ownership, identity, token, checksum, and upgrade checks. Store operator overrides outside tracked defaults.

Treat employer information as confidential. Do not read employer files or copy employer information from other projects, conversations, configuration, or inherited environment variables. If encountered, stop affected work without reproducing it.

Use `.tmp/` for temporary scripts, screenshots, and logs; remove them when finished and keep `.gitkeep`.

## Final Wording Review

During technical work, focus on architecture, correctness, tests, and implementation. After implementation and verification, review this task's changed text and final response once:

Use plain international English. Avoid regional idioms, unnecessary jargon, and fashionable terminology. Keep established technical terms when they are precise and widely used in the industry.

Limit corrections to wording; preserve technical decisions and program behavior.
