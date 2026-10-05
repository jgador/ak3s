package ak3s

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jgador/ak3s/platform"
	"go.yaml.in/yaml/v3"
)

// Version is the AK3S release label, set by the build's linker flags.
var Version = "dev"

// Commit is the source revision, set by the build's linker flags.
var Commit = "unknown"

// Pins identifies the exact binaries and ordered Helm charts shipped in a release.
type Pins struct {
	K3s        string            `yaml:"k3s"`
	K3sSHA256  map[string]string `yaml:"k3s_sha256"`
	Helm       string            `yaml:"helm"`
	HelmSHA256 map[string]string `yaml:"helm_sha256"`
	Charts     []Chart           `yaml:"charts"`
}

// Chart describes a pinned chart archive, its release destination, and default values.
type Chart struct {
	Release    string `yaml:"release"`
	Namespace  string `yaml:"namespace"`
	Chart      string `yaml:"chart"`
	Repository string `yaml:"repository"`
	Version    string `yaml:"version"`
	URL        string `yaml:"url"`
	SHA256     string `yaml:"sha256"`
	Values     string `yaml:"values"`
	SkipCRDs   bool   `yaml:"skip_crds"`
}

// LoadPins reads and validates embedded versions and artifact checksums.
func LoadPins() (Pins, error) {
	var p Pins
	data, err := platform.Files.ReadFile("versions.yaml")
	if err != nil {
		return p, err
	}
	d := yaml.NewDecoder(bytes.NewReader(data))
	d.KnownFields(true)
	if err = d.Decode(&p); err != nil {
		return p, err
	}
	if _, err = ParseK3sVersion(p.K3s); err != nil {
		return p, err
	}
	if !regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`).MatchString(p.Helm) {
		return p, errors.New("Helm must have an exact release pin")
	}
	for _, arch := range []string{"amd64", "arm64"} {
		for _, sum := range []string{p.K3sSHA256[arch], p.HelmSHA256[arch]} {
			if !validSHA(sum) {
				return p, errors.New("missing binary checksum pin")
			}
		}
	}
	seen := map[string]bool{}
	for _, c := range p.Charts {
		if seen[c.Release] || !labelRE.MatchString(c.Release) || !labelRE.MatchString(c.Namespace) || !strings.HasPrefix(c.URL, "https://") || !validSHA(c.SHA256) || !regexp.MustCompile(`^v?[0-9]+\.[0-9]+\.[0-9]+$`).MatchString(c.Version) {
			return p, fmt.Errorf("invalid chart pin: %s", c.Release)
		}
		seen[c.Release] = true
	}
	return p, nil
}
func validSHA(s string) bool {
	b, err := hex.DecodeString(s)
	return err == nil && len(b) == sha256.Size
}

// Verify requires data to match a valid, pinned SHA-256 checksum.
func Verify(data []byte, expected string) error {
	if !validSHA(expected) {
		return errors.New("invalid SHA-256 pin")
	}
	sum := sha256.Sum256(data)
	if !strings.EqualFold(hex.EncodeToString(sum[:]), expected) {
		return errors.New("SHA-256 checksum mismatch")
	}
	return nil
}

// ParseK3sVersion returns the major, minor, patch, and K3s revision numbers.
func ParseK3sVersion(s string) ([4]int, error) {
	var result [4]int
	m := regexp.MustCompile(`^v([0-9]+)\.([0-9]+)\.([0-9]+)\+k3s([0-9]+)$`).FindStringSubmatch(s)
	if m == nil {
		return result, fmt.Errorf("invalid K3s version %q", s)
	}
	for i := range result {
		n, err := strconv.Atoi(m[i+1])
		if err != nil {
			return result, err
		}
		result[i] = n
	}
	return result, nil
}

// CheckUpgrade allows fresh installs and unchanged versions, but rejects downgrades.
// Version increases require allow and cannot skip a Kubernetes minor version.
func CheckUpgrade(current, target string, allow bool) error {
	if current == "" || current == target {
		return nil
	}
	a, err := ParseK3sVersion(current)
	if err != nil {
		return err
	}
	b, err := ParseK3sVersion(target)
	if err != nil {
		return err
	}
	for i := range a {
		if b[i] < a[i] {
			return errors.New("K3s downgrades are refused; restore from a compatible backup instead")
		}
		if b[i] > a[i] {
			break
		}
	}
	if a[0] != b[0] || b[1] > a[1]+1 {
		return errors.New("K3s upgrades must not skip a Kubernetes minor version")
	}
	if !allow {
		return errors.New("K3s version differs; back up the cluster and run ak3s upgrade")
	}
	return nil
}

// Downloader retrieves an artifact and verifies it against the supplied checksum.
type Downloader interface {
	Get(context.Context, string, string) ([]byte, error)
}

// HTTPDownloader uses the supplied client, or a default client with a download timeout.
type HTTPDownloader struct{ Client *http.Client }

// Get requires HTTPS, bounds the response size, and verifies bytes before returning them.
func (h HTTPDownloader) Get(ctx context.Context, url, sum string) ([]byte, error) {
	if !strings.HasPrefix(url, "https://") {
		return nil, errors.New("downloads require HTTPS")
	}
	client := h.Client
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Minute}
	}
	// Never accept an HTTPS -> HTTP redirect, including for release assets.
	copyClient := *client
	previous := client.CheckRedirect
	copyClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "https" {
			return errors.New("insecure download redirect")
		}
		if len(via) >= 10 {
			return errors.New("too many redirects")
		}
		if previous != nil {
			return previous(req, via)
		}
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "ak3s/"+Version)
	resp, err := copyClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: HTTP %d", url, resp.StatusCode)
	}
	const limit = 256 << 20
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if len(data) > limit {
		return nil, errors.New("download exceeds 256 MiB")
	}
	if err := Verify(data, sum); err != nil {
		return nil, fmt.Errorf("download %s: %w", url, err)
	}
	return data, nil
}

// helmBinary reads only the expected executable entry without extracting archive paths.
func helmBinary(archive []byte, arch string) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			return nil, errors.New("Helm archive does not contain expected binary")
		}
		if err != nil {
			return nil, err
		}
		if header.Name != "linux-"+arch+"/helm" {
			continue
		}
		if header.Typeflag != tar.TypeReg || header.Size <= 0 || header.Size > 128<<20 {
			return nil, errors.New("invalid Helm binary archive entry")
		}
		return io.ReadAll(io.LimitReader(tr, header.Size))
	}
}

// K3sURL selects the upstream release asset for the requested architecture.
func (p Pins) K3sURL(arch string) string {
	name := "k3s"
	if arch == "arm64" {
		name += "-arm64"
	}
	return "https://github.com/k3s-io/k3s/releases/download/" + p.K3s + "/" + name
}

// CheckAK3SUpgrade prevents accidentally applying an older release's chart pins.
// Development builds have no ordered release version and are intended for testing.
func CheckAK3SUpgrade(current, target string) error {
	re := regexp.MustCompile(`^v([0-9]+)\.([0-9]+)\.([0-9]+)$`)
	a, b := re.FindStringSubmatch(current), re.FindStringSubmatch(target)
	if a == nil || b == nil {
		return nil
	}
	for i := 1; i <= 3; i++ {
		x, ex := strconv.ParseUint(a[i], 10, 64)
		y, ey := strconv.ParseUint(b[i], 10, 64)
		if ex != nil || ey != nil {
			return errors.New("invalid AK3S release version")
		}
		if y < x {
			return errors.New("AK3S release downgrade refused; use a compatible backup/restore procedure")
		}
		if y > x {
			return nil
		}
	}
	return nil
}
