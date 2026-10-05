# Testing and releases

To try a full installation before deploying to a VPS, start with the [WSL2 testing guide](wsl-testing.md). It provides separate paths for [a published release](wsl-testing.md#a-test-a-published-release) and [local source before release](wsl-testing.md#b-test-local-source-before-release). Both use a disposable Linux environment and run the same [runtime checklist](runtime-testing.md) used on a [VPS](vps.md). The development tests below do not install a cluster.

Use Go 1.25+, Make, Bash, and standard Linux utilities:

```bash
go mod download
make build
make verify
make release
```

The normal tests use simulated host operations and process execution and need no root, cluster, or cloud credentials. They run offline after module download. Release builds produce static Linux amd64/arm64 binaries, the installer, and checksums in `dist/`.

For local credential checks, follow [secret scanning](secret-scanning.md).
`make test-secrets` runs isolated scanner and hook tests with Bash, jq, and
Gitleaks 8.30.1 or newer 8.x. `make secrets-scan` checks the index and working
files; `make secrets-history` checks the complete history available locally.

`make test-port-forward` tests the background UI helper with simulated K3s and
dashboard commands and real loopback sockets. It needs Python 3.9+, curl, and the
Linux tools listed in the [access guide](operations.md#start-all-uis-for-local-testing),
with ports 5173, 8080, 8428, and 9428 free. Tests cover repeated starts and stops,
dead processes, startup failures and timeouts, interrupted startup, stale process
records, occupied ports, and concurrent commands. An additional check runs the
real Vite dashboard when Node.js and its dependencies are installed.
The tests use no cluster or credentials. Validate
actual service access and Windows-to-WSL forwarding on a running test cluster.

## Integration checks

Validate real pinned downloads and Helm rendering without a cluster:

```bash
go test -tags integration -run TestPinnedCharts -v ./internal/ak3s
```

Before releasing, use disposable supported VPSs or full Linux virtual machines to check installation, reruns, upgrades, interrupted-run recovery, ingress and TLS, dashboard permissions, metrics and log ingestion, and backup restoration. Follow the [runtime checklist](runtime-testing.md) and repeat public networking and certificate checks on a real VPS. WSL2 provides a local test environment, but its kernel, network, and startup behavior differ from a VPS. Check etcd quorum (a majority of servers available) if testing a redundant control plane. Unit and rendering tests do not prove runtime readiness.

## Publishing

After validation, push a `vMAJOR.MINOR.PATCH` tag. GitHub Actions verifies, builds, and publishes the binaries, installer, and checksums. Merging a pull request does not publish a release.
