// Program entry point: assembly and startup only, no business logic.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/ravenmk2/octodcp/internal/app"
)

var version = "dev" // default when not injected; release builds inject via -ldflags "-X main.version="

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := app.Run(ctx, os.Args[1:], version, os.Stdout, os.Stderr); err != nil {
		slog.Error("octodcp stopped", "error", err)
		os.Exit(1)
	}
}
