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
	if err := ak3s.NewApp(os.Stdout, os.Stderr).Run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "ak3s:", err)
		os.Exit(1)
	}
}
