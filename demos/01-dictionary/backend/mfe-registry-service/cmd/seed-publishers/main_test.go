package main

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jthomas78/nats-tech-lab/shared/mferegistry"

	. "github.com/onsi/gomega"
)

// The five fixtures the bootstrap mints, in the shape publishers.json carries.
func fixtures() []publisherSeed {
	return []publisherSeed{
		{Publisher: "example-plugin", Plugin: "example-plugin", SigningKey: "UAAA"},
		{Publisher: "example-plugin-slow", Plugin: "example-plugin-slow", SigningKey: "UBBB"},
	}
}

func ops(w []write) []string {
	var out []string
	for _, op := range w {
		out = append(out, op.Op+" "+op.subject())
	}
	return out
}

// --- BR-AS68: convergence ---

func TestPlanSeedsAnEmptyRegistryInOneOrderedPassPerPublisher(t *testing.T) {
	g := NewWithT(t)
	plan, notes := planOps(nil, fixtures())
	g.Expect(notes).To(BeEmpty())
	g.Expect(ops(plan)).To(Equal([]string{
		mferegistry.OpPublisherUpsert + " example-plugin",
		mferegistry.OpPublisherAddKey + " example-plugin key UAAA",
		mferegistry.OpPublisherTransfer + " example-plugin plugin example-plugin",
		mferegistry.OpPublisherUpsert + " example-plugin-slow",
		mferegistry.OpPublisherAddKey + " example-plugin-slow key UBBB",
		mferegistry.OpPublisherTransfer + " example-plugin-slow plugin example-plugin-slow",
	}))
}

func TestPlanIsOrderedByPublisherWhateverTheFileOrder(t *testing.T) {
	g := NewWithT(t)
	shuffled := []publisherSeed{fixtures()[1], fixtures()[0]}
	a, _ := planOps(nil, fixtures())
	b, _ := planOps(nil, shuffled)
	g.Expect(ops(b)).To(Equal(ops(a)))
}

func TestPlanIsEmptyOnASecondRun(t *testing.T) {
	g := NewWithT(t)
	current := []publisher{
		{ID: "example-plugin", Keys: []publisherKey{{PublicKey: "UAAA", State: mferegistry.KeyEnabled}}, Plugins: []string{"example-plugin"}},
		{ID: "example-plugin-slow", Keys: []publisherKey{{PublicKey: "UBBB", State: mferegistry.KeyEnabled}}, Plugins: []string{"example-plugin-slow"}},
	}
	plan, notes := planOps(current, fixtures())
	g.Expect(plan).To(BeEmpty(), "a converged registry must cost zero revisions and zero audit rows")
	g.Expect(notes).To(BeEmpty())
}

// A revoked key is the operator decision the whole demo turns on. A seeder
// that re-enabled it on the next restart would silently undo BR-AS38.
func TestPlanLeavesARevokedKeyRevoked(t *testing.T) {
	g := NewWithT(t)
	current := []publisher{
		{ID: "example-plugin", Keys: []publisherKey{{PublicKey: "UAAA", State: mferegistry.KeyRevoked}}, Plugins: []string{"example-plugin"}},
	}
	plan, notes := planOps(current, fixtures()[:1])
	g.Expect(plan).To(BeEmpty())
	g.Expect(notes).To(ConsistOf(ContainSubstring("revoked")))
}

func TestPlanLeavesARetiredKeyRetired(t *testing.T) {
	g := NewWithT(t)
	current := []publisher{
		{ID: "example-plugin", Keys: []publisherKey{{PublicKey: "UAAA", State: mferegistry.KeyRetired}}, Plugins: []string{"example-plugin"}},
	}
	plan, notes := planOps(current, fixtures()[:1])
	g.Expect(plan).To(BeEmpty())
	g.Expect(notes).To(ConsistOf(ContainSubstring("retired")))
}

func TestPlanDoesNotReverseAnOwnershipTransfer(t *testing.T) {
	g := NewWithT(t)
	current := []publisher{
		{ID: "example-plugin", Keys: []publisherKey{{PublicKey: "UAAA", State: mferegistry.KeyEnabled}}},
		{ID: "acquirer", Plugins: []string{"example-plugin"}},
	}
	plan, notes := planOps(current, fixtures()[:1])
	g.Expect(plan).To(BeEmpty())
	g.Expect(notes).To(ConsistOf(ContainSubstring(`owned by publisher "acquirer"`)))
}

func TestPlanDoesNotTryToStealAKeyHeldByAnotherPublisher(t *testing.T) {
	g := NewWithT(t)
	current := []publisher{
		{ID: "example-plugin", Plugins: []string{"example-plugin"}},
		{ID: "somebody-else", Keys: []publisherKey{{PublicKey: "UAAA", State: mferegistry.KeyEnabled}}},
	}
	plan, notes := planOps(current, fixtures()[:1])
	g.Expect(plan).To(BeEmpty())
	g.Expect(notes).To(ConsistOf(ContainSubstring(`held by publisher "somebody-else"`)))
}

