package domain

import "errors"

var (
	// ErrUnsigned refuses an announcement carrying no signature, before any
	// verifier is consulted. An unsigned announcement is not a verification
	// failure, it is a malformed one, and the two read differently to a
	// publisher debugging an integration.
	ErrUnsigned = errors.New("registry: an announcement must be signed")

	// ErrUnverified refuses an announcement whose signature does not check
	// out under the key it named. The safe direction for a check that
	// decides which code a browser fetches is to fail closed, so a
	// deployment with no trust anchor at all refuses under this cause too
	// (see NoVerifier in verify.go).
	ErrUnverified = errors.New("registry: announcement signature could not be verified")
)

// AnnounceOutcome is what an announcement did. Five values rather than a
// bool because an operator reviewing the pending tier needs the difference,
// and a publisher who believes they registered needs to be told which of
// these happened to them.
type AnnounceOutcome string

const (
	// AnnounceInserted — an id the store had never seen, stored pending.
	AnnounceInserted AnnounceOutcome = "inserted"
	// AnnouncePending — a known id that is still awaiting review; the
	// record is refreshed and stays out of every shell's document.
	AnnouncePending AnnounceOutcome = "pending"
	// AnnounceUpdated — an enabled dynamic entry moving within its own
	// allowlisted origin, applied without review (BR-AS40, decision 74).
	AnnounceUpdated AnnounceOutcome = "updated"
	// AnnounceRequeued — an enabled dynamic entry whose remote crossed an
	// origin boundary; withheld until an operator re-enables it.
	AnnounceRequeued AnnounceOutcome = "requeued"
	// AnnounceIgnored — a static entry outranks the announcement, always
	// (decision 77). Recorded and shown, never silently dropped.
	AnnounceIgnored AnnounceOutcome = "ignored"
)

// Recorded reports whether the outcome leaves an audit row. Every one of
// them does — including the ignored case, which exists precisely so a
// publisher can be shown why nothing happened (decision 77, BR-AS23).
func (o AnnounceOutcome) Recorded() bool { return o != "" }

// DecideAnnounce is the whole of the announcement ruleset, as a pure
// function of what is stored and what arrived. It returns the entry the
// store should hold afterwards — which for AnnounceIgnored is the existing
// entry, unchanged.
//
// The order of the branches is the order of precedence, and it is
// load-bearing:
//
//  1. decision 77 — a static entry outranks an announcement, always. This
//     is checked first, so it beats BR-AS40's no-review update on an entry
//     that is both static and enabled, which is every preloaded entry.
//  2. BR-AS39 — an unknown id lands pending. Never enabled, whatever the
//     payload claims about itself.
//  3. BR-AS40 — an enabled *dynamic* entry follows its publisher within its
//     own origin, and re-queues when the remote crosses one. An enabled entry
//     with no recorded lifecycle is not dynamic and does not get this.
func DecideAnnounce(existing *Entry, incoming Entry) (AnnounceOutcome, Entry) {
	// A plugin never states its own trust tier or activation. ParseManifest
	// refuses a payload that tried; this is the second line, so a caller
	// that built an Entry some other way cannot get past it either
	// (BR-AS39, BR-AS43).
	incoming.Enabled = false
	incoming.Lifecycle = LifecycleDynamic

	if existing == nil {
		return AnnounceInserted, incoming
	}
	if existing.Lifecycle == LifecycleStatic {
		return AnnounceIgnored, *existing
	}
	if !existing.Enabled {
		return AnnouncePending, incoming
	}
	// An enabled entry whose withdrawal class was never recorded is not
	// dynamic — it is unclassified, and decision 74 grants the no-review
	// update to `dynamic` alone. Ignored rather than pending: pending would
	// disable an entry that is running fine, on nothing worse than a legacy
	// row missing a column. The operator classifies it, then it follows its
	// publisher. (Edge left open at the 8a/8b review, closed here because
	// this is the branch a real signature now flows through.)
	if existing.Lifecycle != LifecycleDynamic {
		return AnnounceIgnored, *existing
	}

	if sameOrigin(existing.Remote.URL, incoming.Remote.URL) {
		incoming.Enabled = true
		incoming.Lifecycle = existing.Lifecycle
		return AnnounceUpdated, incoming
	}
	return AnnounceRequeued, incoming
}

// sameOrigin compares scheme, host and port — the same unit the allowlist
// compares, because the boundary BR-AS40 defends is the boundary the
// allowlist already defends. A URL with no origin never matches, including
// against another URL with no origin.
func sameOrigin(a, b string) bool {
	left, okA := originOf(a)
	right, okB := originOf(b)
	return okA && okB && left == right
}

// AnnounceWrite builds the write an accepted announcement performs. Filed
// under the publisher key rather than a shared identity, so the audit trail
// answers "who put this here" without reference to any other source
// (BR-AS42). An empty key produces a write Validate refuses.
func AnnounceWrite(e Entry, publisherKey string, ifRevision int64) Write {
	return Write{Op: OpUpsert, EntryID: e.ID, Actor: publisherKey, Entry: &e, IfRevision: ifRevision}
}
