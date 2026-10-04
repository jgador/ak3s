# Existing Hetzner VM

AK3S installs K3s and its platform components onto your existing Hetzner VM. Run installation inside the VM as root, or from a workstation over SSH. It does not create or delete Hetzner resources or require an API token.

## Host requirements

Use Ubuntu 24.04 or Debian 12+, systemd, x86_64 or arm64, and swap disabled. Start with at least 2 vCPUs, 4 GB RAM, and 40 GB SSD for the platform and small workloads. The inventory examples use the VM's public IPv4 address.

## Firewall requirements

For a single server, allow these inbound connections in the Hetzner firewall:

| Protocol and port | Source | Purpose |
| --- | --- | --- |
| TCP 22 | Your administrative IPv4 address or restricted CIDR | SSH |
| TCP 6443 | Your administrative IPv4 address or restricted CIDR | Kubernetes API |
| TCP 80 and 443 | Any IPv4 address | Application ingress and certificate issuance |

For one administrative address, use its `/32` CIDR. Keep outbound connectivity available for package downloads, container images, and Let's Encrypt. Permit pod/service forwarding in any host firewall you enable. Leave ports 80 and 443 free for K3s ServiceLB.

Headlamp, metrics, and logs use private ClusterIP services by default and need no additional public ports. Use Kubernetes port-forwarding from your workstation as described in [operations](operations.md). If port-forwarding runs inside the VM, reach it through an SSH tunnel or configure the optional HTTPS Headlamp ingress.

## Install AK3S

Follow [the local installation steps](../README.md#installing-from-inside-the-hetzner-vm) using `examples/inventory-local.yaml` as root. The [workstation installation steps](../README.md#installing-from-a-workstation) use an SSH inventory instead.

Set `ansible_host`, `ak3s_node_ip`, and `api_endpoint` to the VM's public IPv4 for this single-server setup. Set `acme_email` to your email address. Run `make install` after preparing the installation tools and configuration. This installs K3s, NGINX, cert-manager, Headlamp, VictoriaMetrics, VictoriaLogs, and the log collector.

For HTTPS, point the application DNS A record at the VM's public IPv4 address. Remove stale AAAA records if IPv6 is not configured. Validate Let's Encrypt staging issuance before switching to production.

## Additional nodes

Create any additional VMs and a Hetzner private network in the Cloud Console before adding them to the inventory. Set each node's `ak3s_node_ip` to its private IPv4 and configure `ak3s_flannel_iface` if needed. Keep `ansible_host` reachable from your installation machine and set `ak3s_node_external_ip` to the node's public IPv4 when appropriate.

Allow node-to-node traffic only on the private network using the [K3s port table](architecture.md#multi-node-networking-and-control-plane). The API endpoint must remain reachable from every node and your installation machine. Bootstrap accepts one server or an odd number of at least three servers, plus optional agents. Expanding a single-server SQLite cluster to multiple servers requires the explicit datastore migration described in [operations](operations.md#configuration-reconciliation-and-upgrades).
