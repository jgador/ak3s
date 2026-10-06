# WSL2 testing before a VPS deployment

Use this guide to try AK3S inside a disposable Ubuntu environment in Windows Subsystem for Linux 2 (WSL2) before installing it on a [VPS](vps.md#what-is-a-vps). It runs the real AK3S command-line interface (CLI), K3s, systemd services, container networking, Helm releases, and persistent volumes. Go and Docker Desktop are unnecessary when using a published AK3S release.

## What the tests cover

| Check | WSL2 tests | Repeat on the VPS |
| --- | --- | --- |
| CLI, pinned versions, dry-run, install, reruns | Published release or local build; published path also checks the release installer | Yes, with VPS configuration |
| Kubernetes, ingress, cert-manager, storage, dashboard, metrics and logs | Real workloads and local HTTPS | Same runtime checklist |
| Service recovery, interruption, backup and restore, upgrades | Disposable distribution tests | VPS reboot and production backup method |
| Public DNS, external API access, provider firewall, Let's Encrypt | Not exercised by local test certificates | Required from an external client |
| Kernel, disk capacity, uptime, redundant control plane | WSL kernel, capped expanding disk, workstation shutdown and restart, single node | Actual VPS kernel and disk, reboot, private network and quorum |

WSL2 provides a local test environment. Its results do not guarantee compatibility with every VPS plan. A full local Linux virtual machine (VM) provides kernel and boot behavior closer to a VPS. Use the same Ubuntu release and AK3S version as the intended VPS, and repeat the checks there before deploying real workloads. A redundant etcd control plane requires quorum: a majority of servers must remain available.

## 1. Create a dedicated distribution

Use an up-to-date WSL2 installation on Windows 11. Run the following in **PowerShell**; Windows may request administrator privileges or a restart while installing or updating WSL:

```powershell
wsl --update
wsl --version
wsl --list --verbose
wsl --list --online
wsl --install --distribution Ubuntu-24.04 --name AK3S-Lab --version 2 --vhd-size 40GB
wsl --list --verbose
wsl --distribution AK3S-Lab
```

`AK3S-Lab` must be a new distribution, and its WSL version must be `2`. Complete Ubuntu's first-run user setup. `--vhd-size 40GB` caps the new distribution's disk to approximate the starting VPS size; use your planned VPS size instead if larger. The `--name` and `--vhd-size` options require a current WSL release and an image that supports named installations. If unavailable, update WSL or [import a fresh official Ubuntu WSL image](https://learn.microsoft.com/en-us/windows/wsl/use-custom-distro) under `AK3S-Lab` with `--version 2`, and check its disk capacity separately. Do not clone your regular development distribution for this test.

AK3S changes packages, kernel settings, services, and files under `/etc` and `/var/lib` inside the test environment. WSL2 distributions share VM resources and a kernel, so a separate distribution does not provide the same isolation as a separate VPS. Stop other clusters and disable Docker Desktop integration for `AK3S-Lab` during testing. Use only fictional demo workloads and test data. Keep the test environment's shell open while testing; workstation sleep and WSL shutdown interrupt the cluster.

## 2. Match the host prerequisites

Inside **AK3S-Lab**, edit `/etc/wsl.conf` with `sudo nano /etc/wsl.conf`. Merge these settings with any existing sections:

```ini
[boot]
systemd=true
```

Keep the distribution's existing hostname. `AK3S-Lab` is only the distribution name used by Windows commands; it is not a browser address. Use `localhost` for local access as described below.

In **Windows**, edit `%UserProfile%\.wslconfig` (for example with `notepad.exe "$env:USERPROFILE\.wslconfig"` in PowerShell). Preserve unrelated settings and merge:

```ini
[wsl2]
memory=4GB
processors=2
swap=0
networkingMode=nat
localhostForwarding=true
```

These settings affect **all WSL2 distributions**, including Docker Desktop's WSL environment. Save a copy of the original configuration and stop other workloads first. The RAM and CPU settings limit the shared WSL VM; run only the test environment to approximate a VPS with 2 virtual CPUs and 4 GB RAM. Network address translation (NAT) keeps the test environment behind Windows rather than making it a server on the local network. This guide does not add router forwarding or open Windows inbound firewall rules.

Save work in other WSL sessions, then restart WSL in **PowerShell**:

```powershell
wsl --shutdown
wsl --distribution AK3S-Lab
```

Back inside **AK3S-Lab**:

```bash
cat /etc/os-release
uname -m
ps -p 1 -o comm=
hostname
nproc
free -h
swapon --show
df -h /
stat -fc %T /sys/fs/cgroup
cat /sys/fs/cgroup/cgroup.controllers
sudo apt-get update
sudo apt-get install -y ca-certificates curl iproute2 iptables kmod
sudo modprobe overlay
sudo modprobe br_netfilter
```

Check for Ubuntu 24.04+, `x86_64` or `aarch64`, `systemd` as process ID (PID) 1, approximately 4 GB RAM, and no active swap. Record the existing hostname; no particular hostname is required. The cgroup filesystem, which controls process resources, should be `cgroup2fs`, with `cpu`, `memory`, and `pids` controllers. Both `modprobe` commands must succeed; built-in features need not appear in `lsmod`. AK3S uses these same module-loading commands during installation.

Ensure at least 40 GB is available on the Windows drive holding the distribution. WSL's expanding virtual disk and `df` can report more capacity than the Windows host has free, even with a virtual-disk size cap. Reserve the space on Windows; a full VM with a dedicated disk behaves more like a VPS when disk space is low. Keep cluster data on the distribution's Linux filesystem, not `/mnt/c` or another Windows mount.

Check that ports 80, 443, and 6443 are unused before installation:

```bash
sudo ss -lntup
```

If a module fails to load, cgroup controllers are missing, or systemd or swap checks fail, resolve the problem before installing. Update WSL and restart it; see [troubleshooting](#troubleshooting) for kernel mismatches. Do not bypass AK3S host checks or substitute another Kubernetes distribution to make the tests pass.

## 3. Choose a published release or local build

Choose one of the following paths inside **AK3S-Lab**. Both continue with the same configuration and runtime checks below. If no release exists yet, use the local build path.

For separate first-install tests of both versions, use a fresh lab for each run, or restore a clean backup made before installing K3s. Installing a different CLI over an existing cluster tests a rerun or upgrade rather than a first installation. Run only one test cluster at a time to avoid port conflicts. Keep the published version's results separate from the local build's results.

### A. Test a published release

Run in **AK3S-Lab**, from its Linux home directory. Replace `vX.Y.Z` with a published [release](https://github.com/jgador/ak3s/releases):

```bash
cd ~
mkdir -p ak3s-test
cd ak3s-test
VERSION=vX.Y.Z
curl -fL "https://github.com/jgador/ak3s/releases/download/$VERSION/install.sh" -o install.sh
less install.sh
sudo bash install.sh "$VERSION"
ak3s version
```

This is the same checksum-verifying installer used on a VPS. Confirm `ak3s version` matches the chosen release. Use the local demo from that same tag in the runtime checklist.

### B. Test local source before release

Build the exact source you intend to release, including any uncommitted changes you want to test. Copy your AK3S checkout into **AK3S-Lab**'s Linux filesystem, for example `~/ak3s`, including those changes. A fresh clone does not include uncommitted changes from another checkout. Avoid building under `/mnt/c` or another Windows mount.

Install Git, Make, and a C compiler inside **AK3S-Lab**. The compiler is used by the race tests in `make verify`:

```bash
sudo apt-get install -y git build-essential
```

If you do not have a local checkout to copy, clone the repository inside **AK3S-Lab**, then select the branch or commit you intend to release:

```bash
cd ~
git clone https://github.com/jgador/ak3s.git
cd ak3s
# Switch to the intended branch or commit before building.
```

Install **Go 1.25+** inside the lab using the [Go installation instructions](https://go.dev/doc/install); check it with `go version`. Ubuntu's default Go package may be older than the required version.

From the checkout's root directory, run:

```bash
go version
git rev-parse HEAD
git status --short
go mod download
make verify
go test -tags integration -run TestPinnedCharts -v ./internal/ak3s
make build
sudo install -m 0755 bin/ak3s /usr/local/bin/ak3s
ak3s version
export AK3S_LOCAL_APP_FILE="$PWD/examples/local-app.yaml"
```

Stop if a check fails. `make verify` runs tests with race detection, `go vet`, formatting checks, and installer syntax checks. The integration check verifies pinned downloads and Helm rendering over outbound HTTPS. Neither command installs a cluster. Installing `bin/ak3s` places the CLI on the lab's path; the cluster installation comes below.

The default build reports version `dev` and a commit identifier. Record the full commit, whether the checkout has uncommitted changes, and the test results. The embedded commit identifier does not identify uncommitted changes; preserve the tested changes before publishing. Rebuild and repeat affected checks after source changes. Use the demo from this checkout when following the runtime checklist; define `AK3S_LOCAL_APP_FILE` again from the checkout root in any new testing terminal.

This path tests the local CLI and cluster behavior. For release artifact and checksum generation, also follow [development checks](testing.md). It does not exercise the published release download and checksum verification; repeat path A after publishing to validate the release installer.

For the local-source path, also build the dashboard image with Docker in the
lab or on another Linux amd64/arm64 machine:

```bash
make dashboard-image
docker save ghcr.io/jgador/ak3s-dashboard:dev -o .tmp/dashboard-image.tar
```

If built elsewhere, transfer that archive privately to the lab. On a fresh lab,
K3s must start before the archive can be imported. Add `platform: false` to the
configuration below for the first local-source install. This installs K3s
without shared add-ons. After completing that first install using the commands
below, return to the checkout and import the image:

```bash
sudo k3s ctr images import .tmp/dashboard-image.tar
rm .tmp/dashboard-image.tar
```

Remove `platform: false` from the operator configuration and rerun
`sudo ak3s apply --dry-run` followed by `sudo ak3s apply` to install the full
platform. Published releases pull their dashboard image automatically and do
not need this bootstrap step. On an existing local test cluster, import the
image before applying. See the [dashboard build guide](../dashboard/README.md#build-and-deploy-local-source)
for image updates and custom registries.

### Configure and install the cluster (both paths)

Create the test configuration:

```bash
sudo install -d -m 0755 /etc/ak3s
sudo nano /etc/ak3s/values.yaml
```

```yaml
acme_email: you@example.com # replace with your own valid contact address
kubernetes_api_endpoint: localhost
node:
  name: localhost
```

This example is for a **new cluster**. `node.name` is the Kubernetes node's identity, independent of the WSL hostname and browser address. Using `localhost` as the node name does not rename the distribution or change network routing. If omitted for a new cluster, AK3S uses the existing hostname in lowercase.

For an already installed cluster, preserve its original `node.name` when rerunning AK3S. For example, if `sudo k3s kubectl --kubeconfig /etc/rancher/k3s/k3s.yaml get nodes` reports `ak3s-lab`, keep `node.name: ak3s-lab` in that cluster's configuration. It can still be accessed through `localhost`. Changing the node name requires an explicit migration; use a fresh disposable cluster to test a different name.

The full platform requires a contact email and creates Let's Encrypt ClusterIssuers. Those issuers may register accounts over outbound HTTPS using ACME (Automated Certificate Management Environment), the protocol for automated certificate issuance. The local demo uses a separate self-signed issuer. No public domain is required. Leave `headlamp_hostname` unset to keep the dashboard private.

Keep `kubernetes_api_endpoint` on loopback (`localhost` or `127.0.0.1`) for this single-node test environment: WSL's NAT IP can change after restart. This setting adds an address to the API certificate; it does not restrict which addresses K3s listens on. K3s and ingress still listen on the node. For the real VPS, use a reachable VPS IP or API DNS name and apply the [VPS firewall requirements](vps.md#firewall-requirements).

Preview, review, then install:

```bash
sudo ak3s install --dry-run
sudo ak3s install
sudo ak3s status
```

Dry-run checks downloads and renders charts but cannot prove runtime readiness. Stop and fix any `BLOCKED` message or failed command. A successful install should leave an active K3s service and a Ready node; the next step checks the platform and real workloads.

## 4. Run the shared runtime checks

Follow [Runtime testing on WSL2 or a VPS](runtime-testing.md) inside **AK3S-Lab**. That checklist deploys a local demo through NGINX, obtains a self-signed certificate through cert-manager, checks storage across pod replacement, tests dashboard permissions, and verifies metrics and log ingestion. It also checks reruns and recovery.

For ingress requests, select the test environment's current primary IPv4, not a pod bridge address:

```bash
export AK3S_TEST_ADDRESS=$(ip -4 route get 192.0.2.1 | awk '{for (i=1;i<=NF;i++) if ($i=="src") {print $(i+1); exit}}')
printf '%s\n' "$AK3S_TEST_ADDRESS"
```

This only looks up a route; it sends no traffic to the example address. Recompute it after every WSL restart. The runtime guide uses `curl --resolve` so neither public DNS nor a hosts-file edit is needed. The demo's `hello.test` hostname selects its ingress rule and certificate; it is separate from the node name. Keep this check on the WSL IP because K3s ServiceLB routes ingress through host-port rules, which localhost forwarding may not expose as a listening socket.

For background access from a local checkout, stop any manual port-forwards from
the runtime checklist and run:

```bash
make port-forward        # starts all four UIs and returns to the shell
make port-forward-status
# When finished:
make port-forward-stop
```

The [access guide](operations.md#start-all-uis-for-local-testing) covers
prerequisites, logs, and the Headlamp login token. Use
`make port-forward DASHBOARD=0` to skip the dashboard forward.
Without a checkout, use the manual port-forwards in separate terminals.

In Windows, open `http://127.0.0.1:5173` for the AK3S dashboard,
`http://127.0.0.1:8080` for Headlamp, `http://127.0.0.1:8428/vmui/` for metrics,
and `http://127.0.0.1:9428/select/vmui/` for logs. The dashboard runs inside the cluster; no local Node.js process is needed.
All four tools read the test cluster. These requests test
Windows-to-WSL access in addition to the Linux checks. With
`localhostForwarding=true`, no SSH tunnel is needed for this local setup.
`localhost` also works if it resolves to IPv4. If localhost forwarding is
unavailable, run the curl checks inside the test environment first and follow
[WSL networking guidance](https://learn.microsoft.com/en-us/windows/wsl/networking).
Do not use `--address 0.0.0.0` to make private services reachable.

### Test shared UI paths

Use the same `/metrics`, `/logs`, and `/headlamp` paths as on the VPS through one
local dashboard port-forward. First build and deploy the updated CLI and dashboard
image using [the local source instructions](#b-test-local-source-before-release).
In your existing `/etc/ak3s/values.yaml`, add these overrides and preserve your
contact email, Kubernetes API endpoint, and node settings:

```yaml
dashboard_shared_paths: true
dashboard_hostname: ""
```

Leave `headlamp_hostname` unset. Apply the configuration, then forward the
dashboard Service from WSL:

```bash
sudo ak3s apply --dry-run
sudo ak3s apply
sudo k3s kubectl --kubeconfig /etc/rancher/k3s/k3s.yaml -n ak3s port-forward --address 127.0.0.1 service/dashboard 5173:80
```

If port 5173 is already occupied, stop the existing dashboard forward or Vite
server first. The port-forward command stays running. In Windows, open:

| URL | UI |
| --- | --- |
| `http://localhost:5173/` | AK3S dashboard |
| `http://localhost:5173/metrics` | VictoriaMetrics |
| `http://localhost:5173/logs` | VictoriaLogs |
| `http://localhost:5173/headlamp` | Headlamp |

`127.0.0.1` also works. The dashboard cards use the address in your browser, so
they stay on the WSL test cluster. Metrics and logs redirect to their native UI
paths under the same prefixes. No public DNS, dashboard login Secret, or HTTPS
certificate is needed for these local requests. Headlamp still needs its temporary
Kubernetes viewer token, as described in the [access guide](operations.md#access).

Verify a metrics query, a log search, and Headlamp resource browsing. This tests
the shared paths and Windows-to-WSL forwarding. Test NGINX ingress, trusted HTTPS,
and the public password prompt separately on the VPS.

If you also use separate tool port-forwards or Vite development, direct Headlamp
access is `http://127.0.0.1:8080/headlamp/` while shared paths are enabled. The
metrics and logs direct URLs stay the same. Set `dashboard_shared_paths: false`
and apply again to return to the default separate-tool setup.

### Test a distribution stop and start

After the runtime checklist has written its persistent-storage marker, stop K3s cleanly in **AK3S-Lab**:

```bash
sudo systemctl stop k3s
sync
exit
```

In **PowerShell**:

```powershell
wsl --terminate AK3S-Lab
wsl --distribution AK3S-Lab
```

Inside the test environment, confirm that the enabled service starts automatically:

```bash
sudo systemctl is-enabled k3s
sudo systemctl is-active k3s
sudo k3s kubectl --kubeconfig /etc/rancher/k3s/k3s.yaml wait --for=condition=Ready nodes --all --timeout=300s
sudo ak3s status
```

Repeat runtime readiness and marker checks with a newly computed `AK3S_TEST_ADDRESS`. If the service does not start, inspect `sudo journalctl -u k3s -n 100` and mark the test as failed until the problem is fixed. Stopping and starting WSL tests the distribution's startup behavior, not a full independent Linux VM boot; also test `sudo reboot` on the disposable VPS.

## Back up and restore the test environment

For this single-node SQLite test environment, exporting the whole distribution while it is stopped provides a recovery test. The export includes cluster credentials and volumes: keep it private and outside the distribution being backed up. Also test the [backup method](operations.md#backups) used to store production data outside the server.

Stop K3s and exit as above. In **PowerShell**, use a new backup filename:

```powershell
New-Item -ItemType Directory -Force "$env:USERPROFILE\AK3S-Backups"
wsl --terminate AK3S-Lab
wsl --export AK3S-Lab "$env:USERPROFILE\AK3S-Backups\ak3s-lab.tar"
```

Keep the original test environment stopped. Import the backup into a **new** distribution and empty install directory:

```powershell
wsl --import AK3S-Restore "$env:LOCALAPPDATA\AK3S-Restore" "$env:USERPROFILE\AK3S-Backups\ak3s-lab.tar" --version 2
wsl --distribution AK3S-Restore
```

Imported distributions may start as root. Verify K3s starts, the node becomes Ready, all platform workloads recover, and the persistent marker remains readable using the runtime checklist. Run only one copy of the backed-up cluster at a time; the restored copy deliberately keeps the test environment's node name and credentials. Do not run both copies to simulate a redundant control plane.

## Clean up

To reuse the current WSL distribution for a fresh AK3S test or another installer,
invoke `$clean-slate` in Codex from this repository. The repository-local
[AK3S cleanup skill](../.agents/skills/clean-slate/SKILL.md) removes the verified
local cluster, its persistent data and credentials, and AK3S configuration,
including `/etc/ak3s/values.yaml`. It preserves repositories, developer tools,
the AK3S CLI, and shared services. It checks ownership before deleting anything
and does not unregister WSL or reinstall a cluster. Recreate the test configuration
before installing AK3S again.

After saving any test results you need, close port-forward commands and stop K3s in each test distribution. In **PowerShell**, inspect the distribution names before deleting. **The following commands permanently delete all data in the named test distributions**; run the `AK3S-Restore` command only if you created that distribution:

```powershell
wsl --list --verbose
wsl --unregister AK3S-Lab
wsl --unregister AK3S-Restore
```

Remove private test exports when no longer needed. Restore the original `%UserProfile%\.wslconfig` settings, then save work in other sessions before `wsl --shutdown` to apply them. The export and import commands also let you restore a clean starting environment for another test run without manually deleting AK3S ownership files.

## Move to the VPS

Create a fresh VPS that meets the [host requirements](vps.md#host-requirements). Install the **same tested AK3S release**, with a real contact email and a Kubernetes API endpoint for the VPS. If you tested local source, publish the tested source and repeat the published-release path first. Do not copy the test environment's datastore, token, kubeconfig, self-signed TLS secret, or WSL configuration into the VPS.

Repeat the runtime checklist there, including its [public VPS checks](runtime-testing.md#public-vps-checks). Confirm external DNS, API access restrictions, HTTP and HTTPS routing, staging then production certificate issuance, reboot recovery, and restoration using the real backup destination. Match the workload and retention settings you intend to run; successful tests with 4 GB RAM do not establish the resources needed for a production deployment.

## Troubleshooting

| Symptom | Check or action |
| --- | --- |
| systemd required or PID 1 is not systemd | Enable `[boot] systemd=true`, update WSL, then shut down and reopen WSL |
| Swap check fails after restart | Set global `swap=0`, remove any test swap entry from `/etc/fstab`, restart WSL, and recheck `swapon --show` |
| `modprobe overlay` or `br_netfilter` fails | Update WSL and restart; the module files must match `uname -r`. Ubuntu's generic kernel modules do not match the WSL kernel. Use a matching WSL kernel and modules installation or a full VM if the feature remains unavailable |
| Missing `memory`, `cpu`, or `pids` cgroup controller | Update WSL, use systemd and cgroup v2, and recheck before installing |
| Pending ServiceLB pods or ingress unreachable | Check other WSL clusters, Docker integration, port conflicts, `kubectl describe`, and the current primary WSL IP |
| Windows browser fails but test curl succeeds | Check localhost forwarding, Windows port conflicts, Windows and Hyper-V firewalls, and whether networking uses NAT or mirrored mode |
| Certificate for `hello.test` is not trusted | Expected for the local self-signed demo; only that demo uses `curl -k`. Public trusted issuance is a separate VPS check |
| ACME errors for a local hostname | Use `local-app.yaml`, not the public-domain example. `hello.test` is not eligible for Let's Encrypt |
| Pods pending, OOMKilled (terminated because memory was exhausted), or image downloads fail | Check actual RAM and disk space, persistent volume claim (PVC) events, outbound HTTPS, registry connectivity, and `journalctl -u k3s` |
| Interrupted installation or failed chart | Fix the underlying failure and rerun AK3S; preserve its ownership, configuration, and state files |
| First install fails, but K3s is active and the node later becomes Ready | Check the API with `sudo k3s kubectl --kubeconfig /etc/rancher/k3s/k3s.yaml get --raw=/readyz`, then rerun `sudo ak3s install` with the same configuration. A Ready node alone does not confirm platform installation; missing ClusterIssuers can make `status` fail until cert-manager is installed |

Reference: Microsoft's [WSL commands](https://learn.microsoft.com/en-us/windows/wsl/basic-commands), [configuration](https://learn.microsoft.com/en-us/windows/wsl/wsl-config), and [systemd setup](https://learn.microsoft.com/en-us/windows/wsl/systemd).
