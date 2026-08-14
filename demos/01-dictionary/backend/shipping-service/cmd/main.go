// Command main bootstraps the dictionary demo monolith: it connects the
// shared infrastructure (NATS JetStream, Postgres, HTTP mux) and calls
// Startup on each module.
//
// @title           EventSourcing CQRS POC — Shipping API
// @version         1.0
// @description     Shipping domain backend for the NATS Tech Lab POC. Demonstrates JetStream event sourcing, NATS KV projections (Shape A / Shape B), and pure event reconstruction (Shape C).
// @host            localhost:7200
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
	"path/filepath"
	"syscall"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	_ "github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/docs"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/internal/httpx"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/internal/monolith"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/internal/waiter"
)

type app struct {
	db             *sql.DB
	js             jetstream.JetStream
	nc             *nats.Conn
	platformFullJS jetstream.JetStream
	natsURL        string
	credsDir       string
	natsMonitorURL string
	natsLogPath    string
	mux            *http.ServeMux
	logger         *slog.Logger
}

func (a *app) DB() *sql.DB                         { return a.db }
func (a *app) JS() jetstream.JetStream             { return a.js }
func (a *app) NC() *nats.Conn                      { return a.nc }
func (a *app) PlatformFullJS() jetstream.JetStream { return a.platformFullJS }
func (a *app) NatsURL() string                     { return a.natsURL }
func (a *app) CredsDir() string                    { return a.credsDir }
func (a *app) NatsMonitorURL() string              { return a.natsMonitorURL }
func (a *app) NatsLogPath() string                 { return a.natsLogPath }
func (a *app) Mux() *http.ServeMux                 { return a.mux }
func (a *app) Logger() *slog.Logger                { return a.logger }

var _ monolith.Monolith = (*app)(nil)

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
	databaseURL := envOr("DATABASE_URL", "postgres://dict:dict@localhost:5432/dictionary?sslmode=disable")
	httpAddr := envOr("HTTP_ADDR", ":8080")
	// Phase 14a — operator mode: the directory rest.Handlers.SwitchTenant
	// scans for <tenant>.creds files. Empty when running locally outside
	// Docker without operator mode configured (PLATFORM connect below then
	// falls back to no credentials, matching today's local-dev behavior).
	credsDir := envOr("NATS_CREDS_DIR", "")
	adminCredsPath := envOr("NATS_ADMIN_CREDS_PATH", "")
	if adminCredsPath == "" && credsDir != "" {
		adminCredsPath = filepath.Join(credsDir, "shipping-admin.creds")
	}
	// PlatformFullJS's credential — see monolith.Monolith.PlatformFullJS'
	// doc comment for why this is deliberately NOT adminCredsPath.
	platformFullCredsPath := envOr("NATS_PLATFORM_CREDS_PATH", "")
	if platformFullCredsPath == "" && credsDir != "" {
		platformFullCredsPath = filepath.Join(credsDir, "platform.creds")
	}
	// Phase 17c — Connections panel proxies the NATS server's HTTP
	// monitoring port (distinct from natsURL's client port 4222).
	natsMonitorURL := envOr("NATS_MONITOR_URL", "http://localhost:8222")
	// Admin UI Log panel — path to NATS's log_file, mounted read-only from
	// the same volume NATS writes into (see docker-compose.yml). Empty
	// outside Docker; the Log panel's endpoint reports unavailable rather
	// than erroring.
	natsLogPath := envOr("NATS_LOG_PATH", "")

	startupCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	// NATS — permanent, restricted shipping-admin PLATFORM connection
	// (monolith.Monolith.NC/JS). Tenant-scoped connections are separate and
	// opened by rest.Handlers.SwitchTenant.
	platformOpts := []nats.Option{nats.Name("shipping-service")}
	if adminCredsPath != "" {
		platformOpts = append(platformOpts, nats.UserCredentials(adminCredsPath))
	}
	var nc *nats.Conn
	err := waiter.Wait(startupCtx, "nats", log, func(context.Context) error {
		var err error
		nc, err = nats.Connect(natsURL, platformOpts...)
		return err
	})
	if err != nil {
		return err
	}
	defer nc.Drain() //nolint:errcheck
	js, err := jetstream.New(nc)
	if err != nil {
		return err
	}

	// Second, unrestricted PLATFORM connection (platform.creds) — see
	// monolith.Monolith.PlatformFullJS' doc comment. Non-fatal if it can't be
	// established: this only degrades cross-account KV bucket introspection
	// (listKVBuckets skips PLATFORM), not Startup itself.
	var platformFullJS jetstream.JetStream
	if platformFullCredsPath != "" {
		pnc, err := nats.Connect(natsURL, nats.Name("shipping-service-platform-full"), nats.UserCredentials(platformFullCredsPath))
		if err != nil {
			log.Error("connect platform-full NATS; cross-account KV introspection will skip PLATFORM", "err", err)
		} else {
			defer pnc.Drain() //nolint:errcheck
			if platformFullJS, err = jetstream.New(pnc); err != nil {
				log.Error("jetstream context for platform-full connection; cross-account KV introspection will skip PLATFORM", "err", err)
				platformFullJS = nil
			}
		}
	}

	// Postgres
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := waiter.Wait(startupCtx, "postgres", log, db.PingContext); err != nil {
		return err
	}

	a := &app{db: db, js: js, nc: nc, platformFullJS: platformFullJS, natsURL: natsURL, credsDir: credsDir, natsMonitorURL: natsMonitorURL, natsLogPath: natsLogPath, mux: http.NewServeMux(), logger: log}

	modules := []monolith.Module{dictionary.Module{}}
	for _, m := range modules {
		if err := m.Startup(ctx, a); err != nil {
			return err
		}
	}

	server := &http.Server{Addr: httpAddr, Handler: httpx.CORS(a.mux)}
	errCh := make(chan error, 1)
	go func() {
		log.Info("http server listening", "addr", httpAddr)
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

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
