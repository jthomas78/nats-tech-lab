package refdata_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/refdata-service/refdata/internal/application/commands"
)

var _ = Describe("Dictionary Localization Domain Rules", func() {
	var (
		ctx     context.Context
		itemH   *commands.ItemHandler
		refH    *commands.ReferenceHandler
		locH    *commands.LocalizationHandler
		itemCtx string
	)

	BeforeEach(func() {
		ctx = context.Background()
		itemCtx = "emea-acme"
		items := newFakeItemRepo()
		refs := newFakeReferenceRepo()
		locs := newFakeLocalizationRepo()
		locales := newFakeLocaleRepo()

		itemH = commands.NewItemHandler(items, refs, nil)
		refH = commands.NewReferenceHandler(items, refs, nil)
		locH = commands.NewLocalizationHandler(items, locs, locales, nil)

		_, err := itemH.RegisterItem(ctx, commands.ItemInput{TypeKey: "currency", Code: "EUR", Context: itemCtx})
		Expect(err).NotTo(HaveOccurred())
	})

	Context("BR-D03: locale resolution follows requested locale -> language -> default locale -> code", func() {
		It("resolves the exact requested locale when present", func() {
			Expect(locH.SetLocalization(ctx, commands.LocalizationInput{
				TypeKey: "currency", Code: "EUR", Context: itemCtx, Locale: "de-DE", Label: "Euro (DE)",
			})).To(Succeed())
			Expect(locH.SetLocalization(ctx, commands.LocalizationInput{
				TypeKey: "currency", Code: "EUR", Context: itemCtx, Locale: "de", Label: "Euro (de)",
			})).To(Succeed())

			resolved, err := locH.ResolveItem(ctx, "currency", itemCtx, "EUR", "de-DE")
			Expect(err).NotTo(HaveOccurred())
			Expect(resolved.Localization.Label).To(Equal("Euro (DE)"))
		})

		It("falls back to the bare language when the exact locale is missing", func() {
			Expect(locH.SetLocalization(ctx, commands.LocalizationInput{
				TypeKey: "currency", Code: "EUR", Context: itemCtx, Locale: "de", Label: "Euro (de)",
			})).To(Succeed())

			resolved, err := locH.ResolveItem(ctx, "currency", itemCtx, "EUR", "de-DE")
			Expect(err).NotTo(HaveOccurred())
			Expect(resolved.Localization.Label).To(Equal("Euro (de)"))
		})

		It("falls back to the context's default locale when neither the requested locale nor its language exist", func() {
			Expect(locH.AddLocale(ctx, itemCtx, "en", true)).To(Succeed())
			Expect(locH.SetLocalization(ctx, commands.LocalizationInput{
				TypeKey: "currency", Code: "EUR", Context: itemCtx, Locale: "en", Label: "Euro",
			})).To(Succeed())

			resolved, err := locH.ResolveItem(ctx, "currency", itemCtx, "EUR", "fr-FR")
			Expect(err).NotTo(HaveOccurred())
			Expect(resolved.Localization.Label).To(Equal("Euro"))
		})

		It("never fails outright — falls back to the code itself when nothing resolves", func() {
			resolved, err := locH.ResolveItem(ctx, "currency", itemCtx, "EUR", "ja-JP")
			Expect(err).NotTo(HaveOccurred())
			Expect(resolved.Localization.Label).To(Equal("EUR"))
		})

		It("still resolves a deprecated item's label (BR-D06 interaction)", func() {
			Expect(itemH.DeprecateItem(ctx, "currency", itemCtx, "EUR")).To(Succeed())
			Expect(locH.SetLocalization(ctx, commands.LocalizationInput{
				TypeKey: "currency", Code: "EUR", Context: itemCtx, Locale: "en", Label: "Euro",
			})).To(Succeed())

			resolved, err := locH.ResolveItem(ctx, "currency", itemCtx, "EUR", "en")
			Expect(err).NotTo(HaveOccurred())
			Expect(resolved.Localization.Label).To(Equal("Euro"))
		})
	})

	Context("Reference expansion", func() {
		It("expands a relation to its target item", func() {
			_, err := itemH.RegisterItem(ctx, commands.ItemInput{TypeKey: "country", Code: "DE", Context: itemCtx})
			Expect(err).NotTo(HaveOccurred())
			Expect(refH.CreateReference(ctx, commands.ReferenceInput{
				Context: itemCtx, FromTypeKey: "country", FromCode: "DE",
				Relation: "defaultCurrency", DeclaredTargetType: "currency",
				ToTypeKey: "currency", ToCode: "EUR",
			})).To(Succeed())

			expanded, err := refH.Expand(ctx, itemCtx, "country", "DE", "defaultCurrency")
			Expect(err).NotTo(HaveOccurred())
			Expect(expanded.Code).To(Equal("EUR"))
		})
	})

	Context("Locale management", func() {
		It("registers a locale and lists it", func() {
			Expect(locH.AddLocale(ctx, itemCtx, "de-DE", false)).To(Succeed())
			locales, err := locH.ListLocales(ctx, itemCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(locales).To(ContainElement("de-DE"))
		})

		It("reports localization completeness for a locale", func() {
			_, err := itemH.RegisterItem(ctx, commands.ItemInput{TypeKey: "currency", Code: "GBP", Context: itemCtx})
			Expect(err).NotTo(HaveOccurred())
			Expect(locH.SetLocalization(ctx, commands.LocalizationInput{
				TypeKey: "currency", Code: "EUR", Context: itemCtx, Locale: "de", Label: "Euro",
			})).To(Succeed())

			total, localized, err := locH.Completeness(ctx, "currency", itemCtx, "de")
			Expect(err).NotTo(HaveOccurred())
			Expect(total).To(Equal(2))
			Expect(localized).To(Equal(1))
		})
	})
})
