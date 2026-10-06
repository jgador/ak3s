package ak3s

import (
	"encoding/json"
	"strings"
	"testing"
)

func freshSnapshot() Snapshot {
	return Snapshot{OS: "linux", Arch: "amd64", Distribution: "ubuntu", DistributionVersion: "24.04", Hostname: "node1", Root: true, Systemd: true, Files: map[string][]byte{}}
}
func managedSnapshot(t *testing.T, c Config) Snapshot {
	t.Helper()
	p, err := BuildPlan(c, testPins(t), freshSnapshot(), "install")
	if err != nil {
		t.Fatal(err)
	}
	s := p.Snapshot
	s.Existing = true
	s.Managed = true
	s.ServiceActive = true
	s.ServiceEnabled = true
	s.Kubeconfig = true
	s.Role = c.Node.Role
	s.K3sVersion = p.Pins.K3s
	s.Files = p.Files
	s.Files[unitPath(c.Node.Role)] = []byte("upstream service")
	p.State.ServiceSHA256 = fingerprint(s.Files[unitPath(c.Node.Role)])
	s.Files[uninstallPath(c.Node.Role)] = []byte("upstream uninstall")
	s.Files[KillallPath] = []byte("upstream killall")
	p.State.GeneratedScripts = map[string]string{uninstallPath(c.Node.Role): fingerprint(s.Files[uninstallPath(c.Node.Role)]), KillallPath: fingerprint(s.Files[KillallPath])}
	s.Files[MarkerPath] = []byte("Managed by AK3S\n")
	p.State.PendingRestart = false
	s.State = &p.State
	s.Files[StatePath] = p.StateJSON()
	return s
}

// TestPlanInstallAndNoUnnecessaryRestart checks initial install actions and avoids restarts for unchanged nodes or platform settings.
func TestPlanInstallAndNoUnnecessaryRestart(t *testing.T) {
	c := testConfig(t)
	p, err := BuildPlan(c, testPins(t), freshSnapshot(), "install")
	if err != nil {
		t.Fatal(err)
	}
	if !p.Restart || !p.InstallBinary || len(p.Actions) < 10 {
		t.Fatal(p)
	}
	p, err = BuildPlan(c, testPins(t), managedSnapshot(t, c), "apply")
	if err != nil {
		t.Fatal(err)
	}
	if p.Restart || p.InstallBinary {
		t.Fatal("unchanged node would restart")
	}
	for _, a := range p.Actions {
		if a.Kind == "update" || a.Kind == "create" {
			t.Fatal("unchanged file written", a)
		}
	}
	c.MetricsRetention = "14d"
	p, err = BuildPlan(c, testPins(t), managedSnapshot(t, testConfig(t)), "apply")
	if err != nil || p.Restart {
		t.Fatal("platform changes must not restart K3s", err)
	}
}

func TestGeneratedFilesAndServiceRepair(t *testing.T) {
	c := testConfig(t)
	for _, change := range []string{"disabled", "inactive", "unit", "script", "installer"} {
		t.Run(change, func(t *testing.T) {
			s := managedSnapshot(t, c)
			switch change {
			case "disabled":
				s.ServiceEnabled = false
			case "inactive":
				s.ServiceActive = false
			case "unit":
				s.Files[unitPath("server")] = []byte("changed")
			case "script":
				delete(s.Files, KillallPath)
			case "installer":
				s.State.InstallerSHA256 = strings.Repeat("0", 64)
			}
			p, err := BuildPlan(c, testPins(t), s, "apply")
			if err != nil || !p.RunInstaller || p.InstallBinary {
				t.Fatal("upstream repair missing", err)
			}
		})
	}
}

func TestLegacyMigrationRequiresIdentity(t *testing.T) {
	c := testConfig(t)
	s := freshSnapshot()
	s.Managed, s.Existing, s.DataPresent = true, true, true
	if _, err := BuildPlan(c, testPins(t), s, "apply"); err == nil {
		t.Fatal("existing data migrated without identity")
	}
	// A failed first install can leave only the ownership marker before state
	// or configuration was written; it must still be possible to retry.
	s.DataPresent = false
	if _, err := BuildPlan(c, testPins(t), s, "apply"); err != nil {
		t.Fatal("marker-only first install cannot recover", err)
	}
}

