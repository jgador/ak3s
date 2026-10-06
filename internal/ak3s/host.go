package ak3s

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"go.yaml.in/yaml/v3"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Command describes a child process, including optional stdin and environment overrides.
type Command struct {
	Name  string
	Args  []string
	Input []byte
	Env   []string
	// IsolatedEnv excludes inherited network settings as well as application settings.
	IsolatedEnv bool
}

// Runner abstracts process execution so tests can record commands without running them.
type Runner interface {

	// Run executes a command and returns its output or an error.
	Run(context.Context, Command) ([]byte, error)
}

// ExecRunner runs real processes with timeouts and a controlled environment.
type ExecRunner struct{}

// Run captures successful output and suppresses failed output that may contain secrets.
func (ExecRunner) Run(ctx context.Context, c Command) ([]byte, error) {
	timeout := 15 * time.Minute
	if filepath.Base(c.Name) == "systemctl" {
		timeout = 5 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.Name, c.Args...)
	// Installer shells start children. Cancel the whole process group before
	// releasing the host lock so a detached installer child cannot keep writing.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = time.Second
	cmd.Stdin = bytes.NewReader(c.Input)
	if c.IsolatedEnv {
		cmd.Env = append(baseEnv(), c.Env...)
	} else {
		cmd.Env = cleanEnv(c.Env)
	}
	output, err := cmd.CombinedOutput()

	// Do not print command output on failure: Helm and validation errors can contain Secrets.
	if err != nil {
		return nil, fmt.Errorf("%s %s failed: %w (output suppressed to protect secrets)", filepath.Base(c.Name), firstArg(c.Args), err)
	}
	return output, nil
}
func firstArg(args []string) string {
	if len(args) > 0 {
		return args[0]
	}
	return ""
}

// baseEnv supplies the fixed environment for local installer operations.
func baseEnv() []string {
	return []string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin", "HOME=/root", "LANG=C.UTF-8"}
}

// cleanEnv builds a predictable child environment with permitted network settings.
func cleanEnv(extra []string) []string {

	// Do not inherit K3S_*, HELM_*, KUBECONFIG or plugin configuration from the shell.
	result := baseEnv()

	// Proxy settings are needed for restricted outbound installations.
	for _, k := range []string{"HTTPS_PROXY", "HTTP_PROXY", "NO_PROXY", "https_proxy", "http_proxy", "no_proxy", "SSL_CERT_FILE", "SSL_CERT_DIR"} {
		if v, ok := os.LookupEnv(k); ok {
			result = append(result, k+"="+v)
		}
	}
	return append(result, extra...)
}

// Reader abstracts the filesystem reads used during host inspection.
type Reader interface {

	// ReadFile returns the contents of a file.
	ReadFile(string) ([]byte, error)

	// Lstat returns file information without following symlinks.
	Lstat(string) (fs.FileInfo, error)

	// Stat returns file information, following symlinks.
	Stat(string) (fs.FileInfo, error)

	// ReadDir lists the entries in a directory.
	ReadDir(string) ([]fs.DirEntry, error)
}

// OSReader implements Reader using the local filesystem.
type OSReader struct{}

// ReadFile returns the contents of a local file.
func (OSReader) ReadFile(p string) ([]byte, error) { return os.ReadFile(p) }

// Lstat returns local file information without following symlinks.
func (OSReader) Lstat(p string) (fs.FileInfo, error) { return os.Lstat(p) }

// Stat returns local file information, following symlinks.
func (OSReader) Stat(p string) (fs.FileInfo, error) { return os.Stat(p) }

// ReadDir lists the entries in a local directory.
func (OSReader) ReadDir(p string) ([]fs.DirEntry, error) { return os.ReadDir(p) }

// Host separates read-only inspection from applying a validated plan.
type Host interface {

	// Inspect observes this node without changing its files or services.
	Inspect(context.Context, Config) (Snapshot, error)

	// Apply executes a validated plan using its prepared artifacts.
	Apply(context.Context, Plan, *Bundle) error
}

// NativeHost manages the local machine through filesystem and process dependencies.
type NativeHost struct {
	Reader Reader
	Runner Runner
}

// NewNativeHost uses real filesystem reads and child processes.
func NewNativeHost() *NativeHost { return &NativeHost{Reader: OSReader{}, Runner: ExecRunner{}} }

