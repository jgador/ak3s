#!/usr/bin/env python3
"""Install and configure the AK3S platform. No cloud credentials are needed."""

import argparse
import copy
import hashlib
import ipaddress
import json
import os
from pathlib import Path
import re
import secrets
import shutil
import subprocess
import sys
import time
from urllib.error import URLError
from urllib.request import Request, urlopen

import yaml

ROOT = Path(__file__).resolve().parents[1]
DEFAULTS = {
    "cluster_name": "ak3s", "tls_sans": [], "acme_environment": "staging",
    "storage_class": "local-path", "metrics_retention": "7d",
    "metrics_storage": "10Gi", "logs_retention": "7d", "logs_storage": "10Gi",
    "logs_max_disk": "8GiB", "headlamp_hostname": "",
}


class ConfigError(ValueError):
    pass


def read_yaml(path):
    with Path(path).open() as stream:
        return yaml.safe_load(stream)


def private_yaml(path, data):
    path = Path(path)
    path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    with os.fdopen(os.open(path, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600), "w") as stream:
        yaml.safe_dump(data, stream, sort_keys=False)
    path.chmod(0o600)


def hostname(value):
    return isinstance(value, str) and len(value) <= 253 and all(
        re.fullmatch(r"[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?", label)
        for label in value.split(".")
    )


def endpoint(value):
    if not isinstance(value, str):
        return False
    try:
        return ipaddress.ip_address(value).version == 4
    except ValueError:
        return hostname(value)


def load_config(path):
    given = read_yaml(path)
    if not isinstance(given, dict):
        raise ConfigError("Cluster configuration must be a mapping.")
    unknown = given.keys() - (DEFAULTS.keys() | {"api_endpoint", "acme_email"})
    if unknown:
        raise ConfigError("Unknown cluster configuration keys: " + ", ".join(sorted(unknown)))
    cfg = copy.deepcopy(DEFAULTS) | given
    if not hostname(cfg["cluster_name"]) or "." in cfg["cluster_name"] or len(cfg["cluster_name"]) > 63:
        raise ConfigError("cluster_name must be a DNS label.")
    if not endpoint(cfg.get("api_endpoint")):
        raise ConfigError("api_endpoint must be an IPv4 address or DNS hostname without a port.")
    if not isinstance(cfg["tls_sans"], list) or not all(endpoint(x) for x in cfg["tls_sans"]):
        raise ConfigError("tls_sans must contain IPv4 addresses or DNS hostnames.")
    if not re.fullmatch(r"[^\s@]+@[^\s@]+\.[^\s@]+", str(cfg.get("acme_email", ""))):
        raise ConfigError("acme_email must be a valid contact email.")
    if cfg["acme_environment"] not in ("staging", "production"):
        raise ConfigError("acme_environment must be staging or production.")
    if not hostname(cfg["storage_class"]):
        raise ConfigError("storage_class must be a Kubernetes resource name.")
    for key in ("metrics_retention", "logs_retention"):
        if not re.fullmatch(r"[1-9][0-9]*[dwy]", str(cfg[key])):
            raise ConfigError(f"{key} must be at least one day, e.g. 7d.")
    for key in ("metrics_storage", "logs_storage"):
        if not re.fullmatch(r"[1-9][0-9]*(Mi|Gi|Ti)", str(cfg[key])):
            raise ConfigError(f"{key} must be a storage quantity, e.g. 10Gi.")
    if not re.fullmatch(r"[1-9][0-9]*(MiB|GiB|TiB)", str(cfg["logs_max_disk"])):
        raise ConfigError("logs_max_disk must be a VictoriaLogs size, e.g. 8GiB.")
    if cfg["headlamp_hostname"] and not hostname(cfg["headlamp_hostname"]):
        raise ConfigError("headlamp_hostname must be a DNS hostname.")
    return cfg


