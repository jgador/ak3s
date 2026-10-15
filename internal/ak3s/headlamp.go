package ak3s

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

const headlampTokenUsage = `Usage: sudo ak3s headlamp-token

Print a new one-hour token for the read-only headlamp-viewer service account.
Paste the token into Headlamp's login page. Keep it private.
Uses only the local AK3S-managed server and does not save the token to a file.
`

func (a *App) headlampToken(ctx context.Context, args []string) error {
	f := flag.NewFlagSet("headlamp-token", flag.ContinueOnError)
	f.SetOutput(a.Err)
	f.Usage = func() { io.WriteString(a.Err, headlampTokenUsage) }
	if err := f.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if f.NArg() != 0 {
		return errors.New("headlamp-token does not accept positional arguments")
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	// Token creation does not depend on operator overrides or installation plans.
	c, err := LoadConfig(nil)
	if err != nil {
		return err
	}
	s, err := a.Host.Inspect(ctx, c)
	if err != nil {
		return fmt.Errorf("inspect local AK3S server; run sudo ak3s headlamp-token: %w", err)
	}
	if !s.Managed || !s.Existing || s.Role != "server" {
		return errors.New("headlamp-token requires an existing local AK3S-managed server")
	}
	if !s.Root {
		return errors.New("run sudo ak3s headlamp-token to access the local K3s kubeconfig")
	}
	if !s.ServiceActive || !s.Kubeconfig {
		return errors.New("headlamp-token requires active K3s and its local kubeconfig")
	}
	if s.State == nil || (s.State.Schema != 1 && s.State.Schema != 2) || s.State.Node.Role != "server" || s.State.PendingUninstall || s.State.PendingRestart {
		return errors.New("headlamp-token requires valid AK3S server state with no pending cluster operation")
	}
	dir, err := os.MkdirTemp("", "ak3s-headlamp-token-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	output, err := a.Runner.Run(ctx, Command{
		Name: K3sPath,
		Args: []string{"kubectl", "--kubeconfig", KubeconfigPath, "--cache-dir", dir,
			"-n", "headlamp", "create", "token", "headlamp-viewer", "--duration=1h", "--request-timeout=10s"},
		IsolatedEnv: true,
	})
	if err != nil {
		return fmt.Errorf("create Headlamp viewer token; check that the AK3S platform is installed: %w", err)
	}
	token := strings.TrimSpace(string(output))
	// ExecRunner combines output streams. Never print warnings or other text as a token.
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return errors.New("invalid Headlamp token response (output suppressed)")
	}
	for _, part := range parts {
		if part == "" || strings.IndexFunc(part, func(r rune) bool {
			return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_')
		}) >= 0 {
			return errors.New("invalid Headlamp token response (output suppressed)")
		}
	}
	_, err = fmt.Fprintln(a.Out, token)
	return err
}
