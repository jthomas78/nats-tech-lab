package organizations_test

import (
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/internal/domain"
)

var _ = Describe("Organization Rules", func() {
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
		It("creates a new Organization in Registered status from just name/type/context", func() {
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
		registered := func() domain.Organization {
			tp, err := domain.Register("Acme Freight", domain.PartnerTypeTransporter, "acme-pacific-fleet")
			Expect(err).NotTo(HaveOccurred())
			return tp
		}
		active := func() domain.Organization {
			tp, err := registered().Activate()
			Expect(err).NotTo(HaveOccurred())
			return tp
		}
		suspended := func() domain.Organization {
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

	Context("BR-TP35: Register optionally accepts Company Information", func() {
		It("populates every optional field when supplied", func() {
			tp, err := domain.RegisterWithDetails(domain.PartnerTypeTransporter, "acme-pacific-fleet", domain.Details{
				Name:              "Acme Trucking",
				TradingAs:         "Acme Haulage",
				CompanyName:       "Acme Trucking (Pty) Ltd",
				RegistrationNo:    "2019/123456/07",
				VatRegistrationNo: "4123456789",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(tp.Name).To(Equal("Acme Trucking"))
			Expect(tp.TradingAs).To(Equal("Acme Haulage"))
			Expect(tp.CompanyName).To(Equal("Acme Trucking (Pty) Ltd"))
			Expect(tp.RegistrationNo).To(Equal("2019/123456/07"))
			Expect(tp.VatRegistrationNo).To(Equal("4123456789"))
			Expect(tp.Status).To(Equal(domain.StatusRegistered), "widening Register must not change the landing status")
		})

		It("leaves omitted fields empty, so the narrow Register stays equivalent", func() {
			widened, err := domain.RegisterWithDetails(domain.PartnerTypeShipper, "acme-pacific-fleet", domain.Details{Name: "Acme Freight"})
			Expect(err).NotTo(HaveOccurred())

			narrow, err := domain.Register("Acme Freight", domain.PartnerTypeShipper, "acme-pacific-fleet")
			Expect(err).NotTo(HaveOccurred())
			Expect(widened).To(Equal(narrow))
		})

		// The required set is unchanged by the widening — BR-TP02 still holds.
		It("still requires name and context", func() {
			_, err := domain.RegisterWithDetails(domain.PartnerTypeShipper, "acme-pacific-fleet", domain.Details{CompanyName: "Acme Ltd"})
			Expect(errors.Is(err, domain.ErrNameRequired)).To(BeTrue())

			_, err = domain.RegisterWithDetails(domain.PartnerTypeShipper, "", domain.Details{Name: "Acme Freight"})
			Expect(errors.Is(err, domain.ErrContextRequired)).To(BeTrue())
		})
	})

	Context("BR-TP32: UpdateDetails mutates Company Information and nothing else", func() {
		registered := func() domain.Organization {
			tp, err := domain.Register("Acme Freight", domain.PartnerTypeShipper, "acme-pacific-fleet")
			Expect(err).NotTo(HaveOccurred())
			tp.ID = "11111111-1111-1111-1111-111111111111"
			tp.Version = 1
			return tp
		}

		It("updates all five editable fields", func() {
			tp, err := registered().UpdateDetails(1, domain.Details{
				Name:              "Acme Freight International",
				TradingAs:         "AFI",
				CompanyName:       "Acme Freight International (Pty) Ltd",
				RegistrationNo:    "2020/654321/07",
				VatRegistrationNo: "4987654321",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(tp.Name).To(Equal("Acme Freight International"))
			Expect(tp.TradingAs).To(Equal("AFI"))
			Expect(tp.CompanyName).To(Equal("Acme Freight International (Pty) Ltd"))
			Expect(tp.RegistrationNo).To(Equal("2020/654321/07"))
			Expect(tp.VatRegistrationNo).To(Equal("4987654321"))
		})

		// type gates document validity (BR-TP07) and fleet-asset attachment
		// (BR-TP12); context is the business-unit scope; status has its own
		// lifecycle. None of the three is reachable through this method.
		It("leaves type, context and status untouched", func() {
			before := registered()
			after, err := before.UpdateDetails(1, domain.Details{Name: "Renamed"})
			Expect(err).NotTo(HaveOccurred())
			Expect(after.Type).To(Equal(before.Type))
			Expect(after.Context).To(Equal(before.Context))
			Expect(after.Status).To(Equal(before.Status))
			Expect(after.ID).To(Equal(before.ID))
		})

		It("rejects an empty name, same as Register", func() {
			_, err := registered().UpdateDetails(1, domain.Details{Name: ""})
			Expect(errors.Is(err, domain.ErrNameRequired)).To(BeTrue())
		})
	})

	Context("BR-TP33/BR-TP34: optimistic concurrency across operator think-time", func() {
		registered := func() domain.Organization {
			tp, err := domain.Register("Acme Freight", domain.PartnerTypeShipper, "acme-pacific-fleet")
			Expect(err).NotTo(HaveOccurred())
			tp.ID = "11111111-1111-1111-1111-111111111111"
			tp.Version = 1
			return tp
		}

		It("bumps version by exactly one on a successful update", func() {
			tp, err := registered().UpdateDetails(1, domain.Details{Name: "Renamed"})
			Expect(err).NotTo(HaveOccurred())
			Expect(tp.Version).To(Equal(2))
		})

		It("rejects a stale version and writes nothing", func() {
			current := registered()
			current.Version = 4

			_, err := current.UpdateDetails(2, domain.Details{Name: "Stale writer"})
			Expect(errors.Is(err, domain.ErrVersionConflict)).To(BeTrue())
			Expect(current.Name).To(Equal("Acme Freight"), "a rejected update must not partially apply")
			Expect(current.Version).To(Equal(4))
		})

		It("rejects a version from the future", func() {
			_, err := registered().UpdateDetails(9, domain.Details{Name: "Impossible"})
			Expect(errors.Is(err, domain.ErrVersionConflict)).To(BeTrue())
		})

		// The rule this sub-phase exists for (ADR-049 finding 5a): two
		// operators open the same edit form, both read version 1, and both
		// save. A `SELECT ... FOR UPDATE` cannot catch this — the two reads
		// are in different transactions, minutes apart. The version check is
		// what makes the second save fail instead of silently overwriting.
		It("lets the first of two concurrent editors win and rejects the second", func() {
			loadedByAlice := registered()
			loadedByBob := registered()

			afterAlice, err := loadedByAlice.UpdateDetails(1, domain.Details{
				Name:        "Acme Freight",
				CompanyName: "Set by Alice",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(afterAlice.Version).To(Equal(2))

			// Bob is still holding version 1. Re-reading the row now yields
			// Alice's version 2, so Bob's save is rejected.
			loadedByBob.Version = afterAlice.Version
			_, err = loadedByBob.UpdateDetails(1, domain.Details{
				Name:        "Acme Freight",
				CompanyName: "Set by Bob",
			})
			Expect(errors.Is(err, domain.ErrVersionConflict)).To(BeTrue())
			Expect(afterAlice.CompanyName).To(Equal("Set by Alice"), "the winner's write must survive intact")
		})
	})
})
