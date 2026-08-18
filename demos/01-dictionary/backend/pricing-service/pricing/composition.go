// Package pricing wires the pricing module: FeeScale, RateSheet, and
// FixedRate, each with a draft/published/rolled-back version lifecycle
// (BR-P01–BR-P15), backed by Postgres in its own "pricing" schema, plus (as
// of Phase 25f) an api.* frontend-to-service adapter for the Sea Freight
// Flow browser (Phase 25e's resolution: the browser talks to pricing-service
// directly, not via shipping-service) — see internal/browserrpc and
// shared/natstenants for the per-tenant NATS connection model this requires.
package pricing

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"

	"github.com/nats-io/nats.go"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/pricing-service/pricing/internal/application/commands"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/pricing-service/pricing/internal/browserrpc"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/pricing-service/pricing/internal/postgres"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/pricing-service/pricing/internal/rest"
	"github.com/jthomas78/nats-tech-lab/shared/natstenants"
)

// Handlers is the composed set of command handlers a caller (REST layer,
// tests) drives.
type Handlers struct {
	FeeScales  *commands.FeeScaleHandler
	RateSheets *commands.RateSheetHandler
	FixedRates *commands.FixedRateHandler
}

// Startup runs the schema migration and wires the Postgres-backed handlers.
func Startup(ctx context.Context, db *sql.DB) (*Handlers, error) {
	if err := postgres.Migrate(ctx, db); err != nil {
		return nil, err
	}

	feeScales := postgres.NewFeeScaleRepository(db)
	rateSheets := postgres.NewRateSheetRepository(db)
	fixedRates := postgres.NewFixedRateRepository(db)

	return &Handlers{
		FeeScales:  commands.NewFeeScaleHandler(feeScales),
		RateSheets: commands.NewRateSheetHandler(rateSheets),
		FixedRates: commands.NewFixedRateHandler(fixedRates),
	}, nil
}

// Mount wires the REST layer's infra-only routes (/healthz) onto mux.
// Business operations are reachable only over api.*/rpc.* — see
// internal/rest's package doc and BUSINESS_RULES-PRICING.md's BR-P26.
func (h *Handlers) Mount(mux *http.ServeMux) {
	rest.Mount(mux)
}

// MountAPI starts the api.* frontend-to-service adapter: one browserrpc.Adapter
// per tenant NATS connection, discovered from credsDir and kept reactively in
// sync with accounts-service's tenant lifecycle (Phase 35: shared/natstenants).
// Every tenant's Adapter shares these exact same command handlers — pricing
// data is scoped by `context`, not by NATS account, so nothing here is
// per-tenant except the connection itself. Callers should Close() the
// returned Manager on shutdown.
func (h *Handlers) MountAPI(ctx context.Context, natsURL, credsDir string, log *slog.Logger) (*natstenants.Manager[*browserrpc.Adapter], error) {
	deps := browserrpc.Deps{
		FeeScales:  h.FeeScales,
		RateSheets: h.RateSheets,
		FixedRates: h.FixedRates,
		Log:        log,
	}
	mgr := natstenants.NewManager(natsURL, credsDir, "pricing-service", log,
		func(_ context.Context, nc *nats.Conn, tenant string) (*browserrpc.Adapter, error) {
			scoped := deps
			scoped.Tenant = tenant
			return browserrpc.New(nc, scoped)
		},
		func(_ string, adapter *browserrpc.Adapter) error {
			return adapter.Stop()
		},
	)
	if err := mgr.EnsureAll(ctx); err != nil {
		return nil, err
	}
	return mgr, nil
}
