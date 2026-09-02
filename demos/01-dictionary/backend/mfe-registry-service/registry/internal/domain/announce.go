package domain

import (
	"errors"
	"reflect"

	"github.com/jthomas78/nats-tech-lab/shared/mferegistry"
)

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
type AnnounceOutcome = mferegistry.AnnounceOutcome

const (
	// AnnounceInserted — an id the store had never seen, stored pending.
	AnnounceInserted = mferegistry.AnnounceInserted
	// AnnouncePending — a known id that is still awaiting review; the
	// record is refreshed and stays out of every shell's document.
	AnnouncePending = mferegistry.AnnouncePending
	// AnnounceUpdated — an enabled dynamic entry moving within its own
	// allowlisted origin, applied without review (BR-AS40, decision 74).
	AnnounceUpdated = mferegistry.AnnounceUpdated
	// AnnounceRequeued — an enabled dynamic entry whose remote crossed an
	// origin boundary; withheld until an operator re-enables it.
	AnnounceRequeued = mferegistry.AnnounceRequeued
	// AnnounceIgnored — a static entry outranks the announcement, always
	// (decision 77). Recorded and shown, never silently dropped.
	AnnounceIgnored = mferegistry.AnnounceIgnored
	// AnnounceConverged — admitted, and identical to what is stored. Only
	// the release watermark moves (decision 10).
	AnnounceConverged = mferegistry.AnnounceConverged
)

/*
	Convergence (BR-AS73, decision 10).

	A catalogue-reset notice makes every live publisher re-announce at once.
	Almost all of those announcements say exactly what the registry already
	holds, and treating each one as an ordinary update would cost a catalogue
	revision and an audit row per plugin — a storm of writes recording that
	nothing changed, and a revision bump that makes every shell in the estate
	re-read a document that did not move.

	So identical content is its own outcome. What it still costs is one thing,
	and it is not optional: the release watermark advances. The counter is
	this protocol's stale-announcement protection — AdmitAnnouncement refuses
	Release < Accepted, and an unregister refuses a release the running
	announcement already spent. If a resync's release number did not become
	the watermark, every number spent on a resync would stay acceptable
	indefinitely, widening the replay window by exactly the number of resyncs.

	Note what convergence does NOT refresh: the stored manifest bytes and
	signature. Those encode the older release, because the counter is inside
	the signed payload. That is correct rather than merely tolerable — they
	remain a genuine signed manifest for the content that is stored, which is
	what BR-AS37 asks of them. Refreshing them would be the write convergence
	exists to avoid.
*/

// contentOf strips everything an announcement is allowed to move without
// changing what the catalogue means: the release counter, the announcement
// stamps, the signed bytes that carry the counter, and the curation fields
// no publisher asserts.
//
// Written as a subtraction rather than a list of compared fields on purpose.
// A field added to Entry later is content by default, so the new field makes
// announcements differ and take the ordinary write path. The other direction
// — a new field silently excluded from the comparison — would converge two
// entries that are not the same, and nothing would report it.
func contentOf(e Entry) Entry {
	e.Release = 0
	e.AnnouncedAt = ""
	e.LastAnnouncedAt = ""
	e.Manifest = nil
	return e
}

// SameAnnouncedContent reports whether storing incoming would change nothing
// about existing except the numbers that are not content.
func SameAnnouncedContent(existing, incoming Entry) bool {
	return reflect.DeepEqual(contentOf(existing), contentOf(incoming))
}

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
	// A plugin never states its own trust tier, activation, availability or
	// announcement stamps. ParseManifest refuses a payload that tried; this is
	// the second line, so a caller that built an Entry some other way cannot
	// get past it either (BR-AS39, BR-AS43, BR-AS70). Cleared as a set rather
	// than field by field — the field-by-field version had already missed
	// Withheld.
	incoming = incoming.WithoutCuration()
	// The one curated fact an announcement does decide for itself: an
	// announced entry is dynamic by construction. Whether it comes back from a
	// withdrawal is decided below, per branch.
	incoming.Lifecycle = LifecycleDynamic

	if existing == nil {
		return AnnounceInserted, incoming
	}
	if existing.Lifecycle == LifecycleStatic {
		return AnnounceIgnored, *existing
	}
	if !existing.Enabled {
		// An operator either has not approved this yet or has withheld
		// approval. Either way a return does not restore availability — the
		// entry is not running, and only an operator can decide that it
		// should (BR-AS55).
		incoming.Withdrawn = existing.Withdrawn
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
		// The one branch convergence applies to. The others all have real
		// work to do: an unknown id must be inserted, a pending one must be
		// refreshed for the operator looking at it, and a cross-origin
		// return must be re-queued. Only an enabled dynamic entry moving
		// within its own origin can be asked to store what it already has.
		// No field is carried over here, unlike the branches below. If the
		// content matched, it matched including Withdrawn and Withheld — so
		// a withdrawn or withheld entry re-announcing differs from what is
		// stored and takes the ordinary write path, which is what puts it
		// back on screen (BR-AS55).
		if SameAnnouncedContent(*existing, incoming) {
			return AnnounceConverged, incoming
		}
		return AnnounceUpdated, incoming
	}
	// A cross-origin return is a new place to fetch code from, so it goes
	// back to an operator. It does not clear the withdrawal on its own: the
	// approval that would put it back on screen is the same approval this
	// branch is asking for (BR-AS55).
	incoming.Withdrawn = existing.Withdrawn
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
