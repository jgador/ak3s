package ak3s

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"go.yaml.in/yaml/v3"
)

const (
	K3sPath        = "/usr/local/bin/k3s"
	ConfigPath     = "/etc/rancher/k3s/config.yaml"
	MarkerPath     = "/etc/rancher/k3s/ak3s-managed"
	StatePath      = "/var/lib/ak3s/state.json"
	KubeconfigPath = "/etc/rancher/k3s/k3s.yaml"
	ModulesPath    = "/etc/modules-load.d/ak3s.conf"
	SysctlPath     = "/etc/sysctl.d/90-ak3s.conf"
)

type State struct {
	PendingRestart bool       `json:"pending_restart"`
	Schema         int        `json:"schema"`
	AK3SVersion    string     `json:"ak3s_version"`
	K3sVersion     string     `json:"k3s_version"`
	ClusterName    string     `json:"cluster_name"`
	Node           NodeConfig `json:"node"`
	// A hash of the join credential detects edits to a token file without exposing it.
	TokenSHA256 string `json:"token_sha256,omitempty"`
}
type Snapshot struct {
	OS, Arch, Distribution, DistributionVersion, Hostname             string
	Root, Systemd, Swap, Existing, Managed, ServiceActive, Kubeconfig bool
	K3sVersion, Role, TokenSHA256                                     string
	Files                                                             map[string][]byte
	State                                                             *State
	Problems                                                          []string
}
type Action struct{ Kind, Target, Detail string }
type Plan struct {
	Config                 Config
	Pins                   Pins
	Snapshot               Snapshot
	Files                  map[string][]byte
	Actions                []Action
	Problems, Warnings     []string
	InstallBinary, Restart bool
	State                  State
}

func unitPath(role string) string { return "/etc/systemd/system/" + serviceName(role) + ".service" }
func serviceName(role string) string {
	if role == "agent" {
		return "k3s-agent"
	}
	return "k3s"
}

