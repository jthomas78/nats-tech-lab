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
	"github.com/jthomas78/nats-tech-lab/shared/natsconn"
	"github.com/jthomas78/nats-tech-lab/shared/natsready"
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
	// Phase 14a — operator mode: refdata-service is cross-tenant (BR-D08),
	// so it always connects as PLATFORM, never a per-tenant account. Empty
	// when running locally outside Docker without operator mode configured.
	natsCredsPath := envOr("NATS_CREDS_PATH", "")
	// Phase 32 (BR-D40): per-tenant connections for the new api.* surface,
	// additive to the single PLATFORM connection above. Empty when running
	// locally outside Docker without operator mode configured — MountAPI then
	// simply finds no tenants to connect.
	natsCredsDir := envOr("NATS_CREDS_DIR", "")
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
	if err := waitForNATS(startupCtx, natsURL, natsCredsPath, log, func(conn *nats.Conn) error {
		nc = conn
		var err error
		js, err = jetstream.New(nc)
		return err
	}); err != nil {
		return err
	}
	defer nc.Drain() //nolint:errcheck

	h, err := refdata.Startup(ctx, db, nc, js, anthropicAPIKey)
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

	// Phase 5d (BR-AS62): answer whether this service is READY, which is not
	// the same question as whether it is connected. The check pings Postgres
	// on every ask, because a service whose database has gone holds its NATS
	// connection open and would otherwise look perfectly alive to anything
	// watching the bus.
	readiness, err := natsready.Mount(nc, "refdata-service", db.PingContext, log)
	if err != nil {
		return err
	}
	defer readiness.Stop() //nolint:errcheck

	// Phase 32 (BR-D40): one api.* adapter per known tenant, additive to the
	// rpc.* adapter above — see shared/natstenants.
	tenantMgr, err := h.MountAPI(ctx, natsURL, natsCredsDir, js, log)
	if err != nil {
		return err
	}
	defer tenantMgr.Close()

	// Phase 32 (BR-D41 amendment): the api.* adapter also runs on this
	// connection's own PLATFORM account, alongside rpcAdapter above — this
	// is what the refdata admin UI's cross-tenant MintRefdataAdminToken
	// credential reaches (frontend/refdata has no tenant/account concept;
	// it is a platform-operator tool, not a Sea Freight Flow-style tenant
	// app — see composition.go's MountPlatformAPI doc comment).
	platformAPIAdapter, err := h.MountPlatformAPI(nc, log)
	if err != nil {
		return err
	}
	defer platformAPIAdapter.Stop() //nolint:errcheck

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

func waitForNATS(ctx context.Context, url, credsPath string, log *slog.Logger, connect func(*nats.Conn) error) error {
	opts := natsconn.Options("refdata-service", credsPath, log)
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
