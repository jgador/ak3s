package ak3s

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// Uninstaller removes a local installation using its generated upstream scripts.
type Uninstaller interface {
	Uninstall(context.Context, Snapshot) error
}

// checkUninstall requires ownership and verified removal scripts. A checkpoint
// remains authoritative after upstream has removed the original ownership marker.
func checkUninstall(s Snapshot) error {
	if (!s.Existing || s.EmptyDataOnly) && !s.Managed && s.State == nil {
		return nil
	}
	if s.State == nil || s.State.Schema != 2 || (!s.Managed && !s.State.PendingUninstall) {
		return errors.New("uninstall requires an AK3S-managed installation migrated with ak3s apply; see docs/migration.md")
	}
	if s.State.Node.Role != "server" && s.State.Node.Role != "agent" {
		return errors.New("invalid node role in AK3S state")
	}
	if s.Role != "" && s.Role != s.State.Node.Role {
		return errors.New("K3s service role differs from AK3S state")
	}
	if len(s.Problems) > 0 {
		return (Plan{Problems: s.Problems}).Check()
	}
	if s.State.PendingUninstall && s.State.UninstallComplete {
		return nil
	}
	for _, path := range []string{uninstallPath(s.State.Node.Role), KillallPath} {
		source := path
		if s.State.PendingUninstall {
			source = removalBackup(path)
		}
		if err := Verify(s.Files[source], s.State.GeneratedScripts[path]); err != nil {
			return fmt.Errorf("unverified removal script %s; restore the generated script or complete migration before uninstalling", source)
		}
	}
	return nil
}

func removalBackup(path string) string {
	if path == KillallPath {
		return RemovalKillallPath
	}
	return RemovalScriptPath
}

// Uninstall locks and rechecks the host before invoking its generated uninstaller.
func (h *NativeHost) Uninstall(ctx context.Context, s Snapshot) error {
	if !s.Root {
		return errors.New("run uninstall as root")
	}
	if err := checkUninstall(s); err != nil {
		return err
	}
	if s.State == nil {
		return nil
	}
	unlock, err := lock(filepath.Dir(StatePath))
	if err != nil {
		return err
	}
	defer unlock()
	c := Config{Node: s.State.Node}
	c.Node.TokenFile = "" // removal does not need a join credential
	fresh, err := h.Inspect(ctx, c)
	if err != nil {
		return err
	}
	if err = checkUninstall(fresh); err != nil {
		return err
	}
	if !sameSnapshot(s, fresh) {
		return errors.New("host changed while planning; rerun AK3S")
	}
	return uninstall(ctx, s, h.Reader, h.Runner, AtomicWrite, os.Remove)
}

func uninstall(ctx context.Context, s Snapshot, reader Reader, runner Runner, write func(string, []byte, fs.FileMode) error, remove func(string) error) error {
	if err := checkUninstall(s); err != nil {
		return err
	}
	if s.State == nil {
		return nil
	}
	state := *s.State
	if state.UninstallComplete {
		return removeBookkeeping(remove)
	}
	script := uninstallPath(state.Node.Role)
	// The upstream uninstaller deletes itself even if cleanup was interrupted.
	// Keep private, unmodified copies so the same operation can be retried.
	for _, path := range []string{script, KillallPath} {
		source := path
		if state.PendingUninstall {
			source = removalBackup(path)
		}
		data, err := readPrivateScript(reader, source)
		if err != nil {
			return err
		}
		if err = Verify(data, state.GeneratedScripts[path]); err != nil {
			return fmt.Errorf("removal script changed: %s", source)
		}
		if err = write(removalBackup(path), data, 0600); err != nil {
			return err
		}
	}
	state.PendingUninstall = true
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	if err = write(StatePath, append(data, '\n'), 0600); err != nil {
		return err
	}
	for _, path := range []string{script, KillallPath} {
		data, err := reader.ReadFile(removalBackup(path))
		if err != nil {
			return err
		}
		if err = Verify(data, state.GeneratedScripts[path]); err != nil {
			return err
		}
		if err = write(path, data, 0755); err != nil {
			return err
		}
	}
	if _, err = runner.Run(ctx, Command{Name: "/bin/sh", Args: []string{script}, IsolatedEnv: true}); err != nil {
		return fmt.Errorf("K3s uninstall interrupted: %w; rerun ak3s uninstall --yes", err)
	}
	// Upstream cleanup is best effort. A zero exit code alone does not establish
	// success, so retain the checkpoint until all local K3s data is removed.
	for _, path := range []string{K3sPath, unitPath(state.Node.Role), unitPath(state.Node.Role) + ".env", script, KillallPath, "/etc/rancher/k3s", "/run/k3s", "/run/flannel", "/var/lib/kubelet", "/var/lib/rancher/k3s"} {
		_, err := reader.Lstat(path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		// Upstream preserves an empty data directory when it is a mount point.
		if err == nil && path == "/var/lib/rancher/k3s" {
			entries, readErr := reader.ReadDir(path)
			if readErr == nil && len(entries) == 0 {
				continue
			}
		}
		return fmt.Errorf("K3s cleanup incomplete at %s; inspect mounts and services, then rerun ak3s uninstall --yes", path)
	}
	state.UninstallComplete = true
	data, err = json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	if err = write(StatePath, append(data, '\n'), 0600); err != nil {
		return err
	}
	return removeBookkeeping(remove)
}

// Only AK3S's own bookkeeping and host settings are removed here. K3s owns
// service, process, network, datastore and volume removal.
func removeBookkeeping(remove func(string) error) error {
	for _, path := range []string{ModulesPath, SysctlPath, RemovalScriptPath, RemovalKillallPath, StatePath} {
		if err := remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return nil
}
