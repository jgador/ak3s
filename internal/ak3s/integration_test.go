//go:build integration

package ak3s

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestPinnedCharts downloads the release-pinned Helm binary and chart archives,
// verifies their checksums, and runs real Helm schema/template validation. It
// needs outbound HTTPS but no root, Docker, systemd, or cluster.
func TestPinnedCharts(t *testing.T) {
	c := testConfig(t)
	c.HeadlampHostname = "dashboard.example.com"
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
	}
}
