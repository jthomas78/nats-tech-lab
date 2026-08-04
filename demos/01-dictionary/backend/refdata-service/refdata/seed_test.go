package refdata_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	refdata "github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata/internal/application/commands"
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
		contexts := newFakeContextRepo()

		h = &refdata.Handlers{
			Types:         commands.NewTypeHandler(newFakeTypeRepo()),
			Items:         commands.NewItemHandler(items, refs, nil),
			References:    commands.NewReferenceHandler(items, refs, nil),
			Localizations: commands.NewLocalizationHandler(items, locs, locales, nil),
			Contexts:      commands.NewContextHandler(contexts),
			// Corpus deliberately nil, same as this fake's original wiring:
			// Seed's draft/publish step is guarded by `if h.Corpus != nil`
			// (Phase 16d), so this suite exercises the working-table seeding
			// only — corpus draft/publish/flattening has its own coverage in
			// corpus_repository_integration_test.go against real Postgres.
		}

		Expect(refdata.Seed(ctx, h)).To(Succeed())
	})

	It("registers _platform as the reserved root context", func() {
		context, err := h.Contexts.Get(ctx, refdata.PlatformContext)
		Expect(err).NotTo(HaveOccurred())
		Expect(context.Parent).To(BeEmpty())
		Expect(context.Tenant).To(BeEmpty(), "the reserved root belongs to no tenant")
	})

	It("registers acme-pacific-fleet as a child of _platform, owned by the acme tenant", func() {
		context, err := h.Contexts.Get(ctx, refdata.PacificFleetContext)
		Expect(err).NotTo(HaveOccurred())
		Expect(context.Parent).To(Equal(refdata.PlatformContext))
		Expect(context.Tenant).To(Equal("acme"))
	})

	It("registers acme-atlantic-fleet as a peer sibling of acme-pacific-fleet under _platform, owned by the acme tenant", func() {
		context, err := h.Contexts.Get(ctx, refdata.BusinessUnitContext)
		Expect(err).NotTo(HaveOccurred())
		Expect(context.Parent).To(Equal(refdata.PlatformContext))
		Expect(context.Tenant).To(Equal("acme"))
	})

	It("registers en as the default locale and es/af-za as secondary locales for every context in the tree", func() {
		for _, c := range []string{refdata.PlatformContext, refdata.PacificFleetContext, refdata.BusinessUnitContext} {
			locales, err := h.Localizations.ListLocales(ctx, c)
			Expect(err).NotTo(HaveOccurred())
			Expect(locales).To(ContainElements("en", "es", "af-za"), "context %q", c)
		}
	})

	It("gives every seeded standards item, registered under _platform, an en, an es, and an af-za label", func() {
		all, err := items.List(ctx, "currency", refdata.PlatformContext)
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

	It("registers ship-status under _platform, mirroring the backend's ShipStatus values, with en/es/af-za labels", func() {
		all, err := items.List(ctx, "ship-status", refdata.PlatformContext)
		Expect(err).NotTo(HaveOccurred())
		Expect(all).To(HaveLen(5))

		docked, err := items.Get(ctx, "ship-status", refdata.PlatformContext, "docked")
		Expect(err).NotTo(HaveOccurred())
		Expect(docked.Attrs["name"]).To(Equal("Docked"))

		localizations, err := locs.ListForItem(ctx, "ship-status", refdata.PlatformContext, "docked")
		Expect(err).NotTo(HaveOccurred())
		Expect(localizations).To(ContainElement(And(HaveField("Locale", "en"), HaveField("Label", "Docked"))))
		Expect(localizations).To(ContainElement(And(HaveField("Locale", "es"), HaveField("Label", "Atracado"))))
		Expect(localizations).To(ContainElement(And(HaveField("Locale", "af-za"), HaveField("Label", "Vasgemeer"))))
	})

	It("gives every seeded string key under _platform an en, an es, and an af-za label (BR-D16)", func() {
		all, err := items.List(ctx, "string", refdata.PlatformContext)
		Expect(err).NotTo(HaveOccurred())
		Expect(len(all)).To(BeNumerically(">", 2), "expected the full Phase 11.10 string catalog, not just the Phase 11.7 proof-of-concept keys")

		for _, item := range all {
			localizations, err := locs.ListForItem(ctx, item.TypeKey, item.Context, item.Code)
			Expect(err).NotTo(HaveOccurred())
			Expect(localizations).To(ContainElement(HaveField("Locale", "en")), "missing en label for string key %q", item.Code)
			Expect(localizations).To(ContainElement(HaveField("Locale", "es")), "missing es label for string key %q", item.Code)
			Expect(localizations).To(ContainElement(HaveField("Locale", "af-za")), "missing af-za label for string key %q", item.Code)
			for _, loc := range localizations {
				Expect(loc.Label).NotTo(BeEmpty(), "string key %q has a blank %s label", item.Code, loc.Locale)
			}
		}
	})

	Context("BR-V06/BR-V07 demonstration data — hazard-class carries all three inheritance states", func() {
		It("leaves the platform's own hazard-class/3 with its plain label — the override does not leak backward into the parent", func() {
			localizations, err := locs.ListForItem(ctx, "hazard-class", refdata.PlatformContext, "3")
			Expect(err).NotTo(HaveOccurred())
			Expect(localizations).To(ContainElement(And(HaveField("Locale", "en"), HaveField("Label", "Flammable Liquids"))))
		})

		It("overrides hazard-class/3 at the company level with an Acme-specific label (BR-V07)", func() {
			localizations, err := locs.ListForItem(ctx, "hazard-class", refdata.PacificFleetContext, "3")
			Expect(err).NotTo(HaveOccurred())
			Expect(localizations).To(ContainElement(And(HaveField("Locale", "en"), HaveField("Label", "Flammable Liquids (Acme Handling Advisory)"))))
		})

		It("adds hazard-class/X1 only at the business-unit level (BR-V06) — it does not exist at _platform or acme-pacific-fleet", func() {
			_, err := items.Get(ctx, "hazard-class", refdata.BusinessUnitContext, "X1")
			Expect(err).NotTo(HaveOccurred())

			_, err = items.Get(ctx, "hazard-class", refdata.PlatformContext, "X1")
			Expect(err).To(HaveOccurred())
			_, err = items.Get(ctx, "hazard-class", refdata.PacificFleetContext, "X1")
			Expect(err).To(HaveOccurred())
		})
	})

	It("is idempotent — seeding twice does not error or duplicate localizations", func() {
		Expect(refdata.Seed(ctx, h)).To(Succeed())

		localizations, err := locs.ListForItem(ctx, "hazard-class", refdata.PacificFleetContext, "3")
		Expect(err).NotTo(HaveOccurred())
		Expect(localizations).To(HaveLen(3))
		Expect(localizations).To(ContainElement(And(HaveField("Locale", "en"), HaveField("Label", "Flammable Liquids (Acme Handling Advisory)"))))
		Expect(localizations).To(ContainElement(And(HaveField("Locale", "es"), HaveField("Label", "Líquidos inflamables (aviso de manejo de Acme)"))))
		Expect(localizations).To(ContainElement(And(HaveField("Locale", "af-za"), HaveField("Label", "Ontvlambare Vloeistowwe (Acme Hanteringsadvies)"))))
	})
})
