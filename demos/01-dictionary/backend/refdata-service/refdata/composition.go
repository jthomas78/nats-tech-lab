// Package refdata wires the dictionary-as-a-service domain: type registry,
// items, typed references, and the Q5 versioned-read KV cache protocol,
// backed by Postgres in its own "refdata" schema and (for the cache/change
// feed only — never as a source of truth) NATS. No dependency on the
// shipping backend's stream, subjects, or tables (Phase 11,
// Dictionary-Service-Plan.md).
package refdata

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata/internal/anthropic"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata/internal/application/commands"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata/internal/browserrpc"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata/internal/domain"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata/internal/kvcache"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata/internal/kvstore"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata/internal/natsrpc"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata/internal/notifybridge"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata/internal/postgres"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata/internal/rest"
	"github.com/jthomas78/nats-tech-lab/shared/jstream"
	"github.com/jthomas78/nats-tech-lab/shared/natsnotify"
	"github.com/jthomas78/nats-tech-lab/shared/natstenants"
)

// ChangeStreamMaxAge bounds the REFDATA change-event feed — it is a
// notification channel, not an event store (Q6), so retention is a
// deliberate, explicit choice rather than the shipping backend's unbounded
// SHIPPING stream.
const ChangeStreamMaxAge = 48 * time.Hour

// RPCTraceStreamName/RPCTraceMaxAge (the RPCTRACE stream backing the
// obs.rpc.* side-channel, BR-D26/BR-D29) were retired in Phase 28g: nothing
// has published to obs.rpc.* since Phase 28b replaced natsrpc.Adapter's
// publishObs call with a natstrace span (see natsrpc/adapter.go's
// ObsSubjectWildcard doc comment), so the stream this const pair
// provisioned had stood permanently empty. The Admin UI's [messages] tab
// now derives from obs.trace.*/the traces KV bucket instead (BR-026's
// Phase 28g amendment, BUSINESS_RULES-SHIPPING.md; corresponding
// BUSINESS_RULES-REFDATA.md amendments on BR-D29/BR-D36).

// KVBucketPrefix names the versioned-read cache buckets: refdata-{context}.
const KVBucketPrefix = "refdata"

// Handlers is the composed set of command handlers a caller (REST layer,
// tests) drives.
type Handlers struct {
	Types         *commands.TypeHandler
	Items         *commands.ItemHandler
	References    *commands.ReferenceHandler
	Regions       *commands.RegionHandler
	Localizations *commands.LocalizationHandler
	KV            *kvstore.Store
	Projector     *kvcache.Projector
	Versions      domain.VersionRepository
	Contexts      *commands.ContextHandler
	Corpus        *commands.CorpusHandler
	VersionReader *kvcache.VersionReader
	Translations  *commands.TranslationHandler
}

