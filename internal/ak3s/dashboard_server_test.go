package ak3s

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"go.yaml.in/yaml/v3"
)

func TestDashboardDeploymentProtectsHostCredentials(t *testing.T) {
	c, err := LoadConfig([]byte("acme_email: person@example.com\nnode:\n  token_file: /private/join-key\nhelm_values:\n  headlamp:\n    password: fictional-private-value\n"))
	if err != nil {
		t.Fatal(err)
	}
	c.sourcePath = "/etc/ak3s/custom.yaml"
	raw, err := DashboardManifests(c)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateManifests(raw); err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{"person@example.com", "/private/join-key", "fictional-private-value", "hostPath", "hostNetwork", "k3s.yaml"} {
		if bytes.Contains(raw, []byte(private)) {
			t.Fatal("unsafe deployment content")
		}
	}
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	kinds := map[string]bool{}
	for {
		var resource struct {
			Kind  string
			Data  map[string]string
			Rules []struct {
				Verbs     []string
				Resources []string
			}
		}
		if err := dec.Decode(&resource); err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		kinds[resource.Kind] = true
		if resource.Kind == "ClusterRole" {
			for _, rule := range resource.Rules {
				for _, verb := range rule.Verbs {
					if verb != "list" && verb != "get" {
						t.Fatal("write permission granted")
					}
				}
				for _, name := range rule.Resources {
					if name != "nodes" && name != "deployments" && name != "statefulsets" && name != "daemonsets" {
						t.Fatal("unexpected resource permission")
					}
				}
			}
		}
		if resource.Kind == "ConfigMap" {
			var metadata dashboardMetadata
			if err := json.Unmarshal([]byte(resource.Data["metadata.json"]), &metadata); err != nil {
				t.Fatal(err)
			}
			if metadata.Configuration.OverrideCount != 3 || metadata.Configuration.Path != c.sourcePath || !strings.Contains(metadata.Configuration.OverridesYAML, "[redacted]") {
				t.Fatal("configuration projection lost")
			}
		}
	}
	for _, kind := range []string{"Namespace", "ServiceAccount", "ClusterRole", "ClusterRoleBinding", "ConfigMap", "Deployment", "Service", "NetworkPolicy"} {
		if !kinds[kind] {
			t.Fatalf("missing %s", kind)
		}
	}
	for _, ref := range []string{"registry.example.com/ak3s:v1.2.3", "localhost:5000/dashboard:dev", "registry.example.com/dashboard@sha256:" + strings.Repeat("a", 64)} {
		c.DashboardImage = ref
		if err := c.Validate(); err != nil {
			t.Fatal(err)
		}
	}
	for _, ref := range []string{"dashboard", "https://registry.example.com/dashboard:dev", "dashboard:dev\n", "dashboard@sha256:bad"} {
		c.DashboardImage = ref
		if c.Validate() == nil {
			t.Fatal("invalid image accepted")
		}
	}
}