// Inspect collects runtime identity and the local installation's current state.
func (h *NativeHost) Inspect(ctx context.Context, c Config) (Snapshot, error) {
	name, err := os.Hostname()
	if err != nil {
		return Snapshot{}, err
	}
	return Inspect(ctx, h.Reader, h.Runner, c, Snapshot{OS: runtime.GOOS, Arch: runtime.GOARCH, Root: os.Geteuid() == 0, Hostname: name})
}

// Inspect reads ownership, configuration, credentials, and host prerequisites.
// It records failed prerequisite checks without changing host files or services.
func Inspect(ctx context.Context, r Reader, runner Runner, c Config, s Snapshot) (Snapshot, error) {
	s.Files = map[string][]byte{}
	exists := func(p string) (bool, error) {
		_, err := r.Stat(p)
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return err == nil, err
	}
	read := func(p string) ([]byte, error) {
		data, err := r.ReadFile(p)
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return data, err
	}
	for _, p := range []string{ConfigPath, MarkerPath, StatePath, ModulesPath, SysctlPath, unitPath("server"), unitPath("agent"), unitPath("server") + ".env", unitPath("agent") + ".env", uninstallPath("server"), uninstallPath("agent"), KillallPath, RemovalScriptPath, RemovalKillallPath} {
		if info, err := r.Lstat(p); err == nil && !info.Mode().IsRegular() {
			return s, fmt.Errorf("managed destination is not a regular file: %s", p)
		} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return s, err
		}
		data, err := read(p)
		if err != nil {
			return s, fmt.Errorf("inspect %s: %w", p, err)
		}
		s.Files[p] = data
	}
	s.Managed = len(s.Files[MarkerPath]) > 0
	if s.Managed && !bytes.HasPrefix(s.Files[MarkerPath], []byte("Managed by AK3S")) {
		return s, errors.New("unrecognized AK3S ownership marker")
	}
	if len(s.Files[StatePath]) > 0 {
		if err := json.Unmarshal(s.Files[StatePath], &s.State); err != nil {
			return s, fmt.Errorf("invalid AK3S state: %w", err)
		}
		if s.State == nil {
			return s, errors.New("empty AK3S state")
		}
	}
	dataOnly := true
	for _, p := range []string{K3sPath, ConfigPath, MarkerPath, StatePath, "/var/lib/rancher/k3s", unitPath("server"), unitPath("agent"), uninstallPath("server"), uninstallPath("agent"), KillallPath} {
		ok, err := exists(p)
		if err != nil {
			return s, err
		}
		s.Existing = s.Existing || ok
		if p == "/var/lib/rancher/k3s" {
			s.DataPresent = ok
		} else if ok {
			dataOnly = false
		}
	}
	if s.DataPresent && dataOnly {
		entries, err := r.ReadDir("/var/lib/rancher/k3s")
		if err != nil {
			return s, err
		}
		s.EmptyDataOnly = len(entries) == 0
	}
	if len(s.Files[unitPath("agent")]) > 0 {
		s.Role = "agent"
	} else if len(s.Files[unitPath("server")]) > 0 {
		s.Role = "server"
	}
	if s.Role == "" && s.State != nil {
		s.Role = s.State.Node.Role
	}
	if len(s.Files[unitPath("agent")]) > 0 && len(s.Files[unitPath("server")]) > 0 {
		s.Problems = append(s.Problems, "both K3s server and agent units exist")
	}
	for _, dir := range []string{"/etc/systemd/system", "/etc/init.d"} {
		entries, err := r.ReadDir(dir)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return s, err
		}
		for _, entry := range entries {
			name := entry.Name()
			if strings.HasPrefix(name, "k3s") && ((dir == "/etc/systemd/system" && strings.HasSuffix(name, ".service") && name != "k3s.service" && name != "k3s-agent.service") || dir == "/etc/init.d") {
				s.Problems = append(s.Problems, "additional K3s service requires manual review: "+filepath.Join(dir, name))
			}
		}
	}
	for _, p := range []string{"/etc/rancher/k3s/config.yaml.d", unitPath("server") + ".d", unitPath("agent") + ".d"} {
		entries, err := r.ReadDir(p)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return s, err
		}
		if len(entries) > 0 {
			s.Problems = append(s.Problems, "unmanaged configuration in a drop-in directory at "+p)
		}
	}

	// Environment files from older installations may override config.yaml.
	// Require operators to migrate them explicitly to preserve cluster identity.
	for _, p := range []string{unitPath(c.Node.Role) + ".env", "/etc/default/" + serviceName(c.Node.Role), "/etc/sysconfig/" + serviceName(c.Node.Role)} {
		data, err := read(p)
		if err != nil {
			return s, err
		}
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line != "" && !strings.HasPrefix(line, "#") {
				s.Problems = append(s.Problems, "migrate service environment before proceeding: "+p)
				break
			}
		}
	}
	osrelease, err := r.ReadFile("/etc/os-release")
	if err != nil {
		return s, err
	}
	for _, line := range strings.Split(string(osrelease), "\n") {
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		v = strings.Trim(v, "\"'")
		if k == "ID" {
			s.Distribution = v
		}
		if k == "VERSION_ID" {
			s.DistributionVersion = v
		}
	}
	version, _ := strconv.ParseFloat(s.DistributionVersion, 64)
	if !(s.Distribution == "ubuntu" && version >= 24.04 || s.Distribution == "debian" && version >= 12) {
		s.Problems = append(s.Problems, "use Ubuntu 24.04+ or Debian 12+")
	}
	s.Systemd, err = exists("/run/systemd/system")
	if err != nil {
		return s, err
	}
	swaps, err := r.ReadFile("/proc/swaps")
	if err != nil {
		return s, err
	}
	s.Swap = len(strings.Fields(string(swaps))) > 5
	s.Kubeconfig, err = exists(KubeconfigPath)
	if err != nil {
		return s, err
	}
	if c.Node.TokenFile != "" && (s.State == nil || !s.State.PendingUninstall) {
		data, err := r.ReadFile(c.Node.TokenFile)
		if err != nil {
			return s, fmt.Errorf("read node.token_file: %w", err)
		}
		token := strings.TrimSpace(string(data))
		if token == "" || len(token) > 4096 || strings.ContainsAny(token, " \t\r\n") {
			return s, errors.New("node.token_file must contain one nonempty token")
		}
		info, err := r.Stat(c.Node.TokenFile)
		if err != nil {
			return s, err
		}
		if info.Mode().Perm()&0077 != 0 {
			return s, errors.New("node.token_file must be readable only by its owner (0600)")
		}

		// Persist only a fingerprint so later runs can detect token changes.
		sum := sha256.Sum256(bytes.TrimSpace(data))
		s.TokenSHA256 = hex.EncodeToString(sum[:])
	}
	if c.Node.TokenFile == "" && len(s.Files[ConfigPath]) > 0 {
		var previous struct {
			Token string `yaml:"token"`
		}
		if err := yaml.Unmarshal(s.Files[ConfigPath], &previous); err != nil {
			return s, fmt.Errorf("inspect K3s config: %w", err)
		}
		if previous.Token != "" {
			sum := sha256.Sum256([]byte(strings.TrimSpace(previous.Token)))
			s.TokenSHA256 = hex.EncodeToString(sum[:])
		}
	}

	if info, err := r.Lstat(K3sPath); err == nil && !info.Mode().IsRegular() {
		return s, errors.New("K3s binary is not a regular file")
	} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return s, err
	}
	// Never execute a binary from an installation that AK3S does not manage.
	if ok, err := exists(K3sPath); err != nil {
		return s, err
	} else if ok && s.Managed {
		out, err := runner.Run(ctx, Command{Name: K3sPath, Args: []string{"--version"}})
		if err != nil {
			return s, err
		}
		fields := strings.Fields(string(out))
		if len(fields) < 3 || fields[0] != "k3s" || fields[1] != "version" {
			return s, errors.New("unexpected K3s version output")
		}
		s.K3sVersion = fields[2]
	}
	if s.Managed && s.Systemd {
		short, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		out, err := runner.Run(short, Command{Name: "systemctl", Args: []string{"is-active", serviceName(c.Node.Role)}})
		s.ServiceActive = err == nil && strings.TrimSpace(string(out)) == "active"
		_, err = runner.Run(short, Command{Name: "systemctl", Args: []string{"is-enabled", "--quiet", serviceName(c.Node.Role)}})
		s.ServiceEnabled = err == nil
	}
	return s, nil
}

// AtomicWrite replaces regular files by renaming a file in the same directory.
// It rejects a symlink at the destination and corrects existing file permissions.
func AtomicWrite(path string, data []byte, mode fs.FileMode) error {
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("destination is not a regular file: %s", path)
		}
		old, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if bytes.Equal(old, data) {
			return os.Chmod(path, mode)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".ak3s-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(mode); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), path)
}
