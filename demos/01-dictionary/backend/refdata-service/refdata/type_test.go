package refdata_test

import (
	"context"
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata/internal/application/commands"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata/internal/domain"
)

var _ = Describe("Dictionary Type Domain Rules", func() {
	var (
		ctx   context.Context
		types *fakeTypeRepo
		items *fakeItemRepo
		refs  *fakeReferenceRepo
		th    *commands.TypeHandler
		ih    *commands.ItemHandler
	)

	BeforeEach(func() {
		ctx = context.Background()
		types = newFakeTypeRepo()
		items = newFakeItemRepo()
		refs = newFakeReferenceRepo()
		th = commands.NewTypeHandler(types)
		ih = commands.NewItemHandler(items, refs, nil)
	})

	Context("BR-D09: dictionary types are categorized into a small controlled vocabulary", func() {
		It("registers a type with category standards", func() {
			Expect(th.RegisterType(ctx, domain.DictionaryType{
				TypeKey: "currency", Name: "Currency", Category: domain.CategoryStandards,
			})).To(Succeed())
		})

		It("registers a type with category domain-enum", func() {
			Expect(th.RegisterType(ctx, domain.DictionaryType{
				TypeKey: "ship-status", Name: "Ship Status", Category: domain.CategoryDomainEnum,
			})).To(Succeed())
		})

		It("registers a type with category domain-string", func() {
			Expect(th.RegisterType(ctx, domain.DictionaryType{
				TypeKey: "l10n", Name: "UI Copy", Category: domain.CategoryDomainString,
			})).To(Succeed())
		})

		It("rejects an unrecognized category", func() {
			err := th.RegisterType(ctx, domain.DictionaryType{
				TypeKey: "mystery", Name: "Mystery", Category: domain.TypeCategory("nonsense"),
			})
			Expect(errors.Is(err, domain.ErrInvalidCategory)).To(BeTrue())
		})

		It("rejects an empty category", func() {
			err := th.RegisterType(ctx, domain.DictionaryType{TypeKey: "mystery", Name: "Mystery"})
			Expect(errors.Is(err, domain.ErrInvalidCategory)).To(BeTrue())
		})

		It("round-trips the category through List", func() {
			Expect(th.RegisterType(ctx, domain.DictionaryType{
				TypeKey: "hazard-class", Name: "Hazard Class", Category: domain.CategoryStandards,
			})).To(Succeed())

			all, err := th.ListTypes(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(all).To(ContainElement(HaveField("Category", domain.CategoryStandards)))
		})
	})

	Context("BR-D10: l10n items are exempt from typed-reference targeting", func() {
		BeforeEach(func() {
			Expect(th.RegisterType(ctx, domain.DictionaryType{
				TypeKey: "l10n", Name: "UI Copy", Category: domain.CategoryDomainString,
			})).To(Succeed())
			_, err := ih.RegisterItem(ctx, commands.ItemInput{TypeKey: "l10n", Code: "filter.all", Context: "acme-test"})
			Expect(err).NotTo(HaveOccurred())
		})

		It("hard-deletes an unreferenced l10n item like any other unreferenced item (BR-D02)", func() {
			Expect(ih.DeleteItem(ctx, "l10n", "acme-test", "filter.all")).To(Succeed())
			_, err := ih.Get(ctx, "l10n", "acme-test", "filter.all")
			Expect(errors.Is(err, domain.ErrItemNotFound)).To(BeTrue())
		})

		It("is never the target of a typed reference — no relation declares l10n as its target type", func() {
			// If some future relation ever declared l10n as a target type, a
			// reference to this item would incorrectly protect it from deletion
			// (BR-D02) despite BR-D10 saying nothing should ever reference it.
			// Guard the invariant directly: nothing references it today.
			referenced, err := refs.IsReferenced(ctx, "l10n", "acme-test", "filter.all")
			Expect(err).NotTo(HaveOccurred())
			Expect(referenced).To(BeFalse())
		})
	})

	Context("BR-D22: typeKey must be a valid subject/KV-key token", func() {
		It("registers a valid lowercase-hyphenated typeKey", func() {
			Expect(th.RegisterType(ctx, domain.DictionaryType{
				TypeKey: "hazard-class-2", Name: "Hazard Class 2", Category: domain.CategoryStandards,
			})).To(Succeed())
		})

		It("rejects an empty typeKey", func() {
			err := th.RegisterType(ctx, domain.DictionaryType{
				TypeKey: "", Name: "Mystery", Category: domain.CategoryStandards,
			})
			Expect(errors.Is(err, domain.ErrInvalidToken)).To(BeTrue())
		})

		It("rejects a typeKey containing a dot", func() {
			err := th.RegisterType(ctx, domain.DictionaryType{
				TypeKey: "hazard.class", Name: "Mystery", Category: domain.CategoryStandards,
			})
			Expect(errors.Is(err, domain.ErrInvalidToken)).To(BeTrue())
		})

		It("rejects a typeKey containing '*'", func() {
			err := th.RegisterType(ctx, domain.DictionaryType{
				TypeKey: "hazard*class", Name: "Mystery", Category: domain.CategoryStandards,
			})
			Expect(errors.Is(err, domain.ErrInvalidToken)).To(BeTrue())
		})

		It("rejects a typeKey containing '>'", func() {
			err := th.RegisterType(ctx, domain.DictionaryType{
				TypeKey: "hazard>class", Name: "Mystery", Category: domain.CategoryStandards,
			})
			Expect(errors.Is(err, domain.ErrInvalidToken)).To(BeTrue())
		})

		It("rejects a typeKey containing whitespace", func() {
			err := th.RegisterType(ctx, domain.DictionaryType{
				TypeKey: "hazard class", Name: "Mystery", Category: domain.CategoryStandards,
			})
			Expect(errors.Is(err, domain.ErrInvalidToken)).To(BeTrue())
		})
	})
})
