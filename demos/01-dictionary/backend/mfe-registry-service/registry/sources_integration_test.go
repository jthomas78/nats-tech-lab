package registry_test

// Phase 8c — the source badge, through real Postgres. Derived from decision
// 80 and BR-AS42.
//
// domain.SourceOf is pure and specified in source_test.go; what these specs
// are about is the QUERY, because the whole rule lives in three clauses of
// it: first row not last, accepted rows only, registry scope only. Each is
// wrong in a way that would look plausible on screen, so each has a spec.

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/domain"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/postgres"
)

var _ = Describe("decision 80 — the source is read from the audit trail", func() {
	var (
		store *postgres.Store
		ctx   context.Context
	)

	allowed := domain.NewAllowlist([]string{"http://localhost:7110"})

	write := func(id, actor string, rev int64) error {
		e := federated(id, "http://localhost:7110/remoteEntry.js")
		_, err := store.Apply(ctx, domain.Write{
			Op: domain.OpUpsert, EntryID: id, Actor: actor, Entry: &e, IfRevision: rev,
		})
		return err
	}

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

	Context("the creating write answers, not the latest one", func() {
		It("keeps an announced entry announced after an operator edits it", func() {
			// The failure this guards against is the quiet one: an operator
			// reviewing an announcement is the FIRST thing that happens to
			// every announced entry, so a latest-actor badge would relabel
			// exactly the entries the badge exists for.
			Expect(write("acme-flow", "pub_7f3a91c4", domain.NoRevision)).To(Succeed())
			Expect(write("acme-flow", domain.SharedAdminActor, 1)).To(Succeed())

			sources, err := store.Sources(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(sources["acme-flow"]).To(Equal(domain.SourceAnnounced))
		})

		It("reports each tier under its own word", func() {
			Expect(write("by-hand", domain.SharedAdminActor, domain.NoRevision)).To(Succeed())
			Expect(write("seeded", domain.PreloadActor, 1)).To(Succeed())
			Expect(write("announced", "pub_7f3a91c4", 2)).To(Succeed())

			sources, err := store.Sources(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(sources).To(Equal(map[string]string{
				"by-hand":   domain.SourceCurated,
				"seeded":    domain.SourcePreload,
				"announced": domain.SourceAnnounced,
			}))
		})
	})

	Context("what does not count as creating an entry", func() {
		It("ignores a refused write, which created nothing", func() {
			// A stale write from a publisher is audited as refused. Reading
			// it as the creating row would credit the entry to whoever
			// failed to write it.
			Expect(write("example-plugin", domain.PreloadActor, domain.NoRevision)).To(Succeed())
			Expect(write("example-plugin", "pub_7f3a91c4", 99)).NotTo(Succeed())

			sources, err := store.Sources(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(sources["example-plugin"]).To(Equal(domain.SourcePreload))
		})

		It("ignores the trust table's rows, which share the audit trail", func() {
			// One table, two revision counters (BR-AS38). A publisher-key
			// write names a publisher in entry_id, and reading it here would
			// invent an entry that does not exist.
			Expect(write("example-plugin", domain.SharedAdminActor, domain.NoRevision)).To(Succeed())
			/* Keyed on the trust table's own counter, which the truncate
			   above deliberately does not reset: it is a different revision
			   from the plugin document's, which is the very thing this spec
			   is about. */
			trust, err := store.Publishers(ctx)
			Expect(err).NotTo(HaveOccurred())
			_, err = store.ApplyPublisher(ctx, domain.PublisherWrite{
				Op: domain.OpPublisherUpsert, PublisherID: "acme", Actor: domain.SharedAdminActor,
				Publisher:  &domain.Publisher{ID: "acme"},
				IfRevision: trust.Revision,
			})
			Expect(err).NotTo(HaveOccurred())

			sources, err := store.Sources(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(sources).To(HaveKey("example-plugin"))
			Expect(sources).NotTo(HaveKey("acme"))
		})

		It("says nothing at all about an id it has no accepted row for", func() {
			// Absent, not defaulted. The caller maps a missing id to
			// SourceUnknown; the store never guesses one.
			sources, err := store.Sources(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(sources).To(BeEmpty())
		})
	})
})
