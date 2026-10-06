# Architecture

AK3S keeps infrastructure costs low by running a K3s control plane, applications, and the [bundled components](../README.md#whats-installed) on one existing [Linux VPS](vps.md#what-is-a-vps). The CLI runs on that server, configures K3s, delegates its host lifecycle to the official installer and generated uninstall scripts, and installs checksum-verified, pinned Helm charts. It uses Linux and Kubernetes interfaces rather than provider-specific APIs.

See [how AK3S uses upstream K3s](k3s-lifecycle.md) for the division of responsibilities and the installation sequence.

## Traffic and services

Application DNS points to the VPS. ServiceLB exposes ports 80 and 443, and NGINX routes requests to application Services. cert-manager issues and renews HTTPS certificates. Leave these ports free and make port 80 publicly reachable for Let's Encrypt HTTP-01 validation, which checks control of the domain through an HTTP request.

ServiceLB does not allocate public IPs, configure DNS, or provision a cloud load balancer. Its default ingress configuration does not preserve the original client IP.

The AK3S dashboard, Headlamp, VictoriaMetrics, and VictoriaLogs use private ClusterIP services.
The dashboard runs as a non-root Deployment in namespace `ak3s`, reads nodes and
workload status through a restricted service account, and mounts sanitized
AK3S configuration. Reconciliation updates this configuration and waits for the
dashboard rollout. By default, a NetworkPolicy blocks pod-network ingress.
Reach these services through [authenticated Kubernetes port-forwarding](operations.md#access).
Optional [shared HTTPS access](operations.md#shared-hostname) allows the bundled
NGINX controller through that policy. NGINX terminates TLS, and the dashboard
proxies `/metrics`, `/logs`, and `/headlamp` to fixed internal Services. A mounted
Secret supplies the dashboard, metrics, and logs login; Headlamp keeps its
Kubernetes authentication. Internal metrics and log collection URLs are unchanged.
`dashboard_shared_paths` also enables those paths through a local dashboard
port-forward without creating an ingress or relaxing the NetworkPolicy. The
deployed dashboard uses relative tool links for both local and HTTPS access.
Logs contain container standard output and standard error (stdout/stderr).
Metrics-server supplies resource metrics; VictoriaMetrics stores longer-term metrics.

## Availability and cost

The default datastore is SQLite. One server failure stops the cluster. Local-path volumes, including metrics and logs, need separate backups and disk monitoring.

A new redundant control plane requires an odd number of at least three embedded-etcd servers and an externally managed API load balancer or virtual IP. etcd is a distributed datastore that requires a quorum, or majority of servers, to remain available. This increases cost and does not replicate application volumes. Converting SQLite to etcd requires explicit migration.

The main expense is the VPS and disk; provider charges for networking and backups may also apply. Support focuses on control-plane operation; managing worker nodes is outside scope.
