package application

import (
	"testing"

	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/domain"
)

/*
The operator view's join (decision 80, BR-AS43).

These rules used to be reachable only through the NATS adapter, which meant
asserting them meant decoding JSON. The join is plain values now, so each rule
below is one call.
*/

type stubDrift struct {
	seen   []string
	result domain.Drift
}

func (s *stubDrift) Snapshot(entry domain.Entry, source string) domain.Drift {
	s.seen = append(s.seen, entry.ID+":"+source)
	return s.result
}

func entry(id, url string) domain.Entry {
	return domain.Entry{
		ID:      id,
		Enabled: true,
		Remote:  domain.Remote{Kind: domain.RemoteFederated, URL: url, Module: "./plugin"},
	}
}

func doc(revision int64, entries ...domain.Entry) domain.Document {
	return domain.Document{SchemaVersion: domain.SchemaVersion, Revision: revision, Entries: entries}
}

func TestCurateCarriesTheDocumentsOwnFacts(t *testing.T) {
	g := NewWithT(t)
	allowed := domain.NewAllowlist([]string{"http://localhost:7110"})

	view := CurateDocument(doc(7, entry("demo", "http://localhost:7110/demo.js")), nil, allowed, nil)

	g.Expect(view.Revision).To(BeEquivalentTo(7))
	g.Expect(view.SchemaVersion).To(Equal(domain.SchemaVersion))
	g.Expect(view.AllowedOrigins).To(ConsistOf("http://localhost:7110"))
	g.Expect(view.Plugins).To(HaveLen(1))
	g.Expect(view.Plugins[0].Entry.ID).To(Equal("demo"))
}

// An empty catalogue must curate to an empty list, not a nil one: a nil slice
// encodes as JSON null, and a surface that renders a table from null shows a
// fault where it should show "nothing here yet".
func TestCurateOfAnEmptyDocumentIsAnEmptyList(t *testing.T) {
	g := NewWithT(t)

	view := CurateDocument(doc(1), nil, domain.NewAllowlist(nil), nil)

	g.Expect(view.Plugins).ToNot(BeNil())
	g.Expect(view.Plugins).To(BeEmpty())
}

func TestConformingIsTheAllowlistsOwnVerdict(t *testing.T) {
	g := NewWithT(t)
	allowed := domain.NewAllowlist([]string{"http://localhost:7110"})
	document := doc(1,
		entry("good", "http://localhost:7110/good.js"),
		entry("elsewhere", "http://evil.example/bad.js"),
	)

	view := CurateDocument(document, nil, allowed, nil)

	g.Expect(view.Plugins[0].Conforming).To(BeTrue())
	g.Expect(view.Plugins[1].Conforming).To(BeFalse())
}

// BR-AS43 / decision 80: a row with no audit history reads as "unknown", the
// one word an operator most needs to see. A blank badge among many would read
// as a rendering fault, and defaulting to "curated" would look like agreement.
func TestARowWithNoRegistrationReadsAsUnknown(t *testing.T) {
	g := NewWithT(t)
	sources := map[string]domain.Registration{
		"known": {Source: domain.SourceAnnounced, By: "publisher-key"},
	}
	document := doc(1, entry("known", "http://x/k.js"), entry("orphan", "http://x/o.js"))

	view := CurateDocument(document, sources, domain.NewAllowlist(nil), nil)

	g.Expect(view.Plugins[0].Source).To(Equal(domain.SourceAnnounced))
	g.Expect(view.Plugins[0].RegisteredBy).To(Equal("publisher-key"))
	g.Expect(view.Plugins[1].Source).To(Equal(domain.SourceUnknown))
	g.Expect(view.Plugins[1].RegisteredBy).To(BeEmpty())
}

// A deployment that runs no checker says so on every row, rather than saying
// nothing — silence in a drift column reads as agreement.
func TestWithNoCheckerEveryRowIsAwaitingCheck(t *testing.T) {
	g := NewWithT(t)

	view := CurateDocument(doc(1, entry("demo", "http://x/d.js")), nil, domain.NewAllowlist(nil), nil)

	g.Expect(view.Plugins[0].Drift.State).To(Equal(domain.DriftNotChecked))
	g.Expect(view.Plugins[0].Drift.Cause).To(Equal("awaiting-check"))
}

// The checker is asked about the entry AND how it got here: an announced
// remote and a curated one are not fetched from the same place.
func TestTheCheckerIsAskedPerEntryWithItsSource(t *testing.T) {
	g := NewWithT(t)
	drift := &stubDrift{result: domain.Drift{State: "in-sync"}}
	sources := map[string]domain.Registration{"known": {Source: domain.SourceAnnounced}}
	document := doc(1, entry("known", "http://x/k.js"), entry("orphan", "http://x/o.js"))

	view := CurateDocument(document, sources, domain.NewAllowlist(nil), drift)

	g.Expect(drift.seen).To(Equal([]string{"known:" + domain.SourceAnnounced, "orphan:" + domain.SourceUnknown}))
	g.Expect(view.Plugins[0].Drift.State).To(Equal("in-sync"))
}

// The joined fields are the operator's alone. None of them is written back
// onto the stored entry, so a later write cannot persist an observation.
func TestTheJoinLeavesTheStoredEntryAlone(t *testing.T) {
	g := NewWithT(t)
	original := entry("demo", "http://x/d.js")
	sources := map[string]domain.Registration{"demo": {Source: domain.SourceCurated, By: "admin"}}

	view := CurateDocument(doc(1, original), sources, domain.NewAllowlist(nil), &stubDrift{result: domain.Drift{State: "drifted"}})

	g.Expect(view.Plugins[0].Entry).To(Equal(original))
}