def load_inventory(path):
    inv = read_yaml(path)
    try:
        groups = inv["all"]["children"]
        servers = groups["k3s_servers"]["hosts"] or {}
        agents = groups.get("k3s_agents", {}).get("hosts") or {}
    except (KeyError, TypeError, AttributeError) as exc:
        raise ConfigError("Inventory needs all.children.k3s_servers.hosts; see examples/inventory.yaml.") from exc
    if not isinstance(servers, dict) or not isinstance(agents, dict):
        raise ConfigError("Inventory hosts must be mappings.")
    if len(servers) != 1 and (len(servers) < 3 or len(servers) % 2 == 0):
        raise ConfigError("Use one server (SQLite) or an odd number of at least three servers (etcd).")
    if servers.keys() & agents.keys():
        raise ConfigError("Each node must belong to exactly one K3s group.")
    seen_hosts, seen_ips = set(), set()
    for name, data in (servers | agents).items():
        if not hostname(name) or "." in name or len(name) > 63:
            raise ConfigError("Node names must be DNS labels of at most 63 characters.")
        if not isinstance(data, dict) or not endpoint(data.get("ansible_host")):
            raise ConfigError("Each host needs an IPv4 address or DNS name in ansible_host.")
        if "ak3s_node_ip" not in data:
            try:
                ipaddress.IPv4Address(data["ansible_host"])
            except ValueError as exc:
                raise ConfigError("Set ak3s_node_ip to an IPv4 address when ansible_host is a DNS name.") from exc
        for field in ("ak3s_node_ip", "ak3s_node_external_ip"):
            if field in data:
                try:
                    if not isinstance(data[field], str) or ipaddress.ip_address(data[field]).version != 4:
                        raise ValueError()
                except (ValueError, TypeError) as exc:
                    raise ConfigError(f"{field} must be an IPv4 address.") from exc
        host, node_ip = data["ansible_host"], data.get("ak3s_node_ip", data["ansible_host"])
        if host in seen_hosts or node_ip in seen_ips:
            raise ConfigError("Each node needs a unique ansible_host and node IP.")
        seen_hosts.add(host)
        seen_ips.add(node_ip)
    return inv, servers, agents


def issuer_manifests(cfg):
    docs = []
    for environment, url in (
        ("staging", "https://acme-staging-v02.api.letsencrypt.org/directory"),
        ("production", "https://acme-v02.api.letsencrypt.org/directory"),
    ):
        docs.append({
            "apiVersion": "cert-manager.io/v1", "kind": "ClusterIssuer",
            "metadata": {"name": f"letsencrypt-{environment}"},
            "spec": {"acme": {"email": cfg["acme_email"], "server": url,
                "privateKeySecretRef": {"name": f"letsencrypt-{environment}-account"},
                "solvers": [{"http01": {"ingress": {"ingressClassName": "nginx"}}}]}}
        })
    return docs


def values_for(chart, cfg):
    values = read_yaml(ROOT / "platform/values" / chart["values"])
    if chart["release"] in ("victoria-metrics", "victoria-logs"):
        prefix = "metrics" if chart["release"] == "victoria-metrics" else "logs"
        server = values["server"]
        server["retentionPeriod"] = cfg[f"{prefix}_retention"]
        server["persistentVolume"].update(storageClassName=cfg["storage_class"], size=cfg[f"{prefix}_storage"])
        if prefix == "logs":
            server["retentionDiskSpaceUsage"] = cfg["logs_max_disk"]
        else:
            server["scrape"]["config"]["global"]["external_labels"] = {"cluster": cfg["cluster_name"]}
    if chart["release"] == "headlamp" and cfg["headlamp_hostname"]:
        host = cfg["headlamp_hostname"]
        values["ingress"] = {
            "enabled": True, "ingressClassName": "nginx",
            "annotations": {"cert-manager.io/cluster-issuer": "letsencrypt-" + cfg["acme_environment"],
                            "nginx.org/ssl-redirect": "true"},
            "hosts": [{"host": host, "paths": [{"path": "/", "type": "Prefix"}]}],
            "tls": [{"secretName": "headlamp-tls", "hosts": [host]}],
        }
    return values


def run(argv, **kwargs):
    return subprocess.run([str(x) for x in argv], check=True, **kwargs)


def require(command):
    if not shutil.which(command):
        raise ConfigError(f"Required tool is missing: {command}.")


