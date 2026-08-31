package registry_test

// Phase 8b — announcement. Derived from BR-AS39, BR-AS40, BR-AS42, BR-AS43
// and decisions 72, 74, 77 and 86.
//
// The one thing these specs cannot exercise end to end is signature
// verification: the trust anchor is Phase 7's and does not exist. What they
// do pin is the seam — nothing verifies by default, so an unsigned or
// unverifiable announcement is refused rather than waved through while the
// anchor is missing.

import (
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/domain"
)

func announced(id, url string) domain.Entry {
	e := federated(id, url)
	e.Enabled = false
	e.Lifecycle = domain.LifecycleDynamic
	return e
}

func enabledDynamic(id, url string) domain.Entry {
	e := federated(id, url)
	e.Enabled = true
	e.Lifecycle = domain.LifecycleDynamic
	return e
}

func enabledStatic(id, url string) domain.Entry {
	e := federated(id, url)
	e.Enabled = true
	e.Lifecycle = domain.LifecycleStatic
	return e
}

var _ = Describe("Announcement", func() {
	Context("BR-AS39: an announcement never activates", func() {
		It("stores an unknown id as announced and disabled", func() {
			out, entry := domain.DecideAnnounce(nil, federated("new-plugin", originA+"/remoteEntry.js"))
			Expect(out).To(Equal(domain.AnnounceInserted))
			Expect(entry.Enabled).To(BeFalse())
		})

		It("marks a newly announced entry dynamic, per decision 86", func() {
			_, entry := domain.DecideAnnounce(nil, federated("new-plugin", originA+"/remoteEntry.js"))
			Expect(entry.Lifecycle).To(Equal(domain.LifecycleDynamic))
		})

		It("cannot be talked into enabling itself, whatever the payload claims", func() {
			claiming := federated("new-plugin", originA+"/remoteEntry.js")
			claiming.Enabled = true
			_, entry := domain.DecideAnnounce(nil, claiming)
			Expect(entry.Enabled).To(BeFalse())
		})

		It("leaves an announced entry out of the document a shell may see", func() {
			_, entry := domain.DecideAnnounce(nil, federated("new-plugin", originA+"/remoteEntry.js"))
			doc := domain.Document{SchemaVersion: domain.SchemaVersion, Revision: 1, Entries: []domain.Entry{entry}}
			Expect(doc.Readable(preloadAllowlist()).Entries).To(BeEmpty())
		})

		It("re-announcing a still-pending entry updates the record and leaves it pending", func() {
			existing := announced("new-plugin", originA+"/remoteEntry.js")
			out, entry := domain.DecideAnnounce(&existing, federated("new-plugin", originA+"/remoteEntry.js"))
			Expect(out).To(Equal(domain.AnnouncePending))
			Expect(entry.Enabled).To(BeFalse())
		})
	})

	Context("BR-AS40: an enabled dynamic id re-announcing is followed within its origin", func() {
		It("applies a same-origin remote change without review", func() {
			existing := enabledDynamic("plugin", originA+"/remoteEntry.js")
			out, entry := domain.DecideAnnounce(&existing, federated("plugin", originA+"/v2/remoteEntry.js"))
			Expect(out).To(Equal(domain.AnnounceUpdated))
			Expect(entry.Enabled).To(BeTrue())
			Expect(entry.Remote.URL).To(Equal(originA + "/v2/remoteEntry.js"))
		})

		It("re-queues a cross-origin move and withholds it until an operator re-enables", func() {
			existing := enabledDynamic("plugin", originA+"/remoteEntry.js")
			out, entry := domain.DecideAnnounce(&existing, federated("plugin", originB+"/remoteEntry.js"))
			Expect(out).To(Equal(domain.AnnounceRequeued))
			Expect(entry.Enabled).To(BeFalse())
		})

		It("treats a scheme or port change as a different origin", func() {
			existing := enabledDynamic("plugin", "https://localhost:7111/remoteEntry.js")
			out, _ := domain.DecideAnnounce(&existing, federated("plugin", "http://localhost:7111/remoteEntry.js"))
			Expect(out).To(Equal(domain.AnnounceRequeued))
		})
	})

	Context("decision 77: a static entry outranks an announcement, always", func() {
		It("ignores an announcement for an enabled static id and changes nothing", func() {
			existing := enabledStatic("example-plugin", originA+"/remoteEntry.js")
			out, entry := domain.DecideAnnounce(&existing, federated("example-plugin", originA+"/v2/remoteEntry.js"))
			Expect(out).To(Equal(domain.AnnounceIgnored))
			Expect(entry.Remote.URL).To(Equal(originA + "/remoteEntry.js"))
		})

		It("beats BR-AS40 even on a same-origin change, which decision 74 alone would have applied", func() {
			static := enabledStatic("example-plugin", originA+"/remoteEntry.js")
			dynamic := enabledDynamic("other-plugin", originA+"/remoteEntry.js")
			incoming := federated("x", originA+"/v2/remoteEntry.js")

			incoming.ID = static.ID
			staticOut, _ := domain.DecideAnnounce(&static, incoming)
			incoming.ID = dynamic.ID
			dynamicOut, _ := domain.DecideAnnounce(&dynamic, incoming)

			Expect(staticOut).To(Equal(domain.AnnounceIgnored))
			Expect(dynamicOut).To(Equal(domain.AnnounceUpdated))
		})

		It("ignores rather than drops, so the publisher can be shown why nothing happened", func() {
			existing := enabledStatic("example-plugin", originA+"/remoteEntry.js")
			out, _ := domain.DecideAnnounce(&existing, federated("example-plugin", originA+"/v2/remoteEntry.js"))
			Expect(out).ToNot(Equal(domain.AnnounceInserted))
			Expect(out.Recorded()).To(BeTrue())
		})
	})

	Context("BR-AS43: a manifest never carries its own trust tier", func() {
		DescribeTable("refuses a self-asserted field rather than ignoring it",
			func(payload string) {
				_, err := domain.ParseManifest([]byte(payload))
				Expect(errors.Is(err, domain.ErrSelfAssertedField)).To(BeTrue())
			},
			Entry("source", `{"id":"a","source":"announced"}`),
			Entry("lifecycle", `{"id":"a","lifecycle":"static"}`),
			Entry("enabled", `{"id":"a","enabled":true}`),
			Entry("revision", `{"id":"a","revision":3}`),
		)

		It("accepts a manifest that describes only what the plugin is", func() {
			m, err := domain.ParseManifest([]byte(`{"id":"a","name":"A","schemaVersion":1,"shellApiVersion":1,` +
				`"remote":{"kind":"federated","url":"http://localhost:7111/remoteEntry.js","module":"./plugin"},"contributions":[]}`))
			Expect(err).ToNot(HaveOccurred())
			Expect(m.ID).To(Equal("a"))
		})
	})

	Context("BR-AS42: an announcement is filed under its publisher key", func() {
		It("never files an announcement under the shared admin identity or the preload actor", func() {
			w := domain.AnnounceWrite(federated("a", originA+"/remoteEntry.js"), "publisher-key-1", domain.NoRevision)
			Expect(w.Actor).To(Equal("publisher-key-1"))
			Expect(w.Actor).ToNot(Equal(domain.SharedAdminActor))
			Expect(w.Actor).ToNot(Equal(domain.PreloadActor))
			Expect(w.Validate()).To(Succeed())
		})

		It("refuses an announcement carrying no publisher key", func() {
			w := domain.AnnounceWrite(federated("a", originA+"/remoteEntry.js"), "", domain.NoRevision)
			Expect(errors.Is(w.Validate(), domain.ErrNoActor)).To(BeTrue())
		})
	})

	Context("decision 72 / Phase 7 seam: nothing is trusted until a verifier exists", func() {
		It("refuses every announcement while no trust anchor is configured", func() {
			_, err := domain.NoVerifier{}.Verify([]byte(`{"id":"a"}`), "any-signature")
			Expect(errors.Is(err, domain.ErrUnverified)).To(BeTrue())
		})

		It("refuses an unsigned announcement without consulting a verifier at all", func() {
			_, err := domain.VerifyAnnouncement(nil, []byte(`{"id":"a"}`), "")
			Expect(errors.Is(err, domain.ErrUnsigned)).To(BeTrue())
		})

		It("refuses a signed announcement when no verifier is wired, rather than accepting it", func() {
			_, err := domain.VerifyAnnouncement(nil, []byte(`{"id":"a"}`), "a-signature")
			Expect(errors.Is(err, domain.ErrUnverified)).To(BeTrue())
		})
	})
})
