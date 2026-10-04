package ak3s

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func checksum(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }
func helmArchive(t *testing.T, name string, kind byte) []byte {
	t.Helper()
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	tw := tar.NewWriter(gz)
	payload := []byte("fake helm")
	h := &tar.Header{Name: name, Mode: 0755, Size: int64(len(payload)), Typeflag: kind}
	if kind != tar.TypeReg {
		h.Size = 0
	}
	if err := tw.WriteHeader(h); err != nil {
		t.Fatal(err)
	}
	if kind == tar.TypeReg {
		tw.Write(payload)
	}
	tw.Close()
	gz.Close()
	return b.Bytes()
}
func TestChecksumArchiveAndPins(t *testing.T) {
	data := []byte("verified")
	if err := Verify(data, checksum(data)); err != nil {
		t.Fatal(err)
	}
	for _, sum := range []string{"", "abc", strings.Repeat("0", 64)} {
		if Verify(data, sum) == nil {
			t.Fatal("bad checksum accepted")
		}
	}
	binary, err := helmBinary(helmArchive(t, "linux-amd64/helm", tar.TypeReg), "amd64")
	if err != nil || string(binary) != "fake helm" {
		t.Fatal(err)
	}
	for _, archive := range [][]byte{[]byte("not gzip"), helmArchive(t, "../helm", tar.TypeReg), helmArchive(t, "linux-amd64/helm", tar.TypeSymlink), helmArchive(t, "linux-arm64/helm", tar.TypeReg)} {
		if _, err := helmBinary(archive, "amd64"); err == nil {
			t.Fatal("invalid archive accepted")
		}
	}
	p := testPins(t)
	if len(p.Charts) != 6 || !strings.HasSuffix(p.K3sURL("amd64"), "/k3s") || !strings.HasSuffix(p.K3sURL("arm64"), "/k3s-arm64") || kubeVersion(p) != "1.36.5" {
		t.Fatal(p)
	}
}
func TestHTTPDownloader(t *testing.T) {
	payload := []byte("verified payload")
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
			w.Write(payload)
		case "/redirect":
			http.Redirect(w, r, "/ok", http.StatusFound)
		case "/insecure":
			http.Redirect(w, r, "http://example.invalid/file", http.StatusFound)
		default:
			http.Error(w, "missing", 404)
		}
	}))
	defer server.Close()
	downloader := HTTPDownloader{Client: server.Client()}
	for _, path := range []string{"/ok", "/redirect"} {
		out, err := downloader.Get(context.Background(), server.URL+path, checksum(payload))
		if err != nil || !bytes.Equal(out, payload) {
			t.Fatal(err)
		}
	}
	for _, path := range []string{"/missing", "/insecure"} {
		if _, err := downloader.Get(context.Background(), server.URL+path, checksum(payload)); err == nil {
			t.Fatal("unsafe response accepted")
		}
	}
	if _, err := downloader.Get(context.Background(), server.URL+"/ok", checksum([]byte("wrong"))); err == nil {
		t.Fatal("corruption ignored")
	}
	if _, err := downloader.Get(context.Background(), "http://example.invalid", checksum(payload)); err == nil {
		t.Fatal("HTTP allowed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := downloader.Get(ctx, server.URL+"/ok", checksum(payload)); err == nil {
		t.Fatal("cancellation ignored")
	}
}

type downloadFunc func(context.Context, string, string) ([]byte, error)

