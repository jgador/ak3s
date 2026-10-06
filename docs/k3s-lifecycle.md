# How AK3S uses upstream K3s

K3s supplies the Kubernetes distribution and its host lifecycle. AK3S supplies
configuration, validation, readiness checks, and the additional VPS components.
Operators need the released AK3S executable and YAML overrides; Go is only needed
to build AK3S from source.

| Responsibility | Owner |
| --- | --- |
| Install or replace the K3s binary | Official K3s `install.sh` |
| Create, enable, and restart the service | Official K3s `install.sh` |
| Generate utility links and removal scripts | Official K3s `install.sh` |
| Stop containers and remove K3s networking and local data | Generated K3s uninstall and killall scripts |
| Pin and verify artifacts, check ownership and upgrades | AK3S |
| Write K3s YAML and host prerequisites | AK3S |
| Wait for the API and local server node, install bundled charts | AK3S |

## Installation and upgrade sequence

1. Read `/etc/ak3s/values.yaml`, inspect the host, and check ownership, identity,
   token consistency, supported versions, and custom configuration conflicts.
2. Download the exact installer revision and K3s binary pinned in
   [`platform/versions.yaml`](../platform/versions.yaml) over HTTPS. Verify their
   SHA-256 checksums. Prepare and validate the pinned charts before host changes.
3. Acquire the local lock and check that the inspected state has not changed.
   Install host prerequisites (`ca-certificates`, `curl`, `iptables`, `kmod`),
   write a recovery checkpoint, and write private K3s configuration.
4. Run the unmodified upstream installer with the explicit node role and
   `--config /etc/rancher/k3s/config.yaml`. `INSTALL_K3S_VERSION` always selects
   the release pin. `INSTALL_K3S_ARTIFACT_URL` points to a private local directory
   containing the verified binary and checksum manifest. Upstream's curl
   downloader reads this `file://` URL, verifies the binary, and installs it.
5. Let the installer generate and enable the service and removal scripts.
   Force a restart when configuration changed, the service was inactive, or a
   previous attempt was interrupted. Upstream does not include `config.yaml`
   in its own change detection.
6. Verify the installed binary, save fingerprints of the generated scripts, and
   wait for readiness. Save the completed K3s checkpoint. On a server, install the
   additional components through the pinned Helm charts and Kubernetes manifests.

The local artifact directory and Helm workspace are private and removed after
the command. The installer cannot select an ambient release channel, alternate
data directory, or token from the calling shell. It receives only the explicit
installer options and a fixed environment. Proxy and certificate settings may
be used for AK3S artifact downloads; they are not persisted into the K3s service.
Cluster settings live in YAML, following
[upstream configuration guidance](https://docs.k3s.io/installation/configuration).

An unchanged active, enabled installation skips the installer. AK3S still checks
readiness and reconciles the requested platform components. It does not write a
K3s executable or systemd unit itself, maintain a copy of upstream service logic,
or run killall during an upgrade.

## Maintaining the pins

The installer commit and SHA-256 are pinned independently from the K3s release.
Use the commit printed by `ak3s version` to review the unmodified source at
`https://raw.githubusercontent.com/k3s-io/k3s/<commit>/install.sh`. Its expected
SHA-256 is in [`platform/versions.yaml`](../platform/versions.yaml). When
updating its pin, review upstream changes to install, restart, and removal
behavior, verify the downloaded SHA-256 independently, and run the isolated
installer contract test. Preserve the exact role and config arguments on every
run, as [upstream manual upgrades](https://docs.k3s.io/upgrades/manual) require.

K3s version changes also require review of release notes, Kubernetes version
skew, backup compatibility, and the architecture-specific checksums. Keep
server upgrades sequential and upgrade servers before agents. The CLI enforces
local version ordering; it cannot establish cluster-wide quorum or version skew.

See [migration](migration.md), [removal](operations.md#removal), and
[development and runtime testing](testing.md) for the operational steps.
