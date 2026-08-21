// Package organizations wires the organizations module: Organization
// registration/lifecycle (BR-TP01-BR-TP06), compliance documents
// (BR-TP07-BR-TP11), and Transporter fleet assets (BR-TP12-BR-TP14) —
// backed by Postgres in its own "organization" schema, plus a
// tenant-scoped rpc.* client (internal/tenants + internal/refdataclient)
// used only for BR-TP14's vehicleTypeCode validation against
// refdata-service, and an internal/browserrpc adapter per tenant connection
// serving the same operations over api.* (Phase 26g's micro registration +
// Phase 26h's endpoints). Phase 33.5 retired the REST half of that dual
// transport — api.*/rpc.* is now the only way this service's business
// operations move; REST serves infra health only (internal/rest).
package organizations

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"go.temporal.io/sdk/client"
	temporalworker "go.temporal.io/sdk/worker"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/internal/application/commands"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/internal/browserrpc"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/internal/domain"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/internal/filetickets"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/internal/postgres"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/internal/rest"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/internal/tenants"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/transporterprofile/activities"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/transporterprofile/orchestration"
	profilepostgres "github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/transporterprofile/postgres"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/transporterprofile/worker"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/transporterprofile/workflow"
)

// Handlers is the composed set of command handlers a caller (REST layer,
// tests) drives.
type Handlers struct {
	Organizations  *commands.OrganizationHandler
	Documents      *commands.ComplianceDocumentHandler
	FleetAssets    *commands.FleetAssetHandler
	OperatingAreas *commands.OperatingAreaHandler
	TrackingCreds  *commands.TrackingCredentialHandler
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

	partners := postgres.NewOrganizationRepository(db)
	docs := postgres.NewComplianceDocumentRepository(db)
	fleet := postgres.NewFleetAssetRepository(db)
	areas := postgres.NewOperatingAreaRepository(db)
	trackingCreds := postgres.NewTrackingCredentialRepository(db)
	audit := postgres.NewAuditLog(db)

	return &Handlers{
		Organizations:  commands.NewOrganizationHandler(partners, audit),
		Documents:      commands.NewComplianceDocumentHandler(partners, docs),
		FleetAssets:    commands.NewFleetAssetHandler(partners, fleet, tenantMgr),
		OperatingAreas: commands.NewOperatingAreaHandler(partners, areas, tenantMgr, audit),
		TrackingCreds:  commands.NewTrackingCredentialHandler(partners, trackingCreds, tenantMgr, tenantMgr),
		DocumentFiles:  commands.NewDocumentFileHandler(docs, filetickets.NewStore(filetickets.DefaultTTL), tenantMgr),
		audit:          audit,
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
// vetting may be nil — the service then still serves every other api.*
// endpoint, and submit-for-vetting answers with ErrVettingUnavailable rather
// than accepting a command nothing would run.
func (h *Handlers) MountAPI(tenantMgr *tenants.Manager, vetting *Vetting, log *slog.Logger) error {
	var factory browserrpc.VettingFactory
	if vetting != nil {
		// BR-TP28's monitor needs the command handlers, which do not exist
		// until Startup has run — the same late-binding MountAPI already does
		// for the adapter's own deps.
		vetting.monitor.handlers = h
		factory = vetting.factory(h)
	}
	return tenantMgr.MountAPI(browserrpc.Deps{
		VettingFactory: factory,
		Organizations:  h.Organizations,
		Documents:      h.Documents,
		DocumentFiles:  h.DocumentFiles,
		FleetAssets:    h.FleetAssets,
		OperatingAreas: h.OperatingAreas,
		TrackingCreds:  h.TrackingCreds,
		Audit:          h.audit,
		Log:            log,
	})
}

// MountTenants starts this service's tenant-scoped NATS connections: one per
// known tenant, discovered from credsDir, each carrying BR-TP14's
// refdataclient plus a browserrpc micro registration. Callers should Close()
// the returned Manager on shutdown.
// secretKey seals BR-TP52's tracking-credential payloads. Passing nil
// disables that feature rather than degrading it — see tenants.Manager.
func MountTenants(ctx context.Context, natsURL, credsDir string, db *sql.DB, log *slog.Logger, secretKey []byte) (*tenants.Manager, error) {
	mgr := tenants.NewManager(natsURL, credsDir, db, log, secretKey)
	if err := mgr.EnsureAll(ctx); err != nil {
		return nil, err
	}
	return mgr, nil
}

// Vetting is the running Temporal worker plus the client it polls through.
// Close stops both, in that order, on shutdown.
type Vetting struct {
	worker   temporalworker.Worker
	client   client.Client
	monitor  *gitMonitor
	profiles *profilepostgres.Projection
	timeouts workflow.ActivityTimeouts
}

// factory builds one tenant's VettingGateway. The Temporal client is shared —
// it is a connection, not tenant state — while the ProfileHandler is the
// tenant's own, since BR-TP26's resubmit appends to that tenant's stream.
//
// The profile projection is deliberately shared too, and that is worth being
// explicit about: organizations.transporter_profiles is keyed on
// organization_id alone, with no tenant column, so one reader serves every
// tenant. Profile isolation lives at the NATS/event layer, not in this table
// — pre-existing 38a behaviour, noted here because the sharing looks like a
// wiring shortcut and is not one.
func (v *Vetting) factory(h *Handlers) browserrpc.VettingFactory {
	return func(_ string, commands *orchestration.ProfileHandler) browserrpc.VettingGateway {
		return worker.NewVettingService(v.client, commands, v.timeouts).
			WithSubmit(h.Organizations, v.profiles, h.Documents).
			WithSignaler(v.client)
	}
}

func (v *Vetting) Close() {
	if v == nil {
		return
	}
	if v.worker != nil {
		v.worker.Stop()
	}
	if v.client != nil {
		v.client.Close()
	}
}

// MountVetting starts BR-TP58's single Temporal worker: one process polling
// the one constant task queue, with activities that resolve their tenant per
// invocation from tenantMgr rather than binding a connection at construction.
//
// This is Phase 38b's missing composition. 38b built and tested the workflow,
// activities and worker packages, but nothing ever constructed them — the
// service opened no Temporal connection and the task queue had no pollers, so
// no transporter profile could leave AwaitingDocumentation, and BR-TP19's
// activation gate made Active and Suspended unreachable with it.
//
// gitOutcome selects activities.MockGitVerifier's behaviour: there is no real
// GIT/insurance system in this lab, and the mock is what keeps BR-TP22's
// compensation branch reachable by hand. An empty value means "pass".
func MountVetting(temporalAddr, gitOutcome string, db *sql.DB, tenantMgr *tenants.Manager, log *slog.Logger) (*Vetting, error) {
	temporalClient, err := client.Dial(client.Options{HostPort: temporalAddr, Logger: log})
	if err != nil {
		return nil, fmt.Errorf("dial temporal at %q: %w", temporalAddr, err)
	}

	outcome := activities.MockGitOutcome(gitOutcome)
	if outcome == "" {
		outcome = activities.GitPass
	}

	// HandleGitStatusDrop is deliberately left unconfigured: BR-TP28's monitor
	// handler is bound to one (tenant, context) pair at construction and needs
	// a resolver of its own before it can share this worker, and nothing can
	// schedule the monitor until BR-TP56's submit path exists to reach Vetted
	// in the first place. Unconfigured, the activity fails closed with an
	// explicit error rather than silently doing nothing.
	monitor := &gitMonitor{
		tenants: tenantMgr, client: temporalClient,
		timeouts: worker.ProductionActivityTimeouts,
	}
	acts := activities.NewProfileActivities(tenantMgr, activities.MockGitVerifier{Outcome: outcome}).
		WithGitStatusDropCommand(monitor).
		WithGitStatusReader(monitor).
		WithCoverExpiryReader(monitor)

	w := worker.New(temporalClient, acts)
	if err := w.Start(); err != nil {
		temporalClient.Close()
		return nil, fmt.Errorf("start vetting worker: %w", err)
	}
	// D6: the polling Schedules 38h-ii replaced are deleted here rather than
	// left to fire into a workflow type this worker no longer registers.
	// Logged, never fatal — a service that cannot reach the schedule API
	// should still come up and vet, and the stale schedules fail loudly on
	// their own.
	if deleted, err := worker.DeleteGitMonitorSchedules(context.Background(), temporalClient); err != nil {
		log.Warn("could not clear retired GIT monitor schedules; they may keep firing into an unregistered workflow", "err", err)
	} else if deleted > 0 {
		log.Info("cleared retired GIT monitor schedules (38h-ii)", "count", deleted)
	}
	log.Info("vetting worker started", "taskQueue", workflow.TaskQueue, "temporal", temporalAddr, "gitOutcome", outcome)
	return &Vetting{
		worker: w, client: temporalClient, monitor: monitor,
		profiles: profilepostgres.NewProjection(db),
		timeouts: worker.ProductionActivityTimeouts,
	}, nil
}

// --- BR-TP28: the GIT monitor's runtime wiring -------------------------

// gitMonitor implements the three ports BR-TP28 needs but 38b never supplied
// an implementation for: resolving the per-tenant drop handler, reading
// whether cover is currently active, and creating the Temporal schedule.
type gitMonitor struct {
	handlers *Handlers
	tenants  *tenants.Manager
	client   client.Client
	timeouts workflow.ActivityTimeouts
}

// GitStatusDrop resolves the drop handler for one (tenant, context) pair.
// Both axes matter: the handler appends to the tenant's own stream, under a
// specific context (BR-TP58).
func (g *gitMonitor) GitStatusDrop(tenant, contextKey string) (activities.GitStatusDropCommand, error) {
	store, err := g.tenants.ProfileStore(tenant)
	if err != nil {
		return nil, err
	}
	return orchestration.NewGitStatusDropHandler(contextKey, store, g.handlers.Organizations), nil
}

// IsGitActive answers BR-TP28's "has cover dropped" question using BR-TP38's
// existing derivation rather than a second, divergent definition of the same
// fact — the same call the profile endpoint makes. Derived from the documents
// on every read, never stored, so the monitor cannot act on a stale flag.
func (g *gitMonitor) IsGitActive(ctx context.Context, organizationID string) (bool, error) {
	docs, err := g.handlers.Documents.ListDocuments(ctx, organizationID)
	if err != nil {
		return false, err
	}
	return domain.DeriveGitStatus(docs, time.Now().UTC()) == domain.GitStatusActive, nil
}

// CoverExpiry is BR-TP60's timer input: the earliest expiry across the
// transporter's current goods-in-transit documents, or nil when none of them
// carries one. Read from the documents on every arming rather than cached, so
// a re-arm after BR-TP61's signal cannot act on a stale date.
func (g *gitMonitor) CoverExpiry(ctx context.Context, organizationID string) (*int64, error) {
	docs, err := g.handlers.Documents.ListDocuments(ctx, organizationID)
	if err != nil {
		return nil, err
	}
	var earliest *int64
	for _, doc := range docs {
		if doc.Type != domain.DocumentTypeGoodsInTransit || doc.ExpiresAt == nil {
			continue
		}
		if earliest == nil || *doc.ExpiresAt < *earliest {
			at := *doc.ExpiresAt
			earliest = &at
		}
	}
	return earliest, nil
}
