# Architecture

AK3S keeps infrastructure costs small by running a K3s control plane, applications, and the [bundled components](../README.md#whats-installed) on one existing [Linux VPS](vps.md#what-is-a-vps). The CLI runs inside that server and installs checksum-verified, pinned binaries and Helm charts. It uses Linux and Kubernetes interfaces rather than provider-specific APIs.

## Traffic and services

Application DNS points to the VPS. ServiceLB exposes ports 80/443, and NGINX routes requests to application Services. cert-manager handles HTTPS issuance and renewal. Leave these ports free and make port 80 publicly reachable for Let's Encrypt HTTP-01 validation.

ServiceLB does not allocate public IPs, configure DNS, or provision a cloud load balancer. Its default ingress configuration does not preserve the original client IP.

Headlamp, VictoriaMetrics, and VictoriaLogs use private ClusterIP services. Reach them through [authenticated Kubernetes port-forwarding](operations.md#access). Logs cover container stdout/stderr. Metrics-server supplies resource metrics; VictoriaMetrics stores longer-term metrics.

## Availability and cost

The default datastore is SQLite. One server failure stops the cluster. Local-path volumes, including metrics and logs, need separate backups and disk monitoring.

A new redundant control plane requires an odd number of at least three embedded-etcd servers and an externally managed API load balancer or virtual IP. This increases cost and does not replicate application volumes. Converting SQLite to etcd requires explicit migration.

The main expense is the VPS and disk; provider charges for networking and backups may also apply. Support focuses on control-plane operation; worker lifecycle management is outside scope.
