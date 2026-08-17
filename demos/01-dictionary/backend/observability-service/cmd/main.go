// Command main bootstraps observability-service (Phase 30c) — the
// PLATFORM-account home for the cross-account NATS/JetStream diagnostics
// (Phase 30d/30e/30f) and trace store (30g) extracted from shipping-service
// (Main-POC-Plan.md's Phase 30 Goal: none of it is shipping domain logic,
// and shipping-service only ever held it because it was the one service
// with the cross-account reach to answer it).
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/observability-service/observability"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	if err := run(log); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	natsURL := envOr("NATS_URL", nats.DefaultURL)
	// PLATFORM only — this service never holds a per-tenant connection.
	// Cross-account reach comes from BR-AC31/BR-AC32's per-tenant service
	// imports, resolved transparently through this one connection once the
	// account-level exports/imports are in place.
	natsCredsPath := envOr("NATS_CREDS_PATH", "")
	httpAddr := envOr("HTTP_ADDR", ":8080")
	cfg := observability.Config{
		// Phase 17c — the Connections/AccountActivity panels proxy the NATS
		// server's HTTP monitoring port (distinct from NATS_URL's client
		// port 4222).
		NatsMonitorURL: envOr("NATS_MONITOR_URL", ""),
		// The Admin UI Log panel — NATS's own log_file (nats.conf), mounted
		// read-only. Empty outside Docker, same as shipping-service's
		// original NatsLogPath.
		NatsLogPath: envOr("NATS_LOG_PATH", ""),
		// Phase 30d — resolves tenantLabel via accounts-service's own
		// GET /api/accounts instead of shipping-service's original
		// LocalAddr-matching trick (accounts_client.go's doc comment).
		AccountsURL:        envOr("ACCOUNTS_URL", ""),
		AccountsAuthSecret: envOr("ACCOUNTS_AUTH_SECRET", ""),
	}

	startupCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	var nc *nats.Conn
	if err := waitForNATS(startupCtx, natsURL, natsCredsPath, func(conn *nats.Conn) error {
		nc = conn
		return nil
	}); err != nil {
		return err
	}
	defer nc.Drain() //nolint:errcheck

	h, err := observability.Startup(ctx, nc, log, cfg)
	if err != nil {
		return err
	}
	defer h.Stop()

	mux := http.NewServeMux()
	h.Mount(mux)

	server := &http.Server{Addr: httpAddr, Handler: mux}
	errCh := make(chan error, 1)
	go func() {
		log.Info("observability-service: http server listening", "addr", httpAddr)
		errCh <- server.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		log.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil && !errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		return nil
	}
}

func waitForNATS(ctx context.Context, url, credsPath string, connect func(*nats.Conn) error) error {
	opts := []nats.Option{nats.Name("observability-service")}
	if credsPath != "" {
		opts = append(opts, nats.UserCredentials(credsPath))
	}
	for {
		conn, err := nats.Connect(url, opts...)
		if err == nil {
			return connect(conn)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
