package browserrpc

import (
	"context"
	"encoding/json"
	"testing"

	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/accounts-service/registry/internal/domain"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/accounts-service/registry/internal/postgres"
)

/*
	Phase 4 — the registry's api.* adapter (BR-AS27, BR-AS31, decision 58).

	Written before the adapter exists. The seam under test is deliberately the
	payload translation, not NATS: Endpoints takes a decoded request and
	returns a response, so the specs that matter — what "unchanged" means, what
	a stale write is answered with — run without a server. The micro.Service
	registration that carries these onto subjects is thin by design and is
	covered by the subject-list assertion below.

	Decision 58 is why this file is long. ETag / If-None-Match / 304 and
	If-Match / 409 / 428 are doing real work on the HTTP surface today, and
	none of it is free over request/reply. Revision is already a domain
	concept, so the payload carries it explicitly — this is re-implementation,
	not deletion, and it is the part of the phase most likely to acquire
	subtle bugs.
*/

type stubService struct {
	doc     domain.Document
	origins []string
	applied domain.Write
	err     error
}

func (s *stubService) Read(context.Context) domain.Document             { return s.doc }
func (s *stubService) Curated(context.Context) (domain.Document, error) { return s.doc, nil }
func (s *stubService) Allowlist() domain.Allowlist                      { return domain.NewAllowlist(s.origins) }
func (s *stubService) Apply(_ context.Context, w domain.Write) (domain.Document, error) {
	s.applied = w
	if s.err != nil {
		return domain.Document{}, s.err
	}
	return s.doc, nil
}

type stubAudit struct{}

func (stubAudit) Audit(context.Context, int) ([]postgres.AuditPage, error) {
	return []postgres.AuditPage{}, nil
}

func entry(id, url string) domain.Entry {
	return domain.Entry{
		ID:      id,
		Enabled: true,
		Remote:  domain.Remote{Kind: domain.RemoteFederated, URL: url, Module: "./plugin"},
	}
}

func doc(revision int64, entries ...domain.Entry) domain.Document {
	if entries == nil {
		entries = []domain.Entry{}
	}
	return domain.Document{SchemaVersion: domain.SchemaVersion, Revision: revision, Entries: entries}
}

func endpoints(svc *stubService) *Endpoints { return New(svc, stubAudit{}) }

// TestSubjectsAreTheGrantedSet pins the subject surface exactly, the way
// rest_test.go's TestMountRoutes pins the route surface.
//
// The list is the credential design: MintShellToken grants exactly the read
// subject, and MintAdminToken grants exactly the other four. A subject added
// here without a corresponding decision about which credential reaches it is
// the failure this catches — and over NATS that failure is quieter than its
// HTTP equivalent, because there is no 404 to notice, just a subject nobody
// happens to be calling yet.
func TestSubjectsAreTheGrantedSet(t *testing.T) {
	g := NewWithT(t)

	g.Expect(Subjects()).To(ConsistOf(
		"api._platform.registry.frontend-plugins.read.v1",
		"api._platform.registry.entries.curated.v1",
		"api._platform.registry.entries.upsert.v1",
		"api._platform.registry.entries.set-enabled.v1",
		"api._platform.registry.audit.list.v1",
	))

	// BR-AS24 restated for the new transport: there is no delete subject, and
	// no subject named for removal in any spelling.
	for _, s := range Subjects() {
		g.Expect(s).NotTo(ContainSubstring("delete"))
		g.Expect(s).NotTo(ContainSubstring("remove"))
	}

	// The read subject is the ONLY one the shell's credential holds
	// (BR-AS27). Asserted here as well as in token_test.go because the two
	// facts live in different modules and only agree by intention.
	g.Expect(ShellReadSubject).To(Equal("api._platform.registry.frontend-plugins.read.v1"))
	g.Expect(Subjects()).To(ContainElement(ShellReadSubject))
}

// --- the read (BR-AS27, decision 58) ---

// The shell states the revision it holds. Answering "unchanged" is the
// request/reply equivalent of a 304, and it is what makes BR-AS28's
// notification-triggered read cheap enough to be boring.
func TestReadAnswersUnchangedWhenTheShellIsCurrent(t *testing.T) {
	g := NewWithT(t)
	e := endpoints(&stubService{doc: doc(12, entry("fleet-ops", "http://localhost:7110/r.js")), origins: []string{"http://localhost:7110"}})

	res, err := e.Read(context.Background(), ReadRequest{HeldRevision: 12})
	g.Expect(err).NotTo(HaveOccurred())

	g.Expect(res.Unchanged).To(BeTrue())
	g.Expect(res.Revision).To(Equal(int64(12)))
	g.Expect(res.Plugins).To(BeNil(), "an unchanged answer carries no document — that is the whole point of it")
}

