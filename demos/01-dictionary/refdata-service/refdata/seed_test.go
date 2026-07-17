package refdata_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	refdata "github.com/jthomas78/nats-tech-lab/demos/01-dictionary/refdata-service/refdata"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/refdata-service/refdata/internal/application/commands"
)

var _ = Describe("Seed", func() {
	var (
		ctx   context.Context
		items *fakeItemRepo
		locs  *fakeLocalizationRepo
		h     *refdata.Handlers
	)

	BeforeEach(func() {
		ctx = context.Background()
		items = newFakeItemRepo()
		refs := newFakeReferenceRepo()
		locs = newFakeLocalizationRepo()
		locales := newFakeLocaleRepo()

		h = &refdata.Handlers{
			Types:         commands.NewTypeHandler(newFakeTypeRepo()),
			Items:         commands.NewItemHandler(items, refs, nil),
			References:    commands.NewReferenceHandler(items, refs, nil),
			Localizations: commands.NewLocalizationHandler(items, locs, locales, nil),
		}

		Expect(refdata.Seed(ctx, h)).To(Succeed())
	})

	It("registers en as the default locale and es/af-za as secondary locales for the seed context", func() {
		locales, err := h.Localizations.ListLocales(ctx, refdata.DefaultContext)
		Expect(err).NotTo(HaveOccurred())
		Expect(locales).To(ContainElements("en", "es", "af-za"))
	})

	It("gives every seeded item an en, an es, and an af-za label", func() {
		all, err := items.List(ctx, "currency", refdata.DefaultContext)
		Expect(err).NotTo(HaveOccurred())
		Expect(all).NotTo(BeEmpty())

		for _, item := range all {
			localizations, err := locs.ListForItem(ctx, item.TypeKey, item.Context, item.Code)
			Expect(err).NotTo(HaveOccurred())
			Expect(localizations).To(ContainElement(HaveField("Locale", "en")))
			Expect(localizations).To(ContainElement(HaveField("Locale", "es")))
			Expect(localizations).To(ContainElement(HaveField("Locale", "af-za")))
		}
	})

	It("registers ship-status mirroring the backend's ShipStatus values, with en/es/af-za labels", func() {
		all, err := items.List(ctx, "ship-status", refdata.DefaultContext)
		Expect(err).NotTo(HaveOccurred())
		Expect(all).To(HaveLen(5))

		docked, err := items.Get(ctx, "ship-status", refdata.DefaultContext, "docked")
		Expect(err).NotTo(HaveOccurred())
		Expect(docked.Attrs["name"]).To(Equal("Docked"))

		localizations, err := locs.ListForItem(ctx, "ship-status", refdata.DefaultContext, "docked")
		Expect(err).NotTo(HaveOccurred())
		Expect(localizations).To(ContainElement(And(HaveField("Locale", "en"), HaveField("Label", "Docked"))))
		Expect(localizations).To(ContainElement(And(HaveField("Locale", "es"), HaveField("Label", "Atracado"))))
		Expect(localizations).To(ContainElement(And(HaveField("Locale", "af-za"), HaveField("Label", "Vasgemeer"))))
	})

	It("gives every seeded ui-copy key an en, an es, and an af-za label (BR-D16)", func() {
		all, err := items.List(ctx, "ui-copy", refdata.DefaultContext)
		Expect(err).NotTo(HaveOccurred())
		Expect(len(all)).To(BeNumerically(">", 2), "expected the full Phase 11.10 ui-copy catalog, not just the Phase 11.7 proof-of-concept keys")

		for _, item := range all {
			localizations, err := locs.ListForItem(ctx, item.TypeKey, item.Context, item.Code)
			Expect(err).NotTo(HaveOccurred())
			Expect(localizations).To(ContainElement(HaveField("Locale", "en")), "missing en label for ui-copy key %q", item.Code)
			Expect(localizations).To(ContainElement(HaveField("Locale", "es")), "missing es label for ui-copy key %q", item.Code)
			Expect(localizations).To(ContainElement(HaveField("Locale", "af-za")), "missing af-za label for ui-copy key %q", item.Code)
			for _, loc := range localizations {
				Expect(loc.Label).NotTo(BeEmpty(), "ui-copy key %q has a blank %s label", item.Code, loc.Locale)
			}
		}
	})

	It("is idempotent — seeding twice does not error or duplicate localizations", func() {
		Expect(refdata.Seed(ctx, h)).To(Succeed())

		localizations, err := locs.ListForItem(ctx, "hazard-class", refdata.DefaultContext, "3")
		Expect(err).NotTo(HaveOccurred())
		Expect(localizations).To(HaveLen(3))
		Expect(localizations).To(ContainElement(And(HaveField("Locale", "en"), HaveField("Label", "Flammable Liquids"))))
		Expect(localizations).To(ContainElement(And(HaveField("Locale", "es"), HaveField("Label", "Líquidos inflamables"))))
		Expect(localizations).To(ContainElement(And(HaveField("Locale", "af-za"), HaveField("Label", "Ontvlambare Vloeistowwe"))))
	})
})
