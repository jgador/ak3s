"""Regression checks for operator errors and configuration boundaries."""

import importlib.util
import hashlib
from pathlib import Path
import stat
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

import yaml

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location("ak3s", ROOT / "scripts/ak3s.py")
ak3s = importlib.util.module_from_spec(spec)
spec.loader.exec_module(ak3s)


class ConfigurationTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.path = Path(self.temp.name)

    def config(self, **changes):
        data = yaml.safe_load((ROOT / "examples/cluster.yaml").read_text()) | changes
        path = self.path / "cluster.yaml"
        path.write_text(yaml.safe_dump(data))
        return ak3s.load_config(path)

    def inventory(self, servers, agents=()):
        path = self.path / "inventory.yaml"
        def hosts(names, offset):
            return {name: {"ansible_host": f"192.0.2.{offset + index}"} for index, name in enumerate(names)}
        path.write_text(yaml.safe_dump({"all": {"children": {
            "k3s_servers": {"hosts": hosts(servers, 10)},
            "k3s_agents": {"hosts": hosts(agents, 100)},
        }}}))
        return ak3s.load_inventory(path)

    def test_operator_errors_fail_before_commands_run(self):
        for changes in (
            {"api_endpoint": "https://192.0.2.10:6443"},
            {"api_endpoint": "192.0.2.10; touch /tmp/unwanted"},
            {"acme_environment": "prod"}, {"acme_email": ""},
            {"metrics_retention": "0d"}, {"logs_storage": "-1Gi"},
            {"headlamp_hostname": "https://dashboard.example.com"},
            {"unknown_option": True}, {"tls_sans": "api.example.com"},
        ):
            with self.subTest(changes=changes), self.assertRaises(ak3s.ConfigError):
                self.config(**changes)

    def test_quorum_topology_and_disjoint_groups(self):
        for servers in ([], ["a", "b"], ["a", "b", "c", "d"]):
            with self.subTest(servers=servers), self.assertRaises(ak3s.ConfigError):
                self.inventory(servers)
        with self.assertRaises(ak3s.ConfigError):
            self.inventory(["a"], ["a"])
        self.inventory(["a"], ["worker-1"])
        self.inventory(["a", "b", "c"])

    def test_private_state_permissions_even_on_existing_files(self):
        path = self.path / "state.yaml"
        path.write_text("old")
        path.chmod(0o644)
        ak3s.private_yaml(path, {"token": "fictional-example"})
        self.assertEqual(stat.S_IMODE(path.stat().st_mode), 0o600)

    def test_chart_cache_rejects_corruption_before_helm(self):
        chart = {"release": "example", "version": "1.0.0", "url": "https://example.com/chart.tgz",
                 "sha256": hashlib.sha256(b"verified-content").hexdigest()}
        cache = self.path / "charts"
        cache.mkdir()
        artifact = cache / "example-1.0.0.tgz"
        artifact.write_bytes(b"verified-content")
        with patch.object(ak3s, "urlopen") as download:
            self.assertEqual(ak3s.cached_chart(chart, self.path), artifact)
            artifact.write_bytes(b"corrupted-content")
            with self.assertRaises(ak3s.ConfigError):
                ak3s.cached_chart(chart, self.path)
            download.assert_not_called()

    def test_configurable_persistence_and_public_ui(self):
        cfg = self.config(storage_class="external-storage", metrics_retention="14d",
                          headlamp_hostname="dashboard.example.com", acme_environment="production")
        charts = ak3s.read_yaml(ROOT / "platform/versions.yaml")["charts"]
        for chart in charts:
            values = ak3s.values_for(chart, cfg)
            if chart["release"] == "headlamp":
                self.assertTrue(values["ingress"]["enabled"])
                self.assertEqual(values["ingress"]["annotations"]["cert-manager.io/cluster-issuer"], "letsencrypt-production")
                self.assertFalse(values["clusterRoleBinding"]["create"])
            if chart["release"] == "victoria-metrics":
                self.assertEqual(values["server"]["persistentVolume"]["storageClassName"], "external-storage")
                self.assertEqual(values["server"]["retentionPeriod"], "14d")

    def test_common_platform_has_no_cloud_dependency(self):
        # Installation must not depend on cloud APIs.
        for path in [*ROOT.glob("ansible/**/*.yaml"), *ROOT.glob("platform/**/*.yaml"), ROOT / "scripts/ak3s.py"]:
            text = path.read_text().lower()
            for provider in ("hetzner", "hcloud"):
                self.assertNotIn(provider, text, str(path))

    def test_topology_guard_prevents_any_remote_mutation(self):
        from argparse import Namespace
        inventory = ROOT / "examples/inventory.yaml"
        state = self.path / "state"
        ak3s.private_yaml(state / "bootstrap.yaml", {
            "ak3s_datastore": "etcd", "ak3s_first_server": "server-1",
        })
        with patch.object(ak3s, "require"), patch.object(ak3s, "run") as run:
            with self.assertRaises(ak3s.ConfigError):
                ak3s.bootstrap(Namespace(inventory=inventory), self.config(), state)
            run.assert_not_called()

    def test_install_preflight_does_not_modify_hosts_or_another_cluster(self):
        from argparse import Namespace
        args = Namespace(inventory=ROOT / "examples/inventory-local.yaml", kubeconfig=None)
        with patch.object(ak3s, "bootstrap") as bootstrap, patch.object(ak3s, "platform") as platform:
            with patch.object(ak3s, "require", side_effect=[None, ak3s.ConfigError("Required tool is missing: helm.")]):
                with self.assertRaises(ak3s.ConfigError):
                    ak3s.install(args, self.config(), self.path)
            bootstrap.assert_not_called()
            platform.assert_not_called()
            args.inventory = ROOT / "examples/inventory.yaml"
            with patch.object(ak3s, "require", side_effect=[None, None, ak3s.ConfigError("Required tool is missing: kubectl.")]):
                with self.assertRaises(ak3s.ConfigError):
                    ak3s.install(args, self.config(), self.path)
            bootstrap.assert_not_called()
            platform.assert_not_called()
            args.kubeconfig = "/tmp/another-cluster.yaml"
            with self.assertRaises(ak3s.ConfigError):
                ak3s.install(args, self.config(), self.path)
            bootstrap.assert_not_called()
            platform.assert_not_called()

    def test_real_ansible_templates_for_sqlite_etcd_and_agents(self):
        # Render through Ansible itself to catch template lookup and Jinja errors.
        inventory = {"all": {"children": {
            "k3s_servers": {"hosts": {name: {"ansible_connection": "local", "ansible_host": f"192.0.2.{index + 10}"}
                                      for index, name in enumerate(("server-1", "server-2", "server-3"))}},
            "k3s_agents": {"hosts": {"worker-1": {"ansible_connection": "local", "ansible_host": "192.0.2.100"}}},
        }}}
        inv = self.path / "render-inventory.yaml"
        inv.write_text(yaml.safe_dump(inventory, sort_keys=False))
        play = self.path / "render-play.yaml"
        play.write_text(yaml.safe_dump([{
            "hosts": "all", "gather_facts": False,
            "vars": {
                "ak3s_mode": "{{ 'server' if inventory_hostname in groups['k3s_servers'] else 'agent' }}",
                "ak3s_join_token": "fictional-example", "ak3s_api_endpoint": "api.example.com", "ak3s_tls_sans": [],
            },
            "tasks": [{"name": "Render configuration", "ansible.builtin.template": {
                "src": str(ROOT / "ansible/templates/k3s-config.yaml.j2"),
                "dest": str(self.path / "{{ inventory_hostname }}.yaml"), "mode": "0600",
            }}],
        }], sort_keys=False))
        command = [str(Path(sys.executable).parent / "ansible-playbook"), "-i", str(inv), str(play)]
        subprocess.run(command, check=True, capture_output=True, text=True)
        first = ak3s.read_yaml(self.path / "server-1.yaml")
        joining = ak3s.read_yaml(self.path / "server-2.yaml")
        worker = ak3s.read_yaml(self.path / "worker-1.yaml")
        self.assertTrue(first["cluster-init"])
        self.assertEqual(first["disable"], ["traefik"])
        self.assertTrue(first["secrets-encryption"])
        self.assertEqual(joining["server"], "https://api.example.com:6443")
        self.assertNotIn("cluster-init", joining)
        self.assertNotIn("disable", worker)
        inventory["all"]["children"]["k3s_servers"]["hosts"] = {"server-1": inventory["all"]["children"]["k3s_servers"]["hosts"]["server-1"]}
        inv.write_text(yaml.safe_dump(inventory, sort_keys=False))
        subprocess.run(command, check=True, capture_output=True, text=True)
        self.assertNotIn("cluster-init", ak3s.read_yaml(self.path / "server-1.yaml"))


if __name__ == "__main__":
    unittest.main()