// TestOwnershipIdentityAndUpgradeGuards checks that unsafe ownership, identity, token and version changes are rejected.
func TestOwnershipIdentityAndUpgradeGuards(t *testing.T) {
	c := testConfig(t)
	pins := testPins(t)
	cases := []struct {
		name   string
		change func(*Snapshot, *Config)
	}{
		{"unmanaged", func(s *Snapshot, c *Config) { s.Managed = false }},
		{"role", func(s *Snapshot, c *Config) { s.Role = "agent" }},
		{"schema", func(s *Snapshot, c *Config) { s.State.Schema = 99 }},
		{"cluster", func(s *Snapshot, c *Config) { c.ClusterName = "other" }},
		{"name", func(s *Snapshot, c *Config) { c.Node.Name = "other" }},
		{"topology", func(s *Snapshot, c *Config) { c.Node.Datastore = "etcd"; c.Node.ServerCount = 3 }},
		{"token", func(s *Snapshot, c *Config) { s.TokenSHA256 = "different" }},
		{"version change", func(s *Snapshot, c *Config) { s.K3sVersion = "v1.36.4+k3s1" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := c
			s := managedSnapshot(t, c)
			tc.change(&s, &cfg)
			if _, err := BuildPlan(cfg, pins, s, "apply"); err == nil {
				t.Fatal("unsafe change allowed")
			}
		})
	}
	if _, err := BuildPlan(c, pins, freshSnapshot(), "upgrade"); err == nil {
		t.Fatal("fresh upgrade allowed")
	}
	s := managedSnapshot(t, c)
	s.K3sVersion = "v1.36.5+k3s1"
	p, err := BuildPlan(c, pins, s, "upgrade")
	if err != nil || !p.InstallBinary {
		t.Fatal(err)
	}
}

// TestHostBlockersAndInterruptedRestart checks failed prerequisites, restart recovery and checkpoint encoding.
func TestHostBlockersAndInterruptedRestart(t *testing.T) {
	c := testConfig(t)
	s := freshSnapshot()
	s.Systemd = false
	s.Swap = true
	s.OS = "darwin"
	s.Arch = "386"
	s.Root = false
	s.Problems = []string{"host prerequisite failed"}
	p, err := BuildPlan(c, testPins(t), s, "install")
	if err != nil {
		t.Fatal(err)
	}
	if p.Check() == nil || len(p.Problems) != 4 || len(p.Warnings) == 0 {
		t.Fatal(p)
	}
	s = managedSnapshot(t, c)
	s.State.PendingRestart = true
	p, err = BuildPlan(c, testPins(t), s, "apply")
	if err != nil || !p.Restart {
		t.Fatal("interrupted restart was lost", err)
	}
	s = managedSnapshot(t, c)
	s.ServiceActive = false
	p, err = BuildPlan(c, testPins(t), s, "apply")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, a := range p.Actions {
		if a.Kind == "start" {
			found = true
		}
	}
	if !found {
		t.Fatal("inactive service not started")
	}
	var state State
	if err = json.Unmarshal(p.StateJSON(), &state); err != nil || state.Schema != 2 {
		t.Fatal(err)
	}
}

// TestLegacyTokenAndTopologyPreserved checks preservation of legacy credentials and rejects implicit identity or topology changes.
func TestLegacyTokenAndTopologyPreserved(t *testing.T) {
	c := testConfig(t)
	c.Node.Name = "old-node"
	s := managedSnapshot(t, c)
	s.State = nil
	s.Files[ConfigPath] = []byte("node-name: old-node\ntoken: super-secret\ndisable: [traefik]\n")
	p, err := BuildPlan(c, testPins(t), s, "apply")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(p.Files[ConfigPath]), "token: super-secret") {
		t.Fatal("lost token")
	}
	for _, a := range p.Actions {
		if strings.Contains(a.Detail, "super-secret") {
			t.Fatal("token leaked")
		}
	}
	s.Files[ConfigPath] = []byte("node-name: other\n")
	if _, err = BuildPlan(c, testPins(t), s, "apply"); err == nil {
		t.Fatal("legacy rename allowed")
	}
	s.Files[ConfigPath] = []byte("node-name: old-node\ncluster-init: true\n")
	if _, err = BuildPlan(c, testPins(t), s, "apply"); err == nil {
		t.Fatal("legacy topology change allowed")
	}
	s.Files[ConfigPath] = []byte("node-name: old-node\nnode-ip: 10.0.0.1\n")
	if _, err = BuildPlan(c, testPins(t), s, "apply"); err == nil {
		t.Fatal("legacy address removed")
	}
}

