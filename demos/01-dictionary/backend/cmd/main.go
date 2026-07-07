// Command main bootstraps the dictionary demo monolith: it connects the
// shared infrastructure (NATS JetStream, Postgres, HTTP mux) and calls
// Startup on each module.
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

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/dictionary"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/internal/httpx"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/internal/monolith"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/internal/waiter"
)

type app struct {
	db     *sql.DB
	js     jetstream.JetStream
	mux    *http.ServeMux
	logger *slog.Logger
}

func (a *app) DB() *sql.DB             { return a.db }
func (a *app) JS() jetstream.JetStream { return a.js }
func (a *app) Mux() *http.ServeMux     { return a.mux }
func (a *app) Logger() *slog.Logger    { return a.logger }

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

	startupCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	// NATS
	var nc *nats.Conn
	err := waiter.Wait(startupCtx, "nats", log, func(context.Context) error {
		var err error
		nc, err = nats.Connect(natsURL)
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

	// Postgres
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := waiter.Wait(startupCtx, "postgres", log, db.PingContext); err != nil {
		return err
	}

	a := &app{db: db, js: js, mux: http.NewServeMux(), logger: log}

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
