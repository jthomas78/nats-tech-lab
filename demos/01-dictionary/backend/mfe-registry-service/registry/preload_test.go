package registry_test

// Phase 8a — preload seeding. Derived from BR-AS41, BR-AS42 and decisions
// 75, 81, 82, 86 and 89, not from the implementation.
//
// Preload is a fallback tier, not a competing source of truth: it answers
// "this store has never heard of that id" and nothing else. Every rule below
// is true without a database, because every one of them is a decision about
// the registry rather than about Postgres.

import (
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/domain"
)

const (
	originA = "http://localhost:7111"
	originB = "http://localhost:7113"
	rogue   = "http://evil.example.com"
)

func preloadAllowlist() domain.Allowlist {
	return domain.NewAllowlist([]string{originA, originB})
}

var _ = Describe("Preload seeding", func() {
	Context("BR-AS41: preload never reverts curation", func() {
		It("seeds an id the store has never seen", func() {
			out := domain.PlanPreload(nil, []domain.Entry{federated("example-plugin", originA+"/remoteEntry.js")}, preloadAllowlist())
			Expect(idsOf(out.Seed)).To(Equal([]string{"example-plugin"}))
			Expect(out.Skipped).To(BeEmpty())
		})

		It("leaves an id the store already holds untouched, whatever the file says", func() {
			stored := federated("example-plugin", originA+"/remoteEntry.js")
			stored.Name = "Renamed by the operator"

			incoming := federated("example-plugin", originB+"/remoteEntry.js")
			incoming.Name = "Whatever the file says"

			out := domain.PlanPreload([]domain.Entry{stored}, []domain.Entry{incoming}, preloadAllowlist())
			Expect(out.Seed).To(BeEmpty())
			Expect(out.Skipped).To(Equal([]string{"example-plugin"}))
		})

		It("does not re-create an id the operator disabled", func() {
			stored := federated("example-plugin", originA+"/remoteEntry.js")
			stored.Enabled = false

			out := domain.PlanPreload([]domain.Entry{stored}, []domain.Entry{federated("example-plugin", originA+"/remoteEntry.js")}, preloadAllowlist())
			Expect(out.Seed).To(BeEmpty())
			Expect(out.Skipped).To(ContainElement("example-plugin"))
		})

		It("is idempotent: a second run after a restart seeds nothing", func() {
			file := []domain.Entry{federated("example-plugin", originA+"/remoteEntry.js")}
			first := domain.PlanPreload(nil, file, preloadAllowlist())
			second := domain.PlanPreload(first.Seed, file, preloadAllowlist())
			Expect(second.Seed).To(BeEmpty())
		})
	})

	Context("decision 81: an off-allowlist origin is refused per entry, never fatally", func() {
		It("withholds only the offending entry and seeds the rest", func() {
			out := domain.PlanPreload(nil, []domain.Entry{
				federated("good-one", originA+"/remoteEntry.js"),
				federated("bad-one", rogue+"/remoteEntry.js"),
				federated("good-two", originB+"/remoteEntry.js"),
			}, preloadAllowlist())

			Expect(idsOf(out.Seed)).To(ConsistOf("good-one", "good-two"))
			Expect(out.Withheld).To(HaveLen(1))
			Expect(out.Withheld[0].ID).To(Equal("bad-one"))
		})

		It("names the id and the cause, so a skipped entry is visible rather than silent", func() {
			out := domain.PlanPreload(nil, []domain.Entry{federated("bad-one", rogue+"/remoteEntry.js")}, preloadAllowlist())
			Expect(out.Withheld).To(HaveLen(1))
			Expect(errors.Is(out.Withheld[0].Cause, domain.ErrOriginNotAllowed)).To(BeTrue())
		})
	})

	Context("decision 86: the registration path supplies the lifecycle class", func() {
		It("marks every seeded entry static, whether or not the file says so", func() {
			out := domain.PlanPreload(nil, []domain.Entry{federated("example-plugin", originA+"/remoteEntry.js")}, preloadAllowlist())
			Expect(out.Seed).To(HaveLen(1))
			Expect(out.Seed[0].Lifecycle).To(Equal(domain.LifecycleStatic))
		})
	})

	Context("BR-AS42: every write names its true actor", func() {
		It("files a preload insert under the preload actor, never the shared admin identity", func() {
			Expect(domain.PreloadActor).To(Equal("preload"))
			Expect(domain.PreloadActor).ToNot(Equal(domain.SharedAdminActor))
		})

		It("builds a write whose actor is preload and whose op is a normal upsert", func() {
			w := domain.PreloadWrite(federated("example-plugin", originA+"/remoteEntry.js"), domain.NoRevision)
			Expect(w.Actor).To(Equal(domain.PreloadActor))
			Expect(w.Op).To(Equal(domain.OpUpsert))
			Expect(w.Validate()).To(Succeed())
		})
	})

	Context("decisions 82 and 86: the preload file asserts nothing the platform owns", func() {
		It("parses a file carrying schemaVersion and plugins", func() {
			file, err := domain.ParsePreload([]byte(`{"schemaVersion":1,"plugins":[{"id":"a","enabled":true}]}`))
			Expect(err).ToNot(HaveOccurred())
			Expect(file.Plugins).To(HaveLen(1))
		})

		It("refuses a file carrying a revision, rather than ignoring it", func() {
			_, err := domain.ParsePreload([]byte(`{"schemaVersion":1,"revision":"dev-1b","plugins":[]}`))
			Expect(errors.Is(err, domain.ErrPreloadRevision)).To(BeTrue())
		})

		It("refuses an entry that states its own lifecycle", func() {
			_, err := domain.ParsePreload([]byte(`{"schemaVersion":1,"plugins":[{"id":"a","lifecycle":"dynamic"}]}`))
			Expect(errors.Is(err, domain.ErrSelfAssertedField)).To(BeTrue())
		})

		It("refuses an entry that states its own source", func() {
			_, err := domain.ParsePreload([]byte(`{"schemaVersion":1,"plugins":[{"id":"a","source":"curated"}]}`))
			Expect(errors.Is(err, domain.ErrSelfAssertedField)).To(BeTrue())
		})

		It("permits a backend approval, which is the operator's answer in the operator's own file", func() {
			// Same reasoning as enabled: the preload file IS the operator
			// speaking, so it may carry the approval a manifest may not
			// (BR-AS62). A static entry is curated the same way a dynamic one
			// is, which is the point of moving the map here.
			file, err := domain.ParsePreload([]byte(`{"schemaVersion":1,"plugins":[{"id":"a","backendServices":["pricing-service"],"approvedBackendServices":["pricing-service"]}]}`))
			Expect(err).ToNot(HaveOccurred())
			Expect(file.Plugins[0].BackendServices).To(Equal([]string{"pricing-service"}))
			Expect(file.Plugins[0].ApprovedBackendServices).To(Equal([]string{"pricing-service"}))
		})

		It("permits enabled, which is the operator's field to set in their own file", func() {
			file, err := domain.ParsePreload([]byte(`{"schemaVersion":1,"plugins":[{"id":"a","enabled":true}]}`))
			Expect(err).ToNot(HaveOccurred())
			Expect(file.Plugins[0].Enabled).To(BeTrue())
		})

		It("refuses a schemaVersion it does not write", func() {
			_, err := domain.ParsePreload([]byte(`{"schemaVersion":2,"plugins":[]}`))
			Expect(errors.Is(err, domain.ErrPreloadSchemaVersion)).To(BeTrue())
		})
	})
})

func idsOf(entries []domain.Entry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.ID)
	}
	return out
}
