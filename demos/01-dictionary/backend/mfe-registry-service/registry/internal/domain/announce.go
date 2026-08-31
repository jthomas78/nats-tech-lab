package domain

import (
	"errors"
	"fmt"
)

var (
	// ErrUnsigned refuses an announcement carrying no signature, before any
	// verifier is consulted. An unsigned announcement is not a verification
	// failure, it is a malformed one, and the two read differently to a
	// publisher debugging an integration.
	ErrUnsigned = errors.New("registry: an announcement must be signed")

	// ErrUnverified refuses an announcement this deployment cannot check.
	// Until Phase 7 supplies a trust anchor, that is every announcement:
	// the safe direction for a check that decides which code a browser
	// fetches is to fail closed.
	ErrUnverified = errors.New("registry: announcement signature could not be verified")
)

// Verifier is Phase 7's seam. It answers with the publisher key an
// announcement was signed by, which is also the audit actor the write is
// filed under (BR-AS42).
type Verifier interface {
	Verify(payload []byte, signature string) (publisherKey string, err error)
}

// NoVerifier is the verifier a deployment has until Phase 7 lands: it
// verifies nothing and says so. It exists so that "no trust anchor" is a
// configured state with a spec on it, rather than a nil check someone can
// forget on the one path where forgetting means injecting JavaScript into an
// operator's browser (decision 72).
type NoVerifier struct{}

func (NoVerifier) Verify([]byte, string) (string, error) { return "", ErrUnverified }

// VerifyAnnouncement is the whole gate an announcement passes before the
// registry considers what it says. A nil verifier is not "skip the check" —
// it is NoVerifier.
func VerifyAnnouncement(v Verifier, payload []byte, signature string) (string, error) {
	if signature == "" {
		return "", ErrUnsigned
	}
	if v == nil {
		v = NoVerifier{}
	}
	key, err := v.Verify(payload, signature)
	if err != nil {
		return "", fmt.Errorf("%w: %s", ErrUnverified, err)
	}
	if key == "" {
		return "", ErrUnverified
	}
	return key, nil
}

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
//  3. BR-AS40 — an enabled dynamic entry follows its publisher within its
//     own origin, and re-queues when the remote crosses one.
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