// A half-seeded registry converges by adding only what is absent — the point
// of reading before writing rather than replaying the whole file.
func TestPlanFillsOnlyTheGapsInAPartiallySeededRegistry(t *testing.T) {
	g := NewWithT(t)
	current := []publisher{
		{ID: "example-plugin", Keys: []publisherKey{{PublicKey: "UAAA", State: mferegistry.KeyEnabled}}},
	}
	plan, notes := planOps(current, fixtures()[:1])
	g.Expect(notes).To(BeEmpty())
	g.Expect(ops(plan)).To(Equal([]string{
		mferegistry.OpPublisherTransfer + " example-plugin plugin example-plugin",
	}))
}

// --- the wire ---

func TestApplyThreadsTheRevisionAndNamesTheOperatorSubjects(t *testing.T) {
	g := NewWithT(t)
	var subjects []string
	var sent []map[string]any
	c := &client{request: func(subject string, data []byte) ([]byte, error) {
		subjects = append(subjects, subject)
		var payload map[string]any
		g.Expect(json.Unmarshal(data, &payload)).To(Succeed())
		sent = append(sent, payload)
		return []byte(`{"revision":7}`), nil
	}}
	next, err := c.apply(write{Op: mferegistry.OpPublisherAddKey, PublisherID: "example-plugin", PublicKey: "UAAA"}, 6)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(next).To(Equal(int64(7)))
	g.Expect(subjects).To(Equal([]string{mferegistry.PublisherWrite}))
	g.Expect(sent[0]["ifRevision"]).To(Equal(float64(6)))
	g.Expect(sent[0]["publicKey"]).To(Equal("UAAA"))
	// The endpoint stamps the actor itself; a client-supplied one would be a
	// claim rather than a fact, so it must not appear on the wire at all.
	g.Expect(sent[0]).NotTo(HaveKey("actor"))
}

func TestApplySurfacesAStaleRevisionRefusalInsteadOfSucceeding(t *testing.T) {
	g := NewWithT(t)
	calls := 0
	c := &client{request: func(string, []byte) ([]byte, error) {
		calls++
		return []byte(`{"error":"registry moved","currentRevision":9,"yourRevision":4}`), nil
	}}
	_, err := c.apply(write{Op: mferegistry.OpPublisherUpsert, PublisherID: "example-plugin"}, 4)
	g.Expect(err).To(MatchError("registry moved"))
	g.Expect(calls).To(Equal(1), "a refused write is a decision, never a timing accident — it must not be retried")
}

func TestPublishersReadsTheListSubjectAndPropagatesFailures(t *testing.T) {
	g := NewWithT(t)
	var subject string
	c := &client{request: func(s string, _ []byte) ([]byte, error) {
		subject = s
		return []byte(`{"revision":3,"publishers":[{"id":"example-plugin","keys":[{"publicKey":"UAAA","state":"enabled"}],"plugins":["example-plugin"]}]}`), nil
	}}
	doc, err := c.publishers()
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(subject).To(Equal(mferegistry.Publishers))
	g.Expect(doc.Revision).To(Equal(int64(3)))
	g.Expect(doc.Publishers[0].Keys[0].State).To(Equal(mferegistry.KeyEnabled))

	for _, raw := range []string{`not JSON`, `{"error":"unavailable"}`} {
		c := &client{request: func(string, []byte) ([]byte, error) { return []byte(raw), nil }}
		_, err := c.publishers()
		g.Expect(err).To(HaveOccurred())
	}
}

// Only the read is retried. The one-shot has to tolerate a registry that is
// still migrating, or the announcer gate it protects starts against an
// unseeded trust table.
func TestAwaitPublishersRetriesTheReadThenGivesUpBounded(t *testing.T) {
	g := NewWithT(t)
	attempts := 0
	c := &client{request: func(string, []byte) ([]byte, error) {
		attempts++
		if attempts < 3 {
			return nil, errors.New("no responders")
		}
		return []byte(`{"revision":1,"publishers":[]}`), nil
	}}
	doc, err := c.awaitPublishers(time.Second, time.Millisecond)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(doc.Revision).To(Equal(int64(1)))
	g.Expect(attempts).To(Equal(3))

	always := &client{request: func(string, []byte) ([]byte, error) { return nil, errors.New("no responders") }}
	_, err = always.awaitPublishers(5*time.Millisecond, time.Millisecond)
	g.Expect(err).To(MatchError(ContainSubstring("no responders")))
}

// --- the seed file ---

func TestValidateRefusesAnIncoherentSeedFileBeforeAnyWrite(t *testing.T) {
	g := NewWithT(t)
	g.Expect(validate(fixtures())).To(Succeed())
	g.Expect(validate([]publisherSeed{{Plugin: "p", SigningKey: "U"}})).To(MatchError(ContainSubstring("names no publisher")))
	g.Expect(validate([]publisherSeed{{Publisher: "a", SigningKey: "U"}})).To(MatchError(ContainSubstring("names no plugin")))
	g.Expect(validate([]publisherSeed{{Publisher: "a", Plugin: "p"}})).To(MatchError(ContainSubstring("no signing key")))
	g.Expect(validate([]publisherSeed{
		{Publisher: "a", Plugin: "p", SigningKey: "U"},
		{Publisher: "b", Plugin: "q", SigningKey: "U"},
	})).To(MatchError(ContainSubstring("same signing key")))
	g.Expect(validate([]publisherSeed{
		{Publisher: "a", Plugin: "p", SigningKey: "U1"},
		{Publisher: "b", Plugin: "p", SigningKey: "U2"},
	})).To(MatchError(ContainSubstring("both claim plugin")))
}
