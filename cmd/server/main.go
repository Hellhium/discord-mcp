// Command server is the discord-mcp entrypoint: it loads the YAML config,
// builds the application and serves /healthz and /mcp on one listener until
// SIGINT or SIGTERM.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Hellhium/discord-mcp/internal/app"
	"github.com/Hellhium/discord-mcp/internal/audit"
	"github.com/Hellhium/discord-mcp/internal/config"
)

// version is overridable at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	configPath := flag.String("config", "", "path to the YAML config file (required)")
	verify := flag.Bool("verify-credentials", true, "check config credentials against Discord and open Gateways at startup")
	flag.Parse()

	if *configPath == "" {
		fmt.Fprintln(os.Stderr, "missing required -config flag")
		os.Exit(2)
	}
	if err := run(*configPath, *verify); err != nil {
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
}

func run(configPath string, verify bool) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	log := audit.New(os.Stdout)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	a, err := app.Build(ctx, cfg, app.Options{Version: version, VerifyCredentials: verify, Log: log})
	if err != nil {
		return err
	}
	defer a.Close()

	srv := &http.Server{
		Addr:              cfg.Server.Listen,
		Handler:           a.Handler,
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Info("discord-mcp listening", "version", version, "listen", cfg.Server.Listen, "verify_credentials", verify)
	for _, line := range a.Summary() {
		log.Info(line)
	}

	errCh := make(chan error, 1)
	go func() {
		err := srv.ListenAndServe()
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		errCh <- err
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		log.Info("shutting down")
		// Wake wait_for_message calls first so Shutdown can drain them.
		a.Close()
		sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(sctx)
	}
}
