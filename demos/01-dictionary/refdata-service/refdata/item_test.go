package refdata_test

import (
	"context"
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/refdata-service/refdata/internal/application/commands"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/refdata-service/refdata/internal/domain"
)

var _ = Describe("Dictionary Item Domain Rules", func() {
	var (
		ctx   context.Context
		items *fakeItemRepo
		refs  *fakeReferenceRepo
		h     *commands.ItemHandler
	)

	BeforeEach(func() {
		ctx = context.Background()
		items = newFakeItemRepo()
		refs = newFakeReferenceRepo()
		h = commands.NewItemHandler(items, refs, nil)
	})

	Context("BR-D01: item codes are unique per {type, context}", func() {
		It("registers a new item", func() {
			item, err := h.RegisterItem(ctx, commands.ItemInput{TypeKey: "currency", Code: "EUR", Context: "emea-acme"})
			Expect(err).NotTo(HaveOccurred())
			Expect(item.Status).To(Equal(domain.StatusActive))
		})

		It("returns ErrDuplicateItemCode for a second registration of the same code in the same context", func() {
			_, err := h.RegisterItem(ctx, commands.ItemInput{TypeKey: "currency", Code: "EUR", Context: "emea-acme"})
			Expect(err).NotTo(HaveOccurred())

			_, err = h.RegisterItem(ctx, commands.ItemInput{TypeKey: "currency", Code: "EUR", Context: "emea-acme"})
			Expect(errors.Is(err, domain.ErrDuplicateItemCode)).To(BeTrue())
		})

		It("allows the same code in a different context", func() {
			_, err := h.RegisterItem(ctx, commands.ItemInput{TypeKey: "currency", Code: "EUR", Context: "emea-acme"})
			Expect(err).NotTo(HaveOccurred())

			_, err = h.RegisterItem(ctx, commands.ItemInput{TypeKey: "currency", Code: "EUR", Context: "apac-globex"})
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("BR-D02: an unreferenced item may be hard-deleted; a referenced item must be deprecated instead", func() {
		BeforeEach(func() {
			_, err := h.RegisterItem(ctx, commands.ItemInput{TypeKey: "currency", Code: "ZAR", Context: "emea-acme"})
			Expect(err).NotTo(HaveOccurred())
		})

		It("hard-deletes an unreferenced item", func() {
			Expect(h.DeleteItem(ctx, "currency", "emea-acme", "ZAR")).To(Succeed())
			_, err := h.Get(ctx, "currency", "emea-acme", "ZAR")
			Expect(errors.Is(err, domain.ErrItemNotFound)).To(BeTrue())
		})

		It("returns ErrItemReferenced when the item is referenced by another item", func() {
			_, err := h.RegisterItem(ctx, commands.ItemInput{TypeKey: "country", Code: "ZA", Context: "emea-acme"})
			Expect(err).NotTo(HaveOccurred())

			refHandler := commands.NewReferenceHandler(items, refs, nil)
			Expect(refHandler.CreateReference(ctx, commands.ReferenceInput{
				Context: "emea-acme", FromTypeKey: "country", FromCode: "ZA",
				Relation: "defaultCurrency", DeclaredTargetType: "currency",
				ToTypeKey: "currency", ToCode: "ZAR",
			})).To(Succeed())

			err = h.DeleteItem(ctx, "currency", "emea-acme", "ZAR")
			Expect(errors.Is(err, domain.ErrItemReferenced)).To(BeTrue())
		})

		It("deprecates a referenced item instead of deleting it", func() {
			Expect(h.DeprecateItem(ctx, "currency", "emea-acme", "ZAR")).To(Succeed())
			item, err := h.Get(ctx, "currency", "emea-acme", "ZAR")
			Expect(err).NotTo(HaveOccurred())
			Expect(item.Status).To(Equal(domain.StatusDeprecated))
		})
	})

	Context("BR-D06: deprecated items still resolve on read but are excluded from assignable-value listings by default", func() {
		BeforeEach(func() {
			_, err := h.RegisterItem(ctx, commands.ItemInput{TypeKey: "currency", Code: "GBP", Context: "emea-acme"})
			Expect(err).NotTo(HaveOccurred())
			_, err = h.RegisterItem(ctx, commands.ItemInput{TypeKey: "currency", Code: "EUR", Context: "emea-acme"})
			Expect(err).NotTo(HaveOccurred())
			Expect(h.DeprecateItem(ctx, "currency", "emea-acme", "GBP")).To(Succeed())
		})

		It("still resolves a deprecated item on direct Get", func() {
			item, err := h.Get(ctx, "currency", "emea-acme", "GBP")
			Expect(err).NotTo(HaveOccurred())
			Expect(item.Status).To(Equal(domain.StatusDeprecated))
		})

		It("excludes deprecated items from ListAssignable", func() {
			assignable, err := h.ListAssignable(ctx, "currency", "emea-acme")
			Expect(err).NotTo(HaveOccurred())
			codes := make([]string, 0, len(assignable))
			for _, item := range assignable {
				codes = append(codes, item.Code)
			}
			Expect(codes).To(ConsistOf("EUR"))
		})
	})
})
