package registry_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/domain"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/postgres"
)

// BR-AS52 — lifecycle is explicit and preserved. Phase 8 already stores the
// class; Phase 5a is about the two ends the storage did not answer: what a
// row written before the column existed means, and what an operator is
// allowed to write into it.
var _ = Describe("BR-AS52 — the lifecycle an operator may write", func() {
	Context("a curated write states the class", func() {
		DescribeTable("accepts only the two classes the shell knows",
			func(lifecycle string, ok bool) {
				err := domain.ValidateLifecycle(lifecycle)
				if ok {
					Expect(err).NotTo(HaveOccurred())
					return
				}
				Expect(err).To(MatchError(domain.ErrUnknownLifecycle))
			},
			Entry("static", domain.LifecycleStatic, true),
			Entry("dynamic", domain.LifecycleDynamic, true),
			// Empty is legal at the door and means "unclassified"; the
			// classification happens once, in the store, so an old row and a
			// new write cannot end up meaning different things.
			Entry("unclassified", "", true),
			Entry("a class the shell has no behavior for", "hot", false),
			Entry("a near miss", "Static", false),
		)
	})

	Context("an unclassified entry is read as static", func() {
		It("classifies an empty lifecycle rather than leaving the shell to guess", func() {
			Expect(domain.LifecycleOf(domain.Entry{})).To(Equal(domain.LifecycleStatic))
		})
		It("never rewrites a class that was stated", func() {
			Expect(domain.LifecycleOf(domain.Entry{Lifecycle: domain.LifecycleDynamic})).
				To(Equal(domain.LifecycleDynamic))
		})
	})
})

var _ = Describe("BR-AS52 — backfilling a legacy row", func() {
	var ctx context.Context
	allowed := domain.NewAllowlist([]string{"http://localhost:7110"})
	BeforeEach(func() {
		if pgUnavailable != "" {
			Skip(pgUnavailable)
		}
		ctx = context.Background()
		_, err := pgDB.ExecContext(ctx, `TRUNCATE registry.entries, registry.audit; UPDATE registry.revision SET revision = 0`)
		Expect(err).NotTo(HaveOccurred())
	})

	// Written the way a pre-lifecycle row actually looks: the column exists
	// with its empty default, and everything else about the row is intact.
	// Base64 of the entry body: the store reads these bytes back as the
	// signed manifest, so a placeholder would fail for the wrong reason.
	const signedBytes = `eyJpZCI6ImxlZ2FjeSIsIm5hbWUiOiJMZWdhY3kiLCJzY2hlbWFWZXJzaW9uIjoxLCJzaGVsbEFwaVZlcnNpb24iOjEsInJlbW90ZSI6eyJraW5kIjoiZmVkZXJhdGVkIiwidXJsIjoiaHR0cDovL2xvY2FsaG9zdDo3MTEwL3IuanMiLCJtb2R1bGUiOiIuL1AifSwiY29udHJpYnV0aW9ucyI6W119`
	legacy := func() {
		_, err := pgDB.ExecContext(ctx,
			`INSERT INTO registry.entries (id, enabled, entry, lifecycle, manifest, signature, signing_key, withheld)
			 VALUES ('legacy', true, $1, '', '`+signedBytes+`', 'SIG', 'KEY', false)`,
			`{"id":"legacy","name":"Legacy","schemaVersion":1,"shellApiVersion":1,"remote":{"kind":"federated","url":"http://localhost:7110/r.js","module":"./P"},"contributions":[]}`)
		Expect(err).NotTo(HaveOccurred())
	}

	It("classifies an unclassified row as static", func() {
		legacy()
		Expect(postgres.Migrate(ctx, pgDB)).To(Succeed())
		var lifecycle string
		Expect(pgDB.QueryRowContext(ctx, `SELECT lifecycle FROM registry.entries WHERE id='legacy'`).Scan(&lifecycle)).To(Succeed())
		Expect(lifecycle).To(Equal(domain.LifecycleStatic))
	})

	It("leaves enablement, withholding and the signed bytes exactly as they were", func() {
		legacy()
		Expect(postgres.Migrate(ctx, pgDB)).To(Succeed())
		var enabled, withheld bool
		var manifest, signature, key string
		Expect(pgDB.QueryRowContext(ctx,
			`SELECT enabled, withheld, manifest, signature, signing_key FROM registry.entries WHERE id='legacy'`).
			Scan(&enabled, &withheld, &manifest, &signature, &key)).To(Succeed())
		Expect(enabled).To(BeTrue())
		Expect(withheld).To(BeFalse())
		// The point of the rule: a backfill is a classification, not a
		// re-publication. Touching these bytes would invalidate a signature
		// nobody asked to change.
		Expect(manifest).To(Equal(signedBytes))
		Expect(signature).To(Equal("SIG"))
		Expect(key).To(Equal("KEY"))
	})

	It("does not change the registry revision", func() {
		legacy()
		var before int64
		Expect(pgDB.QueryRowContext(ctx, `SELECT revision FROM registry.revision`).Scan(&before)).To(Succeed())
		Expect(postgres.Migrate(ctx, pgDB)).To(Succeed())
		var after int64
		Expect(pgDB.QueryRowContext(ctx, `SELECT revision FROM registry.revision`).Scan(&after)).To(Succeed())
		// A migration is not a curated change: a shell holding the old
		// revision has nothing new to load.
		Expect(after).To(Equal(before))
	})

	It("leaves a stated class alone", func() {
		_, err := pgDB.ExecContext(ctx,
			`INSERT INTO registry.entries (id, enabled, entry, lifecycle) VALUES ('announced', true, '{"id":"announced"}', 'dynamic')`)
		Expect(err).NotTo(HaveOccurred())
		Expect(postgres.Migrate(ctx, pgDB)).To(Succeed())
		var lifecycle string
		Expect(pgDB.QueryRowContext(ctx, `SELECT lifecycle FROM registry.entries WHERE id='announced'`).Scan(&lifecycle)).To(Succeed())
		Expect(lifecycle).To(Equal(domain.LifecycleDynamic))
	})

	It("serves the backfilled class through the store", func() {
		legacy()
		Expect(postgres.Migrate(ctx, pgDB)).To(Succeed())
		doc, err := postgres.NewStore(pgDB, allowed).Current(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(doc.Entries).To(HaveLen(1))
		Expect(doc.Entries[0].Lifecycle).To(Equal(domain.LifecycleStatic))
	})
})
