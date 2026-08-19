// Package observability wires observability-service's dependencies —
// mirrors refdata-service's composition.go pattern (Startup builds
// dependencies, Mount registers HTTP routes), minus a Postgres connection:
// this service owns no datastore of its own, only NATS.
package observability

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/observability-service/observability/internal/rest"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/observability-service/observability/internal/tracestore"
)

// Handlers is the composition root Startup returns — cmd/main.go calls
// Mount on it once the HTTP mux exists, and Stop before shutdown.
type Handlers struct {
	rest       *rest.Handlers
	traceStore jetstream.ConsumeContext
}

// Config is Startup's non-NATS configuration — the HTTP-reachable
// dependencies Phase 30d's lifted panels need, all optional (each degrades
// independently: no NatsMonitorURL means Connections/AccountActivity 502,
// no NatsLogPath means the Log panel 503, no AccountsURL means every panel
// still works but with no tenantLabel).
type Config struct {
	NatsMonitorURL     string
	NatsLogPath        string
	AccountsURL        string
	AccountsAuthSecret string
}

// Startup wires this service's dependencies. nc is the single PLATFORM
// connection (BR-AC31/BR-AC32's per-tenant exports/imports resolve
// cross-account reach through it — no per-tenant connection of this
// service's own, unlike shipping-service's TenantResources fan-out Phase 30
// exists specifically to avoid). Phase 30g: also provisions the TRACES
// stream/KV bucket and starts the trace-store projector on the same
// connection — see tracestore.Register's doc comment for why that's safe
// without a second, broader connection the way shipping-service's original
// needed.
func Startup(ctx context.Context, nc *nats.Conn, log *slog.Logger, cfg Config) (*Handlers, error) {
	js, err := jetstream.New(nc)
	if err != nil {
		return nil, err
	}
	traceStore, err := tracestore.Register(ctx, js, nc, log)
	if err != nil {
		return nil, err
	}
	// BR-043 — the Overview tab's trend charts need real history, not just
	// /accstatz's live snapshot. Tied to ctx like traceStore's consumer:
	// Run exits on its own once the process's shutdown signal cancels ctx,
	// so there's nothing separate for Handlers.Stop to drain here.
	history := rest.NewAccstatzHistory(cfg.NatsMonitorURL, log)
	go history.Run(ctx, 10*time.Second)

	return &Handlers{
		rest: rest.New(rest.Deps{
			NC:             nc,
			Log:            log,
			NatsMonitorURL: cfg.NatsMonitorURL,
			NatsLogPath:    cfg.NatsLogPath,
			Accounts: &rest.AccountsClient{
				BaseURL:    cfg.AccountsURL,
				AuthSecret: cfg.AccountsAuthSecret,
				Log:        log,
			},
			History: history,
		}),
		traceStore: traceStore,
	}, nil
}

// Mount registers every route this service currently serves.
func (h *Handlers) Mount(mux *http.ServeMux) {
	h.rest.Mount(mux)
}

// Stop drains the trace-store projector's consumer. Nil-safe (Register
// returns a nil ConsumeContext if nc/js were nil).
func (h *Handlers) Stop() {
	if h.traceStore != nil {
		h.traceStore.Stop()
	}
}
