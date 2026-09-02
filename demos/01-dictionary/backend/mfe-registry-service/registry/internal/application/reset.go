package application

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/domain"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/notify"
	"github.com/jthomas78/nats-tech-lab/shared/mferegistry"
)

/*
	The registry's half of the catalogue-reset notice (BR-AS73).

	Start-up announcement is the primary path and covers every ordinary case.
	This covers the one it cannot: the catalogue lost while the plugins keep
	running, so nothing will ever announce again on its own.

	Two things are worth being explicit about, because both are easy to get
	wrong in a way nothing reports.

	FIRST, this must run BEFORE anything reads the catalogue through the
	service. An ordinary read repairs a cold or stale cache on the way past,
	which would erase the very witness the predicate needs. The witness is
	the read cache: it is written through from the same call that commits
	Postgres and therefore never leads it, so a cache ahead of the source of
	truth means the source of truth went backwards.

	SECOND, the cache is reset once the loss is concluded. Not as tidying:
	BR-AS51 keeps the cache monotonic so an outage never serves withdrawn
	code, and after a genuine loss the held higher revision is a memory of
	entries that no longer exist. Leaving it would serve them during the next
	outage, and would also make this predicate fire again on every restart
	from then on.
*/

// CacheResetter is the cache write that is allowed to go backwards. Separate
// from the Cache interface a read path holds, so the capability is not in
// reach of any code that only caches.
type CacheResetter interface {
	Reset(ctx context.Context, doc domain.Document) error
}

// AnnounceCatalogueReset concludes whether the catalogue was lost and, if it
// was, states that once. It returns whether a notice was published.
//
// It states a fact and issues no command: each publisher decides for itself
// whether to re-announce, and a publisher that does nothing is simply not
// re-announced (BR-AS54, decision 9). Nothing here can withdraw an entry, and
// the code to do so is not reachable from this file.
func (s *Service) AnnounceCatalogueReset(ctx context.Context, reason string, now time.Time) (bool, error) {
	if s.cache == nil {
		// With no cache there is no witness, and a deployment that runs
		// without one has no way to tell a restart from a loss. That is a
		// supported deployment, so it gets silence rather than a guess.
		return false, nil
	}

	cached, ok, err := s.cache.Get(ctx)
	if err != nil || !ok {
		if err != nil {
			s.logWarn("registry: could not read the reset witness, assuming no catalogue loss", err)
		}
		return false, nil
	}

	current, err := s.store.Current(ctx)
	if err != nil {
		// An unreadable source of truth is an outage, not a loss. Concluding
		// a reset here would fire a fleet-wide re-announce every time
		// Postgres was briefly down — a storm caused by the backstop.
		s.logWarn("registry: source of truth unreadable at startup, not concluding a catalogue reset", err)
		return false, nil
	}

	if !domain.CatalogueLost(cached.Document.Revision, current.Revision) {
		return false, nil
	}

	s.log.Warn("registry: the catalogue went backwards; stating a reset so live publishers re-announce",
		"witnessed", cached.Document.Revision, "current", current.Revision, "reason", reason)

	if resetter, canReset := s.cache.(CacheResetter); canReset {
		if resetErr := resetter.Reset(ctx, current); resetErr != nil {
			s.logWarn("registry: catalogue reset concluded but the read cache still holds the lost revision", resetErr)
		}
	}

	payload, err := json.Marshal(mferegistry.ResetNotice{
		JitterMillis: mferegistry.ResetJitterDefault.Milliseconds(),
		Reason:       reason,
		At:           now.UnixMilli(),
	})
	if err != nil {
		return false, err
	}
	s.notifier.Publish(ctx, notify.EntriesReset(), payload)
	return true, nil
}
