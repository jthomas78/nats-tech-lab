package registry_test

// The Phase 2a rules, derived from BUSINESS_RULES-APP-SHELL.md's BR-AS16 to
// BR-AS24 rather than from the implementation. These exercise the domain
// layer directly: every rule below is a decision about the registry itself,
// not about Postgres, KV, or HTTP, so none of them needs a store to be true.

import (
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/domain"
)

func federated(id, url string) domain.Entry {
	return domain.Entry{
		ID:              id,
		Name:            id,
		SchemaVersion:   domain.SchemaVersion,
		ShellAPIVersion: domain.ShellAPIVersion,
		Enabled:         true,
		Remote:          domain.Remote{Kind: domain.RemoteFederated, URL: url, Module: "./plugin"},
		// Namespaced under the entry's own id: a route outside its plugin's
		// prefix is refused on write (BR-AS12, BR-AS69).
		Contributions: []domain.Contribution{{Kind: "route", ID: "vessels", Path: "/" + id + "/vessels", Title: "Vessels"}},
	}
}

var _ = Describe("Registry rules", func() {
	Context("BR-AS17: revision is server-assigned and monotonic", func() {
		It("starts at 1, so it can never be mistaken for the degraded revision", func() {
			Expect(domain.NextRevision(domain.NoRevision)).To(Equal(int64(1)))
		})

		It("increases by one on every accepted write and never repeats", func() {
			seen := map[int64]bool{}
			rev := domain.NoRevision
			for i := 0; i < 5; i++ {
				rev = domain.NextRevision(rev)
				Expect(seen[rev]).To(BeFalse(), "revision %d served twice", rev)
				seen[rev] = true
			}
			Expect(rev).To(Equal(int64(5)))
		})

		It("reserves 0 for the degraded document alone", func() {
			Expect(domain.DegradedRevision).To(Equal(int64(0)))
			Expect(domain.NextRevision(domain.NoRevision)).To(BeNumerically(">", domain.DegradedRevision))
		})
	})

	Context("BR-AS18: writes are revision-checked", func() {
		It("accepts a write keyed on the revision the writer read", func() {
			Expect(domain.CheckRevision(7, 7)).To(Succeed())
		})

		It("refuses a stale write rather than merging it", func() {
			err := domain.CheckRevision(9, 7)
			Expect(errors.Is(err, domain.ErrStaleRevision)).To(BeTrue())
			var stale domain.StaleRevisionError
			Expect(errors.As(err, &stale)).To(BeTrue())
			// The refusal has to say which revision to reapply on top of —
			// the admin surface reloads onto it (decision 46's fourth screen).
			Expect(stale.Current).To(Equal(int64(9)))
			Expect(stale.Supplied).To(Equal(int64(7)))
		})

		It("refuses a write that claims a revision the registry has not reached", func() {
			// Not a merge case and not a race: a writer quoting a future
			// revision read something this registry never served.
			Expect(errors.Is(domain.CheckRevision(7, 9), domain.ErrStaleRevision)).To(BeTrue())
		})
	})

	Context("BR-AS20: origin allowlist, enforced on write and on read", func() {
		allowlist := domain.NewAllowlist([]string{"http://localhost:7110", "https://plugins.example.com"})

		It("permits a remote on a configured origin, whatever its path", func() {
			Expect(allowlist.Permits("http://localhost:7110/remoteEntry.js")).To(BeTrue())
			Expect(allowlist.Permits("https://plugins.example.com/a/b/remoteEntry.js")).To(BeTrue())
		})

		It("refuses a remote on an unconfigured host, port or scheme", func() {
			Expect(allowlist.Permits("http://localhost:7111/remoteEntry.js")).To(BeFalse())
			Expect(allowlist.Permits("http://evil.example.com/remoteEntry.js")).To(BeFalse())
			// Scheme is part of the origin: an https allowlist entry does not
			// bless the same host over http.
			Expect(allowlist.Permits("http://plugins.example.com/remoteEntry.js")).To(BeFalse())
		})

		It("refuses a URL it cannot parse into an origin at all", func() {
			Expect(allowlist.Permits("")).To(BeFalse())
			Expect(allowlist.Permits("remoteEntry.js")).To(BeFalse())
			Expect(allowlist.Permits("javascript:alert(1)")).To(BeFalse())
		})

		It("refuses everything when no origin is configured", func() {
			// An empty REGISTRY_ALLOWED_ORIGINS is not "allow all". A
			// deployment that forgot to configure it curates nothing, which
			// is the safe direction for a list that decides what code the
			// browser fetches.
			Expect(domain.NewAllowlist(nil).Permits("http://localhost:7110/remoteEntry.js")).To(BeFalse())
		})

		It("refuses the write of an entry whose remote is not allowlisted", func() {
			err := allowlist.Check(federated("rogue", "http://evil.example.com/remoteEntry.js"))
			Expect(errors.Is(err, domain.ErrOriginNotAllowed)).To(BeTrue())
			Expect(allowlist.Check(federated("ok", "http://localhost:7110/remoteEntry.js"))).To(Succeed())
		})

		It("withholds a stored entry the allowlist no longer covers", func() {
			// The case the write-time check cannot cover: the row was written
			// when the origin was configured, and the configuration narrowed
			// afterwards. Withheld, not deleted — the row and its audit trail
			// stay (BR-AS24).
			doc := domain.Document{Revision: 4, Entries: []domain.Entry{
				federated("kept", "http://localhost:7110/remoteEntry.js"),
				federated("narrowed-out", "http://localhost:7111/remoteEntry.js"),
			}}

			readable := doc.Readable(allowlist)

			Expect(readable.Entries).To(HaveLen(1))
			Expect(readable.Entries[0].ID).To(Equal("kept"))
			Expect(readable.Revision).To(Equal(int64(4)), "withholding an entry is not a write and consumes no revision")
			Expect(doc.Entries).To(HaveLen(2), "the stored document is unchanged")
		})

		It("withholds a disabled entry too, and says nothing about why", func() {
			disabled := federated("paused", "http://localhost:7110/remoteEntry.js")
			disabled.Enabled = false
			doc := domain.Document{Revision: 4, Entries: []domain.Entry{disabled}}

			Expect(doc.Readable(allowlist).Entries).To(BeEmpty())
		})
	})

	Context("BR-AS22: the registry degrades, it does not fail", func() {
		It("answers with an empty document that says it is degraded", func() {
			doc := domain.Degraded()

			Expect(doc.Degraded).To(BeTrue())
			Expect(doc.Revision).To(Equal(domain.DegradedRevision))
			Expect(doc.SchemaVersion).To(Equal(domain.SchemaVersion))
			Expect(doc.Entries).To(BeEmpty())
		})

		It("is distinguishable from a registry that genuinely curates nothing", func() {
			// Both serve zero plugins. Only one of them is an outage, and the
			// shell reports them differently (BR-AS04).
			empty := domain.Document{SchemaVersion: domain.SchemaVersion, Revision: 3}

			Expect(empty.Degraded).To(BeFalse())
			Expect(empty.Revision).NotTo(Equal(domain.Degraded().Revision))
		})

		It("serves no substitute catalog", func() {
			// There is no server-side built-in set to fall back to: the
			// shell's built-ins ship inside the shell's own bundle and are
			// deliberately never curated.
			Expect(domain.Degraded().Entries).To(BeEmpty())
		})
	})

	Context("BR-AS24: an entry is disabled, never deleted", func() {
		It("offers a write that disables an entry", func() {
			w := domain.Write{Op: domain.OpSetEnabled, EntryID: "example-plugin", Enabled: false, Actor: domain.SharedAdminActor}
			Expect(w.Validate()).To(Succeed())
		})

		It("offers no write that removes one", func() {
			// Stated as an exhaustive list rather than as the absence of a
			// delete: a new op added without a rule fails here.
			Expect(domain.WriteOps()).To(ConsistOf(domain.OpUpsert, domain.OpSetEnabled))
		})

		It("refuses an op it does not know", func() {
			err := domain.Write{Op: "delete", EntryID: "example-plugin", Actor: domain.SharedAdminActor}.Validate()
			Expect(errors.Is(err, domain.ErrUnknownOp)).To(BeTrue())
		})

		It("refuses a write that names no entry", func() {
			Expect(domain.Write{Op: domain.OpSetEnabled, Actor: domain.SharedAdminActor}.Validate()).NotTo(Succeed())
		})
	})

	Context("BR-AS23: the audit records the surface, not an identity", func() {
		It("refuses an authorless write, so no adapter can omit the actor", func() {
			w := domain.Write{Op: domain.OpSetEnabled, EntryID: "example-plugin"}
			Expect(errors.Is(w.Validate(), domain.ErrNoActor)).To(BeTrue())
		})

		It("names the shared administrative identity, not a person", func() {
			// accounts-service authenticates every request as one shared
			// BasicAuth identity, so this is the strongest true claim. The
			// audit must not imply a per-operator one.
			Expect(domain.SharedAdminActor).To(Equal("admin"))
		})
	})

	Context("entry hygiene, so a bad row cannot reach the shell", func() {
		It("refuses an upsert with no entry body", func() {
			Expect(domain.Write{Op: domain.OpUpsert, EntryID: "x", Actor: domain.SharedAdminActor}.Validate()).NotTo(Succeed())
		})

		It("refuses an upsert whose body disagrees with the id it is filed under", func() {
			w := domain.Write{Op: domain.OpUpsert, EntryID: "x", Actor: domain.SharedAdminActor, Entry: entryPtr(federated("y", "http://localhost:7110/remoteEntry.js"))}
			Expect(errors.Is(w.Validate(), domain.ErrEntryIDMismatch)).To(BeTrue())
		})
	})
})

func entryPtr(e domain.Entry) *domain.Entry { return &e }
