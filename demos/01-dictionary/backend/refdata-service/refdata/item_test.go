package refdata_test

import (
	"context"
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata/internal/application/commands"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata/internal/domain"
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

	Context("BR-D12: a deprecated item can be reactivated back to active", func() {
		BeforeEach(func() {
			_, err := h.RegisterItem(ctx, commands.ItemInput{TypeKey: "currency", Code: "JPY", Context: "emea-acme"})
			Expect(err).NotTo(HaveOccurred())
			Expect(h.DeprecateItem(ctx, "currency", "emea-acme", "JPY")).To(Succeed())
		})

		It("flips a deprecated item back to active", func() {
			Expect(h.ReactivateItem(ctx, "currency", "emea-acme", "JPY")).To(Succeed())
			item, err := h.Get(ctx, "currency", "emea-acme", "JPY")
			Expect(err).NotTo(HaveOccurred())
			Expect(item.Status).To(Equal(domain.StatusActive))
		})

		It("reappears in ListAssignable once reactivated", func() {
			Expect(h.ReactivateItem(ctx, "currency", "emea-acme", "JPY")).To(Succeed())
			assignable, err := h.ListAssignable(ctx, "currency", "emea-acme")
			Expect(err).NotTo(HaveOccurred())
			codes := make([]string, 0, len(assignable))
			for _, item := range assignable {
				codes = append(codes, item.Code)
			}
			Expect(codes).To(ContainElement("JPY"))
		})

		It("is a no-op when reactivating an item that is already active", func() {
			Expect(h.ReactivateItem(ctx, "currency", "emea-acme", "JPY")).To(Succeed())
			Expect(h.ReactivateItem(ctx, "currency", "emea-acme", "JPY")).To(Succeed())
			item, err := h.Get(ctx, "currency", "emea-acme", "JPY")
			Expect(err).NotTo(HaveOccurred())
			Expect(item.Status).To(Equal(domain.StatusActive))
		})

		It("returns ErrItemNotFound when reactivating an item that doesn't exist", func() {
			err := h.ReactivateItem(ctx, "currency", "emea-acme", "does-not-exist")
			Expect(errors.Is(err, domain.ErrItemNotFound)).To(BeTrue())
		})
	})

	Context("BR-D18: an item's attrs can be replaced after creation", func() {
		BeforeEach(func() {
			_, err := h.RegisterItem(ctx, commands.ItemInput{
				TypeKey: "l10n", Code: "app.title", Context: "emea-acme",
				Attrs: map[string]any{"name": "Ship Management"},
			})
			Expect(err).NotTo(HaveOccurred())
		})

		It("replaces the entire attrs map, not a per-key merge", func() {
			Expect(h.UpdateItemAttrs(ctx, "l10n", "emea-acme", "app.title",
				map[string]any{"name": "SeaFreight Flow"})).To(Succeed())

			item, err := h.Get(ctx, "l10n", "emea-acme", "app.title")
			Expect(err).NotTo(HaveOccurred())
			Expect(item.Attrs).To(Equal(map[string]any{"name": "SeaFreight Flow"}))
		})

		It("returns ErrItemNotFound when updating attrs of an item that doesn't exist", func() {
			err := h.UpdateItemAttrs(ctx, "l10n", "emea-acme", "does-not-exist", map[string]any{"name": "x"})
			Expect(errors.Is(err, domain.ErrItemNotFound)).To(BeTrue())
		})

		It("works on a deprecated item, mirroring BR-D06's read-regardless-of-status stance", func() {
			Expect(h.DeprecateItem(ctx, "l10n", "emea-acme", "app.title")).To(Succeed())

			Expect(h.UpdateItemAttrs(ctx, "l10n", "emea-acme", "app.title",
				map[string]any{"name": "SeaFreight Flow"})).To(Succeed())

			item, err := h.Get(ctx, "l10n", "emea-acme", "app.title")
			Expect(err).NotTo(HaveOccurred())
			Expect(item.Status).To(Equal(domain.StatusDeprecated))
			Expect(item.Attrs).To(Equal(map[string]any{"name": "SeaFreight Flow"}))
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

	Context("BR-D22: code must be a valid KV-key token", func() {
		It("registers a valid code", func() {
			_, err := h.RegisterItem(ctx, commands.ItemInput{TypeKey: "currency", Code: "CHF", Context: "emea-acme"})
			Expect(err).NotTo(HaveOccurred())
		})

		It("rejects an empty code", func() {
			_, err := h.RegisterItem(ctx, commands.ItemInput{TypeKey: "currency", Code: "", Context: "emea-acme"})
			Expect(errors.Is(err, domain.ErrInvalidKVKeyComponent)).To(BeTrue())
		})

		It("rejects a code containing ':'", func() {
			_, err := h.RegisterItem(ctx, commands.ItemInput{TypeKey: "currency", Code: "CH:F", Context: "emea-acme"})
			Expect(errors.Is(err, domain.ErrInvalidKVKeyComponent)).To(BeTrue())
		})

		It("rejects a code containing whitespace", func() {
			_, err := h.RegisterItem(ctx, commands.ItemInput{TypeKey: "currency", Code: "CH F", Context: "emea-acme"})
			Expect(errors.Is(err, domain.ErrInvalidKVKeyComponent)).To(BeTrue())
		})

		It("accepts a code containing '.' — KV-key charset, not a subject-token charset", func() {
			_, err := h.RegisterItem(ctx, commands.ItemInput{TypeKey: "l10n", Code: "filter.none", Context: "emea-acme"})
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