func TestReadAnswersTheDocumentWhenTheRevisionMoved(t *testing.T) {
	g := NewWithT(t)
	e := endpoints(&stubService{doc: doc(13, entry("fleet-ops", "http://localhost:7110/r.js")), origins: []string{"http://localhost:7110"}})

	res, err := e.Read(context.Background(), ReadRequest{HeldRevision: 12})
	g.Expect(err).NotTo(HaveOccurred())

	g.Expect(res.Unchanged).To(BeFalse())
	g.Expect(res.Revision).To(Equal(int64(13)))
	g.Expect(res.Plugins).To(HaveLen(1))
}

// BR-AS29's wire shape. An unconditional read is a read that names no held
// revision, and it must never be answered "unchanged" — a reconnecting shell
// asking unconditionally is asking precisely because it cannot trust what it
// holds.
func TestUnconditionalReadIsNeverAnsweredUnchanged(t *testing.T) {
	g := NewWithT(t)
	e := endpoints(&stubService{doc: doc(12, entry("fleet-ops", "http://localhost:7110/r.js")), origins: []string{"http://localhost:7110"}})

	res, err := e.Read(context.Background(), ReadRequest{})
	g.Expect(err).NotTo(HaveOccurred())

	g.Expect(res.Unchanged).To(BeFalse())
	g.Expect(res.Revision).To(Equal(int64(12)))
	g.Expect(res.Plugins).To(HaveLen(1), "the caller asked unconditionally and gets a document, even at the revision it happened to hold")
}

// BR-AS22 over the new transport. A degraded document is answered as a
// successful reply that says degraded — never as an error, because an error
// is what a shell would render as "the platform is down" instead of rendering
// its built-ins.
func TestDegradedIsAnAnswerAndNeverAnError(t *testing.T) {
	g := NewWithT(t)
	e := endpoints(&stubService{doc: domain.Degraded()})

	res, err := e.Read(context.Background(), ReadRequest{HeldRevision: 12})
	g.Expect(err).NotTo(HaveOccurred(), "a degraded registry is a state, not a failed request")

	g.Expect(res.Degraded).To(BeTrue())
	g.Expect(res.Plugins).To(BeEmpty())
	// The 3c/decision-48 rule, restated: a degraded answer must not be
	// mistakable for "you are current", or a shell that recovers at the same
	// revision never leaves the degraded state.
	g.Expect(res.Unchanged).To(BeFalse())
	g.Expect(res.Revision).To(Equal(domain.DegradedRevision))
}

// BR-AS20 on the read side: the shell's answer is the filtered document, so a
// disabled or non-conforming entry never crosses the wire to a browser.
func TestReadServesOnlyWhatAShellMaySee(t *testing.T) {
	g := NewWithT(t)
	disabled := entry("withdrawn", "http://localhost:7110/r.js")
	disabled.Enabled = false
	offOrigin := entry("elsewhere", "http://evil.example/r.js")
	e := endpoints(&stubService{
		doc:     doc(4, entry("fleet-ops", "http://localhost:7110/r.js"), disabled, offOrigin),
		origins: []string{"http://localhost:7110"},
	})

	res, err := e.Read(context.Background(), ReadRequest{})
	g.Expect(err).NotTo(HaveOccurred())

	g.Expect(res.Plugins).To(HaveLen(1))
	g.Expect(res.Plugins[0].ID).To(Equal("fleet-ops"))

	// BR-AS04: nothing in the reply names why the others are missing.
	body, err := json.Marshal(res)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(string(body)).NotTo(ContainSubstring("evil.example"))
}

// --- the writes (BR-AS31, decision 58) ---

// The If-Match equivalent. A write states the revision its author read.
func TestUpsertAppliesOnTheRevisionItNames(t *testing.T) {
	g := NewWithT(t)
	svc := &stubService{doc: doc(5), origins: []string{"http://localhost:7110"}}
	e := endpoints(svc)

	got := entry("fleet-ops", "http://localhost:7110/r.js")
	res, err := e.Upsert(context.Background(), UpsertRequest{IfRevision: 4, EntryID: "fleet-ops", Entry: &got})
	g.Expect(err).NotTo(HaveOccurred())

	g.Expect(svc.applied.Op).To(Equal(domain.OpUpsert))
	g.Expect(svc.applied.IfRevision).To(Equal(int64(4)))
	g.Expect(svc.applied.EntryID).To(Equal("fleet-ops"))
	g.Expect(svc.applied.Actor).To(Equal(domain.SharedAdminActor), "BR-AS23: the shared identity, which is all this service can honestly record")
	g.Expect(res.Revision).To(Equal(int64(5)))
}

