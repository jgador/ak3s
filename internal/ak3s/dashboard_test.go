package ak3s

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"strings"
	"testing"
)

func dashboardFixture(t *testing.T) (*App, *bytes.Buffer, *int) {
	t.Helper()
	out, calls := &bytes.Buffer{}, new(int)
	state, _ := json.Marshal(State{Schema: 2, ClusterName: "ak3s", AK3SVersion: "dev", Node: NodeConfig{Name: "lab-node", Role: "server", Datastore: "sqlite"}})
	files := map[string][]byte{
		"/etc/ak3s/values.yaml": []byte("acme_email: person@example.com\nnode:\n  token_file: /private/join-key\nhelm_values:\n  headlamp:\n    password: fictional-private-value\n"),
		MarkerPath:              []byte("Managed by AK3S\n"), StatePath: state,
		KubeconfigPath: []byte("current-context: default\ncontexts:\n- name: default\n  context:\n    cluster: default\nclusters:\n- name: default\n  cluster:\n    server: https://127.0.0.1:6443\nusers:\n- name: default\n  user:\n    client-key-data: fictional-private-key\n"),
	}
	app := &App{Out: out, Err: out, ReadFile: func(path string) ([]byte, error) {
		if data, ok := files[path]; ok {
			return data, nil
		}
		return nil, fs.ErrNotExist
	}}
	app.Runner = runFunc(func(_ context.Context, command Command) ([]byte, error) {
		*calls++
		if command.Name != K3sPath || !command.IsolatedEnv || strings.Contains(strings.Join(command.Args, " "), "secrets") {
			t.Fatal("unsafe dashboard query")
		}
		switch command.Args[len(command.Args)-1] {
		case "--raw=/version":
			return []byte(`{"gitVersion":"v1.37.1+k3s1"}`), nil
		case "json":
			if strings.Contains(strings.Join(command.Args, " "), "get nodes") {
				return []byte(`{"items":[{"metadata":{"name":"lab-node","labels":{"node-role.kubernetes.io/control-plane":"true"}},"status":{"conditions":[{"type":"Ready","status":"True"}],"addresses":[{"type":"InternalIP","address":"192.0.2.10"}],"nodeInfo":{"osImage":"Ubuntu","architecture":"amd64","kubeletVersion":"v1.37.1+k3s1"}}}]}`), nil
			}
			return []byte(`{"items":[{"kind":"Deployment","metadata":{"name":"headlamp","namespace":"headlamp","generation":2,"labels":{"app.kubernetes.io/instance":"headlamp","helm.sh/chart":"headlamp-0.45.0"}},"spec":{"replicas":1,"template":{"spec":{"containers":[{"env":[{"name":"EXAMPLE_SECRET","value":"fictional-container-secret"}]}]}}},"status":{"observedGeneration":2,"readyReplicas":1,"updatedReplicas":1}}]}`), nil
		}
		return nil, errors.New("unexpected query")
	})
	return app, out, calls
}

func TestDashboardLiveProjectionAndRedaction(t *testing.T) {
	app, out, calls := dashboardFixture(t)
	if err := app.Run(context.Background(), []string{"dashboard-snapshot"}); err != nil {
		t.Fatal(err)
	}
	var d dashboardData
	if err := json.Unmarshal(out.Bytes(), &d); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"kubernetesApiEndpoint":"https://127.0.0.1:6443"`) {
		t.Fatal("dashboard response omitted the Kubernetes API endpoint")
	}
	if *calls != 3 || d.Cluster.Name != "ak3s" || len(d.Cluster.Nodes) != 1 || d.Cluster.Nodes[0].Name != "lab-node" || d.Cluster.K3sVersion != "v1.37.1+k3s1" || d.Configuration.OverrideCount != 3 {
		t.Fatal("live data not projected", d)
	}
	if d.Cluster.Health != "degraded" || d.Components[2].Health != "healthy" || d.Components[2].ChartVersion != "0.45.0" || d.Upgrade.AvailableVersion != nil {
		t.Fatal("invented health or release")
	}
	for _, private := range []string{"person@example.com", "/private/join-key", "fictional-private-value", "fictional-private-key", "fictional-container-secret"} {
		if strings.Contains(out.String(), private) {
			t.Fatal("private data leaked")
		}
	}
	if !strings.Contains(out.String(), "[redacted]") {
		t.Fatal("missing redaction")
	}
}

func TestDashboardRefusesRemoteClusterAndSuppressesErrors(t *testing.T) {
	for _, mode := range []string{"remote", "invalid-config", "unmanaged", "query-failure"} {
		t.Run(mode, func(t *testing.T) {
			app, out, calls := dashboardFixture(t)
			read := app.ReadFile
			app.ReadFile = func(path string) ([]byte, error) {
				data, err := read(path)
				if mode == "remote" && path == KubeconfigPath {
					data = bytes.ReplaceAll(data, []byte("127.0.0.1"), []byte("api.example.com"))
				}
				if mode == "invalid-config" && path == "/etc/ak3s/values.yaml" {
					data = []byte("unknown: fictional-private-value")
				}
				if mode == "unmanaged" && path == MarkerPath {
					return nil, fs.ErrNotExist
				}
				return data, err
			}
			if mode == "query-failure" {
				app.Runner = runFunc(func(context.Context, Command) ([]byte, error) { return nil, errors.New("fictional-private-value") })
			}
			err := app.Run(context.Background(), []string{"dashboard-snapshot"})
			if err == nil || out.Len() != 0 || strings.Contains(err.Error(), "fictional-private-value") {
				t.Fatal("unsafe failure", err)
			}
			if mode != "query-failure" && *calls != 0 {
				t.Fatal("queried unsafe cluster")
			}
		})
	}
}

func TestDashboardReadinessDuringRollout(t *testing.T) {
	var workload dashboardResource
	if err := json.Unmarshal([]byte(`{"kind":"Deployment","metadata":{"generation":2},"spec":{"replicas":1},"status":{"observedGeneration":1,"readyReplicas":1,"updatedReplicas":1}}`), &workload); err != nil {
		t.Fatal(err)
	}
	if dashboardWorkloadReady(workload) {
		t.Fatal("outdated generation reported ready")
	}
	workload.Status.ObservedGeneration = 2
	if !dashboardWorkloadReady(workload) {
		t.Fatal("ready deployment rejected")
	}
	workload.Status.UpdatedReplicas = 0
	if dashboardWorkloadReady(workload) {
		t.Fatal("incomplete rollout reported ready")
	}
	workload.Kind = "DaemonSet"
	workload.Status.DesiredNumberScheduled, workload.Status.NumberReady, workload.Status.UpdatedNumberScheduled = 2, 1, 2
	if dashboardWorkloadReady(workload) {
		t.Fatal("partly ready daemonset reported ready")
	}
	workload.Status.NumberReady = 2
	if !dashboardWorkloadReady(workload) {
		t.Fatal("ready daemonset rejected")
	}
}
