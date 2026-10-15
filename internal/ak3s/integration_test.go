//go:build integration

package ak3s

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// TestPinnedK3sArtifacts verifies both upstream release binaries without executing them.
func TestPinnedK3sArtifacts(t *testing.T) {
	pins := testPins(t)
	for _, arch := range []string{"amd64", "arm64"} {
		t.Run(arch, func(t *testing.T) {
			if _, err := (HTTPDownloader{}).Get(context.Background(), pins.K3sURL(arch), pins.K3sSHA256[arch]); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// TestPinnedPostgresRecipe verifies real Helm rendering and the PVC mount without a cluster.
func TestPinnedPostgresRecipe(t *testing.T) {
	c, _ := testApplication(t)
	b, err := (NativePreparer{Downloads: HTTPDownloader{}, Runner: ExecRunner{}}).PrepareApplications(context.Background(), c,
		testPins(t), Snapshot{OS: "linux", Arch: runtime.GOARCH}, false)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if len(b.Applications) != 1 {
		t.Fatal("PostgreSQL recipe missing")
	}
	app := b.Applications[0]
	dec := yaml.NewDecoder(bytes.NewReader(b.Rendered[app.Release]))
	services, statefulsets := 0, 0
	for {
		var resource map[string]any
		if err := dec.Decode(&resource); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			t.Fatal(err)
		}
		if len(resource) == 0 {
			continue
		}
		spec := resource["spec"].(map[string]any)
		switch resource["kind"] {
		case "Service":
			services++
			if spec["type"] != "ClusterIP" && spec["clusterIP"] != "None" {
				t.Fatal("database Service is publicly exposed")
			}
		case "StatefulSet":
			statefulsets++
			if spec["replicas"] != 1 {
				t.Fatal("standalone PostgreSQL has multiple writers")
			}
			pod := spec["template"].(map[string]any)["spec"].(map[string]any)
			container := pod["containers"].([]any)[0].(map[string]any)
			if container["image"] != app.Recipe.Image || pod["automountServiceAccountToken"] != false {
				t.Fatal("image pin or pod isolation missing")
			}
			expectedData := "/var/lib/postgresql/" + strings.Split(app.Recipe.PostgresVersion, ".")[0] + "/docker"
			foundData := false
			for _, item := range container["env"].([]any) {
				env := item.(map[string]any)
				if env["name"] == "PGDATA" {
					foundData = env["value"] == expectedData
				}
			}
			if !foundData {
				t.Fatal("data directory does not match the pinned PostgreSQL major version")
			}
			mount := container["volumeMounts"].([]any)[0].(map[string]any)
			if mount["name"] != "data" || mount["mountPath"] != "/var/lib/postgresql" {
				t.Fatal("database data directory is not inside the mounted PVC")
			}
			volume := pod["volumes"].([]any)[0].(map[string]any)
			if volume["persistentVolumeClaim"].(map[string]any)["claimName"] != app.Release+"-data" {
				t.Fatal("StatefulSet does not mount its PVC")
			}
		case "PersistentVolumeClaim", "Secret":
			t.Fatal("Helm owns data or credentials")
		default:
			t.Fatal("unexpected recipe resource", resource["kind"])
		}
	}
	if services != 2 || statefulsets != 1 {
		t.Fatal("incomplete PostgreSQL chart")
	}
	var pvc map[string]any
	if err := yaml.Unmarshal(app.PVC, &pvc); err != nil {
		t.Fatal(err)
	}
	spec := pvc["spec"].(map[string]any)
	if pvc["kind"] != "PersistentVolumeClaim" || spec["storageClassName"] != "local-path" ||
		spec["resources"].(map[string]any)["requests"].(map[string]any)["storage"] != "10Gi" {
		t.Fatal("persistent storage defaults missing")
	}
}

// TestPinnedCharts downloads the release-pinned Helm binary and chart archives,
// verifies their checksums, and runs real Helm schema and template validation. It
// needs outbound HTTPS but no root, Docker, systemd, or cluster.
func TestPinnedCharts(t *testing.T) {
	c := testConfig(t)
	c.DashboardHostname = "ak3s.example.com"
	p := Plan{Config: c, Pins: testPins(t), Snapshot: Snapshot{OS: "linux", Arch: runtime.GOARCH}}
	b, err := (NativePreparer{Downloads: HTTPDownloader{}, Runner: ExecRunner{}}).Prepare(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if len(b.Rendered) != 6 {
		t.Fatal("missing rendered charts")
	}
	output := t.TempDir()
	k3s, _ := K3sConfig(c, "")
	if err = b.Export(output, k3s); err != nil {
		t.Fatal(err)
	}
	for _, chart := range p.Pins.Charts {
		data, err := os.ReadFile(filepath.Join(output, "rendered", chart.Release+".yaml"))
		if err != nil {
			t.Fatal(err)
		}
		if err = ValidateManifests(data); err != nil {
			t.Fatal(err)
		}
		if chart.Release == "headlamp" && (!strings.Contains(string(data), "-base-url=/headlamp") || !strings.Contains(string(data), "/headlamp/")) {
			t.Fatal("pinned Headlamp chart did not render the base URL and health probes")
		}
		if chart.Release == "nginx-ingress" && (!strings.Contains(string(data), "app.kubernetes.io/instance: nginx-ingress") || !strings.Contains(string(data), "app.kubernetes.io/name: nginx-ingress")) {
			t.Fatal("pinned controller labels do not match dashboard NetworkPolicy")
		}
	}
}