// The 428 equivalent. A write that names no revision is refused rather than
// applied on top of whatever is current: over HTTP a missing If-Match is a
// distinct refusal, and losing that distinction over NATS would turn every
// forgetful client into a silent last-writer-wins.
func TestWriteWithoutARevisionIsRefused(t *testing.T) {
	g := NewWithT(t)
	svc := &stubService{doc: doc(5), origins: []string{"http://localhost:7110"}}
	e := endpoints(svc)

	got := entry("fleet-ops", "http://localhost:7110/r.js")
	_, err := e.Upsert(context.Background(), UpsertRequest{EntryID: "fleet-ops", Entry: &got})

	g.Expect(err).To(MatchError(ErrRevisionRequired))
	g.Expect(svc.applied.EntryID).To(BeEmpty(), "the store must not be reached at all")
}

// The 409 equivalent, with the payload decision 58 requires: a stale refusal
// carries the current revision, so the admin surface can say what to reapply
// on top of rather than telling the operator to guess.
func TestStaleWriteIsRefusedWithTheCurrentRevision(t *testing.T) {
	g := NewWithT(t)
	svc := &stubService{doc: doc(9), origins: []string{"http://localhost:7110"}}
	svc.err = domain.StaleRevisionError{Current: 9, Supplied: 4}
	e := endpoints(svc)

	got := entry("fleet-ops", "http://localhost:7110/r.js")
	_, err := e.Upsert(context.Background(), UpsertRequest{IfRevision: 4, EntryID: "fleet-ops", Entry: &got})

	var stale StaleRefusal
	g.Expect(AsStaleRefusal(err, &stale)).To(BeTrue())
	g.Expect(stale.Current).To(Equal(int64(9)))
	g.Expect(stale.Supplied).To(Equal(int64(4)))
	// Nothing is merged — two curation decisions are not something a server
	// guesses at (decision 27).
	g.Expect(stale.Merged).To(BeFalse())
}

// BR-AS04 over the new transport. The refusal reaches an operator's screen,
// so it may not carry the URL it refused.
func TestOriginRefusalNamesNoURL(t *testing.T) {
	g := NewWithT(t)
	svc := &stubService{doc: doc(1), origins: []string{"http://localhost:7110"}}
	svc.err = domain.ErrOriginNotAllowed
	e := endpoints(svc)

	got := entry("fleet-ops", "http://evil.example/r.js")
	_, err := e.Upsert(context.Background(), UpsertRequest{IfRevision: 1, EntryID: "fleet-ops", Entry: &got})

	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).NotTo(ContainSubstring("evil.example"))
	g.Expect(err.Error()).NotTo(ContainSubstring("http"))
}

func TestSetEnabledCarriesTheRevisionToo(t *testing.T) {
	g := NewWithT(t)
	svc := &stubService{doc: doc(6), origins: []string{"http://localhost:7110"}}
	e := endpoints(svc)

	res, err := e.SetEnabled(context.Background(), SetEnabledRequest{IfRevision: 5, EntryID: "fleet-ops", Enabled: false})
	g.Expect(err).NotTo(HaveOccurred())

	g.Expect(svc.applied.Op).To(Equal(domain.OpSetEnabled))
	g.Expect(svc.applied.Enabled).To(BeFalse())
	g.Expect(svc.applied.IfRevision).To(Equal(int64(5)))
	g.Expect(res.Revision).To(Equal(int64(6)))
}

// BR-AS21 from inside the browser, and the reason the write subjects are
// enumerated rather than granted as a prefix: there is no subject on this
// surface that creates an entry as a side effect of enabling one.
func TestSetEnabledNeverCreatesAnEntry(t *testing.T) {
	g := NewWithT(t)
	svc := &stubService{doc: doc(6), origins: []string{"http://localhost:7110"}}
	e := endpoints(svc)

	_, _ = e.SetEnabled(context.Background(), SetEnabledRequest{IfRevision: 5, EntryID: "never-curated", Enabled: true})

	g.Expect(svc.applied.Op).To(Equal(domain.OpSetEnabled))
	g.Expect(svc.applied.Entry).To(BeNil(), "an enable carries no entry body, so it cannot become an insert")
}

// The curated read is the admin's, not the shell's: it shows the entries the
// shell's read deliberately withholds, which is exactly why it is on a
// separate subject with a separate grant.
func TestCuratedReadShowsWhatTheShellReadWithholds(t *testing.T) {
	g := NewWithT(t)
	disabled := entry("withdrawn", "http://localhost:7110/r.js")
	disabled.Enabled = false
	svc := &stubService{doc: doc(4, entry("fleet-ops", "http://localhost:7110/r.js"), disabled), origins: []string{"http://localhost:7110"}}
	e := endpoints(svc)

	res, err := e.Curated(context.Background())
	g.Expect(err).NotTo(HaveOccurred())

	g.Expect(res.Plugins).To(HaveLen(2))
	g.Expect(res.Revision).To(Equal(int64(4)))
	g.Expect(res.AllowedOrigins).To(ConsistOf("http://localhost:7110"),
		"the admin surface states the configured origins so an operator is not guessing at config")
}
