package pricing_test

import (
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/pricing-service/pricing/internal/domain"
)

var _ = Describe("FixedRate Rules", func() {
	Context("BR-P13: a fixed rate is scoped to one customer and route", func() {
		It("carries a customer key, route key, and active status", func() {
			fr := domain.FixedRate{Name: "contract-1", Context: "acme-pacific-fleet", CustomerKey: "cust-1", RouteKey: "durban-gauteng", Active: true}
			Expect(fr.Active).To(BeTrue())
		})
	})

	Context("BR-P14: draft lifecycle and active-fixed-rate gating", func() {
		It("allows at most one draft and publishes only a draft", func() {
			versions := []domain.FixedRateVersion{{Context: "acme-pacific-fleet", Version: 1, Status: domain.VersionDraft}}
			Expect(errors.Is(domain.CanCreateFixedRateDraft(versions), domain.ErrFixedRateDraftAlreadyExists)).To(BeTrue())

			Expect(domain.FixedRateVersion{Status: domain.VersionDraft}.CanPublish()).To(Succeed())
			Expect(errors.Is(domain.FixedRateVersion{Status: domain.VersionPublished}.CanPublish(), domain.ErrFixedRateOnlyDraftCanPublish)).To(BeTrue())
		})

		It("accepts only a published rollback target", func() {
			target := domain.FixedRateVersion{Context: "acme-pacific-fleet", Version: 2, Status: domain.VersionPublished}
			Expect(domain.CanRollbackFixedRateTo(target)).To(Succeed())
			Expect(errors.Is(domain.CanRollbackFixedRateTo(domain.FixedRateVersion{Status: domain.VersionDraft}), domain.ErrFixedRateRollbackTargetNotPublished)).To(BeTrue())
		})

		It("resolves the active version only when the fixed rate itself is active", func() {
			fr := domain.FixedRate{Active: true}
			versions := []domain.FixedRateVersion{
				{Version: 1, Status: domain.VersionPublished},
				{Version: 2, Status: domain.VersionPublished},
			}
			active, ok := domain.ActiveFixedRateVersion(fr, versions)
			Expect(ok).To(BeTrue())
			Expect(active.Version).To(Equal(2))

			inactive := domain.FixedRate{Active: false}
			_, ok = domain.ActiveFixedRateVersion(inactive, versions)
			Expect(ok).To(BeFalse())
		})
	})

	Context("BR-P15: the additional-drops charge mirrors RateSheetEntry's formula", func() {
		It("charges only for drops beyond the version's included point count", func() {
			v := domain.FixedRateVersion{CentRate: 700000, PointCount: 3, CentAdditionalDropRate: 20000}
			Expect(v.AdditionalDropsCharge(3)).To(Equal(int64(0)))
			Expect(v.AdditionalDropsCharge(6)).To(Equal(int64(60000)))
			Expect(v.AdditionalDropsCharge(1)).To(Equal(int64(0)))
		})
	})
})
