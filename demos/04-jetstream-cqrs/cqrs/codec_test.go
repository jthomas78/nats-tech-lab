package main

// The codec carries no business rule, but it carries every business rule's
// input. An event that decodes into the wrong type rehydrates the wrong
// aggregate, and the wrong aggregate approves a command the rules forbid.

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("the event codec", func() {

	roundTrip := func(id string, in Event) Event {
		body, err := encode(in)
		Expect(err).NotTo(HaveOccurred())
		out, err := decode(vehicleSubject(id, in.EventType()), body)
		Expect(err).NotTo(HaveOccurred())
		return out
	}

	Context("a round trip through a subject and a body", func() {
		It("keeps a Registered whole", func() {
			Expect(roundTrip("V1", Registered{Plate: "CA 123-456"})).
				To(Equal(Registered{Plate: "CA 123-456"}))
		})
		It("keeps a Travelled whole", func() {
			Expect(roundTrip("V1", Travelled{Km: 12.5})).To(Equal(Travelled{Km: 12.5}))
		})
		It("keeps a Retired whole", func() {
			Expect(roundTrip("V1", Retired{Reason: "sold"})).To(Equal(Retired{Reason: "sold"}))
		})
	})

	Context("an event type nobody knows", func() {
		It("is an error, never a silent skip", func() {
			_, err := decode("evt.odometer.vehicle.V1.exploded", []byte(`{}`))
			Expect(err).To(HaveOccurred())
		})
	})

	Context("the vehicle id in a subject", func() {
		It("is read by position", func() {
			id, err := vehicleIDFrom("evt.odometer.vehicle.V1.travelled")
			Expect(err).NotTo(HaveOccurred())
			Expect(id).To(Equal("V1"))
		})
		It("refuses a subject of the wrong shape", func() {
			_, err := vehicleIDFrom("evt.odometer.vehicle.V1")
			Expect(err).To(HaveOccurred())
		})
	})
})
