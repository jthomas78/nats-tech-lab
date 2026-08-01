package refdata_test

import (
	"context"
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata/internal/application/commands"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata/internal/domain"
)

var _ = Describe("Dictionary Reference Domain Rules", func() {
	var (
		ctx   context.Context
		itemH *commands.ItemHandler
		refH  *commands.ReferenceHandler
	)

	BeforeEach(func() {
		ctx = context.Background()
		items := newFakeItemRepo()
		refs := newFakeReferenceRepo()
		itemH = commands.NewItemHandler(items, refs, nil)
		refH = commands.NewReferenceHandler(items, refs, nil)

		_, err := itemH.RegisterItem(ctx, commands.ItemInput{TypeKey: "country", Code: "ZA", Context: "acme-test"})
		Expect(err).NotTo(HaveOccurred())
	})

	Context("BR-D05: a reference must target an active item of the relation's declared type", func() {
		It("creates the reference when the target exists, is active, and matches the declared type", func() {
			_, err := itemH.RegisterItem(ctx, commands.ItemInput{TypeKey: "currency", Code: "ZAR", Context: "acme-test"})
			Expect(err).NotTo(HaveOccurred())

			err = refH.CreateReference(ctx, commands.ReferenceInput{
				Context: "acme-test", FromTypeKey: "country", FromCode: "ZA",
				Relation: "defaultCurrency", DeclaredTargetType: "currency",
				ToTypeKey: "currency", ToCode: "ZAR",
			})
			Expect(err).NotTo(HaveOccurred())
		})

		It("returns ErrReferenceTargetWrongType when ToTypeKey doesn't match the relation's declared type", func() {
			_, err := itemH.RegisterItem(ctx, commands.ItemInput{TypeKey: "uom", Code: "KG", Context: "acme-test"})
			Expect(err).NotTo(HaveOccurred())

			err = refH.CreateReference(ctx, commands.ReferenceInput{
				Context: "acme-test", FromTypeKey: "country", FromCode: "ZA",
				Relation: "defaultCurrency", DeclaredTargetType: "currency",
				ToTypeKey: "uom", ToCode: "KG",
			})
			Expect(errors.Is(err, domain.ErrReferenceTargetWrongType)).To(BeTrue())
		})

		It("returns ErrReferenceTargetNotFound when the target item doesn't exist", func() {
			err := refH.CreateReference(ctx, commands.ReferenceInput{
				Context: "acme-test", FromTypeKey: "country", FromCode: "ZA",
				Relation: "defaultCurrency", DeclaredTargetType: "currency",
				ToTypeKey: "currency", ToCode: "ZAR",
			})
			Expect(errors.Is(err, domain.ErrReferenceTargetNotFound)).To(BeTrue())
		})

		It("returns ErrReferenceTargetNotActive when the target item is deprecated", func() {
			_, err := itemH.RegisterItem(ctx, commands.ItemInput{TypeKey: "currency", Code: "ZAR", Context: "acme-test"})
			Expect(err).NotTo(HaveOccurred())
			Expect(itemH.DeprecateItem(ctx, "currency", "acme-test", "ZAR")).To(Succeed())

			err = refH.CreateReference(ctx, commands.ReferenceInput{
				Context: "acme-test", FromTypeKey: "country", FromCode: "ZA",
				Relation: "defaultCurrency", DeclaredTargetType: "currency",
				ToTypeKey: "currency", ToCode: "ZAR",
			})
			Expect(errors.Is(err, domain.ErrReferenceTargetNotActive)).To(BeTrue())
		})
	})
})
