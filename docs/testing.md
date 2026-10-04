# Testing and release validation

The normal suite runs on an unprivileged Linux machine, WSL, or the ChatGPT Work sandbox. No root, Docker, systemd, K3s, Kubernetes cluster or cloud credentials are required.

```bash
go mod download
make verify
make release
```

Go 1.25+ builds the CLI and tests. The default tests are offline after module download. Bootstrap tests execute Bash with fake curl/uname commands and temporary destinations, using standard Linux core utilities. They never install a binary into a system directory. The release target cross-compiles static amd64 and arm64 binaries with `CGO_ENABLED=0` and writes `checksums.txt`.

Coverage includes recursive sparse merging, explicit false/zero/list replacement, malformed/unknown YAML, default isolation, exact version pins, unsafe transitions, manifest generation, configuration/topology ownership guards, legacy migration, host detection with fixture files, secret-safe command errors, dry-run mutation exclusion, real reconciliation order with fake writes/processes, partial failures, pending restarts, checksum/HTTPS handling, archive extraction, bootstrap replacement safety and CLI commands.

The host inspector takes a filesystem-reader and process-runner interface. The real reconciler takes a process-runner and atomic-write function. CLI tests replace the host and artifact preparer. This exercises the actual desired-state plan and operation ordering without emulating a Linux boot or Kubernetes API.

## Real Helm validation without a cluster

```bash
go test -tags integration -run TestPinnedCharts -v ./internal/ak3s
```

This opt-in test downloads the **actual pinned** Helm binary and six chart archives over HTTPS, verifies every checksum, renders through Helm (including chart schemas), checks Kubernetes document structure and exports the results. It uses temporary files only. It needs network access, but no installed Helm, kubeconfig or cluster. CI runs it separately so failures distinguish offline logic from upstream availability.

A real `ak3s install --dry-run` on the sandbox can report a missing systemd blocker. That is an expected nonzero result, not proof installation would work there. The shared planning path still renders resources when possible. Configuration errors, checksum failures or template errors are real failures.

## VM acceptance checks before production

Sandbox tests cannot prove K3s boot, systemd behavior, cgroups, containerd, node networking, service ports, kubelet readiness, Helm hook execution, live CRDs/webhooks, PVC binding, metrics ingestion, log ingestion, real ACME issuance, or etcd quorum. Validate on disposable supported VMs:

1. Fresh-install on amd64 and arm64 with real systemd; verify Node Ready and platform pods/PVCs.
2. Run install/apply again; confirm no K3s restart for unchanged host settings and preserved data.
3. Test HTTP routing, cert-manager TLS with a self-signed test issuer, and staging ACME with real public DNS.
4. Check Headlamp viewer cannot read Secrets or create deployments; verify short-lived token login.
5. Confirm the six built-in metrics jobs report `up=1`, and demo pod logs appear in VictoriaLogs.
6. Interrupt an install around binary replacement/service restart; verify a subsequent apply recovers.
7. Verify safe patch/minor upgrades with backups and restoration, and rejection of downgrades/skipped minors.
8. Join three etcd servers and an agent on a private network, upgrade sequentially, and verify quorum and volumes.
9. Exercise migration of an original AK3S-owned installation with its existing token and node identity.

The former Python/Docker smoke script was removed with the orchestration it exercised. These VM acceptance checks remain necessary; template validation does not replace runtime integration testing.

## Publishing

Push an explicit `vMAJOR.MINOR.PATCH` tag after reviewing pins and passing VM acceptance. The release workflow runs verification plus real chart rendering, builds both Linux binaries, and publishes them with the bootstrap and checksums. A pull request does not itself publish a release. Changing AK3S user defaults does not change an existing user's sparse override file.
