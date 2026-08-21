package organizations_test

import (
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/internal/domain"
)

var _ = Describe("FleetAsset Rules", func() {
	Context("BR-TP12: a fleet asset may only belong to a Transporter", func() {
		It("allows adding a fleet asset for a Transporter", func() {
			asset, err := domain.AddFleetAsset(domain.PartnerTypeTransporter, "CA123456", "1HGCM82633A004352", "Volvo", "FH16", "superlink-tautliner")
			Expect(err).NotTo(HaveOccurred())
			Expect(asset.RegistrationNo).To(Equal("CA123456"))
			Expect(asset.VehicleTypeCode).To(Equal("superlink-tautliner"))
		})

		It("rejects adding a fleet asset for a Shipper", func() {
			_, err := domain.AddFleetAsset(domain.PartnerTypeShipper, "CA123456", "1HGCM82633A004352", "Volvo", "FH16", "superlink-tautliner")
			Expect(errors.Is(err, domain.ErrFleetAssetRequiresTransporter)).To(BeTrue())
		})
	})

	Context("BR-TP13: registrationNo and vehicleTypeCode are required; vin/make/model stay optional free text", func() {
		It("creates a fleet asset from just registrationNo and vehicleTypeCode", func() {
			asset, err := domain.AddFleetAsset(domain.PartnerTypeTransporter, "CA123456", "", "", "", "superlink-tautliner")
			Expect(err).NotTo(HaveOccurred())
			Expect(asset.VIN).To(BeEmpty())
			Expect(asset.Make).To(BeEmpty())
			Expect(asset.Model).To(BeEmpty())
		})

		It("rejects a missing registrationNo", func() {
			_, err := domain.AddFleetAsset(domain.PartnerTypeTransporter, "", "1HGCM82633A004352", "Volvo", "FH16", "superlink-tautliner")
			Expect(errors.Is(err, domain.ErrRegistrationNoRequired)).To(BeTrue())
		})

		It("rejects a missing vehicleTypeCode", func() {
			_, err := domain.AddFleetAsset(domain.PartnerTypeTransporter, "CA123456", "1HGCM82633A004352", "Volvo", "FH16", "")
			Expect(errors.Is(err, domain.ErrVehicleTypeCodeRequired)).To(BeTrue())
		})
	})

	Context("BR-TP14: vehicleTypeCode existence in refdata's corpus is NOT checked here", func() {
		It("accepts any non-empty vehicleTypeCode at the domain layer, deferring corpus validation to 26d's tenant-scoped rpc.* adapter", func() {
			_, err := domain.AddFleetAsset(domain.PartnerTypeTransporter, "CA123456", "", "", "", "not-a-real-vehicle-type-code")
			Expect(err).NotTo(HaveOccurred(), "BR-TP14 is enforced by refdata-service via rpc.*, not by this pure domain function")
		})
	})
})
