// Command main bootstraps the refdata-service: Postgres connection, schema
// migration, seed data, and the HTTP API for the dictionary-as-a-service
// domain (Phase 11).
//
// @title           Reference Data Service API
// @version         1.0
// @description     Dictionary-as-a-service backend for the NATS Tech Lab POC. Plain Postgres CRUD reference data (currencies, countries, Incoterms, units of measure, hazard classes) with typed cross-references.
// @host            localhost:7201
// @BasePath        /
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
	"github.com/nats-io/nats.go/jetstream"

	_ "github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/docs"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata"
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

	databaseURL := envOr("DATABASE_URL", "postgres://refdata:refdata@localhost:5433/refdata?sslmode=disable")
	natsURL := envOr("NATS_URL", nats.DefaultURL)
	httpAddr := envOr("HTTP_ADDR", ":8080")
	anthropicAPIKey := envOr("ANTHROPIC_API_KEY", "")

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

	var js jetstream.JetStream
	var nc *nats.Conn
	if err := waitForNATS(startupCtx, natsURL, func(conn *nats.Conn) error {
		nc = conn
		var err error
		js, err = jetstream.New(nc)
		return err
	}); err != nil {
		return err
	}
	defer nc.Drain() //nolint:errcheck

	h, err := refdata.Startup(ctx, db, js, anthropicAPIKey)
	if err != nil {
		return err
	}
	if anthropicAPIKey == "" {
		log.Info("ANTHROPIC_API_KEY not set — AI-assisted translation drafting (Phase 11.12) is disabled")
	}

	rpcAdapter, err := h.MountRPC(nc, js, log)
	if err != nil {
		return err
	}
	defer rpcAdapter.Stop() //nolint:errcheck

	mux := http.NewServeMux()
	h.Mount(mux, log)

	server := &http.Server{Addr: httpAddr, Handler: mux}
	errCh := make(chan error, 1)
	go func() {
		log.Info("refdata-service: http server listening", "addr", httpAddr)
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

func waitForNATS(ctx context.Context, url string, connect func(*nats.Conn) error) error {
	for {
		conn, err := nats.Connect(url, nats.Name("refdata-service"))
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
