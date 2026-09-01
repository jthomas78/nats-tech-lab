package registry_test

// Phase 5b — publisher availability, persisted. Derived from BR-AS54 and
// BR-AS55.
//
// Availability has to survive a restart or it is not a fact about the plugin,
// it is a fact about one process's memory: a registry that forgot a
// withdrawal on restart would serve the withdrawn code again on the next
// shell boot. So `withdrawn` is a column beside `enabled` and `withheld`,
// and the three answer three different questions:
//
//   enabled   — has an operator approved this
//   withheld  — did we take it away because its key was revoked
//   withdrawn — did its publisher say it is gone
//
// The release counter is a column too, for a narrower reason: once an
// unregister advances it, the signed manifest still carries the OLD number,
// and a store that read the release out of the manifest would let the stale
// announcement back in.

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/domain"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/postgres"
)

var _ = Describe("BR-AS55 — publisher availability is stored, not remembered", func() {
	var (
		store *postgres.Store
		ctx   context.Context
	)

	allowed := domain.NewAllowlist([]string{"http://localhost:7110"})

	BeforeEach(func() {
		if pgUnavailable != "" {
			Skip("Postgres integration fixture unavailable: " + pgUnavailable)
		}
		ctx = context.Background()
		_, err := pgDB.ExecContext(ctx, `TRUNCATE registry.entries, registry.audit`)
		Expect(err).NotTo(HaveOccurred())
		_, err = pgDB.ExecContext(ctx, `UPDATE registry.revision SET revision = 0`)
		Expect(err).NotTo(HaveOccurred())
		store = postgres.NewStore(pgDB, allowed)
	})

	// live curates an enabled dynamic entry the way an operator would.
	live := func(id string) domain.Document {
		e := federated(id, "http://localhost:7110/remoteEntry.js")
		e.Lifecycle = domain.LifecycleDynamic
		e.Release = 4
		doc, err := store.Apply(ctx, domain.Write{
			Op: domain.OpUpsert, EntryID: id, Actor: domain.SharedAdminActor,
			Entry: &e, IfRevision: domain.NoRevision,
		})
		Expect(err).NotTo(HaveOccurred())
		return doc
	}

	// withdraw runs the domain decision against the stored entry and writes
	// what it decided, which is what the transport will do.
	withdraw := func(doc domain.Document, id string, release int64) domain.Document {
		var existing *domain.Entry
		for i := range doc.Entries {
			if doc.Entries[i].ID == id {
				existing = &doc.Entries[i]
			}
		}
		Expect(existing).ToNot(BeNil())
		outcome, next, err := domain.DecideUnregister(existing, domain.UnregisterCommand{
			PluginID: id, Publisher: "platform-team", SigningKey: "UABC", Release: release,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(outcome).To(Equal(domain.UnregisterWithdrawn))
		written, err := store.Apply(ctx, domain.UnregisterWrite(next, "UABC", doc.Revision))
		Expect(err).NotTo(HaveOccurred())
		return written
	}

	find := func(doc domain.Document, id string) domain.Entry {
		for _, e := range doc.Entries {
			if e.ID == id {
				return e
			}
		}
		Fail("no entry " + id)
		return domain.Entry{}
	}

	It("survives a restart, because it is a column and not memory", func() {
		doc := withdraw(live("fleet"), "fleet", 7)
		Expect(find(doc, "fleet").Withdrawn).To(BeTrue())

		// A second Store on the same database is what a restarted service is.
		reread, err := postgres.NewStore(pgDB, allowed).Current(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(find(reread, "fleet").Withdrawn).To(BeTrue())
	})

	It("leaves approval, class and the signed columns alone", func() {
		doc := withdraw(live("fleet"), "fleet", 7)
		got := find(doc, "fleet")
		Expect(got.Enabled).To(BeTrue(), "availability is not approval")
		Expect(got.Lifecycle).To(Equal(domain.LifecycleDynamic))
		Expect(got.Contributions).To(HaveLen(1))
		Expect(got.Withheld).To(BeFalse(), "nobody revoked a key here")
	})

	It("advances the stored release past the announcement that is being withdrawn", func() {
		doc := withdraw(live("fleet"), "fleet", 7)
		Expect(find(doc, "fleet").Release).To(Equal(int64(7)))
	})

	It("starts an entry nobody withdrew as available", func() {
		Expect(find(live("fleet"), "fleet").Withdrawn).To(BeFalse())
	})

	It("withdraws one entry and not its siblings", func() {
		first := live("fleet")
		e := federated("weather", "http://localhost:7110/remoteEntry.js")
		e.Lifecycle = domain.LifecycleDynamic
		second, err := store.Apply(ctx, domain.Write{
			Op: domain.OpUpsert, EntryID: "weather", Actor: domain.SharedAdminActor,
			Entry: &e, IfRevision: first.Revision,
		})
		Expect(err).NotTo(HaveOccurred())

		doc := withdraw(second, "fleet", 7)
		Expect(find(doc, "fleet").Withdrawn).To(BeTrue())
		Expect(find(doc, "weather").Withdrawn).To(BeFalse())
	})

	It("lets an operator put a withdrawn entry back by enabling it", func() {
		// Approval outranks availability in both directions (BR-AS55). An
		// operator re-enabling is looking at this entry and saying run it,
		// which is a decision the publisher's absence does not overrule.
		doc := withdraw(live("fleet"), "fleet", 7)
		off, err := store.Apply(ctx, domain.Write{
			Op: domain.OpSetEnabled, EntryID: "fleet", Enabled: false,
			Actor: domain.SharedAdminActor, IfRevision: doc.Revision,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(find(off, "fleet").Withdrawn).To(BeTrue(), "disabling says nothing about availability")

		on, err := store.Apply(ctx, domain.Write{
			Op: domain.OpSetEnabled, EntryID: "fleet", Enabled: true,
			Actor: domain.SharedAdminActor, IfRevision: off.Revision,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(find(on, "fleet").Withdrawn).To(BeFalse())
	})

	It("withdraws a SIGNED entry without rewriting the bytes it was signed as", func() {
		// The case the columns exist for. A signed row is reassembled from
		// its manifest, and those bytes still say the old release and say
		// nothing about availability — so if either fact lived only in the
		// manifest, the withdrawal would vanish on the next read and the
		// stale announcement could walk back in (BR-AS37, BR-AS47).
		e := signedEntry()
		e.Lifecycle = domain.LifecycleDynamic
		e.Release = 4
		doc, err := store.Apply(ctx, domain.Write{
			Op: domain.OpUpsert, EntryID: e.ID, Actor: "UABC",
			Entry: &e, IfRevision: domain.NoRevision,
		})
		Expect(err).NotTo(HaveOccurred())

		after := withdraw(doc, e.ID, 7)
		got := find(after, e.ID)
		Expect(got.Withdrawn).To(BeTrue())
		Expect(got.Release).To(Equal(int64(7)))
		Expect(got.Manifest.Bytes).To(Equal(signedBytes), "a withdrawal is not a re-publication")
		Expect(got.Manifest.Signature).To(Equal(signedSignature))
	})

	It("audits the withdrawal under the key that signed it", func() {
		withdraw(live("fleet"), "fleet", 7)
		var actor, outcome string
		Expect(pgDB.QueryRowContext(ctx,
			`SELECT actor, outcome FROM registry.audit WHERE entry_id = 'fleet' ORDER BY id DESC LIMIT 1`).
			Scan(&actor, &outcome)).To(Succeed())
		Expect(actor).To(Equal("UABC"))
		Expect(outcome).To(Equal(domain.AuditAccepted))
	})

	It("refuses a withdrawal keyed on a revision someone else has moved past", func() {
		doc := live("fleet")
		existing := find(doc, "fleet")
		_, next, err := domain.DecideUnregister(&existing, domain.UnregisterCommand{
			PluginID: "fleet", Publisher: "platform-team", SigningKey: "UABC", Release: 7,
		})
		Expect(err).NotTo(HaveOccurred())

		// An operator writes in between, so the unregister is keyed on a
		// revision that is no longer current.
		_, err = store.Apply(ctx, domain.Write{
			Op: domain.OpSetEnabled, EntryID: "fleet", Enabled: false,
			Actor: domain.SharedAdminActor, IfRevision: doc.Revision,
		})
		Expect(err).NotTo(HaveOccurred())

		_, err = store.Apply(ctx, domain.UnregisterWrite(next, "UABC", doc.Revision))
		Expect(err).To(MatchError(domain.ErrStaleRevision), "two decisions are not merged (BR-AS18)")
	})
})
