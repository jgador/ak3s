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

## Redundant control plane

Connect servers through a trusted private network or encrypted VPN. Allow only cluster nodes to reach TCP 6443 and 10250 and UDP 8472; allow etcd servers to reach TCP 2379 and 2380. Keep kubelet, VXLAN, and etcd ports private.

Set `node.ip` to each server's private IPv4 and `node.flannel_iface` if detection selects the wrong interface. Provide a reachable API DNS name backed by your own TCP 6443 load balancer or virtual IP. See [configuration](configuration.md#control-plane-redundancy).
