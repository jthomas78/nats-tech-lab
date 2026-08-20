// Package tradingpartner wires the trading-partner module: TradingPartner
// registration/lifecycle (BR-TP01-BR-TP06), compliance documents
// (BR-TP07-BR-TP11), and Transporter fleet assets (BR-TP12-BR-TP14) —
// backed by Postgres in its own "trading_partner" schema, plus a
// tenant-scoped rpc.* client (internal/tenants + internal/refdataclient)
// used only for BR-TP14's vehicleTypeCode validation against
// refdata-service, and an internal/browserrpc adapter per tenant connection
// serving the same operations over api.* (Phase 26g's micro registration +
// Phase 26h's endpoints). Phase 33.5 retired the REST half of that dual
// transport — api.*/rpc.* is now the only way this service's business
// operations move; REST serves infra health only (internal/rest).
package tradingpartner

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/trading-partner-service/tradingpartner/internal/application/commands"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/trading-partner-service/tradingpartner/internal/browserrpc"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/trading-partner-service/tradingpartner/internal/filetickets"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/trading-partner-service/tradingpartner/internal/postgres"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/trading-partner-service/tradingpartner/internal/rest"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/trading-partner-service/tradingpartner/internal/tenants"
)

// Handlers is the composed set of command handlers a caller (REST layer,
// tests) drives.
type Handlers struct {
	TradingPartners *commands.TradingPartnerHandler
	Documents       *commands.ComplianceDocumentHandler
	FleetAssets     *commands.FleetAssetHandler
	// DocumentFiles is Phase 38c-ii's byte path: it serves both the api.*
	// ticket-minting endpoints and the HTTP ingress that spends those tickets,
	// which is deliberate — one handler owning both halves is what keeps a
	// ticket's grant the single source of truth about who may transfer what.
	DocumentFiles *commands.DocumentFileHandler
	audit         *postgres.AuditLog
}

// Startup runs the schema migration and wires the Postgres-backed handlers.
// tenantMgr supplies BR-TP14's refdata validator — see MountTenants.
func Startup(ctx context.Context, db *sql.DB, tenantMgr *tenants.Manager) (*Handlers, error) {
	if err := postgres.Migrate(ctx, db); err != nil {
		return nil, err
	}

	partners := postgres.NewTradingPartnerRepository(db)
	docs := postgres.NewComplianceDocumentRepository(db)
	fleet := postgres.NewFleetAssetRepository(db)
	audit := postgres.NewAuditLog(db)

	return &Handlers{
		TradingPartners: commands.NewTradingPartnerHandler(partners, audit),
		Documents:       commands.NewComplianceDocumentHandler(partners, docs),
		FleetAssets:     commands.NewFleetAssetHandler(partners, fleet, tenantMgr),
		DocumentFiles:   commands.NewDocumentFileHandler(docs, filetickets.NewStore(filetickets.DefaultTTL), tenantMgr),
		audit:           audit,
	}, nil
}

// Mount wires this service's HTTP surface onto mux: infra health, plus Phase
// 38c-ii's two compliance-document byte routes. Everything else still moves
// over api.*/rpc.* only — Phase 33.5's retirement of the business REST surface
// stands, and BR-TP17's allowlist test pins exactly what HTTP serves.
func (h *Handlers) Mount(mux *http.ServeMux, log *slog.Logger) {
	rest.Mount(mux, h.DocumentFiles, log)
}

// MountAPI registers the api.* adapter on every tenant connection tenantMgr
// holds. Must run after Startup, and Startup must run after MountTenants:
// Startup needs the Manager for BR-TP14's validator, while the adapter needs
// the handlers Startup builds (see tenants.Manager's doc comment). Until this
// runs, the service has no business transport at all — REST no longer
// carries a fallback (Phase 33.5).
func (h *Handlers) MountAPI(tenantMgr *tenants.Manager, log *slog.Logger) error {
	return tenantMgr.MountAPI(browserrpc.Deps{
		TradingPartners: h.TradingPartners,
		Documents:       h.Documents,
		DocumentFiles:   h.DocumentFiles,
		FleetAssets:     h.FleetAssets,
		Audit:           h.audit,
		Log:             log,
	})
}

// MountTenants starts this service's tenant-scoped NATS connections: one per
// known tenant, discovered from credsDir, each carrying BR-TP14's
// refdataclient plus a browserrpc micro registration. Callers should Close()
// the returned Manager on shutdown.
func MountTenants(ctx context.Context, natsURL, credsDir string, db *sql.DB, log *slog.Logger) (*tenants.Manager, error) {
	mgr := tenants.NewManager(natsURL, credsDir, db, log)
	if err := mgr.EnsureAll(ctx); err != nil {
		return nil, err
	}
	return mgr, nil
}
