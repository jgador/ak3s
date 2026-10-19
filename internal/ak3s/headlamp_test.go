package ak3s

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestHeadlampTokenUsesLocalServerAndPrintsOnlyToken(t *testing.T) {
	app, host, out, prepared := appFixture(t)
	host.s = managedSnapshot(t, testConfig(t))
	app.ReadFile = func(string) ([]byte, error) {
		t.Fatal("token creation read operator configuration")
		return nil, nil
	}
	var cache string
	calls := 0
	app.Runner = runFunc(func(ctx context.Context, cmd Command) ([]byte, error) {
		calls++
		if cmd.Name != K3sPath || !cmd.IsolatedEnv || len(cmd.Env) != 0 || len(cmd.Input) != 0 || len(cmd.Args) != 12 {
			t.Fatalf("unexpected token command: %#v", cmd)
		}
		cache = cmd.Args[4]
		want := []string{"kubectl", "--kubeconfig", KubeconfigPath, "--cache-dir", cache,
			"-n", "headlamp", "create", "token", "headlamp-viewer", "--duration=1h", "--request-timeout=10s"}
		if !reflect.DeepEqual(cmd.Args, want) {
			t.Fatalf("unexpected token arguments: %v", cmd.Args)
		}
		if info, err := os.Stat(cache); err != nil || info.Mode().Perm() != 0700 {
			t.Fatal("token cache is not private", err)
		}
		if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > 20*time.Second {
			t.Fatal("token command has no bounded timeout")
		}
		return []byte("header.payload.signature\n"), nil
	})
	if err := app.Run(context.Background(), []string{"headlamp-token"}); err != nil {
		t.Fatal(err)
	}
	if out.String() != "header.payload.signature\n" || calls != 1 || host.inspections != 1 || host.applies != 0 || *prepared != 0 {
		t.Fatal("token output or execution boundary changed")
	}
	if _, err := os.Stat(cache); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("token cache was retained", err)
	}
}

func TestHeadlampTokenRejectsUnavailableServer(t *testing.T) {
	for _, mode := range []string{"inspect", "unmanaged", "absent", "agent", "nonroot", "inactive", "kubeconfig", "state", "schema", "state-role", "uninstall", "restart"} {
		t.Run(mode, func(t *testing.T) {
			app, host, out, prepared := appFixture(t)
			host.s = managedSnapshot(t, testConfig(t))
			switch mode {
			case "inspect":
				host.inspectErr = errors.New("inspection unavailable")
			case "unmanaged":
				host.s.Managed = false
			case "absent":
				host.s.Existing = false
			case "agent":
				host.s.Role = "agent"
			case "nonroot":
				host.s.Root = false
			case "inactive":
				host.s.ServiceActive = false
			case "kubeconfig":
				host.s.Kubeconfig = false
			case "state":
				host.s.State = nil
			case "schema":
				host.s.State.Schema = 99
			case "state-role":
				host.s.State.Node.Role = "agent"
			case "uninstall":
				host.s.State.PendingUninstall = true
			case "restart":
				host.s.State.PendingRestart = true
			}
			app.Runner = runFunc(func(context.Context, Command) ([]byte, error) {
				t.Fatal("token requested from an unavailable server")
				return nil, nil
			})
			if err := app.Run(context.Background(), []string{"headlamp-token"}); err == nil {
				t.Fatal("unavailable server accepted")
			}
			if out.Len() != 0 || host.applies != 0 || *prepared != 0 {
				t.Fatal("failure printed a token or changed the host")
			}
		})
	}
}

func TestHeadlampTokenFailureDoesNotExposeOutputAndCleansCache(t *testing.T) {
	for _, response := range []string{"failure", "", "header.payload.", "Warning: response\nheader.payload.signature", "header.pay load.signature"} {
		t.Run(response, func(t *testing.T) {
			app, host, out, _ := appFixture(t)
			host.s = managedSnapshot(t, testConfig(t))
			failure := errors.New("API unavailable")
			var cache string
			app.Runner = runFunc(func(_ context.Context, cmd Command) ([]byte, error) {
				cache = cmd.Args[4]
				if response == "failure" {
					return []byte("fictional-private-output"), failure
				}
				return []byte(response), nil
			})
			err := app.Run(context.Background(), []string{"headlamp-token"})
			if err == nil || out.Len() != 0 || strings.Contains(err.Error(), "fictional-private-output") || strings.Contains(err.Error(), "header.payload") {
				t.Fatal("failed token request exposed output")
			}
			if response == "failure" && !errors.Is(err, failure) {
				t.Fatal("token request error was lost")
			}
			if _, err := os.Stat(cache); !errors.Is(err, fs.ErrNotExist) {
				t.Fatal("failed token request retained its cache", err)
			}
		})
	}
}

func TestHeadlampTokenHelpAndFlagsDoNotInspectHost(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"--config", "operator.yaml"}, {"--duration=24h"}, {"--dry-run"}, {"extra"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			app, host, out, _ := appFixture(t)
			err := app.Run(context.Background(), append([]string{"headlamp-token"}, args...))
			if (err == nil) != (args[0] == "--help") || host.inspections != 0 {
				t.Fatal("help or invalid arguments inspected the host", err)
			}
			if args[0] == "--help" && !strings.Contains(out.String(), "one-hour token") {
				t.Fatal("token help is missing")
			}
		})
	}
}
