# Architecture

AK3S keeps infrastructure costs low by running a K3s control plane, applications, and the [bundled components](../README.md#whats-installed) on one existing [Linux VPS](vps.md#what-is-a-vps). The CLI runs on that server, configures K3s, delegates its host lifecycle to the official installer and generated uninstall scripts, and installs checksum-verified, pinned Helm charts. It uses Linux and Kubernetes interfaces rather than provider-specific APIs.

See [how AK3S uses upstream K3s](k3s-lifecycle.md) for the division of responsibilities and the installation sequence.

## Traffic and services

Application DNS points to the VPS. ServiceLB exposes ports 80 and 443, and NGINX routes requests to application Services. cert-manager issues and renews HTTPS certificates. Leave these ports free and make port 80 publicly reachable for Let's Encrypt HTTP-01 validation, which checks control of the domain through an HTTP request.

ServiceLB does not allocate public IPs, configure DNS, or provision a cloud load balancer. Its default ingress configuration does not preserve the original client IP.

Headlamp, VictoriaMetrics, and VictoriaLogs use private ClusterIP services. Reach them through [authenticated Kubernetes port-forwarding](operations.md#access). Logs contain container standard output and standard error (stdout/stderr). Metrics-server supplies resource metrics; VictoriaMetrics stores longer-term metrics.

## Availability and cost

The default datastore is SQLite. One server failure stops the cluster. Local-path volumes, including metrics and logs, need separate backups and disk monitoring.

A new redundant control plane requires an odd number of at least three embedded-etcd servers and an externally managed API load balancer or virtual IP. etcd is a distributed datastore that requires a quorum, or majority of servers, to remain available. This increases cost and does not replicate application volumes. Converting SQLite to etcd requires explicit migration.

The main expense is the VPS and disk; provider charges for networking and backups may also apply. Support focuses on control-plane operation; managing worker nodes is outside scope.
