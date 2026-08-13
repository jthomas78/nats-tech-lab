package pricing_test

import (
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/pricing-service/pricing/internal/domain"
)

var _ = Describe("RateSheet Rules", func() {
	Context("BR-P07: a rate sheet is named, context-scoped, and customer-scoped", func() {
		It("carries a name, context, customer key, and type", func() {
			rs := domain.RateSheet{Name: "standard", Context: "acme-pacific-fleet", CustomerKey: "cust-1", Type: domain.RateSheetNormal}
			Expect(rs.Type).To(Equal(domain.RateSheetNormal))
			Expect(domain.RateSheetFixedRate).To(Equal(domain.RateSheetType("fixed-rate")))
		})
	})

	Context("BR-P08 and BR-P09: active-sheet gating + draft lifecycle", func() {
		It("allows at most one draft and publishes only a draft", func() {
			versions := []domain.RateSheetVersion{{Context: "acme-pacific-fleet", Version: 1, Status: domain.VersionDraft}}
			Expect(errors.Is(domain.CanCreateRateSheetDraft(versions), domain.ErrRateSheetDraftAlreadyExists)).To(BeTrue())

			Expect(domain.RateSheetVersion{Status: domain.VersionDraft}.CanPublish()).To(Succeed())
			Expect(errors.Is(domain.RateSheetVersion{Status: domain.VersionPublished}.CanPublish(), domain.ErrRateSheetOnlyDraftCanPublish)).To(BeTrue())
		})

		It("accepts only a published rollback target", func() {
			target := domain.RateSheetVersion{Context: "acme-pacific-fleet", Version: 3, Status: domain.VersionPublished}
			Expect(domain.CanRollbackRateSheetTo(target)).To(Succeed())
			Expect(errors.Is(domain.CanRollbackRateSheetTo(domain.RateSheetVersion{Status: domain.VersionDraft}), domain.ErrRateSheetRollbackTargetNotPublished)).To(BeTrue())
		})

		It("resolves the active version only when the sheet itself is active", func() {
			sheet := domain.RateSheet{Active: true}
			versions := []domain.RateSheetVersion{
				{Version: 1, Status: domain.VersionPublished},
				{Version: 2, Status: domain.VersionDraft},
			}
			active, ok := domain.ActiveRateSheetVersion(sheet, versions)
			Expect(ok).To(BeTrue())
			Expect(active.Version).To(Equal(1))

			inactive := domain.RateSheet{Active: false}
			_, ok = domain.ActiveRateSheetVersion(inactive, versions)
			Expect(ok).To(BeFalse(), "an inactive rate sheet must never resolve a version, even if one is published")
		})
	})

	Context("BR-P10: a version may override its fee scale", func() {
		It("resolves the override name when set, and reports absence otherwise", func() {
			name := "premium"
			withOverride := domain.RateSheetVersion{FeeScaleOverride: &name}
			resolved, ok := withOverride.ResolvedFeeScaleName()
			Expect(ok).To(BeTrue())
			Expect(resolved).To(Equal("premium"))

			without := domain.RateSheetVersion{}
			_, ok = without.ResolvedFeeScaleName()
			Expect(ok).To(BeFalse())
		})
	})

	Context("BR-P11 and BR-P12: lane entries and the additional-drops charge", func() {
		It("charges only for drops beyond the entry's included point count", func() {
			entry := domain.RateSheetEntry{
				RouteKey:               "durban-gauteng",
				VehicleType:            "flatbed",
				CentBaseRate:           500000,
				DropPointCount:         2,
				CentAdditionalDropRate: 15000,
			}
			Expect(entry.AdditionalDropsCharge(2)).To(Equal(int64(0)), "within the included point count, no extra charge")
			Expect(entry.AdditionalDropsCharge(5)).To(Equal(int64(45000)), "3 extra drops * 15000")
			Expect(entry.AdditionalDropsCharge(1)).To(Equal(int64(0)), "fewer addresses than the included count must not go negative")
		})
	})
})
