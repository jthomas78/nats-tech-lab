// Package application composes the registry's store, read cache and change
// notification behind the two calls decision 35 named: Read and Apply.
package application

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/domain"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/notify"
	"github.com/jthomas78/nats-tech-lab/shared/natsnotify"
)

// Store is the source of truth this service reads and writes through.
type Store interface {
	Current(ctx context.Context) (domain.Document, error)
	Apply(ctx context.Context, w domain.Write) (domain.Document, error)
	Publishers(ctx context.Context) (domain.PublisherDocument, error)
	ApplyPublisher(ctx context.Context, w domain.PublisherWrite) (domain.PublisherDocument, error)
	Sources(ctx context.Context) (map[string]domain.Registration, error)
}

// Cache is the read cache. Optional: a nil Cache means every read goes to
// Postgres, which is a supported deployment, not an error.
type Cache interface {
	Get(ctx context.Context) (domain.Cached, bool, error)
	Put(ctx context.Context, doc domain.Document) error
}

// Service is the registry module's whole behaviour.
type Service struct {
	store     Store
	cache     Cache
	allowlist domain.Allowlist
	notifier  *natsnotify.Notifier
	log       *slog.Logger
	// Set by WithDrift, and optional: see curated.go.
	drift DriftSnapshotter
}

func New(store Store, cache Cache, allowlist domain.Allowlist, notifier *natsnotify.Notifier, log *slog.Logger) *Service {
	return &Service{store: store, cache: cache, allowlist: allowlist, notifier: notifier, log: log}
}

// Curated returns the stored document unfiltered — what the admin surface
// needs in order to fix a disabled or non-conforming entry.
func (s *Service) Curated(ctx context.Context) (domain.Document, error) {
	return s.store.Current(ctx)
}

// Sources says how each entry got here, for the operator surface only
// (decision 80). Never folded into Curated: it is one more query, it is
// wanted by exactly one screen, and the shell must not be given a reason to
// care which tier a plugin arrived through.
//
// A read failure is not fatal to the screen. The panel shows every entry with
// an unknown source rather than no entries at all — the badge is context, and
// losing it is not a reason to lose the catalogue.
func (s *Service) Sources(ctx context.Context) map[string]domain.Registration {
	out, err := s.store.Sources(ctx)
	if err != nil {
		s.logWarn("registry: entry sources could not be read; the panel will say unknown", err)
		return map[string]domain.Registration{}
	}
	return out
}

// Read returns the document as a shell may see it.
//
// Postgres first, KV on failure, degraded when neither answers (BR-AS22).
// That order is the opposite of a hot-path cache and is deliberate: this
// document is read once per shell boot, so correctness is worth more than
// the microseconds, and the cache earns its keep as the thing that keeps the
// shell booting through a Postgres outage.
func (s *Service) Read(ctx context.Context) domain.Document {
	doc, err := s.store.Current(ctx)
	if err == nil {
		if s.cache != nil {
			// Repair a cold or stale cache on the way past, so the fallback
			// is warm before the outage that needs it.
			if putErr := s.cache.Put(ctx, doc); putErr != nil {
				s.logWarn("registry: could not refresh the read cache", putErr)
			}
		}
		return doc.Readable(s.allowlist)
	}
	s.logWarn("registry: source of truth is unreadable, trying the cache", err)

	if s.cache != nil {
		cached, ok, cacheErr := s.cache.Get(ctx)
		if cacheErr != nil {
			s.logWarn("registry: read cache is unreadable", cacheErr)
		} else if ok {
			/* Served, and labelled. Decision 105 judged monotonic writes
			   insufficient on their own: this copy may predate a revocation,
			   and handing it over unmarked would present stale trust as
			   current. The reader gets the catalogue, its revision and its
			   age, and decides for itself. */
			doc := cached.Document
			doc.Degraded = true
			doc.AsOf = cached.StoredAt
			return doc.Readable(s.allowlist)
		}
	}
	return domain.Degraded()
}