// Startup runs the schema migration, seeds reference-standard data, wires
// the Postgres-backed handlers, and — when js is non-nil — the KV cache and
// REFDATA change-event stream (Phase 11.3). js may be nil for tests that
// only exercise the domain/Postgres layers; production always supplies it.
// anthropicAPIKey wires the AI-assisted translation drafter (BR-D07, Phase
// 11.12) when non-empty; Handlers.Translations stays nil otherwise, and the
// REST layer reports the feature as unconfigured rather than failing.
//
// nc is the connection js was built from, and is used for one thing: opting
// the evt.* seam into BR-045/BR-D45's publish-side observation at
// construction (Phase 43e). It must be the tenant's own connection, since
// that is what places the obs.pubsub.* envelope inside the tenant's account.
// Nil nc means no observation, which is what the JetStream-less test wiring
// already passes.
func Startup(ctx context.Context, db *sql.DB, nc *nats.Conn, js jetstream.JetStream, anthropicAPIKey string) (*Handlers, error) {
	if err := postgres.Migrate(ctx, db); err != nil {
		return nil, err
	}

	types := postgres.NewTypeRepository(db)
	items := postgres.NewItemRepository(db)
	refs := postgres.NewReferenceRepository(db)
	locs := postgres.NewLocalizationRepository(db)
	locales := postgres.NewLocaleRepository(db)
	versions := postgres.NewVersionRepository(db)
	contexts := postgres.NewContextRepository(db)
	corpus := postgres.NewCorpusRepository(db)

	var kv *kvstore.Store
	var projector *kvcache.Projector
	var notifier domain.ChangeNotifier // stays a true nil interface when js is nil
	var corpusNotifier domain.CorpusNotifier
	var versionReader *kvcache.VersionReader
	if js != nil {
		if _, err := jstream.CreateStream(ctx, js, kvcache.ChangeStreamName, []string{kvcache.ChangeSubjectWildcard}, jstream.WithMaxAge(ChangeStreamMaxAge)); err != nil {
			return nil, err
		}
		kv = kvstore.New(js, KVBucketPrefix)
		namespaces := kvcache.NewTypeNamespaces(types)
		// Phase 43a (BR-D45): every evt.{context}.refdata.{typeKey}.changed
		// publish also emits an obs.pubsub.* observation, which PLATFORM
		// imports under monitor.{tenant}.pubsub.> for the Messages panel.
		projector = kvcache.NewProjector(kv, items, locs, refs, versions, namespaces,
			jstream.NewPublisher(js, jstream.WithObservation(nc)))
		notifier = projector
		corpusNotifier = kvcache.NewVersionNotifier(kv, corpus, namespaces)
		versionReader = kvcache.NewVersionReader(kv, namespaces)
	}

	var translations *commands.TranslationHandler
	if anthropicAPIKey != "" {
		translations = commands.NewTranslationHandler(items, locs, locales, anthropic.New(anthropicAPIKey))
	}

	h := &Handlers{
		Types:         commands.NewTypeHandler(types),
		Items:         commands.NewItemHandler(items, refs, notifier),
		References:    commands.NewReferenceHandler(items, refs, notifier),
		Regions:       commands.NewRegionHandler(items, refs, notifier),
		Localizations: commands.NewLocalizationHandler(items, locs, locales, notifier),
		KV:            kv,
		Projector:     projector,
		Versions:      versions,
		Contexts:      commands.NewContextHandler(contexts),
		Corpus:        commands.NewCorpusHandler(corpus, corpusNotifier),
		VersionReader: versionReader,
		Translations:  translations,
	}

	if err := Seed(ctx, h); err != nil {
		return nil, err
	}

	return h, nil
}

// Mount wires the REST layer (routes + Swagger UI) onto mux.
func (h *Handlers) Mount(mux *http.ServeMux, log *slog.Logger) {
	rest.NewHandlers(rest.Deps{
		Types:         h.Types,
		Items:         h.Items,
		References:    h.References,
		Regions:       h.Regions,
		Localizations: h.Localizations,
		KV:            h.KV,
		Projector:     h.Projector,
		Versions:      h.Versions,
		Contexts:      h.Contexts,
		Corpus:        h.Corpus,
		VersionReader: h.VersionReader,
		Translations:  h.Translations,
		Log:           log,
	}).Mount(mux)
}

// MountRPC starts the rpc.* dual-transport adapter (Phase 12.10, extended
// 12.11) — a second transport onto the same command handlers Mount's REST
// routes call. js is accepted for signature symmetry with Startup (and
// because callers already hold it for the REFDATA/RPCTRACE streams above)
// but is no longer forwarded into natsrpc.Deps: Phase 28b replaced the
// natsrpc adapter's publishObs/RPCTRACE side-channel with natstrace's
// obs.trace.* spans (BR-D39), which publish over the adapter's own
// connection and need no JetStream handle. Callers should Stop() the
// returned adapter on shutdown.
func (h *Handlers) MountRPC(nc *nats.Conn, js jetstream.JetStream, log *slog.Logger) (*natsrpc.Adapter, error) {
	return natsrpc.New(nc, natsrpc.Deps{
		Localizations: h.Localizations,
		Items:         h.Items,
		VersionReader: h.VersionReader,
		Projector:     h.Projector,
		Contexts:      h.Contexts,
		References:    h.References,
		Log:           log,
	})
}

// browserrpcDeps builds the browserrpc.Deps shared by every tenant
// connection's Adapter (Tenant is overwritten per-connection by
// natstenants.Manager) — the exact same command handlers Mount's REST routes
// and MountRPC's rpc.* adapter already call (BR-D40/BR-D41).
func (h *Handlers) browserrpcDeps(log *slog.Logger) browserrpc.Deps {
	return browserrpc.Deps{
		Types:         h.Types,
		Items:         h.Items,
		References:    h.References,
		Localizations: h.Localizations,
		Contexts:      h.Contexts,
		Corpus:        h.Corpus,
		VersionReader: h.VersionReader,
		Projector:     h.Projector,
		Versions:      h.Versions,
		Translations:  h.Translations,
		Log:           log,
	}
}