def prepare(cfg, state):
    state.mkdir(parents=True, exist_ok=True, mode=0o700)
    state.chmod(0o700)
    charts = read_yaml(ROOT / "platform/versions.yaml")["charts"]
    for chart in charts:
        private_yaml(state / "values" / chart["values"], values_for(chart, cfg))
    with (state / "issuers.yaml").open("w") as stream:
        yaml.safe_dump_all(issuer_manifests(cfg), stream, sort_keys=False)
    return charts


def bootstrap(args, cfg, state):
    inventory, servers, agents = load_inventory(args.inventory)
    del inventory
    require("ansible-playbook")
    state.mkdir(parents=True, exist_ok=True, mode=0o700)
    state.chmod(0o700)
    versions = read_yaml(ROOT / "platform/versions.yaml")
    settings = state / "bootstrap.yaml"
    previous = read_yaml(settings) if settings.exists() else {}
    topology = "sqlite" if len(servers) == 1 else "etcd"
    if previous and previous.get("ak3s_datastore") != topology:
        raise ConfigError("Datastore topology changes require a deliberate K3s migration; see docs/operations.md.")
    if previous and previous.get("ak3s_first_server") != next(iter(servers)):
        raise ConfigError("Keep the original first server first in inventory for subsequent bootstrap runs.")
    private_yaml(settings, {
        "ak3s_join_token": previous.get("ak3s_join_token") or secrets.token_hex(32),
        "ak3s_k3s_version": versions["k3s"],
        "ak3s_installer_sha256": versions["k3s_installer_sha256"],
        "ak3s_api_endpoint": cfg["api_endpoint"], "ak3s_tls_sans": cfg["tls_sans"],
        "ak3s_state_dir": str(state), "ak3s_datastore": topology,
        "ak3s_first_server": next(iter(servers)),
    })
    # Only the controller settings file holds the join token; never pass it on argv.
    run(["ansible-playbook", "-i", Path(args.inventory).resolve(), ROOT / "ansible/site.yaml",
         "--extra-vars", "@" + str(settings)],
        env=os.environ | {"ANSIBLE_CONFIG": str(ROOT / "ansible/ansible.cfg")})
    raw = state / "kubeconfig.raw"
    kube = read_yaml(raw)
    for cluster in kube["clusters"]:
        cluster["cluster"]["server"] = f'https://{cfg["api_endpoint"]}:6443'
    private_yaml(state / "kubeconfig", kube)
    raw.unlink()
    print(f"K3s ready: {len(servers)} server(s), {len(agents)} agent(s). Kubeconfig: {state / 'kubeconfig'}")


def cached_chart(chart, state):
    cache = state / "charts"
    cache.mkdir(parents=True, exist_ok=True, mode=0o700)
    path = cache / f'{chart["release"]}-{chart["version"]}.tgz'
    if path.exists():
        if hashlib.sha256(path.read_bytes()).hexdigest() != chart["sha256"]:
            raise ConfigError(f'Cached chart checksum failed for {chart["release"]}; remove it and retry.')
        return path
    for attempt in range(3):
        try:
            with urlopen(Request(chart["url"], headers={"User-Agent": "AK3S/0.1"}), timeout=30) as response:
                data = response.read()
            break
        except (URLError, TimeoutError) as exc:
            if attempt == 2:
                raise ConfigError(f'Cannot download pinned chart {chart["release"]}: {exc}') from exc
            time.sleep(1)
    if hashlib.sha256(data).hexdigest() != chart["sha256"]:
        raise ConfigError(f'Upstream chart checksum failed for {chart["release"]}.')
    path.write_bytes(data)
    return path


