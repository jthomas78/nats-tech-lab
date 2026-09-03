// Command main bootstraps pricing-service: Postgres connection, schema
// migration, the HTTP API for the FeeScale/RateSheet/FixedRate domain, and
// (Phase 25f) the api.* frontend-to-service adapter — one NATS connection
// per known tenant, discovered from NATS_CREDS_DIR — for the Sea Freight
// Flow browser. See pricing/composition.go's doc comment.
package main

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/nats-io/nats.go"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/pricing-service/pricing"
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

	databaseURL := envOr("DATABASE_URL", "postgres://pricing:pricing@localhost:5432/pricing?sslmode=disable")
	httpAddr := envOr("HTTP_ADDR", ":8080")
	natsURL := envOr("NATS_URL", nats.DefaultURL)
	credsDir := envOr("NATS_CREDS_DIR", "")

	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return err
	}
	defer db.Close()

	startupCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	if err := waitForPostgres(startupCtx, db); err != nil {
		return err
	}

	h, err := pricing.Startup(ctx, db)
	if err != nil {
		return err
	}

	// Phase 25f: one api.* adapter per known tenant, so a Sea Freight Flow
	// browser authenticated into any tenant's account can reach
	// pricing-service immediately — see shared/natstenants.
	tenantMgr, err := h.MountAPI(ctx, natsURL, credsDir, log)
	if err != nil {
		return err
	}
	defer tenantMgr.Close()

	mux := http.NewServeMux()
	h.Mount(mux)

	server := &http.Server{Addr: httpAddr, Handler: mux}
	errCh := make(chan error, 1)
	go func() {
		log.Info("pricing-service: http server listening", "addr", httpAddr)
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

func waitForPostgres(ctx context.Context, db *sql.DB) error {
	for {
		if err := db.PingContext(ctx); err == nil {
			return nil
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
