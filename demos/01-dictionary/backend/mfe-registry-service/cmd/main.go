// Command main bootstraps mfe-registry-service: the curated micro-frontend
// registry — which micro-frontends the platform will let an application shell
// load, from which origins, at which revision.
//
// It was accounts-service's `registry` bounded context until the split. The
// move was a deployment change rather than an untangling because the context
// already shared no table and no foreign key with accounts (decision 39), and
// because Phase 4 had already retired the registry's HTTP surface: both
// frontends reach it by subject, and NATS resolves a subject to whichever
// process is listening. Neither the Admin UI nor the shell changed.
//
// What deliberately did NOT move is credential minting. accounts-service owns
// the NATS trust chain and mints the shell's and the operator's browser
// credentials, which means it must name this service's subjects in a grant —
// so the subject list lives in shared/mferegistry and both sides read one
// copy (BR-AS25/AS27).
//
// No HTTP API surface: the registry answers on api.* only. The listener below
// serves /healthz so compose has something to wait on, and nothing else —
// registry/internal/rest keeps an exhaustive empty mount so a reintroduced
// route is a test failure rather than a discovery.
package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry"
	"github.com/jthomas78/nats-tech-lab/shared/natsconn"
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

	databaseURL := envOr("DATABASE_URL", "postgres://mfe_registry:mfe_registry@localhost:5437/mfe_registry?sslmode=disable")
	natsURL := envOr("NATS_URL", nats.DefaultURL)
	// PLATFORM account: the registry's subjects are all _platform-context,
	// and core NATS never crosses an account boundary — a connection on any
	// other account would be registering endpoints no browser can reach.
	natsCredsPath := envOr("NATS_CREDS_PATH", "")
	httpAddr := envOr("HTTP_ADDR", ":8080")
	// The origins this platform will let a shell fetch plugin code from
	// (BR-AS20). Configuration, never a stored row: the allowlist is the
	// envelope the registry itself sits inside, so a compromised write path
	// must not be able to widen it. Empty permits nothing.
	allowlist := registry.ParseAllowedOrigins(os.Getenv("REGISTRY_ALLOWED_ORIGINS"))

	// Unlike accounts-service's optional platform connection, this one is the
	// service's whole reason to exist: without it there is no api.* surface
	// and a shell reading the registry gets no answer at all.
	//
	// Checked here rather than left to the connect loop below, because the
	// loop retries for a minute before giving up — and the first run after
	// the service split spent that minute silent, with a container marked Up
	// and nothing in its logs, for the mundane reason that nsc had not minted
	// mfe-registry-service.creds yet. A missing creds file will not become
	// present by waiting; say so immediately and by name.
	if natsCredsPath == "" {
		return errors.New("NATS_CREDS_PATH is required")
	}
	if _, err := os.Stat(natsCredsPath); err != nil {
		return fmt.Errorf("NATS credentials %s are unreadable — run nats/bootstrap-operator.sh: %w", natsCredsPath, err)
	}

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

	log.Info("mfe-registry-service: connecting", "nats", natsURL, "creds", natsCredsPath)
	var nc *nats.Conn
	opts := natsconn.Options("mfe-registry-service", natsCredsPath, log)
	if err := waitForNATS(startupCtx, natsURL, opts, func(conn *nats.Conn) error {
		nc = conn
		return nil
	}); err != nil {
		return err
	}
	defer nc.Drain() //nolint:errcheck

	// The KV read cache is optional and its absence is a warning, not a
	// failure: Postgres is the source of truth and a miss falls through to
	// it. The cache is what keeps a shell answerable through a Postgres
	// outage, which is a different guarantee from being able to start.
	var js jetstream.JetStream
	if j, err := jetstream.New(nc); err != nil {
		log.Warn("registry: no read cache", "error", err)
	} else {
		js = j
	}

	module, err := registry.Startup(startupCtx, db, js, nc, allowlist, log)
	if err != nil {
		return fmt.Errorf("registry startup: %w", err)
	}
	defer module.Stop() //nolint:errcheck
	log.Info("mfe-registry-service: curated frontend plugin registry mounted on PLATFORM api.*", "allowedOrigins", allowlist.Origins())

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	// Exhaustive and empty (BR-AS21/AS24): registry curation has no HTTP
	// surface, and this call is what makes a reintroduced route testable
	// rather than a thing someone notices in production.
	registry.MountHTTP(mux)

	server := &http.Server{Addr: httpAddr, Handler: mux}
	errCh := make(chan error, 1)
	go func() {
		log.Info("mfe-registry-service: http server listening", "addr", httpAddr)
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

func waitForNATS(ctx context.Context, url string, opts []nats.Option, connect func(*nats.Conn) error) error {
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
