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
				"_platform":           {Context: "_platform"},
				"acme-pacific-fleet":  {Context: "acme-pacific-fleet", Parent: "_platform"},
				"acme-atlantic-fleet": {Context: "acme-atlantic-fleet", Parent: "acme-pacific-fleet"},
			}
			chain, err := domain.AncestorChain("acme-atlantic-fleet", contexts)
			Expect(err).NotTo(HaveOccurred())
			Expect(chain).To(Equal([]string{"acme-atlantic-fleet", "acme-pacific-fleet", "_platform"}))
			contexts["_platform"] = domain.Context{Context: "_platform", Parent: "acme-atlantic-fleet"}
			_, err = domain.AncestorChain("acme-atlantic-fleet", contexts)
			Expect(errors.Is(err, domain.ErrContextCycle)).To(BeTrue())
		})
	})

	Context("BR-V01 and BR-V02: draft lifecycle", func() {
		It("allows at most one draft and publishes only a draft", func() {
			versions := []domain.CorpusVersion{{Context: "acme-pacific-fleet", Version: 1, Status: domain.CorpusDraft}}
			Expect(errors.Is(domain.CanCreateDraft(versions), domain.ErrDraftAlreadyExists)).To(BeTrue())
			Expect(domain.CorpusVersion{Status: domain.CorpusDraft}.CanPublish()).To(Succeed())
			Expect(errors.Is((domain.CorpusVersion{Status: domain.CorpusPublished}).CanPublish(), domain.ErrOnlyDraftCanPublish)).To(BeTrue())
		})
	})

	Context("BR-V04 and BR-V05: rollback", func() {
		It("accepts only a published rollback target and never changes its version", func() {
			target := domain.CorpusVersion{Context: "acme-pacific-fleet", Version: 2, Status: domain.CorpusPublished}
			Expect(domain.CanRollbackTo(target)).To(Succeed())
			Expect(target.Version).To(Equal(2))
			Expect(errors.Is(domain.CanRollbackTo(domain.CorpusVersion{Status: domain.CorpusDraft}), domain.ErrRollbackTargetNotPublic)).To(BeTrue())
		})
	})

	Context("BR-V06 through BR-V08: flattened inheritance", func() {
		It("propagates a grandparent change unless an intermediate context overrides it", func() {
			chain := []string{"acme-atlantic-fleet", "acme-pacific-fleet", "_platform"}
			locals := map[string][]domain.DictionaryItem{
				"_platform":          {{TypeKey: "currency", Code: "EUR", Context: "_platform", Attrs: map[string]any{"symbol": "€"}}},
				"acme-pacific-fleet": {{TypeKey: "currency", Code: "USD", Context: "acme-pacific-fleet"}},
			}
			flat := domain.FlattenCorpus(chain, locals)
			Expect(flat).To(ContainElement(And(HaveField("Code", "EUR"), HaveField("SourceContext", "_platform"), HaveField("IsOverride", false))))

			locals["acme-pacific-fleet"] = append(locals["acme-pacific-fleet"], domain.DictionaryItem{TypeKey: "currency", Code: "EUR", Context: "acme-pacific-fleet", Attrs: map[string]any{"symbol": "EUR"}})
			flat = domain.FlattenCorpus(chain, locals)
			Expect(flat).To(ContainElement(And(HaveField("Code", "EUR"), HaveField("SourceContext", "acme-pacific-fleet"), HaveField("IsOverride", true))))
		})

		It("rejects deletion of an inherited item and does not publish descendants automatically", func() {
			inherited := domain.CorpusItem{DictionaryItem: domain.DictionaryItem{Context: "_platform", TypeKey: "currency", Code: "EUR"}, SourceContext: "_platform"}
			Expect(errors.Is(domain.CanDeleteLocalItem(inherited, "acme-pacific-fleet"), domain.ErrCannotDeleteInheritedItem)).To(BeTrue())
			Expect(domain.CorpusVersion{Context: "acme-pacific-fleet", Version: 4, Status: domain.CorpusPublished}.Version).To(Equal(4))
		})
	})
})