// Apply performs one curated write, refreshes the cache and announces the
// change. The notify goes last and can never fail the write: a dropped
// notification is recovered by the shell's unconditional read on reconnect.
func (s *Service) Apply(ctx context.Context, w domain.Write) (domain.Document, error) {
	doc, err := s.store.Apply(ctx, w)
	if err != nil {
		return domain.Document{}, err
	}
	/*
		Past this line the write is durable, so the two steps that follow run
		on a context that outlives the request (decision 49). They are not
		optional work — the cache is what keeps shells booting through a
		Postgres outage, and the notification is what makes BR-AS19's live
		change live. Leaving them on the request context meant a client that
		hung up between the commit and here left the KV copy stale and every
		watching shell unaware of a revision that had already happened.
	*/
	after := context.WithoutCancel(ctx)
	if s.cache != nil {
		if putErr := s.cache.Put(after, doc); putErr != nil {
			s.logWarn("registry: write committed but the read cache was not refreshed", putErr)
		}
	}
	payload, _ := json.Marshal(struct {
		Revision int64 `json:"revision"`
	}{doc.Revision})
	s.notifier.Publish(after, notify.Changed(), payload)
	return doc, nil
}

// Publishers returns the trust table (BR-AS38).
//
// No cache and no change notification, unlike the plugin document: the trust
// table is read by the operator surface and by the announce path, never by a
// shell, so there is nothing to keep warm and nobody to tell.
func (s *Service) Publishers(ctx context.Context) (domain.PublisherDocument, error) {
	return s.store.Publishers(ctx)
}

// ApplyPublisher performs one curated change to the trust table.
//
// Most trust writes move the trust table's own revision and leave the plugin
// document's alone: adding a key has not changed the catalogue, and bumping
// the other counter would make every shell re-read for nothing.
//
// A revocation is the exception. The store withholds the entries that key
// signed in the same transaction (BR-AS38, decision 104), so the plugin
// document has moved and the shells have to be told — once, for the whole
// revocation, not once per entry.
func (s *Service) ApplyPublisher(ctx context.Context, w domain.PublisherWrite) (domain.PublisherDocument, error) {
	before := int64(0)
	revoking := w.Op == domain.OpPublisherSetKeyState && w.KeyState == domain.KeyRevoked
	if revoking {
		if doc, err := s.store.Current(ctx); err == nil {
			before = doc.Revision
		}
	}
	trust, err := s.store.ApplyPublisher(ctx, w)
	if err != nil {
		return domain.PublisherDocument{}, err
	}
	if revoking {
		s.announceWithholding(ctx, before)
	}
	return trust, nil
}

// announceWithholding refreshes the cache and hints the shells, but only if
// the revocation actually withheld something — a revocation that touched no
// entry does not move the plugin revision, and there is nothing to tell.
//
// Past this point the revocation is durable, so the work runs on a context
// that outlives the request, for the reasons Apply gives.
func (s *Service) announceWithholding(ctx context.Context, before int64) {
	after := context.WithoutCancel(ctx)
	doc, err := s.store.Current(after)
	if err != nil {
		s.logWarn("registry: keys revoked but the plugin document could not be re-read", err)
		return
	}
	if doc.Revision == before {
		return
	}
	if s.cache != nil {
		if putErr := s.cache.Put(after, doc); putErr != nil {
			s.logWarn("registry: entries withheld but the read cache was not refreshed", putErr)
		}
	}
	payload, _ := json.Marshal(struct {
		Revision int64 `json:"revision"`
	}{doc.Revision})
	s.notifier.Publish(after, notify.Changed(), payload)
}

// Allowlist is the configured origin set, for the admin surface to show an
// operator why an entry was refused without having to guess at the config.
func (s *Service) Allowlist() domain.Allowlist { return s.allowlist }

func (s *Service) logWarn(msg string, err error) {
	if s.log != nil {
		s.log.Warn(msg, "error", err)
	}
}
