package registry_test

// BR-AS85 — the registry carries what the shell admits, and holds the same
// door.
//
// The round-trip half. A registry is a CARRIER of a publisher's manifest, not
// an author of one, so a group re-encodes in the form it was written: a
// publisher who wrote the shorthand string does not get an object back. This
// is not cosmetic. Drift compares a curated entry against the manifest it was
// made from by re-marshalling both sides (application/drift.go), so any
// rewriting here that is not a pure function of what was written shows up as
// permanent, unexplainable drift on `contributions`.

import (
	"encoding/json"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/domain"
)

// navOf decodes one navigation contribution from its JSON text.
func navOf(body string) (domain.Contribution, error) {
	var c domain.Contribution
	err := json.Unmarshal([]byte(body), &c)
	return c, err
}

var _ = Describe("BR-AS85: a navigation group survives the registry unchanged", func() {
	Context("the object form", func() {
		It("decodes into an identity and a separate display label", func() {
			c, err := navOf(`{"kind":"navigation","id":"n","label":"L","route":"r","group":{"id":"jetstream","label":"JetStream"}}`)
			Expect(err).NotTo(HaveOccurred())
			Expect(c.Group).NotTo(BeNil())
			Expect(c.Group.ID).To(Equal("jetstream"))
			Expect(c.Group.Label).To(Equal("JetStream"))
		})

		It("re-encodes as an object, not as a string", func() {
			c, err := navOf(`{"kind":"navigation","id":"n","label":"L","route":"r","group":{"id":"jetstream","label":"JetStream"}}`)
			Expect(err).NotTo(HaveOccurred())
			out, err := json.Marshal(c)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(out)).To(ContainSubstring(`"group":{"id":"jetstream","label":"JetStream"}`))
		})
	})

	Context("the string shorthand", func() {
		It("decodes verbatim into both fields, without being held to the id pattern", func() {
			// Slugging the spelling would make merging depend on the
			// spelling, which is the thing BR-AS83 exists to prevent.
			c, err := navOf(`{"kind":"navigation","id":"n","label":"L","route":"r","group":"Features"}`)
			Expect(err).NotTo(HaveOccurred())
			Expect(c.Group.ID).To(Equal("Features"))
			Expect(c.Group.Label).To(Equal("Features"))
		})

		It("re-encodes as the same string, never promoted to an object", func() {
			c, err := navOf(`{"kind":"navigation","id":"n","label":"L","route":"r","group":"Features"}`)
			Expect(err).NotTo(HaveOccurred())
			out, err := json.Marshal(c)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(out)).To(ContainSubstring(`"group":"Features"`))
			Expect(string(out)).NotTo(ContainSubstring(`"group":{`))
		})
	})

	Context("what is dropped and what is refused", func() {
		It("drops an unrecognised key inside the object exactly as the shell drops it", func() {
			// A registry stricter than the shell would refuse a plugin the
			// shell admits, which is the build/registry split this rule
			// exists to close. BR-AS07 already forbids the plugin choosing
			// placement, so the key changes nothing and costs nothing.
			c, err := navOf(`{"kind":"navigation","id":"n","label":"L","route":"r","group":{"id":"a","label":"A","order":5}}`)
			Expect(err).NotTo(HaveOccurred())
			out, err := json.Marshal(c)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(out)).To(ContainSubstring(`"group":{"id":"a","label":"A"}`))
			Expect(string(out)).NotTo(ContainSubstring("order"))
		})

		It("refuses a group that is neither a string nor an object", func() {
			_, err := navOf(`{"kind":"navigation","id":"n","label":"L","route":"r","group":7}`)
			Expect(err).To(HaveOccurred())
		})

		It("leaves an absent group absent, and emits no group key", func() {
			c, err := navOf(`{"kind":"navigation","id":"n","label":"L","route":"r"}`)
			Expect(err).NotTo(HaveOccurred())
			Expect(c.Group).To(BeNil())
			out, err := json.Marshal(c)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(out)).NotTo(ContainSubstring("group"))
		})

		It("treats an explicit null as no group at all", func() {
			c, err := navOf(`{"kind":"navigation","id":"n","label":"L","route":"r","group":null}`)
			Expect(err).NotTo(HaveOccurred())
			Expect(c.Group).To(BeNil())
		})
	})

	Context("the default route declaration", func() {
		It("decodes and re-encodes a declared default", func() {
			c, err := navOf(`{"kind":"route","id":"r","path":"/p/r","title":"T","default":true}`)
			Expect(err).NotTo(HaveOccurred())
			Expect(c.Default).To(BeTrue())
			out, err := json.Marshal(c)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(out)).To(ContainSubstring(`"default":true`))
		})

		It("emits no default key when none was declared", func() {
			c, err := navOf(`{"kind":"route","id":"r","path":"/p/r","title":"T"}`)
			Expect(err).NotTo(HaveOccurred())
			Expect(c.Default).To(BeFalse())
			out, err := json.Marshal(c)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(out)).NotTo(ContainSubstring("default"))
		})
	})

	It("round-trips a whole entry through JSON unchanged, twice", func() {
		// Idempotence is what the unsigned Postgres projection needs: the
		// column is written with json.Marshal and read back with Unmarshal,
		// so a second pass that differs from the first would drift forever.
		e := admissibleEntry()
		e.Contributions[1].Group = &domain.NavGroup{ID: "jetstream", Label: "JetStream"}
		e.Contributions[0].Default = true

		first, err := json.Marshal(e)
		Expect(err).NotTo(HaveOccurred())
		var back domain.Entry
		Expect(json.Unmarshal(first, &back)).To(Succeed())
		second, err := json.Marshal(back)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(second)).To(Equal(string(first)))
		Expect(back.Contributions[1].Group.Label).To(Equal("JetStream"))
		Expect(back.Contributions[0].Default).To(BeTrue())
	})
})
