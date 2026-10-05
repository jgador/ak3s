package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/jgador/ak3s/internal/ak3s"
)

// main runs the CLI with signal cancellation and reports failures to stderr.
func main() {

	// Cancel downloads and child processes when the CLI is interrupted.
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Keep process exit handling here; application logic returns errors to callers.
	if err := ak3s.NewApp(os.Stdout, os.Stderr).Run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "ak3s:", err)
		os.Exit(1)
	}
}