def platform(args, cfg, state):
    require("helm")
    require("kubectl")
    charts = prepare(cfg, state)
    kubeconfig = Path(args.kubeconfig).resolve() if args.kubeconfig else state / "kubeconfig"
    if not kubeconfig.is_file():
        raise ConfigError("Run bootstrap first or supply --kubeconfig explicitly.")
    archives = {chart["release"]: cached_chart(chart, state) for chart in charts}
    kubectl = ["kubectl", "--kubeconfig", kubeconfig]
    run(kubectl + ["get", "--raw=/readyz"])
    for chart in charts:
        run(["helm", "upgrade", "--install", chart["release"], archives[chart["release"]],
             "--namespace", chart["namespace"], "--create-namespace", "--kubeconfig", kubeconfig,
             "--values", state / "values" / chart["values"], "--reset-values", "--atomic",
             "--wait", "--timeout", "10m", *(["--skip-crds"] if chart.get("skip_crds") else [])])
        if chart["release"] == "cert-manager":
            run(kubectl + ["wait", "--for=condition=Established", "crd/clusterissuers.cert-manager.io", "--timeout=120s"])
            run(kubectl + ["apply", "-f", state / "issuers.yaml"])
    run(kubectl + ["apply", "-f", ROOT / "platform/manifests/headlamp-rbac.yaml"])
    run(kubectl + ["apply", "-f", ROOT / "platform/manifests/metrics-rbac.yaml"])
    print("AK3S platform installed. Run status, then see docs/operations.md for UI access.")


def install(args, cfg, state):
    if args.kubeconfig:
        raise ConfigError("install uses the bootstrapped cluster; use platform --kubeconfig for an existing K3s cluster.")
    # Check configuration and controller tools before changing any host.
    inventory, servers, agents = load_inventory(args.inventory)
    require("ansible-playbook")
    require("helm")
    # K3s supplies kubectl only when bootstrap installs onto this machine.
    variables = inventory["all"].get("vars", {})
    local_server = any(
        host.get("ansible_connection", variables.get("ansible_connection")) == "local"
        for host in servers.values()
    )
    if not local_server:
        require("kubectl")
    bootstrap(args, cfg, state)
    platform(args, cfg, state)


def render(cfg, state):
    require("helm")
    charts = prepare(cfg, state)
    rendered = state / "rendered"
    rendered.mkdir(exist_ok=True)
    for chart in charts:
        with (rendered / (chart["release"] + ".yaml")).open("w") as stream:
            run(["helm", "template", chart["release"], cached_chart(chart, state), "--namespace", chart["namespace"],
                 "--kube-version", "1.36.0", "--values", state / "values" / chart["values"],
                 *([] if chart.get("skip_crds") else ["--include-crds"])], stdout=stream)
    print(f"Rendered {len(charts)} releases into {rendered}.")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("command", choices=["install", "validate", "bootstrap", "platform", "render", "status"])
    parser.add_argument("--config", default="cluster.yaml")
    parser.add_argument("--inventory", default="inventory.yaml")
    parser.add_argument("--state-dir", default=".ak3s")
    parser.add_argument("--kubeconfig", help="Explicit kubeconfig for platform/status on an existing cluster")
    args = parser.parse_args()
    # Prefer the pinned Ansible installation when running under the project venv.
    os.environ["PATH"] = str(Path(sys.executable).parent) + os.pathsep + os.environ.get("PATH", "")
    state = Path(args.state_dir).resolve()
    try:
        if args.command == "status":
            require("kubectl")
            kube = Path(args.kubeconfig).resolve() if args.kubeconfig else state / "kubeconfig"
            if not kube.is_file():
                raise ConfigError("Run bootstrap first or supply --kubeconfig.")
            cmd = ["kubectl", "--kubeconfig", kube]
            for resources in ("nodes", "pods", "svc", "ingress", "clusterissuers"):
                run(cmd + ["get", resources, "-A"])
            return 0
        cfg = load_config(args.config)
        if args.command == "validate":
            _, servers, agents = load_inventory(args.inventory)
            print(f"Configuration valid: {len(servers)} server(s), {len(agents)} agent(s).")
        elif args.command == "install":
            install(args, cfg, state)
        elif args.command == "bootstrap":
            bootstrap(args, cfg, state)
        elif args.command == "platform":
            platform(args, cfg, state)
        elif args.command == "render":
            render(cfg, state)
    except (ConfigError, FileNotFoundError, yaml.YAMLError) as exc:
        print(f"ak3s: {exc}", file=sys.stderr)
        return 1
    except subprocess.CalledProcessError as exc:
        print(f"ak3s: command failed (exit {exc.returncode}); fix the reported issue and rerun.", file=sys.stderr)
        return exc.returncode
    return 0


if __name__ == "__main__":
    sys.exit(main())
