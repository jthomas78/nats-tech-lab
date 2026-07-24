package refdata_test

import (
	"context"
	"errors"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata/internal/application/commands"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata/internal/domain"
)

var _ = Describe("Dictionary Translation Domain Rules", func() {
	var (
		ctx     context.Context
		itemCtx string
		itemH   *commands.ItemHandler
		locH    *commands.LocalizationHandler
		transH  *commands.TranslationHandler
		drafter *fakeTranslationDrafter
	)

	BeforeEach(func() {
		ctx = context.Background()
		itemCtx = "emea-acme"
		items := newFakeItemRepo()
		refs := newFakeReferenceRepo()
		locs := newFakeLocalizationRepo()
		locales := newFakeLocaleRepo()
		drafter = newFakeTranslationDrafter()

		itemH = commands.NewItemHandler(items, refs, nil)
		locH = commands.NewLocalizationHandler(items, locs, locales, nil)
		transH = commands.NewTranslationHandler(items, locs, locales, drafter)

		_, err := itemH.RegisterItem(ctx, commands.ItemInput{TypeKey: "currency", Code: "EUR", Context: itemCtx})
		Expect(err).NotTo(HaveOccurred())
		Expect(locH.SetLocalization(ctx, commands.LocalizationInput{
			TypeKey: "currency", Code: "EUR", Context: itemCtx, Locale: "en", Label: "Euro", Description: "Single currency of the Eurozone",
		})).To(Succeed())
	})

	Context("BR-D07: AI drafts never persist without explicit human save; persisted localizations record their source", func() {
		It("returns a draft without writing any localization for the target locale", func() {
			drafts, err := transH.DraftTranslations(ctx, commands.DraftInput{
				TypeKey: "currency", Code: "EUR", Context: itemCtx, TargetLocales: []string{"de-de"},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(drafts).To(HaveLen(1))
			Expect(drafts[0].Locale).To(Equal("de-de"))
			Expect(drafts[0].Label).NotTo(BeEmpty())

			_, err = locH.ResolveItem(ctx, "currency", itemCtx, "EUR", "de-de")
			Expect(err).NotTo(HaveOccurred())
			existing, listErr := locH.ListForItem(ctx, "currency", itemCtx, "EUR")
			Expect(listErr).NotTo(HaveOccurred())
			for _, l := range existing {
				Expect(l.Locale).NotTo(Equal("de-de"), "drafting must not persist a localization row")
			}
		})

		It("records source: ai once a caller explicitly saves an accepted draft", func() {
			drafts, err := transH.DraftTranslations(ctx, commands.DraftInput{
				TypeKey: "currency", Code: "EUR", Context: itemCtx, TargetLocales: []string{"de-de"},
			})
			Expect(err).NotTo(HaveOccurred())
			draft := drafts[0]

			Expect(locH.SetLocalization(ctx, commands.LocalizationInput{
				TypeKey: "currency", Code: "EUR", Context: itemCtx, Locale: "de-de",
				Label: draft.Label, Description: draft.Description, Source: domain.SourceAI,
			})).To(Succeed())

			saved, err := locH.ResolveItem(ctx, "currency", itemCtx, "EUR", "de-de")
			Expect(err).NotTo(HaveOccurred())
			Expect(saved.Localization.Source).To(Equal(domain.SourceAI))
		})

		It("records source: manual for a hand-typed translation — the default when Source is unset", func() {
			Expect(locH.SetLocalization(ctx, commands.LocalizationInput{
				TypeKey: "currency", Code: "EUR", Context: itemCtx, Locale: "es-es", Label: "Euro",
			})).To(Succeed())

			saved, err := locH.ResolveItem(ctx, "currency", itemCtx, "EUR", "es-es")
			Expect(err).NotTo(HaveOccurred())
			Expect(saved.Localization.Source).To(Equal(domain.SourceManual))
		})

		It("rejects a source other than manual or ai", func() {
			err := locH.SetLocalization(ctx, commands.LocalizationInput{
				TypeKey: "currency", Code: "EUR", Context: itemCtx, Locale: "fr-fr", Label: "Euro", Source: "bogus",
			})
			Expect(err).To(MatchError(domain.ErrInvalidSource))
		})

		It("surfaces a per-locale drafting failure without aborting the other requested locales", func() {
			drafter.failLocale("de-de", errors.New("model unavailable"))

			drafts, err := transH.DraftTranslations(ctx, commands.DraftInput{
				TypeKey: "currency", Code: "EUR", Context: itemCtx, TargetLocales: []string{"de-de", "es-es"},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(drafts).To(HaveLen(2))
			Expect(drafts[0].Locale).To(Equal("de-de"))
			Expect(drafts[0].Error).To(ContainSubstring("model unavailable"))
			Expect(drafts[1].Locale).To(Equal("es-es"))
			Expect(drafts[1].Error).To(BeEmpty())
			Expect(drafts[1].Label).NotTo(BeEmpty())
		})
	})

	Context("BR-D24: bulk AI translation drafting calls the model sequentially, never concurrently", func() {
		It("calls the drafter once per requested target locale, in the requested order", func() {
			_, err := transH.DraftTranslations(ctx, commands.DraftInput{
				TypeKey: "currency", Code: "EUR", Context: itemCtx,
				TargetLocales: []string{"de-de", "es-es", "fr-fr"},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(drafter.callLocales()).To(Equal([]string{"de-de", "es-es", "fr-fr"}))
		})

		It("never has more than one draft call in flight at a time", func() {
			drafter.latency = 20 * time.Millisecond

			_, err := transH.DraftTranslations(ctx, commands.DraftInput{
				TypeKey: "currency", Code: "EUR", Context: itemCtx,
				TargetLocales: []string{"de-de", "es-es", "fr-fr"},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(drafter.maxConcurrent()).To(Equal(1))
		})
	})
})
