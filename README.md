# AK3S

AK3S installs a lightweight, self-managed Kubernetes platform on an existing Hetzner VM. Download one static Go CLI, `ak3s`, and run it on each node. Users do not need Go, Python, Ansible, Git, a repository checkout, or a separately installed Helm. AK3S downloads and verifies the exact K3s binary, Helm binary, and charts pinned in its release.

| Capability | Component |
| --- | --- |
| Kubernetes and runtime | K3s and containerd |
| Networking and DNS | Flannel VXLAN, kube-proxy, CoreDNS |
| Service exposure | K3s ServiceLB |
| Ingress | NGINX Kubernetes Ingress Controller, open-source edition |
| HTTPS certificates | cert-manager and Let's Encrypt |
| Kubernetes UI | Headlamp |
| Metrics | Single-node VictoriaMetrics with built-in scraping |
| Logs | Single-node VictoriaLogs and a vlagent collector per node |
| Resource metrics and storage | K3s metrics-server and local-path provisioner |

Traefik is disabled. AK3S uses the [NGINX-maintained controller](https://github.com/nginx/kubernetes-ingress), with `nginx.org/` annotations, not the community `ingress-nginx` controller. Metrics, logs and Headlamp remain private ClusterIP services by default. Metrics and logs retain seven days by default.

## Install

Use an existing Ubuntu 24.04+ or Debian 12+ VM with systemd, swap disabled, and Linux amd64 or arm64. Start with 2 vCPUs, 4 GB RAM and 40 GB SSD. Installation requires root and outbound HTTPS. Read the [firewall requirements](docs/hetzner.md#firewall-requirements) first.

Choose an **actually published** version from [Releases](https://github.com/jgador/ak3s/releases). The following uses `vX.Y.Z` as a placeholder. A merged source change does not publish a binary until a release tag is created.

```bash
VERSION=vX.Y.Z # replace with an existing release
curl -fL "https://github.com/jgador/ak3s/releases/download/$VERSION/install.sh" -o install.sh
# Inspect install.sh, then download and checksum-verify the selected binary:
sudo bash install.sh "$VERSION"
```

The optional bootstrap needs Bash, curl, sha256sum and standard Linux core utilities. It only installs the CLI. Alternatively download `ak3s_linux_amd64` or `ak3s_linux_arm64` and `checksums.txt` from the same release, verify with `sha256sum`, and place the executable on your PATH. No compiler is needed. `AK3S_INSTALL_DIR` changes the bootstrap destination, default `/usr/local/bin`.

Create `/etc/ak3s/values.yaml` containing only your overrides:

```yaml
acme_email: you@example.com
api_endpoint: 203.0.113.10 # your node's IPv4 or API DNS name
```

Then run:

```bash
sudo ak3s install --dry-run
sudo ak3s install
sudo ak3s status
```

AK3S uses embedded defaults. There is no safe default ACME contact email, so installation with the platform enabled requires yours. The default API endpoint is `127.0.0.1`; set an externally reachable address for remote administration or joining nodes. Node IP and hostname otherwise use K3s/host detection. Configuration at `/etc/ak3s/values.yaml` is optional and auto-loaded when present. A file in the current directory is used **only** with `--config ./values.yaml`, avoiding accidental selection of a different cluster configuration.

```bash
sudo ak3s install --config ./values.yaml --dry-run
sudo ak3s apply --config ./values.yaml
ak3s config --defaults
ak3s config --config ./values.yaml
ak3s render --config ./values.yaml --output ./rendered
ak3s version
```

`install` and `apply` run the same reconciliation. `upgrade` explicitly permits a safe K3s version increase to the pin in the currently installed AK3S binary. Flags follow the subcommand; `ak3s help` lists them. Commands target only the local node and its explicit `/etc/rancher/k3s/k3s.yaml`, never your ambient kubectl context.

For an application, point its DNS A record at an ingress node and allow inbound TCP 80/443. Edit the hostname in [examples/app.yaml](examples/app.yaml), then use:

```bash
sudo k3s kubectl apply -f examples/app.yaml
sudo k3s kubectl -n demo get certificate,order,challenge
```

The example uses Let's Encrypt staging, whose certificates are untrusted by browsers. Switch its issuer annotation to `letsencrypt-production` and enable `nginx.org/ssl-redirect: "true"` after issuance works. The example can be downloaded separately; it is not required to install AK3S.

## Configuration and dry-run

Defaults, Helm values and manifests are embedded in the released CLI. Your YAML remains separate and is never rewritten by AK3S. Nested mappings merge recursively; scalars and entire lists replace defaults. Explicit `false`, empty strings and empty lists are preserved. Unknown AK3S keys, duplicate keys, nulls, multiple documents and aliases are rejected. Advanced per-chart overrides go under `helm_values.<release>` and are validated by Helm when rendering. See [configuration](docs/configuration.md) and [examples/values.yaml](examples/values.yaml).

`install --dry-run`, `apply --dry-run` and `upgrade --dry-run` use the real inspection, validation, planning, checksum verification and rendering paths. They print planned file creates/updates, service starts/restarts, installations and Helm reconciliations. All downloaded tools, charts and render files live in a private temporary directory that is deleted on completion. No packages, host files, services, ownership markers, persistent caches or cluster resources are changed.

For reachable managed clusters, AK3S reads installed Helm releases and runs Helm server dry-run for those already installed. For fresh installations, server validation is deferred. A dry-run does not execute hooks, start containers, test networking, establish a datastore, prove runtime readiness, or test ACME issuance. Missing systemd/swap/OS prerequisites are reported as blockers and produce a nonzero exit even after successful rendering. Run as root to inspect protected existing state; dry-run itself does not require root on a fresh readable host.

## Upgrades and multi-node operation

1. Back up K3s data, the server token, application volumes and your overrides.
2. Download a specific newer AK3S release using the same bootstrap or manual download workflow.
3. Review `ak3s version`, `ak3s config` and `sudo ak3s upgrade --dry-run`.
4. Run `sudo ak3s upgrade` on one node at a time, servers before agents; check readiness after each node.

This upgrades the platform to the **embedded pins**, without modifying user YAML or searching for latest component releases. `upgrade` does not self-update the CLI. Downgrades and skipped Kubernetes minor versions are rejected. Normal install/apply refuses to change an already installed K3s version. Helm releases reconcile with `--reset-values --atomic --wait`; rollback is per release, not a platform-wide transaction. Repeated runs do not restart K3s when its binary, configuration and unit are unchanged. Helm still reconciles releases and may create a release revision.

Single-server clusters use SQLite. Multi-server clusters use embedded etcd with an intended odd count of at least three. Additional servers and agents run the CLI locally with join settings. There is no SSH inventory runner. See [multi-node instructions](docs/configuration.md#additional-nodes), [operations and migration](docs/operations.md), and [architecture](docs/architecture.md).

## Development and testing

Only contributors need Go 1.25+ and Git. The only Go module dependency is YAML parsing. Bash and core utilities are used to test the optional bootstrap.

```bash
go mod download
make verify             # race tests, coverage, vet, formatting, bootstrap syntax
make release            # static Linux amd64 + arm64 binaries and checksums
# Real pinned downloads + Helm schema/template validation, no cluster needed:
go test -tags integration -run TestPinnedCharts -v ./internal/ak3s
```

The default test suite uses fake host, filesystem-reader, process and download boundaries. It runs without root, systemd, K3s, Docker, cloud credentials or a Kubernetes cluster. After Go dependencies are available it needs no internet. The optional integration test needs HTTPS access to the pinned archives. See [testing](docs/testing.md) for coverage and the remaining real-VM acceptance checks.

| Path | Purpose |
| --- | --- |
| `cmd/ak3s/` | CLI entry point |
| `internal/ak3s/` | Config, inspection, planning, downloads, rendering and reconciliation |
| `platform/defaults.yaml` | Embedded AK3S defaults |
| `platform/versions.yaml` | Exact binary/chart versions and checksums |
| `platform/values/`, `platform/manifests/` | Preserved platform values and RBAC |
| `install.sh` | Optional release-binary downloader |
| `examples/`, `docs/` | Sparse settings, application example and operations |

AK3S is self-managed: operators own node lifecycle, backups, storage, identity, capacity and availability. It does not provision Hetzner resources. Licensed under [MIT](LICENSE); upstream components retain their own licenses.