// MountAPI starts refdata-service's per-tenant api.* surface (Phase 32,
// BR-D40) — one browserrpc.Adapter per tenant discovered in credsDir,
// kept in sync with accounts-service's notify.accounts.account.* lifecycle
// events via shared/natstenants (Phase 35). This is additive to MountRPC's
// single PLATFORM-connection rpc.* adapter, not a replacement for it.
// Callers should Close() the returned Manager on shutdown.
// platformJS is passed so MountAPI can also start the BR-D42 notify bridge —
// see internal/notifybridge for why the fan-out has to cross from the
// PLATFORM connection's evt.* feed onto the per-tenant connections.
func (h *Handlers) MountAPI(ctx context.Context, natsURL, credsDir string, platformJS jetstream.JetStream, log *slog.Logger) (*natstenants.Manager[*browserrpc.Adapter], error) {
	deps := h.browserrpcDeps(log)
	mgr := natstenants.NewManager(natsURL, credsDir, "refdata-service", log,
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
	notifybridge.Run(ctx, platformJS, tenantPublisher{mgr: mgr, log: log}, log)
	return mgr, nil
}

// tenantPublisher adapts natstenants.Manager.Range to notifybridge.Publisher
// (BR-D42's fan-out leg) — a change notification is a best-effort hint to
// refetch, not a delivery guarantee, so a failed publish on one tenant is
// logged and skipped rather than propagated.
type tenantPublisher struct {
	mgr *natstenants.Manager[*browserrpc.Adapter]
	log *slog.Logger
}

func (p tenantPublisher) PublishToAll(ctx context.Context, subj natsnotify.Subject, data []byte) {
	p.mgr.Range(func(tenant string, nc *nats.Conn, _ *browserrpc.Adapter) {
		p.publishTo(ctx, tenant, nc, subj, data)
	})
}

// publishTo is one tenant's leg of the fan-out, split out from the Range
// callback so it can be exercised directly against a real connection — the
// Manager the fan-out iterates needs a creds directory and live tenant
// accounts, which would make a test of the observation a test of tenant
// discovery instead.
func (p tenantPublisher) publishTo(ctx context.Context, tenant string, nc *nats.Conn, subj natsnotify.Subject, data []byte) {
	// A Notifier per tenant, per publish. It is a connection plus a gate, and
	// which connection is the whole point here (BR-D45): emitting on this
	// tenant's own connection is what puts the observation inside that
	// tenant's account, so PLATFORM's import remap names the right tenant
	// (BR-AC34). A single Notifier shared across the fan-out could not be
	// attributed at all — which is why Notifier holds its connection rather
	// than taking one per call, and why this is the one site in the repo that
	// builds one per message rather than at construction.
	natsnotify.New(nc, p.log, natsnotify.WithObservation(nc)).Publish(ctx, subj, data)
}

// MountPlatformAPI additionally registers the api.* adapter on
// refdata-service's own PLATFORM connection (the same nc MountRPC's rpc.*
// adapter already runs on) — not a per-tenant connection at all. This is
// what lets the cross-tenant refdata-admin credential
// (accounts-service/auth's MintRefdataAdminToken) reach both the business
// and admin api.* subjects: frontend/refdata is a platform-operator tool
// with no tenant/account concept of its own (unlike Sea Freight Flow), so
// its browser connects to the SAME account this service's rpc.* adapter
// already lives on, rather than any one tenant's account.
//
// deps.Tenant is set to "_platform" purely as micro registration metadata
// (Admin UI Services-panel label) — it is unrelated to, and must not be
// confused with, the {context} routing token a caller's subject carries
// (BR-D40/BR-D41): this one physical adapter still serves every {context}
// value, tenant-owned or not, exactly like every other PLATFORM-mounted
// capability in this service (internal/natsrpc's own ContextListSubject
// uses the identical "_platform" literal for the same non-per-context
// reason).
//
// Callers should Stop() the returned Adapter on shutdown.
func (h *Handlers) MountPlatformAPI(nc *nats.Conn, log *slog.Logger) (*browserrpc.Adapter, error) {
	deps := h.browserrpcDeps(log)
	deps.Tenant = "_platform"
	return browserrpc.New(nc, deps)
}
