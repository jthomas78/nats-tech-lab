package application

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/domain"
)

type DriftCatalog interface {
	Curated(context.Context) (domain.Document, error)
	Sources(context.Context) map[string]domain.Registration
}

type ManifestFetcher interface {
	Fetch(context.Context, string) ([]byte, error)
}

// These bounds are deployment policy, not operator-writeable row data. The
// zero value is the production schedule: two seconds per attempt, one retry
// after 200ms, and another pass one minute after the previous pass finished.
type DriftSchedule struct{ Timeout, RetryDelay, PollInterval time.Duration }

type driftObservation struct {
	entry  string
	result domain.Drift
}

type DriftChecker struct {
	catalog  DriftCatalog
	origins  domain.FetchOrigins
	fetch    ManifestFetcher
	schedule DriftSchedule
	mu       sync.RWMutex
	last     map[string]driftObservation
}

func NewDriftChecker(catalog DriftCatalog, origins domain.FetchOrigins, fetch ManifestFetcher, schedule DriftSchedule) *DriftChecker {
	if schedule.Timeout <= 0 {
		schedule.Timeout = 2 * time.Second
	}
	if schedule.RetryDelay <= 0 {
		schedule.RetryDelay = 200 * time.Millisecond
	}
	if schedule.PollInterval <= 0 {
		schedule.PollInterval = time.Minute
	}
	return &DriftChecker{catalog: catalog, origins: origins, fetch: fetch, schedule: schedule, last: map[string]driftObservation{}}
}

// Snapshot reads only memory. The entry must still be the one that was
// compared: an operator can curate while a fetch is in flight, and reporting
// agreement against the old copy would be a particularly convincing lie.
func (c *DriftChecker) Snapshot(entry domain.Entry, source string) domain.Drift {
	if target, result := c.origins.Target(entry, source); target == "" {
		return result
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	observation, ok := c.last[entry.ID]
	body, _ := json.Marshal(entry)
	if !ok || string(body) != observation.entry {
		return domain.UncheckedDrift("awaiting-check")
	}
	result := observation.result
	result.Fields = append([]string(nil), result.Fields...)
	return result
}

// Run belongs to the service lifetime, never a browser request. One pass at
// a time also bounds concurrency: a slow origin costs its own two attempts,
// not an ever-growing queue of overlapping polls.
func (c *DriftChecker) Run(ctx context.Context) {
	if c.origins.Empty() {
		<-ctx.Done()
		return
	}
	for ctx.Err() == nil {
		c.poll(ctx)
		if !driftWait(ctx, c.schedule.PollInterval) {
			return
		}
	}
}

func (c *DriftChecker) poll(ctx context.Context) {
	readCtx, cancel := context.WithTimeout(ctx, c.schedule.Timeout)
	doc, err := c.catalog.Curated(readCtx)
	var sources map[string]domain.Registration
	if err == nil {
		sources = c.catalog.Sources(readCtx)
	}
	cancel()
	if err != nil {
		c.mu.Lock()
		for id, previous := range c.last {
			previous.result = domain.UncheckedDrift("catalog-unavailable")
			previous.result.AttemptedAt = time.Now().UTC()
			c.last[id] = previous
		}
		c.mu.Unlock()
		return
	}
	seen := map[string]bool{}
	for _, entry := range doc.Entries {
		if ctx.Err() != nil {
			return
		}
		seen[entry.ID] = true
		target, result := c.origins.Target(entry, sources[entry.ID].Source)
		if target != "" {
			for attempt := 0; attempt < 2; attempt++ {
				fetchCtx, stop := context.WithTimeout(ctx, c.schedule.Timeout)
				body, err := c.fetch.Fetch(fetchCtx, target)
				if err == nil {
					err = fetchCtx.Err()
				}
				stop()
				if err != nil {
					result = domain.FailedDrift(err)
				} else {
					result = domain.CompareManifest(entry, body)
				}
				result.AttemptedAt = time.Now().UTC()
				// Publish a failure immediately, including during backoff.
				// Keeping the previous success here would briefly claim that
				// an origin we just failed to read still agrees.
				c.remember(entry, result)
				if result.State != domain.DriftNotChecked || attempt == 1 || !driftWait(ctx, c.schedule.RetryDelay) {
					break
				}
			}
		} else {
			c.remember(entry, result)
		}
	}
	c.mu.Lock()
	for id := range c.last {
		if !seen[id] {
			delete(c.last, id)
		}
	}
	c.mu.Unlock()
}

func (c *DriftChecker) remember(entry domain.Entry, result domain.Drift) {
	// Entries contain slices and signed bytes. Keep a snapshot, not an
	// alias that a caller could edit into matching a different manifest.
	body, _ := json.Marshal(entry)
	c.mu.Lock()
	c.last[entry.ID] = driftObservation{entry: string(body), result: result}
	c.mu.Unlock()
}

func driftWait(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