func TestDashboardAPIProjectionPaginationAndTokenRotation(t *testing.T) {
	token, reads := "fictional-token-one", 0
	seen := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.Header.Get("Authorization") != "Bearer "+token {
			t.Error("invalid API authentication or method")
		}
		seen[r.URL.Path]++
		token = "fictional-token-two"
		switch r.URL.Path {
		case "/version":
			io.WriteString(w, `{"gitVersion":"v1.37.1+k3s1"}`)
		case "/api/v1/nodes":
			if r.URL.Query().Get("limit") != "100" {
				t.Error("unbounded resource page")
			}
			if r.URL.Query().Get("continue") == "" {
				io.WriteString(w, `{"metadata":{"continue":"next/+="},"items":[{"metadata":{"name":"lab-node"},"status":{"conditions":[{"type":"Ready","status":"True"}],"nodeInfo":{"kubeletVersion":"v1.37.1+k3s1"}}}]}`)
			} else {
				if r.URL.Query().Get("continue") != "next/+=" {
					t.Error("pagination token changed")
				}
				io.WriteString(w, `{"items":[]}`)
			}
		case "/apis/apps/v1/daemonsets":
			io.WriteString(w, `{"items":[{"metadata":{"name":"collector","namespace":"observability","generation":1,"labels":{"app.kubernetes.io/instance":"victoria-logs-collector"}},"spec":{"template":{"spec":{"containers":[{"env":[{"name":"EXAMPLE_SECRET","value":"fictional-private-value"}]}]}}},"status":{"observedGeneration":1,"desiredNumberScheduled":2,"numberReady":1,"updatedNumberScheduled":2}}]}`)
		case "/apis/apps/v1/deployments", "/apis/apps/v1/statefulsets":
			io.WriteString(w, `{"items":[]}`)
		case "/forbidden":
			http.Error(w, "fictional-private-value", 403)
		}
	}))
	defer server.Close()
	api := dashboardAPI{client: server.Client(), endpoint: server.URL, readToken: func() ([]byte, error) { reads++; return []byte(token), nil }}
	d, err := api.snapshot(context.Background(), dashboardMetadata{ClusterName: "ak3s", NodeName: "lab-node"})
	if err != nil {
		t.Fatal(err)
	}
	if reads != 6 || seen["/api/v1/nodes"] != 2 || len(d.Cluster.Nodes) != 1 || d.Cluster.K3sVersion != "v1.37.1+k3s1" {
		t.Fatal("API data incomplete")
	}
	if len(d.Components) != 1 || d.Components[0].Health != "degraded" {
		t.Fatal("typed DaemonSet list lost readiness information")
	}
	raw, _ := json.Marshal(d)
	if bytes.Contains(raw, []byte("fictional-private-value")) {
		t.Fatal("workload fields leaked")
	}
	if err := api.query(context.Background(), "/forbidden", &d); err == nil || strings.Contains(err.Error(), "fictional-private-value") {
		t.Fatal("API failure not sanitized")
	}
}

func TestDashboardHTTPAccessAndFailures(t *testing.T) {
	assets := fstest.MapFS{"index.html": {Data: []byte("<html>AK3S</html>")}, "assets/app.js": {Data: []byte("const app = true;")}}
	calls := 0
	h := dashboardHandler(assets, func(context.Context) (dashboardData, error) {
		calls++
		return dashboardData{CollectedAt: "now"}, nil
	})
	for _, tc := range []struct {
		path, method, host, remote, origin, site string
		status                                   int
	}{
		{path: "/", status: 200},
		{path: "/assets/app.js", status: 200},
		{path: "/assets/", status: 404},
		{path: "/missing", status: 404},
		{path: "/api/snapshot", status: 200},
		{path: "/api/snapshot", status: 200},
		{path: "/api/snapshot?file=private", status: 400},
		{path: "/api/snapshot", method: "POST", status: 405},
		{path: "/api/snapshot", host: "untrusted.example.com", status: 403},
		{path: "/api/snapshot", origin: "http://untrusted.example.com", status: 403},
		{path: "/api/snapshot", site: "cross-site", status: 403},
		{path: "/", remote: "192.0.2.10:1234", status: 403},
		{path: "/healthz", remote: "192.0.2.10:1234", status: 200},
		{path: "/readyz", remote: "192.0.2.10:1234", status: 200},
	} {
		t.Run(tc.path+tc.method+tc.host+tc.origin+tc.site+tc.remote, func(t *testing.T) {
			method := tc.method
			if method == "" {
				method = "GET"
			}
			r := httptest.NewRequest(method, "http://localhost:5173"+tc.path, nil)
			r.RemoteAddr = "127.0.0.1:1234"
			if tc.host != "" {
				r.Host = tc.host
			}
			if tc.remote != "" {
				r.RemoteAddr = tc.remote
			}
			r.Header.Set("Origin", tc.origin)
			r.Header.Set("Sec-Fetch-Site", tc.site)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatalf("got %d, want %d", w.Code, tc.status)
			}
			if w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("response may be cached")
			}
		})
	}
	if calls != 1 {
		t.Fatal("snapshot cache did not coalesce requests")
	}
	h = dashboardHandler(assets, func(context.Context) (dashboardData, error) {
		return dashboardData{}, errors.New("fictional-private-value")
	})
	for _, path := range []string{"/api/snapshot", "/readyz"} {
		r := httptest.NewRequest("GET", "http://localhost:5173"+path, nil)
		r.RemoteAddr = "127.0.0.1:1234"
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 503 || strings.Contains(w.Body.String(), "fictional-private-value") {
			t.Fatal("unsafe failure response")
		}
	}
}
