package tradingpartner_test

import (
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/trading-partner-service/tradingpartner/internal/domain"
)

// Specs for BR-TP46-BR-TP50 (Phase 38d-ii) — operating areas.
//
// Parentage is supplied to the domain rather than derived here: a region's
// country comes from refdata's `country` relation (BR-D47), which is the
// corpus's own statement of the hierarchy. Inferring it from the code's ISO
// prefix would be a second, unapproved source of truth for the same fact.
var _ = Describe("OperatingArea Rules", func() {
	// za builds a South African region; zaCountry is the country-level
	// assignment covering all of them.
	za := func(code string) domain.OperatingArea {
		return domain.OperatingArea{Level: domain.AreaLevelRegion, Code: code, CountryCode: "ZA"}
	}
	zaCountry := domain.OperatingArea{Level: domain.AreaLevelCountry, Code: "ZA", CountryCode: "ZA"}

	Context("BR-TP46: an operating area may only belong to a Transporter", func() {
		It("allows a Transporter to declare a region", func() {
			area, err := domain.AddOperatingArea(domain.PartnerTypeTransporter, nil,
				domain.AreaLevelRegion, "ZA-GP", "ZA")
			Expect(err).NotTo(HaveOccurred())
			Expect(area.Code).To(Equal("ZA-GP"))
			Expect(area.Level).To(Equal(domain.AreaLevelRegion))
			Expect(area.CountryCode).To(Equal("ZA"))
		})

		It("rejects an operating area for a Shipper", func() {
			_, err := domain.AddOperatingArea(domain.PartnerTypeShipper, nil,
				domain.AreaLevelRegion, "ZA-GP", "ZA")
			Expect(errors.Is(err, domain.ErrOperatingAreaRequiresTransporter)).To(BeTrue())
		})
	})

	Context("BR-TP47: level must be COUNTRY or REGION, and code and country are required", func() {
		It("allows a country-level assignment", func() {
			area, err := domain.AddOperatingArea(domain.PartnerTypeTransporter, nil,
				domain.AreaLevelCountry, "ZA", "ZA")
			Expect(err).NotTo(HaveOccurred())
			Expect(area.Level).To(Equal(domain.AreaLevelCountry))
		})

		It("rejects an unknown level", func() {
			_, err := domain.AddOperatingArea(domain.PartnerTypeTransporter, nil,
				domain.AreaLevel("MUNICIPALITY"), "ZA-GP-JHB", "ZA")
			Expect(errors.Is(err, domain.ErrOperatingAreaInvalidLevel)).To(BeTrue())
		})

		It("rejects an empty code", func() {
			_, err := domain.AddOperatingArea(domain.PartnerTypeTransporter, nil,
				domain.AreaLevelRegion, "", "ZA")
			Expect(errors.Is(err, domain.ErrOperatingAreaCodeRequired)).To(BeTrue())
		})

		It("rejects a region with no resolved country", func() {
			// An unresolvable country means refdata could not answer BR-D47's
			// relation; assigning the area anyway would create coverage whose
			// parentage BR-TP48 cannot later evaluate.
			_, err := domain.AddOperatingArea(domain.PartnerTypeTransporter, nil,
				domain.AreaLevelRegion, "ZA-GP", "")
			Expect(errors.Is(err, domain.ErrOperatingAreaCountryRequired)).To(BeTrue())
		})

		It("requires a country-level code to equal its own country", func() {
			_, err := domain.AddOperatingArea(domain.PartnerTypeTransporter, nil,
				domain.AreaLevelCountry, "ZA", "BW")
			Expect(errors.Is(err, domain.ErrOperatingAreaCountryMismatch)).To(BeTrue())
		})
	})

	Context("BR-TP48: a country and a region inside it may not both be assigned", func() {
		It("rejects a region whose country is already assigned", func() {
			_, err := domain.AddOperatingArea(domain.PartnerTypeTransporter,
				[]domain.OperatingArea{zaCountry},
				domain.AreaLevelRegion, "ZA-GP", "ZA")
			Expect(errors.Is(err, domain.ErrOperatingAreaCoveredByCountry)).To(BeTrue())
		})

		It("rejects a country when a region inside it is already assigned", func() {
			_, err := domain.AddOperatingArea(domain.PartnerTypeTransporter,
				[]domain.OperatingArea{za("ZA-GP")},
				domain.AreaLevelCountry, "ZA", "ZA")
			Expect(errors.Is(err, domain.ErrOperatingAreaCoversExistingRegion)).To(BeTrue())
		})

		It("allows a region when a DIFFERENT country is assigned", func() {
			_, err := domain.AddOperatingArea(domain.PartnerTypeTransporter,
				[]domain.OperatingArea{{Level: domain.AreaLevelCountry, Code: "BW", CountryCode: "BW"}},
				domain.AreaLevelRegion, "ZA-GP", "ZA")
			Expect(err).NotTo(HaveOccurred())
		})

		It("allows a country when only OTHER countries' regions are assigned", func() {
			_, err := domain.AddOperatingArea(domain.PartnerTypeTransporter,
				[]domain.OperatingArea{{Level: domain.AreaLevelRegion, Code: "BW-CE", CountryCode: "BW"}},
				domain.AreaLevelCountry, "ZA", "ZA")
			Expect(err).NotTo(HaveOccurred())
		})

		It("allows several regions of the same country side by side", func() {
			_, err := domain.AddOperatingArea(domain.PartnerTypeTransporter,
				[]domain.OperatingArea{za("ZA-GP"), za("ZA-WC")},
				domain.AreaLevelRegion, "ZA-KZN", "ZA")
			Expect(err).NotTo(HaveOccurred())
		})

		It("rejects rather than silently removing the redundant rows", func() {
			// The alternative design — collapsing the now-redundant regions
			// on write — would delete rows the operator never touched, which
			// BR-TP50's audit trail would then have to explain. The rejection
			// leaves the existing set untouched.
			existing := []domain.OperatingArea{za("ZA-GP"), za("ZA-WC")}
			_, err := domain.AddOperatingArea(domain.PartnerTypeTransporter, existing,
				domain.AreaLevelCountry, "ZA", "ZA")
			Expect(err).To(HaveOccurred())
			Expect(existing).To(HaveLen(2), "the existing set must not be mutated by a refused add")
		})
	})
})
