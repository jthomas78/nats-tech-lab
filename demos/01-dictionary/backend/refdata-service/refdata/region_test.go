package refdata_test

import (
	"context"
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata/internal/application/commands"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata/internal/domain"
)

// Specs for BR-D46–BR-D48 (Phase 38d-ii) — the `region` corpus and the
// Country -> Region hierarchy behind Operating Areas.
//
// Note on BR-D47's "at most one" half: it is enforced structurally, not by
// code. dictionary_references' primary key is
// (context, from_type_key, from_code, relation), so a second `country`
// relation from the same region cannot exist. These specs therefore assert
// the "at least one" half, which is the part a guard can get wrong.
var _ = Describe("Region Corpus Domain Rules (BR-D46-BR-D48)", func() {
	const ctxKey = "_platform"

	var (
		ctx   context.Context
		items *fakeItemRepo
		refs  *fakeReferenceRepo
		h     *commands.RegionHandler
	)

	// seedCountry registers an active country item so a region has something
	// legal to reference.
	seedCountry := func(code string) {
		ih := commands.NewItemHandler(items, refs, nil)
		_, err := ih.RegisterItem(ctx, commands.ItemInput{
			TypeKey: domain.CountryTypeKey, Code: code, Context: ctxKey,
		})
		Expect(err).NotTo(HaveOccurred())
	}

	BeforeEach(func() {
		ctx = context.Background()
		items = newFakeItemRepo()
		refs = newFakeReferenceRepo()
		h = commands.NewRegionHandler(items, refs, nil)
	})

	Context("BR-D46: the region type is a standards-governed, code-keyed corpus", func() {
		It("registers a region as an active item under the region type key", func() {
			seedCountry("ZA")

			item, err := h.RegisterRegion(ctx, commands.RegionInput{
				Context: ctxKey, Code: "ZA-GP", CountryCode: "ZA", Name: "Gauteng",
			})

			Expect(err).NotTo(HaveOccurred())
			Expect(item.TypeKey).To(Equal(domain.RegionTypeKey))
			Expect(item.Code).To(Equal("ZA-GP"))
			Expect(item.Status).To(Equal(domain.StatusActive))
			Expect(item.Attrs).To(HaveKeyWithValue("name", "Gauteng"))
		})

		It("rejects a region code that is not KV-key legal", func() {
			seedCountry("ZA")

			_, err := h.RegisterRegion(ctx, commands.RegionInput{
				Context: ctxKey, Code: "ZA GP", CountryCode: "ZA", Name: "Gauteng",
			})

			Expect(err).To(MatchError(domain.ErrInvalidKVKeyComponent))
		})

		It("rejects a duplicate region code in the same context (BR-D01)", func() {
			seedCountry("ZA")
			_, err := h.RegisterRegion(ctx, commands.RegionInput{
				Context: ctxKey, Code: "ZA-GP", CountryCode: "ZA", Name: "Gauteng",
			})
			Expect(err).NotTo(HaveOccurred())

			_, err = h.RegisterRegion(ctx, commands.RegionInput{
				Context: ctxKey, Code: "ZA-GP", CountryCode: "ZA", Name: "Gauteng",
			})

			Expect(err).To(MatchError(domain.ErrDuplicateItemCode))
		})
	})

	Context("BR-D47: every region declares exactly one parent country", func() {
		It("creates the country relation alongside the item", func() {
			seedCountry("ZA")

			_, err := h.RegisterRegion(ctx, commands.RegionInput{
				Context: ctxKey, Code: "ZA-WC", CountryCode: "ZA", Name: "Western Cape",
			})
			Expect(err).NotTo(HaveOccurred())

			ref, err := refs.Get(ctx, ctxKey, domain.RegionTypeKey, "ZA-WC", domain.RegionCountryRelation)
			Expect(err).NotTo(HaveOccurred())
			Expect(ref.ToTypeKey).To(Equal(domain.CountryTypeKey))
			Expect(ref.ToCode).To(Equal("ZA"))
		})

		It("rejects a region with no country code", func() {
			_, err := h.RegisterRegion(ctx, commands.RegionInput{
				Context: ctxKey, Code: "ZA-WC", CountryCode: "", Name: "Western Cape",
			})

			Expect(err).To(MatchError(domain.ErrRegionCountryRequired))
		})

		It("rejects a region whose country does not exist (BR-D05)", func() {
			_, err := h.RegisterRegion(ctx, commands.RegionInput{
				Context: ctxKey, Code: "BW-CE", CountryCode: "BW", Name: "Central",
			})

			Expect(err).To(MatchError(domain.ErrReferenceTargetNotFound))
		})

		It("rejects a region whose country is deprecated (BR-D05)", func() {
			seedCountry("BW")
			ih := commands.NewItemHandler(items, refs, nil)
			Expect(ih.DeprecateItem(ctx, domain.CountryTypeKey, ctxKey, "BW")).To(Succeed())

			_, err := h.RegisterRegion(ctx, commands.RegionInput{
				Context: ctxKey, Code: "BW-CE", CountryCode: "BW", Name: "Central",
			})

			Expect(err).To(MatchError(domain.ErrReferenceTargetNotActive))
		})

		It("leaves no orphan region item behind when the country relation is refused", func() {
			// The item write must not survive a rejected reference: a region
			// with no country is exactly the state BR-D47 forbids, and an
			// orphan here would be indistinguishable from one.
			_, err := h.RegisterRegion(ctx, commands.RegionInput{
				Context: ctxKey, Code: "BW-CE", CountryCode: "BW", Name: "Central",
			})
			Expect(err).To(HaveOccurred())

			_, err = items.Get(ctx, domain.RegionTypeKey, ctxKey, "BW-CE")
			Expect(errors.Is(err, domain.ErrItemNotFound)).To(BeTrue(),
				"a refused region must not leave a country-less item behind")
		})
	})

	Context("BR-D48: one canonical item per region; languages are localizations", func() {
		It("rejects a second item for the same region under a translated name", func() {
			// V2's anti-pattern: `Wes-Kaap` as its own row beside
			// `Western Cape`, each carrying independent assignments. Here the
			// code is the identity, so the second registration collides.
			seedCountry("ZA")
			_, err := h.RegisterRegion(ctx, commands.RegionInput{
				Context: ctxKey, Code: "ZA-WC", CountryCode: "ZA", Name: "Western Cape",
			})
			Expect(err).NotTo(HaveOccurred())

			_, err = h.RegisterRegion(ctx, commands.RegionInput{
				Context: ctxKey, Code: "ZA-WC", CountryCode: "ZA", Name: "Wes-Kaap",
			})

			Expect(err).To(MatchError(domain.ErrDuplicateItemCode))
		})
	})
})
