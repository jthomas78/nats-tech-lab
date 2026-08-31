package domain

import "time"

// Cached is a cached document together with when that copy was stored.
//
// The pair is one value because they are only meaningful together: a reader
// told a document is stale needs to know how stale, and a timestamp with no
// document is nothing. In domain rather than in either adapter, so the store
// side and the read side agree on the shape without one importing the other.
type Cached struct {
	Document Document
	StoredAt time.Time
}

// SupersedesCached is the whole of the read cache's write rule (BR-AS51,
// decision 105): a lower revision never replaces a higher one.
//
// The rule lives here rather than in the storage adapter because it is a
// statement about trust, not about storage. The cache is what a shell reads
// when Postgres is unreachable, so a late or reordered write that put back a
// document from before a revocation would serve withdrawn code during exactly
// the outage the cache exists to survive. Bounding how stale the copy can get
// is the point; it is not an optimisation.
//
// Equal is accepted, so a service that restarts and re-puts what it already
// holds is not a conflict. DegradedRevision is 0 and so is refused against any
// real revision — the outage document is an answer to a reader, never
// something to remember.
func SupersedesCached(held, incoming int64) bool { return incoming >= held }
