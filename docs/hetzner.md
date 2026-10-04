# Existing Hetzner VM

AK3S installs K3s and its platform components onto your existing Hetzner VM. Run installation inside the VM as root, using SSH from a workstation if desired. The CLI itself runs locally on the VM. It does not create or delete Hetzner resources or require an API token.

## Host requirements

Use Ubuntu 24.04 or Debian 12+, systemd, x86_64 or arm64, and swap disabled. Start with at least 2 vCPUs, 4 GB RAM, and 40 GB SSD for the platform and small workloads. The sparse settings example uses the VM's public IPv4 address.

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

Follow the [CLI installation steps](../README.md#install). Download an explicit release, create `/etc/ak3s/values.yaml` with your `acme_email` and `api_endpoint`, then run `sudo ak3s install --dry-run` and `sudo ak3s install`. Set `node.ip` explicitly when automatic interface selection is unsuitable. No Hetzner API token or language runtime is needed.

For HTTPS, point the application DNS A record at the VM's public IPv4 address. Remove stale AAAA records if IPv6 is not configured. Validate Let's Encrypt staging issuance before switching to production.

## Additional nodes

Create additional VMs and a Hetzner private network first. Run the CLI on each VM with the [per-node join settings](configuration.md#additional-nodes). Use `node.ip` for the private IPv4, `node.flannel_iface` when needed, and `node.external_ip` only for an address owned by that VM.

Allow node-to-node traffic only on the private network using the [K3s port table](architecture.md#multi-node-networking-and-control-plane). The API endpoint must remain reachable from every node and your administration clients. Use one SQLite server or an odd number of at least three etcd servers, plus optional agents. Expanding a SQLite cluster into etcd requires an explicit migration; ordinary install/apply refuses the topology change.
