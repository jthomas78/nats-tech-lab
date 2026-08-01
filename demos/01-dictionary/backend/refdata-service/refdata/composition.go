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
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata/internal/domain"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata/internal/jstream"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata/internal/kvcache"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata/internal/kvstore"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata/internal/natsrpc"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata/internal/postgres"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata/internal/rest"
)

// ChangeStreamMaxAge bounds the REFDATA change-event feed — it is a
// notification channel, not an event store (Q6), so retention is a
// deliberate, explicit choice rather than the shipping backend's unbounded
// SHIPPING stream.
const ChangeStreamMaxAge = 48 * time.Hour

// RPCTraceStreamName / RPCTraceMaxAge back the obs.rpc.* observability
// side-channel (BR-D26) with a short JetStream-backed replay window (BR-D29)
// so a reconnecting Admin UI tab can catch up on the last 10 minutes of
// rpc.* traffic — see ARCHITECTURE-COMMUNICATIONS.md §6.
const (
	RPCTraceStreamName = "RPCTRACE"
	RPCTraceMaxAge     = 10 * time.Minute
)

// KVBucketPrefix names the versioned-read cache buckets: refdata-{context}.
const KVBucketPrefix = "refdata"

// Handlers is the composed set of command handlers a caller (REST layer,
// tests) drives.
type Handlers struct {
	Types         *commands.TypeHandler
	Items         *commands.ItemHandler
	References    *commands.ReferenceHandler
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
func Startup(ctx context.Context, db *sql.DB, js jetstream.JetStream, anthropicAPIKey string) (*Handlers, error) {
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
		if _, err := jstream.CreateChangeStream(ctx, js, kvcache.ChangeStreamName, []string{kvcache.ChangeSubjectWildcard}, ChangeStreamMaxAge); err != nil {
			return nil, err
		}
		if _, err := jstream.CreateChangeStream(ctx, js, RPCTraceStreamName, []string{natsrpc.ObsSubjectWildcard}, RPCTraceMaxAge); err != nil {
			return nil, err
		}
		kv = kvstore.New(js, KVBucketPrefix)
		namespaces := kvcache.NewTypeNamespaces(types)
		projector = kvcache.NewProjector(kv, items, locs, refs, versions, namespaces, jstream.NewPublisher(js))
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
// routes call. js may be nil (mirrors Startup's own nil-safety) — publishObs
// then falls back to plain core-NATS publish with no RPCTRACE retention
// (BR-D29). Callers should Stop() the returned adapter on shutdown.
func (h *Handlers) MountRPC(nc *nats.Conn, js jetstream.JetStream, log *slog.Logger) (*natsrpc.Adapter, error) {
	return natsrpc.New(nc, natsrpc.Deps{
		Localizations: h.Localizations,
		Items:         h.Items,
		VersionReader: h.VersionReader,
		Projector:     h.Projector,
		Contexts:      h.Contexts,
		JS:            js,
		Log:           log,
	})
}
