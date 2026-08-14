// Package tradingpartner wires the trading-partner module: TradingPartner
// registration/lifecycle (BR-TP01-BR-TP06), compliance documents
// (BR-TP07-BR-TP11), and Transporter fleet assets (BR-TP12-BR-TP14) —
// backed by Postgres in its own "trading_partner" schema, plus a
// tenant-scoped rpc.* client (internal/tenants + internal/refdataclient)
// used only for BR-TP14's vehicleTypeCode validation against
// refdata-service, and an internal/browserrpc adapter per tenant connection
// serving the same operations over api.* (Phase 26g's micro registration +
// Phase 26h's endpoints). REST and api.* are a dual transport, as
// pricing-service does — neither replaces the other.
package tradingpartner

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/trading-partner-service/tradingpartner/internal/application/commands"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/trading-partner-service/tradingpartner/internal/browserrpc"
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
	audit           *postgres.AuditLog
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
		audit:           audit,
	}, nil
}

// Mount wires the REST layer's routes onto mux.
func (h *Handlers) Mount(mux *http.ServeMux, log *slog.Logger) {
	rest.NewHandlers(rest.Deps{
		TradingPartners: h.TradingPartners,
		Documents:       h.Documents,
		FleetAssets:     h.FleetAssets,
		Audit:           h.audit,
		Log:             log,
	}).Mount(mux)
}

// MountAPI registers the api.* adapter on every tenant connection tenantMgr
// holds. Must run after Startup, and Startup must run after MountTenants:
// Startup needs the Manager for BR-TP14's validator, while the adapter needs
// the handlers Startup builds (see tenants.Manager's doc comment). Until this
// runs, the service answers over REST only.
func (h *Handlers) MountAPI(tenantMgr *tenants.Manager, log *slog.Logger) error {
	return tenantMgr.MountAPI(browserrpc.Deps{
		TradingPartners: h.TradingPartners,
		Documents:       h.Documents,
		FleetAssets:     h.FleetAssets,
		Audit:           h.audit,
		Log:             log,
	})
}

// ProtectedHandler wraps mux with the BasicAuth gate every route in this
// service requires — mirrors accounts-service's own gate (operator-admin
// work, shared-secret placeholder until WorkOS-backed human auth lands).
// Exported here (rather than internal/rest) so cmd/main.go, which sits
// outside this package's internal/ visibility, can wrap the mux it builds
// from Mount.
func ProtectedHandler(mux *http.ServeMux, secret string) http.Handler {
	return rest.BasicAuth(secret, mux)
}

// MountTenants starts this service's tenant-scoped NATS connections: one per
// known tenant, discovered from credsDir, each carrying BR-TP14's
// refdataclient plus a browserrpc micro registration. Callers should Close()
// the returned Manager on shutdown.
func MountTenants(ctx context.Context, natsURL, credsDir string, log *slog.Logger) (*tenants.Manager, error) {
	mgr := tenants.NewManager(natsURL, credsDir, log)
	if err := mgr.EnsureAll(ctx); err != nil {
		return nil, err
	}
	return mgr, nil
}
