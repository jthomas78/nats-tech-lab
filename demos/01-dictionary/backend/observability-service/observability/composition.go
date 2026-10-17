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

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/observability-service/observability/internal/pubsubstore"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/observability-service/observability/internal/rest"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/observability-service/observability/internal/servicelist"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/observability-service/observability/internal/tracequery"
)

// Handlers is the composition root Startup returns — cmd/main.go calls
// Mount on it once the HTTP mux exists, and Stop before shutdown.
type Handlers struct {
	rest        *rest.Handlers
	pubsubStore jetstream.ConsumeContext
	// stopTraceQuery stops the Phase 54 trace query gateway. Nil when
	// TempoURL is unset (the default stack has no Tempo).
	stopTraceQuery func() error
	// stopServiceList stops the Phase 54b live service list (BR-072). It is
	// mounted on every stack, Tempo or not: discovery does not read traces.
	stopServiceList func() error
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
	// TempoURL is Tempo's HTTP API, set by compose.runtime.yaml (Phase 54,
	// decision 9). Empty means the trace query gateway is not mounted, so
	// its subjects get no responders.
	TempoURL string
}

// Startup wires this service's dependencies. nc is the single PLATFORM
// connection (BR-AC31/BR-AC32's per-tenant exports/imports resolve
// cross-account reach through it — no per-tenant connection of this
// service's own, unlike shipping-service's TenantResources fan-out Phase 30
// exists specifically to avoid). It also starts the pubsub-store projector
// on the same connection. The TRACES trace store that Phase 30g added here
// was retired in Phase 54d; Tempo holds traces now (BR-068).
func Startup(ctx context.Context, nc *nats.Conn, log *slog.Logger, cfg Config) (*Handlers, error) {
	js, err := jetstream.New(nc)
	if err != nil {
		return nil, err
	}
	// Phase 43b (BR-047): the pub/sub store, on its own PUBSUB stream and
	// pubsub-messages bucket.
	pubsubStore, err := pubsubstore.Register(ctx, js, nc, log)
	if err != nil {
		return nil, err
	}
	// BR-043 — the Overview tab's trend charts need real history, not just
	// /accstatz's live snapshot. Tied to ctx:
	// Run exits on its own once the process's shutdown signal cancels ctx,
	// so there's nothing separate for Handlers.Stop to drain here.
	history := rest.NewAccstatzHistory(cfg.NatsMonitorURL, log)
	go history.Run(ctx, 10*time.Second)

	// Phase 54 (BR-067, BR-070): the trace query gateway, on this same
	// PLATFORM connection and nowhere else.
	var stopTraceQuery func() error
	if cfg.TempoURL != "" {
		backend := tracequery.NewTempo(cfg.TempoURL, &http.Client{Timeout: tracequery.QueryTimeout})
		stopTraceQuery, err = tracequery.Mount(nc, backend)
		if err != nil {
			pubsubStore.Stop()
			return nil, err
		}
		log.Info("trace query gateway mounted", "subjects", tracequery.Subjects())
	}

	accounts := &rest.AccountsClient{
		BaseURL:    cfg.AccountsURL,
		AuthSecret: cfg.AccountsAuthSecret,
		Log:        log,
	}

	// Phase 54b (BR-072): the live service list, on the PLATFORM connection.
	stopServiceList, err := servicelist.Mount(nc, accounts, servicelist.Options{})
	if err != nil {
		if stopTraceQuery != nil {
			_ = stopTraceQuery()
		}
		pubsubStore.Stop()
		return nil, err
	}
	log.Info("service list mounted", "subject", servicelist.Subject)

	return &Handlers{
		rest: rest.New(rest.Deps{
			NC:             nc,
			Log:            log,
			NatsMonitorURL: cfg.NatsMonitorURL,
			NatsLogPath:    cfg.NatsLogPath,
			Accounts:       accounts,
			History:        history,
		}),
		pubsubStore:     pubsubStore,
		stopTraceQuery:  stopTraceQuery,
		stopServiceList: stopServiceList,
	}, nil
}

// Mount registers every route this service currently serves.
func (h *Handlers) Mount(mux *http.ServeMux) {
	h.rest.Mount(mux)
}

// Stop drains the pubsub-store projector's consumer and the NATS gateways.
// Nil-safe (Register returns a nil ConsumeContext if nc/js were nil).
func (h *Handlers) Stop() {
	if h.stopServiceList != nil {
		_ = h.stopServiceList()
	}
	if h.stopTraceQuery != nil {
		_ = h.stopTraceQuery()
	}
	if h.pubsubStore != nil {
		h.pubsubStore.Stop()
	}
}
