# Architecture

AK3S currently supports existing Hetzner Cloud VMs. A single Go CLI handles host prerequisites and K3s locally on each node; its pinned Helm binary handles shared Kubernetes add-ons. `ak3s install` runs both stages without Python, Ansible or a repository checkout. Infrastructure stays under your control in the Hetzner Cloud Console, and installation needs no Hetzner API credentials. Check the [host and firewall requirements](hetzner.md) before installing.

## Defaults and resource budget

The baseline retains K3s containerd, Flannel VXLAN, CoreDNS, ServiceLB, kube-proxy, metrics-server, the local-path provisioner, and K3s network-policy enforcement. Traefik is the only disabled bundled component. NGINX uses standard Ingress resources and no NGINX custom resources or snippets. cert-manager is the only added component requiring CRDs.

Single-node VictoriaMetrics scrapes itself, VictoriaLogs, NGINX, the API server, kubelet, cAdvisor, and annotated application Services every 30 seconds. Kubelet metrics go through the authenticated API proxy with TLS validation against the cluster CA. Its service account needs read access to `nodes/proxy`; treat that account as privileged because Kubernetes does not restrict proxy access to metrics paths. No Prometheus server, VictoriaMetrics operator, vmagent, or Grafana deployment is necessary. Metrics-server still supplies `kubectl top`, Headlamp resource usage, and HPA resource metrics.

VictoriaLogs stores logs from container stdout/stderr. A lightweight vlagent DaemonSet follows `/var/log/containers` symlinks into `/var/log/pods`, enriches records with pod metadata, and forwards them to VictoriaLogs. Its queue is capped at 1 GiB per destination, above the collector's minimum queue size. Host OS journals are outside the default collection scope. Both stores retain seven days by default, with configurable persistence and memory limits. There is no alerting subsystem or complete managed-service dashboard suite in the baseline.

Platform requests total roughly 600 millicores and 1 GB of RAM on one node, in addition to K3s and system processes. Limits and requests are starting values; cardinality, log volume, and workload size matter. Start at 4 GB RAM, measure memory pressure and disk usage, and adjust sparse `helm_values` in your `values.yaml` before adopting larger workloads. The installation temporarily runs Helm hook jobs too.

## Single node and availability

One server uses SQLite and schedules workloads on the server, avoiding a dedicated control plane. ServiceLB reserves host ports 80 and 443 on available nodes for NGINX's LoadBalancer Service. `externalTrafficPolicy: Cluster` lets ingress traffic reach the single NGINX replica from any ServiceLB node; it does not preserve the original client IP.

DNS A records must point to a node where ServiceLB has bound these ports. On a private network, configure external NAT or forwarding for public HTTP/HTTPS. ServiceLB does not allocate a public IP, program DNS, or create a provider load balancer. HTTP-01 requires public port 80 and correct DNS, including avoiding stale AAAA records. A firewall or process already using 80/443 prevents ServiceLB from binding.

With one node, node failure stops the whole platform. The local-path volumes for metrics, logs, and applications remain bound to that node. PVC capacity is not an enforced disk quota for local-path. VictoriaLogs adds a retention size cap and both stores stop ingesting when free disk falls below 1 GiB. Monitor actual disk usage; all local volumes compete with container images and the OS.

## Multi-node networking and control plane

AK3S accepts one SQLite server or an intended odd number of at least three etcd servers. On a fresh multi-server cluster, the first server initializes embedded etcd; additional servers join through `api_endpoint` with `node.join: true` and a private token file. Agents join using the same local CLI with `platform: false`. Server nodes remain schedulable. Adding agents does not change the datastore. See [configuration](configuration.md#additional-nodes) for per-node examples.

Keep nodes on a trusted Hetzner private network or independently managed VPN. Set `node.ip` to each node's private IPv4 and `node.flannel_iface` when automatic selection would choose the wrong interface. Set `node.external_ip` only when the node owns the reachable address. AK3S does not create networks/firewall rules or automate SSH inventory traversal. `server_count` describes intended topology and is not proof of live etcd quorum.

| Port | Allowed source | Purpose |
| --- | --- | --- |
| TCP 22 | Administrative addresses | SSH |
| TCP 6443 | Administrative addresses and all nodes | Kubernetes API and K3s joining |
| TCP 80, 443 | Application clients / public internet | NGINX via ServiceLB and HTTP-01 |
| UDP 8472 | Cluster nodes only | Flannel VXLAN |
| TCP 10250 | Cluster nodes only | Kubelet and metrics-server |
| TCP 2379, 2380 | Server nodes only, with embedded etcd | Datastore quorum |

Do not expose VXLAN, kubelet, or etcd to the internet. Flannel VXLAN traffic is unencrypted; a private trusted network or encrypted external VPN supplies the transport trust. No NodePort range needs public access for the baseline. Host firewalls must also permit pod/service forwarding; cloud firewall rules alone do not configure a host firewall.

For API availability across server failure, configure an external TCP load balancer or a managed virtual IP yourself and use its DNS name in `api_endpoint`. That endpoint must work from the administration clients and every node and route TCP 6443 to healthy servers. Using the first server address is sufficient for initial setup but leaves registration and administrator access dependent on that server. Keep the datastore topology fixed when rerunning the CLI; conversion from SQLite to etcd is an explicit migration.

Multi-server etcd protects the control-plane datastore, while the default one-replica ingress and single-node metrics/log stores retain their own availability limits. Change ingress replica count and spread replicas when availability requires it. Local-path persistence does not become replicated when more nodes are added. Select external or provider storage only when an application requires it.

## Access

Headlamp is a ClusterIP service by default. Its pod has no cluster-admin binding and requires the user's Kubernetes token. A separate `headlamp-viewer` account gets the built-in `view` role plus read-only node, namespace, and resource-metrics access. A one-hour TokenRequest token is sufficient for browsing; secrets and writes are unavailable. Give named users appropriate Kubernetes RBAC for management, or configure an existing OIDC provider separately. Public Headlamp ingress is opt-in and uses cert-manager TLS.

VictoriaMetrics and VictoriaLogs have no public ingress or application-level authentication in the baseline. Use authenticated Kubernetes port-forwarding to reach VMUI and Logs UI. ClusterIP is not a security boundary against other pods; introduce namespace network policies when workloads require tenant isolation. Avoid exposing the stores directly without authenticated access controls.

## Cost model

The primary cost is the Hetzner Cloud server and its local disk. There is no baseline charge for a cloud load balancer, managed control plane, block storage add-on, or registry. Hetzner may charge separately for IPv4, excess traffic, snapshots, and backups. Three control-plane servers and redundant storage improve availability but increase infrastructure and operational cost. AK3S provides reproducible platform configuration; operational responsibility remains with the owner.
