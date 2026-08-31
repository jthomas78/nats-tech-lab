package registry_test

// Phase 8c — the source badge. Derived from BR-AS42, BR-AS43 and decision 80.
//
// The badge answers "how did this entry get here", and the whole of the
// design is that it is DERIVED, never stored. Two rules follow, and both are
// about refusing to let something else answer the question:
//
//   · the entry itself never says (BR-AS43 — a stored source would be a
//     publisher's claim about its own trust path), and
//   · the audit row that answers is the FIRST accepted one, never the last
//     (decision 80 — an operator disabling an announced plugin has not
//     turned it into a curated one).

import (
	"encoding/json"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/domain"
)

var _ = Describe("BR-AS42/decision 80 — where an entry came from", func() {
	Context("the creating actor names the tier", func() {
		It("reads the shared operator identity as curation", func() {
			Expect(domain.SourceOf(domain.SharedAdminActor)).To(Equal(domain.SourceCurated))
		})

		It("reads the preload actor as preload", func() {
			Expect(domain.SourceOf(domain.PreloadActor)).To(Equal(domain.SourcePreload))
		})

		It("reads any publisher key as an announcement", func() {
			// Open set, so this is the default and not a third literal: a
			// publisher key nobody has seen before must read as announced,
			// never as unknown.
			Expect(domain.SourceOf("pub_7f3a91c4")).To(Equal(domain.SourceAnnounced))
			Expect(domain.SourceOf("acme-freight")).To(Equal(domain.SourceAnnounced))
		})
	})

	Context("what it refuses to guess", func() {
		It("says unknown when there is no creating actor to read", func() {
			// A row older than the audit trail, or one whose history aged
			// out. Defaulting to curated would dress up exactly the case an
			// operator most needs to see.
			Expect(domain.SourceOf("")).To(Equal(domain.SourceUnknown))
		})

		It("is not a field an entry can carry", func() {
			// BR-AS43 in one assertion: there is nowhere on Entry for a
			// publisher to assert this, so the projection cannot be talked
			// into agreeing with a manifest.
			entry := federated("example-plugin", originA+"/remoteEntry.js")
			body, err := json.Marshal(entry)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(body)).NotTo(ContainSubstring("source"))
		})
	})
})
