package pricing_test

import (
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/pricing-service/pricing/internal/domain"
)

var _ = Describe("FeeScale Rules", func() {
	Context("BR-P01: a fee scale is a named, context-scoped schedule", func() {
		It("carries a name and context and can be soft-deleted", func() {
			fs := domain.FeeScale{Name: "standard", Context: "acme-pacific-fleet"}
			Expect(fs.Deleted).To(BeFalse())
			fs.Deleted = true
			Expect(fs.Deleted).To(BeTrue())
		})
	})

	Context("BR-P02: draft lifecycle", func() {
		It("allows at most one draft and publishes only a draft", func() {
			versions := []domain.FeeScaleVersion{{Context: "acme-pacific-fleet", Version: 1, Status: domain.VersionDraft}}
			Expect(errors.Is(domain.CanCreateDraft(versions), domain.ErrDraftAlreadyExists)).To(BeTrue())

			Expect(domain.FeeScaleVersion{Status: domain.VersionDraft}.CanPublish()).To(Succeed())
			Expect(errors.Is(domain.FeeScaleVersion{Status: domain.VersionPublished}.CanPublish(), domain.ErrOnlyDraftCanPublish)).To(BeTrue())
		})

		It("accepts only a published rollback target and never changes its version", func() {
			target := domain.FeeScaleVersion{Context: "acme-pacific-fleet", Version: 3, Status: domain.VersionPublished}
			Expect(domain.CanRollbackTo(target)).To(Succeed())
			Expect(target.Version).To(Equal(3))
			Expect(errors.Is(domain.CanRollbackTo(domain.FeeScaleVersion{Status: domain.VersionDraft}), domain.ErrRollbackTargetNotPublished)).To(BeTrue())
		})
	})

	Context("BR-P03: range boundary matching", func() {
		version := domain.FeeScaleVersion{
			Status: domain.VersionPublished,
			Ranges: []domain.FeeScaleRange{
				{CentLowerLimit: 0, CentUpperLimit: 1000, RateType: domain.RateFlat, CentFee: 50},
				{CentLowerLimit: 1000, CentUpperLimit: 5000, RateType: domain.RateFlat, CentFee: 200},
			},
		}

		It("matches the zero-lower-bound range inclusively at both ends", func() {
			fee, err := version.CalculateFee(0)
			Expect(err).NotTo(HaveOccurred())
			Expect(fee).To(Equal(int64(50)))

			fee, err = version.CalculateFee(1000)
			Expect(err).NotTo(HaveOccurred())
			Expect(fee).To(Equal(int64(50)), "the boundary value belongs to the range whose upper limit it equals, per the first range's inclusive-inclusive rule")
		})

		It("matches every other range exclusive-lower, inclusive-upper", func() {
			fee, err := version.CalculateFee(1001)
			Expect(err).NotTo(HaveOccurred())
			Expect(fee).To(Equal(int64(200)))

			fee, err = version.CalculateFee(5000)
			Expect(err).NotTo(HaveOccurred())
			Expect(fee).To(Equal(int64(200)))
		})
	})

	Context("BR-P04: a range charges flat or percentage, never both", func() {
		It("computes a flat cent fee for a FLAT range", func() {
			v := domain.FeeScaleVersion{Ranges: []domain.FeeScaleRange{
				{CentLowerLimit: 0, CentUpperLimit: 10000, RateType: domain.RateFlat, CentFee: 150, PercentageFee: 0.05},
			}}
			fee, err := v.CalculateFee(9999)
			Expect(err).NotTo(HaveOccurred())
			Expect(fee).To(Equal(int64(150)), "PercentageFee must be ignored when RateType is flat")
		})

		It("computes a rounded percentage fee for a PERCENTAGE range", func() {
			v := domain.FeeScaleVersion{Ranges: []domain.FeeScaleRange{
				{CentLowerLimit: 0, CentUpperLimit: 10000, RateType: domain.RatePercentage, CentFee: 999999, PercentageFee: 0.1},
			}}
			fee, err := v.CalculateFee(1050)
			Expect(err).NotTo(HaveOccurred())
			Expect(fee).To(Equal(int64(105)), "CentFee must be ignored when RateType is percentage")
		})

		It("rejects a range whose rate type is not one of flat or percentage", func() {
			Expect(errors.Is(domain.ValidateRange(domain.FeeScaleRange{RateType: "bogus"}), domain.ErrInvalidRateType)).To(BeTrue())
			Expect(domain.ValidateRange(domain.FeeScaleRange{RateType: domain.RateFlat})).To(Succeed())
		})
	})

	Context("BR-P05: a bid above the highest range is rejected, not silently zero-fee", func() {
		It("returns ErrBidAboveHighestRange instead of a zero fee", func() {
			v := domain.FeeScaleVersion{Ranges: []domain.FeeScaleRange{
				{CentLowerLimit: 0, CentUpperLimit: 1000, RateType: domain.RateFlat, CentFee: 50},
			}}
			_, err := v.CalculateFee(1001)
			Expect(errors.Is(err, domain.ErrBidAboveHighestRange)).To(BeTrue())
		})
	})

	Context("BR-P06: the active version is the latest published version, never a draft", func() {
		It("picks the highest-numbered published version and ignores drafts", func() {
			versions := []domain.FeeScaleVersion{
				{Version: 1, Status: domain.VersionPublished},
				{Version: 3, Status: domain.VersionDraft},
				{Version: 2, Status: domain.VersionPublished},
			}
			active, ok := domain.ActiveVersion(versions)
			Expect(ok).To(BeTrue())
			Expect(active.Version).To(Equal(2))
		})

		It("reports no active version when none is published", func() {
			_, ok := domain.ActiveVersion([]domain.FeeScaleVersion{{Version: 1, Status: domain.VersionDraft}})
			Expect(ok).To(BeFalse())
		})
	})

	Context("BR-P16: listing excludes soft-deleted fee scales", func() {
		It("keeps non-deleted fee scales and drops deleted ones", func() {
			all := []domain.FeeScale{
				{Name: "standard", Context: "acme-pacific-fleet"},
				{Name: "legacy", Context: "acme-pacific-fleet", Deleted: true},
				{Name: "premium", Context: "acme-pacific-fleet"},
			}
			visible := domain.VisibleFeeScales(all)
			Expect(visible).To(HaveLen(2))
			Expect(visible[0].Name).To(Equal("standard"))
			Expect(visible[1].Name).To(Equal("premium"))
		})

		It("returns an empty slice, not nil, when every fee scale is deleted", func() {
			all := []domain.FeeScale{{Name: "legacy", Context: "acme-pacific-fleet", Deleted: true}}
			Expect(domain.VisibleFeeScales(all)).To(BeEmpty())
		})
	})
})
