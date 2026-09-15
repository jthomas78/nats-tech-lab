package main

// One Context per business rule, named by its ID, so a failing spec tree
// points straight at the rule it broke.

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// registered is a vehicle in service -- the state most rules are checked in.
func registered() Vehicle {
	return Vehicle{}.Apply(Registered{Plate: "CA 123-456"})
}

// retired is a vehicle that has left service.
func retired() Vehicle {
	return registered().Apply(Retired{Reason: "sold"})
}

var _ = Describe("the vehicle aggregate", func() {

	Context("BR-OD01 — km must be greater than 0", func() {
		It("accepts a real trip", func() {
			e, err := registered().Travel(RecordTrip{Km: 12.5})
			Expect(err).NotTo(HaveOccurred())
			Expect(e).To(Equal(Travelled{Km: 12.5}))
		})

		It("accepts a tiny trip", func() {
			_, err := registered().Travel(RecordTrip{Km: 0.1})
			Expect(err).NotTo(HaveOccurred())
		})

		It("rejects zero", func() {
			_, err := registered().Travel(RecordTrip{Km: 0})
			Expect(err).To(MatchError(ErrNonPositiveKm))
		})

		It("rejects a negative trip, so a total can never fall", func() {
			_, err := registered().Travel(RecordTrip{Km: -12.5})
			Expect(err).To(MatchError(ErrNonPositiveKm))
		})
	})

	Context("BR-OD02 — a vehicle must be registered before it travels", func() {
		It("refuses a trip for a vehicle it has never seen", func() {
			_, err := Vehicle{}.Travel(RecordTrip{Km: 12.5})
			Expect(err).To(MatchError(ErrNotRegistered))
		})

		It("allows the trip once the vehicle is registered", func() {
			_, err := registered().Travel(RecordTrip{Km: 12.5})
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("BR-OD03 — a vehicle cannot be registered twice", func() {
		It("registers an unknown vehicle", func() {
			e, err := Vehicle{}.Register(RegisterVehicle{Plate: "CA 123-456"})
			Expect(err).NotTo(HaveOccurred())
			Expect(e).To(Equal(Registered{Plate: "CA 123-456"}))
		})

		It("refuses a second registration", func() {
			_, err := registered().Register(RegisterVehicle{Plate: "CA 123-456"})
			Expect(err).To(MatchError(ErrAlreadyRegistered))
		})

		It("refuses to re-register a retired vehicle", func() {
			_, err := retired().Register(RegisterVehicle{Plate: "CA 123-456"})
			Expect(err).To(MatchError(ErrAlreadyRegistered))
		})
	})

	Context("BR-OD04 — a retired vehicle refuses trips", func() {
		It("refuses a trip after retirement", func() {
			_, err := retired().Travel(RecordTrip{Km: 12.5})
			Expect(err).To(MatchError(ErrRetired))
		})
	})

	Context("BR-OD05 — a vehicle cannot be retired unless it is registered", func() {
		It("retires a registered vehicle", func() {
			e, err := registered().Retire(RetireVehicle{Reason: "sold"})
			Expect(err).NotTo(HaveOccurred())
			Expect(e).To(Equal(Retired{Reason: "sold"}))
		})

		It("refuses to retire a vehicle it has never seen", func() {
			_, err := Vehicle{}.Retire(RetireVehicle{Reason: "sold"})
			Expect(err).To(MatchError(ErrNotRegistered))
		})

		It("refuses to retire the same vehicle twice", func() {
			_, err := retired().Retire(RetireVehicle{Reason: "sold again"})
			Expect(err).To(MatchError(ErrRetired))
		})
	})

	Context("rehydration", func() {
		It("starts unknown, which is what an empty replay must produce", func() {
			Expect(Vehicle{}.Status).To(Equal(StatusUnknown))
		})

		It("carries no total, because no rule reads one", func() {
			v := registered().Apply(Travelled{Km: 12.5}).Apply(Travelled{Km: 12.5})
			Expect(v).To(Equal(registered()))
		})
	})
})

var _ = Describe("the odometer read model", func() {
	t1 := time.Date(2026, 9, 14, 8, 0, 0, 0, time.UTC)
	t2 := t1.Add(time.Hour)

	It("counts up, one event per trip", func() {
		o := Odometer{}.
			Apply(Registered{Plate: "CA 123-456"}, t1).
			Apply(Travelled{Km: 12.5}, t1).
			Apply(Travelled{Km: 12.5}, t2)

		Expect(o.TotalKm).To(Equal(25.0))
		Expect(o.Trips).To(Equal(2))
		Expect(o.LastTripAt).To(Equal(t2))
		Expect(o.Plate).To(Equal("CA 123-456"))
	})

	// Apply the SAME trip twice and the total doubles. So a total of 25 after
	// ONE publish of 12.5 is not a rounding question -- it is proof the event
	// was projected twice, which is what a deleted durable consumer causes.
	It("distinguishes one projection from two", func() {
		once := Odometer{}.Apply(Travelled{Km: 12.5}, t1)
		twice := once.Apply(Travelled{Km: 12.5}, t1)
		Expect(once.TotalKm).NotTo(Equal(twice.TotalKm))
	})

	It("records retirement without disturbing the total", func() {
		o := Odometer{}.
			Apply(Registered{Plate: "CA 123-456"}, t1).
			Apply(Travelled{Km: 12.5}, t1).
			Apply(Retired{Reason: "sold"}, t2)

		Expect(o.Status).To(Equal(StatusRetired))
		Expect(o.TotalKm).To(Equal(12.5))
	})

	It("uses the stream timestamp, so a re-projection gives the same answer", func() {
		first := Odometer{}.Apply(Travelled{Km: 12.5}, t1)
		again := Odometer{}.Apply(Travelled{Km: 12.5}, t1)
		Expect(first).To(Equal(again))
	})
})

// A fold's position is the only state these rules read. There is no vehicle
// here and no total -- BR-OD06..08 answer "may this event be applied", and
// nothing about what applying it would produce.
var _ = Describe("the fold position", func() {

	Context("BR-OD06 — a sequence ahead of the position is applied", func() {
		It("applies the first event a fresh fold sees", func() {
			f := Fold{}
			next, err := f.Next(1)
			Expect(err).NotTo(HaveOccurred())
			Expect(next).To(BeTrue())
		})

		It("applies the next sequence in the log", func() {
			next, err := Fold{LastSeq: 41}.Next(42)
			Expect(err).NotTo(HaveOccurred())
			Expect(next).To(BeTrue())
		})

		// A gap is not an error. The consumer filters a subject, so the
		// sequences one vehicle's fold sees are never contiguous.
		It("applies a sequence that skips a gap", func() {
			next, err := Fold{LastSeq: 41}.Next(97)
			Expect(err).NotTo(HaveOccurred())
			Expect(next).To(BeTrue())
		})
	})

	Context("BR-OD07 — a sequence equal to the position is a redelivery", func() {
		It("ignores it, without an error", func() {
			next, err := Fold{LastSeq: 42}.Next(42)
			Expect(err).NotTo(HaveOccurred())
			Expect(next).To(BeFalse())
		})

		// The caller acks a redelivery. Refusing it would nak for ever,
		// because the second copy will never become new.
		It("is the answer JetStream's at-least-once delivery needs", func() {
			f := Fold{LastSeq: 42}
			for i := 0; i < 3; i++ {
				next, err := f.Next(42)
				Expect(err).NotTo(HaveOccurred())
				Expect(next).To(BeFalse())
			}
		})
	})

	Context("BR-OD08 — a sequence behind the position is out of order", func() {
		It("refuses it", func() {
			next, err := Fold{LastSeq: 42}.Next(41)
			Expect(err).To(MatchError(ErrOutOfOrder))
			Expect(next).To(BeFalse())
		})

		// This is the whole reason the rule exists. The old code said
		// `seq <= lastSeq -> return nil`, which acked an event it never
		// applied. The total was then short for ever, with no error.
		It("separates a late event from a repeated one", func() {
			f := Fold{LastSeq: 42}

			repeated, err := f.Next(42)
			Expect(err).NotTo(HaveOccurred())
			Expect(repeated).To(BeFalse())

			late, err := f.Next(7)
			Expect(err).To(MatchError(ErrOutOfOrder))
			Expect(late).To(BeFalse())
		})

		It("reports the sequence it refused and the position it held", func() {
			_, err := Fold{LastSeq: 42}.Next(7)
			Expect(err.Error()).To(ContainSubstring("7"))
			Expect(err.Error()).To(ContainSubstring("42"))
		})
	})

	Context("the fold advances only on an applied event", func() {
		It("moves to the applied sequence", func() {
			f := Fold{}
			_, err := f.Next(9)
			Expect(err).NotTo(HaveOccurred())
			Expect(f.Advance(9)).To(Equal(Fold{LastSeq: 9}))
		})

		// Advance is a separate call on purpose. The caller writes the
		// projection first and moves the position with it, in one KV value,
		// so a crash between the two cannot lose or double an event.
		It("leaves the position alone while deciding", func() {
			f := Fold{LastSeq: 9}
			_, _ = f.Next(10)
			Expect(f.LastSeq).To(Equal(uint64(9)))
		})
	})
})
