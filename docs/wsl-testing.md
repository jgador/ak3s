# WSL2 rehearsal before a VPS deployment

Use this guide to try AK3S inside a disposable WSL2 Ubuntu environment before installing it on a [VPS](vps.md#what-is-a-vps). It runs the real AK3S CLI, K3s, systemd services, container networking, Helm releases, and persistent volumes. Go and Docker Desktop are unnecessary when using a published AK3S release.

## What the rehearsal covers

| Check | WSL2 rehearsal | Repeat on the VPS |
| --- | --- | --- |
| Installer, pins, dry-run, install, reruns | Same AK3S release and commands | Yes, with VPS configuration |
| Kubernetes, ingress, cert-manager, storage, dashboard, metrics/logs | Real workloads and local HTTPS | Same runtime checklist |
| Service recovery, interruption, backup/restore, upgrades | Disposable distro exercises | VPS reboot and production backup method |
| Public DNS, external API access, provider firewall, Let's Encrypt | Not exercised by local test certificates | Required from an external client |
| Kernel, disk capacity, uptime, redundant control plane | WSL kernel, capped expanding disk, workstation lifecycle, single node | Actual VPS kernel/disk, reboot, private network and quorum |

WSL2 is a rehearsal environment, not a guarantee of compatibility with every VPS plan. A full local Linux VM provides closer kernel and boot behavior. Use the same Ubuntu release and AK3S version as the intended VPS, and repeat the checks there before deploying real workloads.

## 1. Create a dedicated distro

Use an up-to-date WSL2 installation on Windows 11. Run the following in **PowerShell**; Windows may request elevation or a restart while installing/updating WSL:

```powershell
wsl --update
wsl --version
wsl --list --verbose
wsl --list --online
wsl --install --distribution Ubuntu-24.04 --name AK3S-Lab --version 2 --vhd-size 40GB
wsl --list --verbose
wsl --distribution AK3S-Lab
```

`AK3S-Lab` must be a new distro, and its version must be `2`. Complete Ubuntu's first-run user setup. `--vhd-size 40GB` caps the new distro disk to approximate the starting VPS size; use your planned VPS size instead if larger. The `--name` and `--vhd-size` options require a current WSL release and an image that supports named installations. If unavailable, update WSL or [import a fresh official Ubuntu WSL image](https://learn.microsoft.com/en-us/windows/wsl/use-custom-distro) under `AK3S-Lab` with `--version 2`, and check its disk capacity separately. Do not clone an everyday development distro for this exercise.

AK3S changes packages, kernel settings, services, and files under `/etc` and `/var/lib` inside the lab. WSL2 distributions share VM resources and a kernel, so a separate distro is not the same isolation as a separate VPS. Stop other clusters and disable Docker Desktop integration for `AK3S-Lab` during the rehearsal. Use only fictional demo workloads and lab data. Keep the lab shell open while testing; workstation sleep and WSL shutdown interrupt the cluster.

## 2. Match the host prerequisites

Inside **AK3S-Lab**, edit `/etc/wsl.conf` with `sudo nano /etc/wsl.conf`. Merge these settings with any existing sections:

```ini
[boot]
systemd=true

[network]
hostname=ak3s-lab
```

In **Windows**, edit `%UserProfile%\.wslconfig` (for example with `notepad.exe "$env:USERPROFILE\.wslconfig"` in PowerShell). Preserve unrelated settings and merge:

```ini
[wsl2]
memory=4GB
processors=2
swap=0
networkingMode=nat
localhostForwarding=true
```

These settings affect **all WSL2 distros**, including Docker Desktop's WSL environment. Save a copy of the original configuration and stop other workloads first. The RAM/CPU settings limit the shared WSL VM; run only the lab to approximate a 2-vCPU/4-GB VPS. NAT keeps the default rehearsal behind Windows rather than making it a LAN server. This guide does not add router forwarding or open Windows inbound firewall rules.

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

Check for Ubuntu 24.04+, `x86_64` or `aarch64`, PID 1 `systemd`, hostname `ak3s-lab`, approximately 4 GB RAM, and no active swap. The cgroup filesystem should be `cgroup2fs`, with `cpu`, `memory`, and `pids` controllers. Both `modprobe` commands must succeed; built-in features need not appear in `lsmod`. AK3S uses these same module-loading commands during installation.

Ensure at least 40 GB is actually available on the Windows drive holding the distro. WSL's expanding virtual disk and `df` can advertise more capacity than the Windows host has free, even with a virtual-disk size cap. Reserve the space on Windows; a full VM with a dedicated disk provides closer disk-pressure behavior. Keep cluster data on the distro's Linux filesystem, not `/mnt/c` or another Windows mount.

Check that ports 80, 443, and 6443 are unused before installation:

```bash
sudo ss -lntup
```

If a module fails to load, cgroup controllers are missing, or systemd/swap checks fail, resolve that before installing. Update WSL and restart it; see [troubleshooting](#troubleshooting) for kernel mismatches. Do not bypass AK3S host checks or substitute another Kubernetes distribution to get the rehearsal to pass.

## 3. Install the same AK3S release as the VPS

Run in **AK3S-Lab**, from its Linux home directory. Replace `vX.Y.Z` with a published [release](https://github.com/jgador/ak3s/releases):

```bash
cd ~
mkdir -p ak3s-rehearsal
cd ak3s-rehearsal
VERSION=vX.Y.Z
curl -fL "https://github.com/jgador/ak3s/releases/download/$VERSION/install.sh" -o install.sh
less install.sh
sudo bash install.sh "$VERSION"
ak3s version
```

This is the same checksum-verifying installer used on a VPS. If testing unpublished source instead, clone this repository into the lab's Linux filesystem, follow [development checks](testing.md), build it inside the lab, and use `sudo install -m 0755 bin/ak3s /usr/local/bin/ak3s`. Record that it is a development build; this does not exercise the published release installer. Use the checkout's `examples/local-app.yaml` for the runtime demo if it is not available in the published tag yet.

Create the lab configuration:

```bash
sudo install -d -m 0755 /etc/ak3s
sudo nano /etc/ak3s/values.yaml
```

```yaml
acme_email: you@example.com # replace with your own valid contact address
api_endpoint: 127.0.0.1
node:
  name: ak3s-lab
```

The full platform requires a contact email and creates Let's Encrypt ClusterIssuers. Those issuers may register ACME accounts over outbound HTTPS, but the local demo uses a separate self-signed issuer. No public domain is required. Leave `headlamp_hostname` unset to keep the dashboard private.

Keep `api_endpoint` on loopback for this single-node lab: WSL's NAT IP can change after restart. This setting is a certificate endpoint, not a bind restriction; K3s and ingress still listen on the node. For the real VPS, use a reachable VPS IP or API DNS name and apply the [VPS firewall requirements](vps.md#firewall-requirements).

Preview, review, then install:

```bash
sudo ak3s install --dry-run
sudo ak3s install
sudo ak3s status
```

Dry-run checks downloads and renders charts but cannot prove runtime readiness. Stop and fix any `BLOCKED` message or failed command. A successful install should leave an active K3s service and a Ready node; the next step checks the platform and real workloads.

## 4. Run the shared runtime checks

Follow [Runtime testing on WSL2 or a VPS](runtime-testing.md) inside **AK3S-Lab**. That checklist deploys a local demo through NGINX, obtains a self-signed certificate through cert-manager, checks storage across pod replacement, tests dashboard permissions, and verifies metrics/log ingestion. It also checks reruns and recovery.

For ingress requests, select the lab's current primary IPv4, not a pod bridge address:

```bash
export AK3S_TEST_ADDRESS=$(ip -4 route get 192.0.2.1 | awk '{for (i=1;i<=NF;i++) if ($i=="src") {print $(i+1); exit}}')
printf '%s\n' "$AK3S_TEST_ADDRESS"
```

This only looks up a route; it sends no traffic to the example address. Recompute it after every WSL restart. The runtime guide uses `curl --resolve` so neither public DNS nor a hosts-file edit is needed.

For Headlamp and the observability UIs, keep their port-forward commands in separate lab terminals. In Windows, try `http://127.0.0.1:8080`, `http://127.0.0.1:8428/vmui/`, and `http://127.0.0.1:9428/select/vmui/`. These exercise Windows-to-WSL access in addition to the Linux checks. If localhost forwarding is unavailable, run the curl checks inside the lab first and follow [WSL networking guidance](https://learn.microsoft.com/en-us/windows/wsl/networking). Do not expose private services with `--address 0.0.0.0` as a workaround.

### Test a distro stop and start

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

Inside the lab, confirm that the enabled service starts automatically:

```bash
sudo systemctl is-enabled k3s
sudo systemctl is-active k3s
sudo k3s kubectl --kubeconfig /etc/rancher/k3s/k3s.yaml wait --for=condition=Ready node/ak3s-lab --timeout=300s
sudo ak3s status
```

Repeat runtime readiness and marker checks with a newly computed `AK3S_TEST_ADDRESS`. If the service does not start, inspect `sudo journalctl -u k3s -n 100` and fail the rehearsal until fixed. A WSL stop/start exercises distro lifecycle, not a full independent Linux VM boot; also test `sudo reboot` on the disposable VPS.

## Back up and restore the rehearsal

For this single-node SQLite lab, a stopped whole-distro export is a convenient recovery exercise. It includes cluster credentials and volumes: keep it private and outside the distro being backed up. This is not a substitute for testing the [off-host backup method](operations.md#backups) used in production.

Stop K3s and exit as above. In **PowerShell**, use a new backup filename:

```powershell
New-Item -ItemType Directory -Force "$env:USERPROFILE\AK3S-Backups"
wsl --terminate AK3S-Lab
wsl --export AK3S-Lab "$env:USERPROFILE\AK3S-Backups\ak3s-lab.tar"
```

Keep the original lab stopped. Import the backup into a **new** distro and empty install directory:

```powershell
wsl --import AK3S-Restore "$env:LOCALAPPDATA\AK3S-Restore" "$env:USERPROFILE\AK3S-Backups\ak3s-lab.tar" --version 2
wsl --distribution AK3S-Restore
```

Imported distros may start as root. Verify K3s starts, the node becomes Ready, all platform workloads recover, and the persistent marker remains readable using the runtime checklist. Run only one copy of the backed-up cluster at a time; the restored copy deliberately keeps the lab's node name and credentials. Do not run both copies to simulate a redundant control plane.

## Clean up

After retaining any wanted lab results, close port-forwards and stop K3s in each lab distro. In **PowerShell**, inspect the distro names before deleting. **The following commands permanently delete all data in the named lab distros**; run the restore line only if you created it:

```powershell
wsl --list --verbose
wsl --unregister AK3S-Lab
wsl --unregister AK3S-Restore
```

Remove private lab exports when no longer needed. Restore the original `%UserProfile%\.wslconfig` settings, then save work in other sessions before `wsl --shutdown` to apply them. The export/import workflow also lets you restore a clean baseline for another test run without manually deleting AK3S ownership files.

## Move to the VPS

Create a fresh VPS that meets the [host requirements](vps.md#host-requirements). Install the **same tested AK3S release**, with a real contact email and VPS API endpoint. Do not copy the lab's datastore, token, kubeconfig, self-signed TLS secret, or WSL configuration into the VPS.

Repeat the runtime checklist there, including its [public VPS checks](runtime-testing.md#public-vps-checks). Confirm external DNS, API access restrictions, HTTP/HTTPS routing, staging then production certificate issuance, reboot recovery, and restoration using the real backup destination. Match the workload and retention settings you intend to run; a successful 4-GB lab does not size a production deployment.

## Troubleshooting

| Symptom | Check or action |
| --- | --- |
| systemd required / PID 1 is not systemd | Enable `[boot] systemd=true`, update WSL, then shut down and reopen WSL |
| Swap blocker returns after restart | Set global `swap=0`, remove any lab swap entry from `/etc/fstab`, restart WSL, and recheck `swapon --show` |
| `modprobe overlay` or `br_netfilter` fails | Update WSL and restart; the module files must match `uname -r`. Ubuntu's generic kernel modules do not match the WSL kernel. Use a matching WSL kernel/modules installation or a full VM if the feature remains unavailable |
| Missing memory/cpu/pids cgroup controller | Update WSL, use systemd and cgroup v2, and recheck before installing |
| Pending ServiceLB pods / ingress unreachable | Check other WSL clusters, Docker integration, port conflicts, `kubectl describe`, and the current primary WSL IP |
| Windows browser fails but lab curl succeeds | Check localhost forwarding, Windows port conflicts, Windows/Hyper-V firewall, and current NAT vs mirrored networking settings |
| Certificate for `hello.test` is not trusted | Expected for the local self-signed demo; only that demo uses `curl -k`. Public trusted issuance is a separate VPS check |
| ACME errors for a local hostname | Use `local-app.yaml`, not the public-domain example. `hello.test` is not eligible for Let's Encrypt |
| Pods pending, OOMKilled, or image downloads fail | Check actual RAM/disk, PVC events, outbound HTTPS, registry connectivity, and `journalctl -u k3s` |
| Interrupted installation or failed chart | Fix the underlying failure and rerun AK3S; preserve its ownership/configuration/state files |

Reference: Microsoft's [WSL commands](https://learn.microsoft.com/en-us/windows/wsl/basic-commands), [configuration](https://learn.microsoft.com/en-us/windows/wsl/wsl-config), and [systemd setup](https://learn.microsoft.com/en-us/windows/wsl/systemd).
