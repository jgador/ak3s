package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/jgador/ak3s/internal/ak3s"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := ak3s.ServeDashboard(ctx, os.DirFS("/app/dashboard")); err != nil {
		fmt.Fprintln(os.Stderr, "ak3s-dashboard:", err)
		os.Exit(1)
	}
}
