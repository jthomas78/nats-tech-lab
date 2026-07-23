package refdata_test

import (
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata/internal/domain"
)

var _ = Describe("Corpus Versioning and Template Inheritance Rules", func() {
	Context("context hierarchy traversal", func() {
		It("returns a child-first ancestor chain and rejects a cycle", func() {
			contexts := map[string]domain.Context{
				"global":    {Context: "global"},
				"emea":      {Context: "emea", Parent: "global"},
				"emea-acme": {Context: "emea-acme", Parent: "emea"},
			}
			chain, err := domain.AncestorChain("emea-acme", contexts)
			Expect(err).NotTo(HaveOccurred())
			Expect(chain).To(Equal([]string{"emea-acme", "emea", "global"}))
			contexts["global"] = domain.Context{Context: "global", Parent: "emea-acme"}
			_, err = domain.AncestorChain("emea-acme", contexts)
			Expect(errors.Is(err, domain.ErrContextCycle)).To(BeTrue())
		})
	})

	Context("BR-V01 and BR-V02: draft lifecycle", func() {
		It("allows at most one draft and publishes only a draft", func() {
			versions := []domain.CorpusVersion{{Context: "global", Version: 1, Status: domain.CorpusDraft}}
			Expect(errors.Is(domain.CanCreateDraft(versions), domain.ErrDraftAlreadyExists)).To(BeTrue())
			Expect(domain.CorpusVersion{Status: domain.CorpusDraft}.CanPublish()).To(Succeed())
			Expect(errors.Is((domain.CorpusVersion{Status: domain.CorpusPublished}).CanPublish(), domain.ErrOnlyDraftCanPublish)).To(BeTrue())
		})
	})

	Context("BR-V04 and BR-V05: rollback", func() {
		It("accepts only a published rollback target and never changes its version", func() {
			target := domain.CorpusVersion{Context: "emea", Version: 2, Status: domain.CorpusPublished}
			Expect(domain.CanRollbackTo(target)).To(Succeed())
			Expect(target.Version).To(Equal(2))
			Expect(errors.Is(domain.CanRollbackTo(domain.CorpusVersion{Status: domain.CorpusDraft}), domain.ErrRollbackTargetNotPublic)).To(BeTrue())
		})
	})

	Context("BR-V06 through BR-V08: flattened inheritance", func() {
		It("propagates a grandparent change unless an intermediate context overrides it", func() {
			chain := []string{"emea-acme", "emea", "global"}
			locals := map[string][]domain.DictionaryItem{
				"global": {{TypeKey: "currency", Code: "EUR", Context: "global", Attrs: map[string]any{"symbol": "€"}}},
				"emea":   {{TypeKey: "currency", Code: "USD", Context: "emea"}},
			}
			flat := domain.FlattenCorpus(chain, locals)
			Expect(flat).To(ContainElement(And(HaveField("Code", "EUR"), HaveField("SourceContext", "global"), HaveField("IsOverride", false))))

			locals["emea"] = append(locals["emea"], domain.DictionaryItem{TypeKey: "currency", Code: "EUR", Context: "emea", Attrs: map[string]any{"symbol": "EUR"}})
			flat = domain.FlattenCorpus(chain, locals)
			Expect(flat).To(ContainElement(And(HaveField("Code", "EUR"), HaveField("SourceContext", "emea"), HaveField("IsOverride", true))))
		})

		It("rejects deletion of an inherited item and does not publish descendants automatically", func() {
			inherited := domain.CorpusItem{DictionaryItem: domain.DictionaryItem{Context: "global", TypeKey: "currency", Code: "EUR"}, SourceContext: "global"}
			Expect(errors.Is(domain.CanDeleteLocalItem(inherited, "emea-acme"), domain.ErrCannotDeleteInheritedItem)).To(BeTrue())
			Expect(domain.CorpusVersion{Context: "emea-acme", Version: 4, Status: domain.CorpusPublished}.Version).To(Equal(4))
		})
	})
})
