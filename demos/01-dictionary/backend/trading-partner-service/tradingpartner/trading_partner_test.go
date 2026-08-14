package tradingpartner_test

import (
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/trading-partner-service/tradingpartner/internal/domain"
)

var _ = Describe("TradingPartner Rules", func() {
	Context("BR-TP01: type is required and drawn from a controlled vocabulary", func() {
		It("registers a Shipper or a Transporter", func() {
			_, err := domain.Register("Acme Freight", domain.PartnerTypeShipper, "acme-pacific-fleet")
			Expect(err).NotTo(HaveOccurred())

			_, err = domain.Register("Acme Trucking", domain.PartnerTypeTransporter, "acme-pacific-fleet")
			Expect(err).NotTo(HaveOccurred())
		})

		It("rejects an unknown type", func() {
			_, err := domain.Register("Acme Freight", domain.PartnerType("BROKER"), "acme-pacific-fleet")
			Expect(errors.Is(err, domain.ErrInvalidPartnerType)).To(BeTrue())
		})
	})

	Context("BR-TP02: Register requires name/type/context, everything else optional", func() {
		It("creates a new TradingPartner in Registered status from just name/type/context", func() {
			tp, err := domain.Register("Acme Freight", domain.PartnerTypeShipper, "acme-pacific-fleet")
			Expect(err).NotTo(HaveOccurred())
			Expect(tp.Name).To(Equal("Acme Freight"))
			Expect(tp.Type).To(Equal(domain.PartnerTypeShipper))
			Expect(tp.Context).To(Equal("acme-pacific-fleet"))
			Expect(tp.Status).To(Equal(domain.StatusRegistered))
			Expect(tp.TradingAs).To(BeEmpty(), "tradingAs is optional at Register time")
			Expect(tp.RegistrationNo).To(BeEmpty(), "registrationNo is optional at Register time")
			Expect(tp.VatRegistrationNo).To(BeEmpty(), "vatRegistrationNo is optional at Register time")
		})

		It("rejects a missing name", func() {
			_, err := domain.Register("", domain.PartnerTypeShipper, "acme-pacific-fleet")
			Expect(errors.Is(err, domain.ErrNameRequired)).To(BeTrue())
		})

		It("rejects a missing context", func() {
			_, err := domain.Register("Acme Freight", domain.PartnerTypeShipper, "")
			Expect(errors.Is(err, domain.ErrContextRequired)).To(BeTrue())
		})
	})

	Context("BR-TP03/BR-TP04/BR-TP05: the 3x3 transition legality matrix", func() {
		registered := func() domain.TradingPartner {
			tp, err := domain.Register("Acme Freight", domain.PartnerTypeTransporter, "acme-pacific-fleet")
			Expect(err).NotTo(HaveOccurred())
			return tp
		}
		active := func() domain.TradingPartner {
			tp, err := registered().Activate()
			Expect(err).NotTo(HaveOccurred())
			return tp
		}
		suspended := func() domain.TradingPartner {
			tp, err := active().Suspend("insurance lapsed")
			Expect(err).NotTo(HaveOccurred())
			return tp
		}

		// The three legal edges.
		It("Registered -> Active via Activate", func() {
			tp, err := registered().Activate()
			Expect(err).NotTo(HaveOccurred())
			Expect(tp.Status).To(Equal(domain.StatusActive))
		})

		It("Active -> Suspended via Suspend", func() {
			tp, err := active().Suspend("insurance lapsed")
			Expect(err).NotTo(HaveOccurred())
			Expect(tp.Status).To(Equal(domain.StatusSuspended))
		})

		It("Suspended -> Active via Reactivate", func() {
			tp, err := suspended().Reactivate()
			Expect(err).NotTo(HaveOccurred())
			Expect(tp.Status).To(Equal(domain.StatusActive))
		})

		// The six illegal edges — each rejected, none mutate status.
		It("rejects Activate from Active (already active)", func() {
			_, err := active().Activate()
			Expect(errors.Is(err, domain.ErrNotRegistered)).To(BeTrue())
		})

		It("rejects Activate from Suspended", func() {
			_, err := suspended().Activate()
			Expect(errors.Is(err, domain.ErrNotRegistered)).To(BeTrue())
		})

		It("rejects Suspend from Registered (nothing to suspend from)", func() {
			_, err := registered().Suspend("insurance lapsed")
			Expect(errors.Is(err, domain.ErrNotActive)).To(BeTrue())
		})

		It("rejects Suspend from Suspended (already suspended)", func() {
			_, err := suspended().Suspend("insurance lapsed")
			Expect(errors.Is(err, domain.ErrNotActive)).To(BeTrue())
		})

		It("rejects Reactivate from Registered (not suspended)", func() {
			_, err := registered().Reactivate()
			Expect(errors.Is(err, domain.ErrNotSuspended)).To(BeTrue())
		})

		It("rejects Reactivate from Active (not suspended)", func() {
			_, err := active().Reactivate()
			Expect(errors.Is(err, domain.ErrNotSuspended)).To(BeTrue())
		})
	})

	Context("BR-TP04: Suspend requires a non-empty reason", func() {
		It("rejects an empty reason at the domain boundary, regardless of status", func() {
			tp, err := domain.Register("Acme Trucking", domain.PartnerTypeTransporter, "acme-pacific-fleet")
			Expect(err).NotTo(HaveOccurred())
			tp, err = tp.Activate()
			Expect(err).NotTo(HaveOccurred())

			_, err = tp.Suspend("")
			Expect(errors.Is(err, domain.ErrSuspendReasonRequired)).To(BeTrue())
		})
	})
})
