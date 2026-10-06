# VPS setup

## What is a VPS?

A **VPS (virtual private server)** is a virtual machine rented from a hosting provider. It has allocated CPU, RAM, disk, and networking, and gives you root access to manage its operating system. Hetzner Cloud is one example; AK3S does not depend on a provider's API or managed Kubernetes service.

AK3S runs inside an existing VPS and does not provision servers, public IPs, DNS records, firewalls, load balancers, or provider storage. You create and manage those resources with your provider. A local full Linux virtual machine (VM) can also meet the same requirements.

## Host requirements

Use Ubuntu 24.04+ or Debian 12+, systemd, disabled swap, and Linux amd64/arm64. Start with 2 virtual CPUs (vCPUs), 4 GB RAM, and 40 GB SSD, then adjust resources for your applications and retained metrics and logs. Installation needs root, outbound HTTPS, and access to distribution package repositories and container registries.

Choose a full VM with a kernel that supports container cgroups (resource controls), overlayfs (container filesystem layers), bridge netfilter (filtering for bridged traffic), and VXLAN (an overlay network protocol). Restricted container-based VPS plans may prevent kernel-module loading, forwarding, or container networking even when they provide root access. The OS image and virtualization capabilities matter more than the provider name.

Use a fresh server without an existing K3s installation. AK3S refuses to overwrite an unmanaged K3s installation. Keep application and Kubernetes data on the Linux filesystem.

## Firewall requirements

| Inbound port | Allowed source | Purpose |
| --- | --- | --- |
| TCP 22 | Your administrative addresses | SSH |
| TCP 6443 | Your administrative addresses | Kubernetes API |
| TCP 80, 443 | Application clients and the internet | Ingress and HTTPS certificate issuance |

Apply these rules to both the provider firewall or security group and any host firewall. Allow outbound downloads and permit pod and Service traffic forwarding in any host firewall. Leave ports 80 and 443 free. Headlamp, metrics, and logs need no public ports.

Follow the [installation steps](../README.md#install). Set `kubernetes_api_endpoint` to a reachable VPS IPv4 or API DNS name. Point application DNS A records at the VPS's public IPv4; remove stale AAAA records if IPv6 is unavailable. If your provider uses network address translation (NAT), arrange the required inbound port mappings and use the externally reachable address for clients.

Try the [WSL2 tests](wsl-testing.md) first, then repeat the [runtime checks](runtime-testing.md) on the VPS. Public DNS, provider firewall rules, and Let's Encrypt HTTP-01 validation require checks on the actual VPS even after successful local tests.

## Configure and install

Install the published [AK3S CLI](../README.md#install), then use this sequence
for a new VPS with shared HTTPS access. Copy the complete configuration below
into `/etc/ak3s/values.yaml`, replacing the contact email, Kubernetes API address,
and dashboard hostname. For an existing cluster, preserve its installed node
name and other overrides.

```bash
sudo install -d -m 0755 /etc/ak3s
sudo nano /etc/ak3s/values.yaml
```

```yaml
acme_email: you@example.com
acme_environment: staging
kubernetes_api_endpoint: 203.0.113.10
dashboard_shared_paths: true
dashboard_hostname: ak3s.example.com
dashboard_auth_secret: dashboard-auth
headlamp_hostname: ""
platform: false
node:
  name: k3s-server-01
```

The Kubernetes API uses the configured address on HTTPS port `6443`.
Point the dashboard hostname's DNS A record to the VPS, and make ports 80 and
443 reachable as described above. A nonempty `dashboard_hostname` enables
shared paths automatically; the explicit `dashboard_shared_paths: true` also
keeps them enabled if you later remove the public hostname.

Start with `platform: false` to install K3s before creating the login Secret:

```bash
sudo ak3s install --dry-run
sudo ak3s install
sudo k3s kubectl --kubeconfig /etc/rancher/k3s/k3s.yaml get nodes
```

After the node is Ready, [create the dashboard login Secret](operations.md#create-the-login-secret)
in namespace `ak3s`. Its name must match `dashboard_auth_secret`; the example
uses `dashboard-auth`. The public dashboard needs this Secret before its pod
can start.

Edit the same operator file and change only the platform setting:

```yaml
platform: true
```

Then install the shared add-ons and dashboard:

```bash
sudo ak3s apply --dry-run
sudo ak3s apply
sudo ak3s status
sudo k3s kubectl --kubeconfig /etc/rancher/k3s/k3s.yaml -n ak3s get ingress/dashboard certificate/dashboard-tls
```

After staging certificate issuance succeeds, change the certificate setting:

```yaml
acme_environment: production
```

Run `sudo ak3s apply --dry-run` and `sudo ak3s apply` again. Wait for a trusted
production certificate before entering credentials in a browser. The completed
configuration has `platform: true` and `acme_environment: production`.

| URL | UI |
| --- | --- |
| `https://ak3s.example.com/` | AK3S dashboard |
| `https://ak3s.example.com/metrics` | VictoriaMetrics |
| `https://ak3s.example.com/logs` | VictoriaLogs |
| `https://ak3s.example.com/headlamp` | Headlamp |

Use your hostname in these URLs. Dashboard, metrics, and logs share the login
Secret's username and password; Headlamp requires its Kubernetes login token.
See [shared hostname access](operations.md#shared-hostname) for verification
and credential rotation.

## Redundant control plane

Connect servers through a trusted private network or encrypted VPN. Allow only cluster nodes to reach TCP 6443 and 10250 and UDP 8472; allow etcd servers to reach TCP 2379 and 2380. Keep kubelet, VXLAN, and etcd ports private.

Set `node.ip` to each server's private IPv4 and `node.flannel_iface` if detection selects the wrong interface. Provide a reachable API DNS name backed by your own TCP 6443 load balancer or virtual IP. See [configuration](configuration.md#control-plane-redundancy).
