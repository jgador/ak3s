# VM setup

Use an existing Hetzner VM with Ubuntu 24.04+ or Debian 12+, systemd, disabled swap, and amd64/arm64. Start with 2 vCPUs, 4 GB RAM, and 40 GB SSD. AK3S runs inside the VM and does not provision cloud resources.

## Firewall requirements

| Inbound port | Allowed source | Purpose |
| --- | --- | --- |
| TCP 22 | Your administrative addresses | SSH |
| TCP 6443 | Your administrative addresses | Kubernetes API |
| TCP 80, 443 | Application clients / internet | Ingress and HTTPS issuance |

Allow outbound downloads and permit pod/service forwarding in any host firewall. Leave ports 80/443 free. Headlamp, metrics, and logs need no public ports.

Follow the [installation steps](../README.md#install). Point application DNS A records at the VM's public IPv4; remove stale AAAA records if IPv6 is unavailable.

## Redundant control plane

Connect servers through a trusted private network or encrypted VPN. Allow only cluster nodes to reach TCP 6443/10250 and UDP 8472; allow etcd servers to reach TCP 2379/2380. Keep kubelet, VXLAN, and etcd ports private.

Set `node.ip` to each server's private IPv4 and `node.flannel_iface` if detection selects the wrong interface. Provide a reachable API DNS name backed by your own TCP 6443 load balancer or virtual IP. See [configuration](configuration.md#control-plane-redundancy).
