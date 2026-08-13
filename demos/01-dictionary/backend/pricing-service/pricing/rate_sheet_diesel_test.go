package pricing_test

import (
	"errors"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/pricing-service/pricing/internal/domain"
)

// Helper: a published RateSheetVersion with one entry carrying diesel baseline
// fields. Minor version 0 = no overlays yet (BR-P23).
func baseEntry() domain.RateSheetEntry {
	return domain.RateSheetEntry{
		RouteKey:               "durban-gauteng",
		VehicleType:            "flatbed",
		CentBaseRate:           100_00, // R100.00
		DropPointCount:         1,
		CentAdditionalDropRate: 5_00, // R5.00 per extra drop
		DieselPct:              20.0,
		InitialDieselCents:     200_00, // R2.00/L
	}
}

func day(s string) time.Time {
	t, _ := time.Parse("2006-01-02", s)
	return t
}

var _ = Describe("RateSheet Diesel Overlay Rules", func() {

	Context("BR-P17: a rate-sheet version has a major.minor identity; diesel price changes bump minor only, content changes bump major", func() {
		It("a freshly-published version starts at minor version 0 with no overlays", func() {
			v := domain.RateSheetVersion{
				Version:      1,
				MinorVersion: 0,
				Status:       domain.VersionPublished,
				Entries:      []domain.RateSheetEntry{baseEntry()},
				Overlays:     nil,
			}
			Expect(v.MinorVersion).To(Equal(0))
			Expect(v.Overlays).To(BeEmpty())
		})

		It("appending a diesel overlay increments MinorVersion and does not change Version", func() {
			v := domain.RateSheetVersion{
				Version:      1,
				MinorVersion: 0,
				Status:       domain.VersionPublished,
				Entries:      []domain.RateSheetEntry{baseEntry()},
			}
			price := domain.DieselPrice{ActiveDate: day("2025-01-01"), CoastalCents: 250_00}
			v2, err := v.AppendDieselOverlay(price)
			Expect(err).NotTo(HaveOccurred())
			Expect(v2.Version).To(Equal(1), "major version must not change")
			Expect(v2.MinorVersion).To(Equal(1))
		})
	})

	Context("BR-P18: diesel price index maps an active date to a coastal/inland price; lookup returns the greatest active_date ≤ query date", func() {
		prices := []domain.DieselPrice{
			{ActiveDate: day("2025-01-01"), CoastalCents: 200_00},
			{ActiveDate: day("2025-03-01"), CoastalCents: 230_00},
			{ActiveDate: day("2025-06-01"), CoastalCents: 250_00},
		}

		It("returns the price in effect on an exact match date", func() {
			p, ok := domain.DieselPriceOn(prices, day("2025-03-01"))
			Expect(ok).To(BeTrue())
			Expect(p.CoastalCents).To(Equal(int64(230_00)))
		})

		It("returns the most-recent price for a date between two entries", func() {
			p, ok := domain.DieselPriceOn(prices, day("2025-04-15"))
			Expect(ok).To(BeTrue())
			Expect(p.CoastalCents).To(Equal(int64(230_00)))
		})

		It("returns not-found when no price was indexed on or before the query date", func() {
			_, ok := domain.DieselPriceOn(prices, day("2024-12-31"))
			Expect(ok).To(BeFalse())
		})
	})

	Context("BR-P19: a rate-sheet entry carries diesel baseline fields alongside its authored base rate", func() {
		It("entry exposes DieselPct and InitialDieselCents", func() {
			e := baseEntry()
			Expect(e.DieselPct).To(Equal(20.0))
			Expect(e.InitialDieselCents).To(Equal(int64(200_00)))
		})
	})

	Context("BR-P20: a diesel price change auto-appends a contiguous overlay; adjusted rate formula: base + base·(pct/100)·((current−initial)/initial)", func() {
		It("computes the correct adjusted rate for a known diesel move", func() {
			// base=10000, pct=20, initial=20000, current=25000
			// delta = (25000-20000)/20000 = 0.25
			// surcharge = 10000 * 0.20 * 0.25 = 500
			// adjusted = 10000 + 500 = 10500
			e := baseEntry()
			e.CentBaseRate = 100_00
			e.DieselPct = 20.0
			e.InitialDieselCents = 200_00

			adj, err := domain.AdjustedRate(e, 250_00)
			Expect(err).NotTo(HaveOccurred())
			Expect(adj).To(Equal(int64(105_00)))
		})

		It("returns the base rate unchanged when current diesel equals initial (zero delta)", func() {
			e := baseEntry()
			adj, err := domain.AdjustedRate(e, e.InitialDieselCents)
			Expect(err).NotTo(HaveOccurred())
			Expect(adj).To(Equal(e.CentBaseRate))
		})

		It("appending an overlay closes the previous overlay's window at the new start date", func() {
			v := domain.RateSheetVersion{
				Version:      1,
				MinorVersion: 1,
				Status:       domain.VersionPublished,
				Entries:      []domain.RateSheetEntry{baseEntry()},
				Overlays: []domain.DieselOverlay{
					{
						MinorVersion:    1,
						RouteKey:        "durban-gauteng",
						VehicleType:     "flatbed",
						StartDate:       day("2025-01-01"),
						EndDate:         nil,
						CentAdjustedRate: 105_00,
					},
				},
			}
			price2 := domain.DieselPrice{ActiveDate: day("2025-06-01"), CoastalCents: 260_00}
			v2, err := v.AppendDieselOverlay(price2)
			Expect(err).NotTo(HaveOccurred())
			Expect(v2.Overlays).To(HaveLen(2))
			Expect(v2.Overlays[0].EndDate).NotTo(BeNil(), "previous overlay window must be closed")
			Expect(*v2.Overlays[0].EndDate).To(Equal(day("2025-06-01")))
			Expect(v2.Overlays[1].EndDate).To(BeNil(), "new overlay has no end date yet")
		})
	})

	Context("BR-P21: no diesel price indexed on or before the load's effective date → reject (fail-closed)", func() {
		It("returns ErrNoDieselPrice when the index has no entry covering the date", func() {
			v := domain.RateSheetVersion{
				Version:      1,
				MinorVersion: 1,
				Status:       domain.VersionPublished,
				Entries:      []domain.RateSheetEntry{baseEntry()},
				Overlays: []domain.DieselOverlay{
					{
						MinorVersion:    1,
						RouteKey:        "durban-gauteng",
						VehicleType:     "flatbed",
						StartDate:       day("2025-01-01"),
						EndDate:         nil,
						CentAdjustedRate: 105_00,
					},
				},
			}
			// effectiveDate before any overlay — falls to base-rate path (BR-P23), not ErrNoDieselPrice
			// ErrNoDieselPrice fires at append time when no diesel price covers the new active_date
			emptyPrices := []domain.DieselPrice{}
			_, err := domain.AppendDieselOverlayFromIndex(v, emptyPrices, day("2025-06-01"))
			Expect(errors.Is(err, domain.ErrNoDieselPrice)).To(BeTrue())
		})
	})

	Context("BR-P22: load pricing resolves active major → entry → overlay window containing effectiveDate → adjusted rate + drop surcharge", func() {
		v := domain.RateSheetVersion{
			Version:      1,
			MinorVersion: 2,
			Status:       domain.VersionPublished,
			Entries:      []domain.RateSheetEntry{baseEntry()},
			Overlays: []domain.DieselOverlay{
				{
					MinorVersion:    1,
					RouteKey:        "durban-gauteng",
					VehicleType:     "flatbed",
					StartDate:       day("2025-01-01"),
					EndDate:         ptr(day("2025-06-01")),
					CentAdjustedRate: 105_00,
				},
				{
					MinorVersion:    2,
					RouteKey:        "durban-gauteng",
					VehicleType:     "flatbed",
					StartDate:       day("2025-06-01"),
					EndDate:         nil,
					CentAdjustedRate: 110_00,
				},
			},
		}

		It("returns the adjusted rate from the overlay window containing effectiveDate", func() {
			rate, err := v.RateForLoad("durban-gauteng", "flatbed", day("2025-03-15"), 1)
			Expect(err).NotTo(HaveOccurred())
			Expect(rate).To(Equal(int64(105_00)))
		})

		It("returns the latest overlay rate for a date in the open-ended window", func() {
			rate, err := v.RateForLoad("durban-gauteng", "flatbed", day("2026-01-01"), 1)
			Expect(err).NotTo(HaveOccurred())
			Expect(rate).To(Equal(int64(110_00)))
		})

		It("includes the additional-drop surcharge on top of the adjusted rate", func() {
			// 1 included drop, 2 address count → 1 extra drop at R5.00
			rate, err := v.RateForLoad("durban-gauteng", "flatbed", day("2026-01-01"), 2)
			Expect(err).NotTo(HaveOccurred())
			Expect(rate).To(Equal(int64(110_00 + 5_00)))
		})

		It("returns ErrEntryNotFound for an unregistered route/vehicle combination", func() {
			_, err := v.RateForLoad("cape-town-jhb", "flatbed", day("2026-01-01"), 1)
			Expect(errors.Is(err, domain.ErrEntryNotFound)).To(BeTrue())
		})
	})

	Context("BR-P23: effectiveDate before first overlay window falls back to the authored base rate; a new major starts with no overlays", func() {
		It("returns the authored base rate when effectiveDate precedes all overlays", func() {
			v := domain.RateSheetVersion{
				Version:      1,
				MinorVersion: 1,
				Status:       domain.VersionPublished,
				Entries:      []domain.RateSheetEntry{baseEntry()},
				Overlays: []domain.DieselOverlay{
					{
						MinorVersion:    1,
						RouteKey:        "durban-gauteng",
						VehicleType:     "flatbed",
						StartDate:       day("2025-01-01"),
						EndDate:         nil,
						CentAdjustedRate: 105_00,
					},
				},
			}
			rate, err := v.RateForLoad("durban-gauteng", "flatbed", day("2024-12-31"), 1)
			Expect(err).NotTo(HaveOccurred())
			Expect(rate).To(Equal(baseEntry().CentBaseRate), "authored base rate used when before first overlay")
		})

		It("a freshly-published version with no overlays always returns the authored base rate", func() {
			v := domain.RateSheetVersion{
				Version: 1,
				Status:  domain.VersionPublished,
				Entries: []domain.RateSheetEntry{baseEntry()},
			}
			rate, err := v.RateForLoad("durban-gauteng", "flatbed", day("2025-07-01"), 1)
			Expect(err).NotTo(HaveOccurred())
			Expect(rate).To(Equal(baseEntry().CentBaseRate))
		})
	})

	Context("BR-P24: an entry with no authored diesel baseline is skipped when an overlay is appended, not corrupted to zero", func() {
		It("appends no overlay for an entry whose InitialDieselCents is zero", func() {
			noBaselineEntry := domain.RateSheetEntry{
				RouteKey:       "chi-mia",
				VehicleType:    "dry-van-53ft",
				CentBaseRate:   180_00,
				DropPointCount: 1,
				// DieselPct/InitialDieselCents left at their zero value —
				// e.g. a pre-Phase-25i seeded entry that never authored one.
			}
			v := domain.RateSheetVersion{
				Version:      1,
				MinorVersion: 0,
				Status:       domain.VersionPublished,
				Entries:      []domain.RateSheetEntry{noBaselineEntry},
			}
			price := domain.DieselPrice{ActiveDate: day("2025-01-01"), CoastalCents: 250_00}
			v2, err := v.AppendDieselOverlay(price)
			Expect(err).NotTo(HaveOccurred())
			Expect(v2.MinorVersion).To(Equal(1), "minor version still bumps even when no overlay was appended")
			Expect(v2.Overlays).To(BeEmpty(), "no overlay is appended for an entry lacking a diesel baseline")
		})

		It("skips only the baseline-less entry, still overlaying entries that do carry one", func() {
			noBaselineEntry := domain.RateSheetEntry{RouteKey: "chi-mia", VehicleType: "dry-van-53ft", CentBaseRate: 180_00}
			v := domain.RateSheetVersion{
				Version:      1,
				MinorVersion: 0,
				Status:       domain.VersionPublished,
				Entries:      []domain.RateSheetEntry{noBaselineEntry, baseEntry()},
			}
			price := domain.DieselPrice{ActiveDate: day("2025-01-01"), CoastalCents: 250_00}
			v2, err := v.AppendDieselOverlay(price)
			Expect(err).NotTo(HaveOccurred())
			Expect(v2.Overlays).To(HaveLen(1))
			Expect(v2.Overlays[0].RouteKey).To(Equal(baseEntry().RouteKey))
		})

		It("a baseline-less entry keeps resolving to its authored base rate via RateForLoad", func() {
			noBaselineEntry := domain.RateSheetEntry{RouteKey: "chi-mia", VehicleType: "dry-van-53ft", CentBaseRate: 180_00}
			v := domain.RateSheetVersion{
				Version:      1,
				MinorVersion: 1,
				Status:       domain.VersionPublished,
				Entries:      []domain.RateSheetEntry{noBaselineEntry},
			}
			rate, err := v.RateForLoad("chi-mia", "dry-van-53ft", day("2026-01-01"), 1)
			Expect(err).NotTo(HaveOccurred())
			Expect(rate).To(Equal(int64(180_00)))
		})
	})
})

func ptr(t time.Time) *time.Time { return &t }