func BuildPlan(c Config, pins Pins, s Snapshot, command string) (Plan, error) {
	if c.Node.Name == "" {
		c.Node.Name = strings.ToLower(s.Hostname)
	}
	p := Plan{Config: c, Pins: pins, Snapshot: s, Files: map[string][]byte{}, Problems: append([]string{}, s.Problems...)}
	if err := c.Validate(); err != nil {
		return p, err
	}
	if err := c.ValidateInstall(); err != nil {
		return p, err
	}
	if s.OS != "linux" || (s.Arch != "amd64" && s.Arch != "arm64") {
		p.Problems = append(p.Problems, "only Linux amd64 and arm64 are supported")
	}
	if !s.Systemd {
		p.Problems = append(p.Problems, "systemd is required for installation")
	}
	if s.Swap {
		p.Problems = append(p.Problems, "disable swap before installation")
	}
	if !s.Root {
		p.Warnings = append(p.Warnings, "apply requires root; dry-run can inspect readable state without root")
	}
	if s.Existing && !s.Managed {
		return p, errors.New("existing K3s is not managed by AK3S; refusing to overwrite it")
	}
	if s.Managed && s.Role != "" && s.Role != c.Node.Role {
		return p, errors.New("changing node role requires an explicit migration")
	}
	if command == "upgrade" && !s.Managed {
		return p, errors.New("upgrade requires an existing AK3S-managed node")
	}
	if err := CheckUpgrade(s.K3sVersion, pins.K3s, command == "upgrade"); err != nil {
		return p, err
	}
	if s.State != nil {
		old := s.State
		if err := CheckAK3SUpgrade(old.AK3SVersion, Version); err != nil {
			return p, err
		}
		if old.Schema != 1 {
			return p, errors.New("unsupported AK3S state schema")
		}
		if old.ClusterName != c.ClusterName || old.Node.Role != c.Node.Role || old.Node.Datastore != c.Node.Datastore || old.Node.Join != c.Node.Join || old.Node.Name != c.Node.Name {
			return p, errors.New("cluster identity, node name, role and datastore topology cannot change implicitly")
		}
		if old.Node.TokenFile != c.Node.TokenFile || old.TokenSHA256 != s.TokenSHA256 {
			return p, errors.New("join token changed; restore the original token file before continuing")
		}
	}
	var legacyToken string
	if previous := s.Files[ConfigPath]; len(previous) > 0 {
		var old map[string]any
		if err := yaml.Unmarshal(previous, &old); err != nil {
			return p, fmt.Errorf("read existing K3s config: %w", err)
		}
		known := map[string]bool{}
		for _, key := range []string{"node-name", "node-ip", "node-external-ip", "flannel-iface", "token", "token-file", "disable", "write-kubeconfig-mode", "secrets-encryption", "tls-san", "cluster-init", "server"} {
			known[key] = true
		}
		for key := range old {
			if !known[key] {
				return p, fmt.Errorf("unmanaged K3s setting %q; migrate it before reconciling", key)
			}
		}
		if s.State == nil {
			if old["node-name"] != nil && old["node-name"] != c.Node.Name {
				return p, errors.New("preserve the existing node-name in node.name when migrating")
			}
			join := old["server"] != nil
			etcd := old["cluster-init"] == true || (join && c.Node.Role == "server")
			if join != c.Node.Join || (c.Node.Role == "server" && etcd != (c.Node.Datastore == "etcd")) {
				return p, errors.New("preserve existing join and datastore topology when migrating")
			}
			for key, value := range map[string]string{"node-ip": c.Node.IP, "node-external-ip": c.Node.ExternalIP, "flannel-iface": c.Node.FlannelIface} {
				if old[key] != nil && old[key] != value {
					return p, fmt.Errorf("preserve existing %s in node settings when migrating", key)
				}
			}
		}
		if token, ok := old["token"].(string); ok && token != "" {
			if c.Node.TokenFile != "" {
				sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
				if hex.EncodeToString(sum[:]) != s.TokenSHA256 {
					return p, errors.New("token file does not match existing legacy token")
				}
			} else {
				legacyToken = token
			}
		}
	}
	config, err := K3sConfig(c, legacyToken)
	if err != nil {
		return p, err
	}
	p.Files[ConfigPath] = config
	p.Files[unitPath(c.Node.Role)] = serviceUnit(c.Node.Role)
	p.Files[ModulesPath] = []byte("overlay\nbr_netfilter\n")
	p.Files[SysctlPath] = []byte("net.ipv4.ip_forward = 1\nnet.bridge.bridge-nf-call-iptables = 1\n")
	p.InstallBinary = s.K3sVersion != pins.K3s
	if p.InstallBinary {
		kind := "install"
		if s.K3sVersion != "" {
			kind = "upgrade"
		}
		p.Actions = append(p.Actions, Action{kind, "K3s", s.K3sVersion + " -> " + pins.K3s + " (verify pinned SHA-256)"})
	}
	for _, path := range []string{ModulesPath, SysctlPath, ConfigPath, unitPath(c.Node.Role)} {
		if !bytes.Equal(s.Files[path], p.Files[path]) {
			kind := "create"
			if len(s.Files[path]) > 0 {
				kind = "update"
			}
			p.Actions = append(p.Actions, Action{kind, path, "atomic replacement"})
			if path == ConfigPath || path == unitPath(c.Node.Role) {
				p.Restart = true
			}
		}
	}
	p.Restart = p.Restart || p.InstallBinary || (s.State != nil && s.State.PendingRestart)
	p.Actions = append(p.Actions, Action{"record", "AK3S ownership and state", MarkerPath + "; " + StatePath})
	p.Actions = append(p.Actions, Action{"reconcile", "host prerequisites", "ensure iptables, CA certificates, kernel modules and forwarding"})
	if p.Restart {
		p.Actions = append(p.Actions, Action{"restart", serviceName(c.Node.Role), "wait for readiness"})
	} else if !s.ServiceActive {
		p.Actions = append(p.Actions, Action{"start", serviceName(c.Node.Role), "enable service and wait for readiness"})
	}
	if c.Platform {
		for _, chart := range pins.Charts {
			p.Actions = append(p.Actions, Action{"reconcile", chart.Release, chart.Version + "; Helm upgrade --install --reset-values --atomic --wait"})
		}
		p.Actions = append(p.Actions, Action{"apply", "ClusterIssuers and RBAC", "idempotent kubectl apply"})
	}
	p.State = State{PendingRestart: true, Schema: 1, AK3SVersion: Version, K3sVersion: pins.K3s, ClusterName: c.ClusterName, Node: c.Node, TokenSHA256: s.TokenSHA256}
	p.Warnings = append(p.Warnings, "Helm rollback is per release; there is no whole-platform rollback")
	if c.Node.Datastore == "etcd" {
		p.Warnings = append(p.Warnings, "run one server at a time and verify quorum; server_count describes intended topology, not observed quorum")
	}
	if !s.Kubeconfig && c.Platform {
		p.Warnings = append(p.Warnings, "cluster API validation is deferred until K3s is ready; local chart templates and schemas are checked")
	}
	return p, nil
}
func (p Plan) StateJSON() []byte {
	data, _ := json.MarshalIndent(p.State, "", "  ")
	return append(data, '\n')
}
func (p Plan) Check() error {
	if len(p.Problems) > 0 {
		return errors.New(strings.Join(p.Problems, "; "))
	}
	return nil
}