// TestVersionTransitions checks K3s version parsing and permitted upgrade paths.
func TestVersionTransitions(t *testing.T) {
	for _, tc := range []struct {
		from, to  string
		allow, ok bool
	}{{"", "v1.36.5+k3s1", false, true}, {"v1.36.5+k3s1", "v1.36.5+k3s1", false, true}, {"v1.36.4+k3s1", "v1.36.5+k3s1", false, false}, {"v1.36.4+k3s1", "v1.36.5+k3s1", true, true}, {"v1.35.9+k3s1", "v1.36.5+k3s1", true, true}, {"v1.34.9+k3s1", "v1.36.5+k3s1", true, false}, {"v1.36.6+k3s1", "v1.36.5+k3s1", true, false}, {"v1.36.5+k3s2", "v1.36.5+k3s1", true, false}, {"v2.0.0+k3s1", "v1.36.5+k3s1", true, false}, {"invalid", "v1.36.5+k3s1", true, false}, {"v1.36.5+k3s1", "latest", true, false}} {
		if err := CheckUpgrade(tc.from, tc.to, tc.allow); (err == nil) != tc.ok {
			t.Errorf("%+v: %v", tc, err)
		}
	}
	for _, s := range []string{"latest", "v1.36.5", "v1.36.5+k3s1-extra", "v999999999999999999999999.1.1+k3s1"} {
		if _, err := ParseK3sVersion(s); err == nil {
			t.Fatal("invalid version", s)
		}
	}
}

// TestAK3SReleaseDowngrades checks AK3S release ordering and development build handling.
func TestAK3SReleaseDowngrades(t *testing.T) {
	for _, tc := range []struct {
		from, to string
		ok       bool
	}{{"v0.2.0", "v0.3.0", true}, {"v0.3.0", "v0.2.0", false}, {"v1.0.0", "v1.0.0", true}, {"dev", "v0.1.0", true}, {"v1.2.3", "dev", true}, {"v1.2.3", "v1.2.2", false}} {
		if err := CheckAK3SUpgrade(tc.from, tc.to); (err == nil) != tc.ok {
			t.Fatal(tc, err)
		}
	}
}

// TestHostNameChangeAndUnknownK3sSettings checks that hostname changes and unmanaged K3s settings block reconciliation.
func TestHostNameChangeAndUnknownK3sSettings(t *testing.T) {
	c := testConfig(t)
	s := managedSnapshot(t, c)
	s.Hostname = "renamed"
	if _, err := BuildPlan(c, testPins(t), s, "apply"); err == nil {
		t.Fatal("implicit hostname rename allowed")
	}
	s = managedSnapshot(t, c)
	s.Files[ConfigPath] = append(s.Files[ConfigPath], []byte("cluster-cidr: 10.0.0.0/16\n")...)
	if _, err := BuildPlan(c, testPins(t), s, "apply"); err == nil {
		t.Fatal("custom critical K3s option erased")
	}
}

// TestLegacyJoiningTokenMigration checks that inline join tokens migrate only to a matching token file.
func TestLegacyJoiningTokenMigration(t *testing.T) {
	c := testConfig(t)
	c.APIEndpoint = "10.0.0.1"
	c.Platform = false
	c.Node.Role = "agent"
	c.Node.Join = true
	c.Node.TokenFile = "/root/token"
	c.Node.Name = "worker-1"
	s := managedSnapshot(t, c)
	s.State = nil
	s.TokenSHA256 = checksum([]byte("original-token"))
	s.Files[ConfigPath] = []byte("node-name: worker-1\nserver: https://10.0.0.1:6443\ntoken: original-token\n")
	p, err := BuildPlan(c, testPins(t), s, "apply")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(p.Files[ConfigPath]), "original-token") || !strings.Contains(string(p.Files[ConfigPath]), "token-file: /root/token") {
		t.Fatal("token was not migrated to reference")
	}
	s.TokenSHA256 = checksum([]byte("different"))
	if _, err = BuildPlan(c, testPins(t), s, "apply"); err == nil {
		t.Fatal("wrong legacy token accepted")
	}
}
