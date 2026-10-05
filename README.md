# AK3S

AK3S is an alternative to Azure Kubernetes Service (AKS) for small deployments that need to keep costs low. It installs K3s and essential platform services on an existing Linux VPS.

A **VPS (virtual private server)** is a virtual machine rented from a hosting provider, with allocated CPU, RAM, disk, networking, and root access. AK3S works inside a VPS that meets its [requirements](docs/vps.md), regardless of provider; Hetzner Cloud is one example.

Support focuses on the **control plane**, which manages the Kubernetes cluster. The default single server also runs your applications, avoiding a separate worker VPS. You manage maintenance, backups, security, and availability.

## What's installed

| Capability | Component | Purpose |
| --- | --- | --- |
| Kubernetes | K3s and containerd | Run and manage containers |
| Networking and DNS | Flannel, kube-proxy, CoreDNS | Connect workloads and discover services |
| Load balancer | K3s ServiceLB | Expose LoadBalancer Services through VPS ports |
| Ingress | NGINX Kubernetes Ingress Controller | Route HTTP/HTTPS to applications |
| Certificates | cert-manager and Let's Encrypt | Issue and renew HTTPS certificates |
| Dashboard | Headlamp | Browse workloads and cluster resources |
| Metrics | VictoriaMetrics | Store and query infrastructure and application metrics |
| Logs | VictoriaLogs and vlagent | Collect and query container logs |
| Resource metrics | metrics-server | Support `kubectl top` and resource-based autoscaling |
| Storage | local-path provisioner | Provide persistent volumes on the VPS's disk |

These components provide Kubernetes capabilities on a single server. ServiceLB does not create a cloud load balancer, and local storage is not replicated. Headlamp, metrics, and logs are private by default; metrics and logs retain data for seven days.

Traefik is disabled. The ingress controller uses [`nginx.org/` annotations](https://github.com/nginx/kubernetes-ingress).

## Install

Use Ubuntu 24.04+ or Debian 12+, systemd, disabled swap, and Linux amd64/arm64. Start with 2 virtual CPUs (vCPUs), 4 GB RAM, and 40 GB SSD. Installation needs root and outbound HTTPS. Configure the [firewall](docs/vps.md#firewall-requirements) first.

To try the full setup locally before renting a VPS, follow the [Windows Subsystem for Linux 2 (WSL2) testing guide](docs/wsl-testing.md). It uses the same installer and runtime checks as a VPS, with local networking and test certificates.

Replace `vX.Y.Z` with a published [release](https://github.com/jgador/ak3s/releases):

```bash
VERSION=vX.Y.Z
curl -fL "https://github.com/jgador/ak3s/releases/download/$VERSION/install.sh" -o install.sh
# Review the script before running it.
sudo bash install.sh "$VERSION"
```

Create `/etc/ak3s/values.yaml`:

```yaml
acme_email: you@example.com
api_endpoint: 203.0.113.10 # replace with your VPS's IPv4 or API DNS name
```

```bash
sudo ak3s install --dry-run
sudo ak3s install
sudo ak3s status
```

The installer downloads the AK3S binary and verifies its checksum; Go and a repository checkout are unnecessary. Its command-line interface (CLI) installs the pinned K3s binary and uses Helm, a Kubernetes package manager, to install the pinned platform charts. Dry-run previews changes without changing the host or cluster.

For an application, download [app.yaml](examples/app.yaml), change its hostname, point DNS to the VPS, and apply it:

```bash
sudo k3s kubectl apply -f app.yaml
```

The example uses Let's Encrypt staging. After testing issuance, select `letsencrypt-production` and enable `nginx.org/ssl-redirect`.

## Reference

- [VPS and firewall setup](docs/vps.md)
- [WSL2 testing before a VPS deployment](docs/wsl-testing.md)
- [Runtime testing on WSL2 or a VPS](docs/runtime-testing.md)
- [Configuration](docs/configuration.md)
- [Access, upgrades, backups, and troubleshooting](docs/operations.md)
- [Architecture and availability](docs/architecture.md)

## Development

With Go 1.25+ and Make:

```bash
go mod download
make build    # bin/ak3s for your machine
make verify   # tests, vet, formatting, shell syntax
make release  # Linux amd64/arm64 binaries and checksums in dist/
```

See [testing and releases](docs/testing.md). Licensed under [MIT](LICENSE).

Codex hooks in `.codex/hooks.json` run `gofmt` on changed Go files after edits and shell commands. The `Stop` hook requests one final language review using its [language policy](.codex/hooks/language-policy.md), then allows the turn to finish. It requires Python 3 and asks Codex to review wording in context rather than replacing technical terms automatically.

Review and trust new or changed hooks with `/hooks` in Codex. [Codex skips them until their current definitions are trusted](https://learn.chatgpt.com/docs/hooks#review-and-trust-hooks).

### Coding agent temporary files

Use `.tmp/` for temporary test runs and files, including Playwright scripts, screenshots, traces, and logs. Remove them when finished. Git ignores the contents; keep `.gitkeep`.
