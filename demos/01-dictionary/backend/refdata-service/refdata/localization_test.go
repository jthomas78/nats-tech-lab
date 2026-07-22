package refdata_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata/internal/application/commands"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata/internal/domain"
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
				TypeKey: "currency", Code: "EUR", Context: itemCtx, Locale: "de-de", Label: "Euro (DE)",
			})).To(Succeed())
			Expect(locH.SetLocalization(ctx, commands.LocalizationInput{
				TypeKey: "currency", Code: "EUR", Context: itemCtx, Locale: "de", Label: "Euro (de)",
			})).To(Succeed())

			resolved, err := locH.ResolveItem(ctx, "currency", itemCtx, "EUR", "de-de")
			Expect(err).NotTo(HaveOccurred())
			Expect(resolved.Localization.Label).To(Equal("Euro (DE)"))
		})

		It("falls back to the bare language when the exact locale is missing", func() {
			Expect(locH.SetLocalization(ctx, commands.LocalizationInput{
				TypeKey: "currency", Code: "EUR", Context: itemCtx, Locale: "de", Label: "Euro (de)",
			})).To(Succeed())

			resolved, err := locH.ResolveItem(ctx, "currency", itemCtx, "EUR", "de-de")
			Expect(err).NotTo(HaveOccurred())
			Expect(resolved.Localization.Label).To(Equal("Euro (de)"))
		})

		It("falls back to the context's default locale when neither the requested locale nor its language exist", func() {
			Expect(locH.AddLocale(ctx, itemCtx, "en", true)).To(Succeed())
			Expect(locH.SetLocalization(ctx, commands.LocalizationInput{
				TypeKey: "currency", Code: "EUR", Context: itemCtx, Locale: "en", Label: "Euro",
			})).To(Succeed())

			resolved, err := locH.ResolveItem(ctx, "currency", itemCtx, "EUR", "fr-fr")
			Expect(err).NotTo(HaveOccurred())
			Expect(resolved.Localization.Label).To(Equal("Euro"))
		})

		It("treats en as the implicit default in the fallback chain when no locale is marked default (BR-D15)", func() {
			// No AddLocale(..., true) anywhere — the context has no marked default.
			Expect(locH.SetLocalization(ctx, commands.LocalizationInput{
				TypeKey: "currency", Code: "EUR", Context: itemCtx, Locale: "en", Label: "Euro",
			})).To(Succeed())

			resolved, err := locH.ResolveItem(ctx, "currency", itemCtx, "EUR", "fr-fr")
			Expect(err).NotTo(HaveOccurred())
			Expect(resolved.Localization.Label).To(Equal("Euro"))
		})

		It("never fails outright — falls back to the code itself when nothing resolves", func() {
			resolved, err := locH.ResolveItem(ctx, "currency", itemCtx, "EUR", "ja-jp")
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
			Expect(locH.AddLocale(ctx, itemCtx, "de-de", false)).To(Succeed())
			locales, err := locH.ListLocales(ctx, itemCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(locales).To(ContainElement("de-de"))
		})

		It("rejects a locale with any upper case character (BR-D20)", func() {
			err := locH.AddLocale(ctx, itemCtx, "de-DE", false)
			Expect(err).To(MatchError(domain.ErrInvalidLocaleFormat))

			locales, err := locH.ListLocales(ctx, itemCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(locales).NotTo(ContainElement("de-DE"))
		})

		It("rejects setting a localization with an upper case locale (BR-D20)", func() {
			err := locH.SetLocalization(ctx, commands.LocalizationInput{
				TypeKey: "currency", Code: "EUR", Context: itemCtx, Locale: "de-DE", Label: "Euro (DE)",
			})
			Expect(err).To(MatchError(domain.ErrInvalidLocaleFormat))
		})

		It("reports en as the default when no locale is marked default (BR-D15)", func() {
			defaultLocale, err := locH.DefaultLocale(ctx, itemCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(defaultLocale).To(Equal("en"))
		})

		It("moves the default when another locale is marked default — at most one per context (BR-D14)", func() {
			Expect(locH.AddLocale(ctx, itemCtx, "en", true)).To(Succeed())
			Expect(locH.AddLocale(ctx, itemCtx, "es", true)).To(Succeed())

			defaultLocale, err := locH.DefaultLocale(ctx, itemCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(defaultLocale).To(Equal("es"))
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