func (f downloadFunc) Get(ctx context.Context, url, sum string) ([]byte, error) {
	return f(ctx, url, sum)
}
func TestPrepareUsesRealRenderingPathAndIsolatedWorkspace(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(map[bool]string{false: "fresh", true: "existing"}[existing], func(t *testing.T) {
			c := testConfig(t)
			p := Plan{Config: c, Pins: testPins(t), Snapshot: freshSnapshot(), InstallBinary: true}
			p.Snapshot.Managed = existing
			p.Snapshot.Kubeconfig = existing
			p.Snapshot.ServiceActive = existing
			calls := []Command{}
			downloads := 0
			rendered := []byte("apiVersion: v1\nkind: Namespace\nmetadata: {name: example}\n")
			n := NativePreparer{Downloads: downloadFunc(func(_ context.Context, url, sum string) ([]byte, error) {
				downloads++
				if !validSHA(sum) {
					t.Fatal("download without pin")
				}
				if strings.Contains(url, "get.helm.sh") {
					return helmArchive(t, "linux-amd64/helm", tar.TypeReg), nil
				}
				return []byte("archive"), nil
			}), Runner: runFunc(func(_ context.Context, c Command) ([]byte, error) {
				calls = append(calls, c)
				if c.Args[0] == "list" {
					return []byte(`[{"name":"nginx-ingress","namespace":"ingress-nginx","chart":"nginx-ingress-2.7.3","status":"deployed"}]`), nil
				}
				if c.Args[0] == "template" {
					return rendered, nil
				}
				return nil, nil
			})}
			b, err := n.Prepare(context.Background(), p)
			if err != nil {
				t.Fatal(err)
			}
			dir := b.Dir
			if downloads != 8 || len(b.Rendered) != 6 || len(b.Manifests) != 3 {
				t.Fatal("incomplete preparation")
			}
			serverDryRuns := 0
			for _, cmd := range calls {
				if cmd.Args[0] == "upgrade" {
					serverDryRuns++
					if !strings.Contains(strings.Join(cmd.Args, " "), "--dry-run=server") {
						t.Fatal("preflight performed a real upgrade")
					}
				}
				for _, env := range cmd.Env {
					if strings.HasPrefix(env, "HELM_") && !strings.Contains(env, dir) {
						t.Fatal("ambient Helm state used")
					}
				}
			}
			if existing && serverDryRuns != 1 || !existing && serverDryRuns != 0 {
				t.Fatal("wrong live validation path")
			}
			b.Close()
			if _, err := os.Stat(dir); !os.IsNotExist(err) {
				t.Fatal("temporary workspace retained")
			}
		})
	}
}
func TestPrepareFailuresCleanUp(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	p := Plan{Config: testConfig(t), Pins: testPins(t), Snapshot: freshSnapshot()}
	for _, failure := range []string{"download", "archive", "template", "manifest"} {
		t.Run(failure, func(t *testing.T) {
			n := NativePreparer{Downloads: downloadFunc(func(_ context.Context, url, _ string) ([]byte, error) {
				if failure == "download" {
					return nil, errors.New("failed")
				}
				if strings.Contains(url, "get.helm.sh") && failure != "archive" {
					return helmArchive(t, "linux-amd64/helm", tar.TypeReg), nil
				}
				return []byte("archive"), nil
			}), Runner: runFunc(func(context.Context, Command) ([]byte, error) {
				if failure == "template" {
					return nil, errors.New("template failed")
				}
				return []byte("not: manifest"), nil
			})}
			if _, err := n.Prepare(context.Background(), p); err == nil {
				t.Fatal("failure ignored")
			}
			entries, _ := os.ReadDir(tmp)
			if len(entries) > 0 {
				t.Fatal("temporary files leaked")
			}
		})
	}
	p.Config.Platform = false
	b, err := (NativePreparer{}).Prepare(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	b.Close()
}
func TestBootstrapInstaller(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "tools")
	os.Mkdir(bin, 0700)
	destination := filepath.Join(root, "installed")
	payload := []byte("release binary")
	for _, tc := range []struct {
		name, arch, mode string
		ok               bool
	}{{"amd64", "x86_64", "valid", true}, {"arm64", "aarch64", "valid", true}, {"corrupt", "x86_64", "corrupt", false}, {"missing", "x86_64", "missing", false}, {"duplicate", "x86_64", "duplicate", false}, {"unsupported", "i386", "valid", false}} {
		t.Run(tc.name, func(t *testing.T) {
			os.WriteFile(filepath.Join(bin, "uname"), []byte("#!/bin/sh\nif [ \"$1\" = -s ]; then echo Linux; else echo "+tc.arch+"; fi\n"), 0700)
			script := `#!/bin/sh
while [ "$#" -gt 0 ]; do
 case "$1" in
  https://*) url="$1" ;;
  -o) shift; output="$1" ;;
 esac
 shift
done
case "$url" in
 */checksums.txt) printf '%s' "$TEST_CHECKSUMS" > "$output" ;;
 *) printf '%s' 'release binary' > "$output" ;;
esac
`
			os.WriteFile(filepath.Join(bin, "curl"), []byte(script), 0700)
			manifest := checksum(payload) + "  ak3s_linux_amd64\n" + checksum(payload) + "  ak3s_linux_arm64\n"
			switch tc.mode {
			case "corrupt":
				manifest = strings.Repeat("0", 64) + "  ak3s_linux_amd64\n"
			case "missing":
				manifest = ""
			case "duplicate":
				manifest += checksum(payload) + "  ak3s_linux_amd64\n"
			}
			os.MkdirAll(destination, 0700)
			os.WriteFile(filepath.Join(destination, "ak3s"), []byte("old binary"), 0755)
			cmd := exec.Command("bash", "../../install.sh", "v0.2.0")
			cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"), "AK3S_INSTALL_DIR="+destination, "TEST_CHECKSUMS="+manifest)
			out, err := cmd.CombinedOutput()
			if (err == nil) != tc.ok {
				t.Fatalf("%v: %s", err, out)
			}
			installed, _ := os.ReadFile(filepath.Join(destination, "ak3s"))
			want := "old binary"
			if tc.ok {
				want = string(payload)
			}
			if string(installed) != want {
				t.Fatal("binary unexpectedly replaced")
			}
		})
	}
	for _, version := range []string{"", "latest", "v1.2.3;touch bad", "../v1.2.3"} {
		cmd := exec.Command("bash", "../../install.sh", version)
		cmd.Stdout = io.Discard
		cmd.Stderr = io.Discard
		if cmd.Run() == nil {
			t.Fatal("bad release version accepted")
		}
	}
}
