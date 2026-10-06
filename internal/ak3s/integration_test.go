//go:build integration

package ak3s

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
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
