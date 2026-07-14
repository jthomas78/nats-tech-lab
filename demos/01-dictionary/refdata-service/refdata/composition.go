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

	"github.com/nats-io/nats.go/jetstream"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/refdata-service/refdata/internal/application/commands"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/refdata-service/refdata/internal/domain"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/refdata-service/refdata/internal/jstream"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/refdata-service/refdata/internal/kvcache"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/refdata-service/refdata/internal/kvstore"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/refdata-service/refdata/internal/postgres"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/refdata-service/refdata/internal/rest"
)

// ChangeStreamMaxAge bounds the REFDATA change-event feed — it is a
// notification channel, not an event store (Q6), so retention is a
// deliberate, explicit choice rather than the shipping backend's unbounded
// SHIPPING stream.
const ChangeStreamMaxAge = 48 * time.Hour

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
}

// Startup runs the schema migration, seeds reference-standard data, wires
// the Postgres-backed handlers, and — when js is non-nil — the KV cache and
// REFDATA change-event stream (Phase 11.3). js may be nil for tests that
// only exercise the domain/Postgres layers; production always supplies it.
func Startup(ctx context.Context, db *sql.DB, js jetstream.JetStream) (*Handlers, error) {
	if err := postgres.Migrate(ctx, db); err != nil {
		return nil, err
	}

	types := postgres.NewTypeRepository(db)
	items := postgres.NewItemRepository(db)
	refs := postgres.NewReferenceRepository(db)
	locs := postgres.NewLocalizationRepository(db)
	locales := postgres.NewLocaleRepository(db)
	versions := postgres.NewVersionRepository(db)

	var kv *kvstore.Store
	var projector *kvcache.Projector
	var notifier domain.ChangeNotifier // stays a true nil interface when js is nil
	if js != nil {
		if _, err := jstream.CreateChangeStream(ctx, js, kvcache.ChangeStreamName, []string{kvcache.ChangeSubjectWildcard}, ChangeStreamMaxAge); err != nil {
			return nil, err
		}
		kv = kvstore.New(js, KVBucketPrefix)
		projector = kvcache.NewProjector(kv, items, locs, refs, versions, jstream.NewPublisher(js))
		notifier = projector
	}

	h := &Handlers{
		Types:         commands.NewTypeHandler(types),
		Items:         commands.NewItemHandler(items, refs, notifier),
		References:    commands.NewReferenceHandler(items, refs, notifier),
		Localizations: commands.NewLocalizationHandler(items, locs, locales, notifier),
		KV:            kv,
		Projector:     projector,
		Versions:      versions,
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
		Log:           log,
	}).Mount(mux)
}
