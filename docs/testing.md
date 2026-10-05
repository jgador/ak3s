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

## Integration checks

Validate real pinned downloads and Helm rendering without a cluster:

```bash
go test -tags integration -run TestPinnedCharts -v ./internal/ak3s
```

Before releasing, use disposable supported VPSs or full Linux virtual machines to check installation, reruns, upgrades, interrupted-run recovery, ingress and TLS, dashboard permissions, metrics and log ingestion, and backup restoration. Follow the [runtime checklist](runtime-testing.md) and repeat public networking and certificate checks on a real VPS. WSL2 provides a local test environment, but its kernel, network, and startup behavior differ from a VPS. Check etcd quorum (a majority of servers available) if testing a redundant control plane. Unit and rendering tests do not prove runtime readiness.

## Publishing

After validation, push a `vMAJOR.MINOR.PATCH` tag. GitHub Actions verifies, builds, and publishes the binaries, installer, and checksums. Merging a pull request does not publish a release.
