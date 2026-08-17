package browserrpc

import (
	"strings"
	"testing"
)

// browserDenyPattern is the exact subject pattern accounts-service's
// MintBrowserToken denies for a browser credential (auth/token.go, BR-D41).
// Duplicated here as a literal rather than imported: accounts-service is a
// separate Go module, so the two agree on the published permission contract
// only, the same convention every cross-service wire shape in this repo
// follows. If that literal ever changes on one side, this test fails on the
// other — which is the point.
const browserDenyPattern = "api.*.refdata.admin.>"

// businessSubjects and adminSubjects must together be every subject New()
// registers. The registration loop is the source of truth; these lists are
// the permission-side classification of it.
var businessSubjects = []string{
	ItemGetSubject,
	ItemGetVersionedSubject,
	TypeListSubject,
	LocalesListSubject,
	CompletenessSubject,
	CacheStatusSubject,
	ContextListSubject,
	ContextGetSubject,
	TypesListSubject,
	ItemLocalizationsListSubject,
	ItemReferencesListSubject,
}

var adminSubjects = []string{
	ContextRegisterSubject,
	ContextSetVisibleSubject,
	CorpusCreateDraftSubject,
	CorpusGetDraftSubject,
	CorpusPutDraftItemSubject,
	CorpusPutDraftLocalizationSubject,
	CorpusPublishSubject,
	CorpusRollbackSubject,
	CorpusListVersionsSubject,
	CorpusGetVersionSubject,
	CorpusDiffSubject,
	TypeRegisterSubject,
	LocaleAddSubject,
	ItemRegisterSubject,
	ItemDeprecateSubject,
	ItemReactivateSubject,
	ItemUpdateAttrsSubject,
	ItemDeleteSubject,
	ReferenceCreateSubject,
	LocalizationSetSubject,
	TranslationDraftSubject,
}

// subjectMatches implements NATS subject-pattern matching for the two
// wildcard forms these patterns use: '*' (exactly one token) and '>' (one or
// more trailing tokens). Reimplemented rather than asserted with a string
// prefix, because a prefix check would pass even for a pattern that NATS
// itself would not match — which is exactly the failure mode BR-D41's deny
// grant has to be proven against.
func subjectMatches(pattern, subject string) bool {
	p := strings.Split(pattern, ".")
	s := strings.Split(subject, ".")
	for i, tok := range p {
		if tok == ">" {
			return len(s) > i // '>' needs at least one token to cover
		}
		if i >= len(s) {
			return false
		}
		if tok != "*" && tok != s[i] {
			return false
		}
	}
	return len(s) == len(p)
}

func TestSubjectMatchesHandlesWildcardForms(t *testing.T) {
	// Guards the matcher itself — a broken matcher would make the two
	// BR-D41 tests below vacuously pass.
	cases := []struct {
		pattern, subject string
		want             bool
	}{
		{"api.*.refdata.admin.>", "api.acme.refdata.admin.corpus.publish.v1", true},
		{"api.*.refdata.admin.>", "api.acme.refdata.admin.x", true},
		{"api.*.refdata.admin.>", "api.acme.refdata.admin", false}, // '>' needs ≥1 token
		{"api.*.refdata.admin.>", "api.acme.refdata.item.get.v1", false},
		{"api.*.refdata.admin.>", "api.a.b.refdata.admin.x", false}, // '*' is exactly one token
		{"api.*.refdata.item.get.v1", "api.acme.refdata.item.get.v1", true},
		{"api.*.refdata.item.get.v1", "api.acme.refdata.item.get", false},
	}
	for _, c := range cases {
		if got := subjectMatches(c.pattern, c.subject); got != c.want {
			t.Errorf("subjectMatches(%q, %q) = %v, want %v", c.pattern, c.subject, got, c.want)
		}
	}
}

// TestBrowserDenyCoversEveryAdminSubject is the half of BR-D41 that makes the
// namespace split real: every admin subject this adapter registers must be
// caught by the browser credential's deny pattern. A new admin subject added
// outside the api.*.refdata.admin.* namespace would be reachable by every
// already-minted browser token in its tenant account — this fails instead.
func TestBrowserDenyCoversEveryAdminSubject(t *testing.T) {
	for _, subject := range adminSubjects {
		concrete := strings.Replace(subject, "*", "acme", 1)
		if !subjectMatches(browserDenyPattern, concrete) {
			t.Errorf("admin subject %q (as %q) is NOT denied to a browser token by %q — BR-D41 violated", subject, concrete, browserDenyPattern)
		}
	}
}

// TestBrowserDenyCoversNoBusinessSubject is the other half: the deny must not
// be so broad that it also blocks the reads a browser legitimately makes. A
// deny of api.*.refdata.> instead of api.*.refdata.admin.> would silently
// break every label and locale read the frontends make over NATS.
func TestBrowserDenyCoversNoBusinessSubject(t *testing.T) {
	for _, subject := range businessSubjects {
		concrete := strings.Replace(subject, "*", "acme", 1)
		if subjectMatches(browserDenyPattern, concrete) {
			t.Errorf("business subject %q (as %q) IS denied to a browser token by %q — browsers could not read reference data", subject, concrete, browserDenyPattern)
		}
	}
}

// TestEverySubjectIsClassified guards the two lists above against drifting
// out of sync with New()'s registration loop: a subject registered but
// classified as neither business nor admin would be silently exempt from
// both BR-D41 assertions.
func TestEverySubjectIsClassified(t *testing.T) {
	classified := make(map[string]bool, len(businessSubjects)+len(adminSubjects))
	for _, s := range businessSubjects {
		classified[s] = true
	}
	for _, s := range adminSubjects {
		if classified[s] {
			t.Errorf("subject %q is classified as both business and admin", s)
		}
		classified[s] = true
	}
	for _, s := range registeredSubjects() {
		if !classified[s] {
			t.Errorf("subject %q is registered by New() but classified as neither business nor admin — BR-D41 cannot be asserted for it", s)
		}
	}
	if len(classified) != len(registeredSubjects()) {
		t.Errorf("classified %d subjects but New() registers %d", len(classified), len(registeredSubjects()))
	}
}
