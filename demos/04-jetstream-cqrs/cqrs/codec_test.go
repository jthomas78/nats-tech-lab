package main

// The codec carries no business rule, but it carries every business rule's
// input. An event that decodes into the wrong type rehydrates the wrong
// aggregate, and the wrong aggregate approves a command the rules forbid.

import (
	"encoding/json"
	"time"

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

// Both KV buckets hold a document that embeds Fold. Embedding is a Go detail
// and must stay one: a bucket written before phase 04.7 has to read back
// unchanged, and `nats kv get` has to keep printing the same shape.
var _ = Describe("what the KV buckets hold", func() {

	It("writes the write-side snapshot as {state, lastSeq}", func() {
		snap := Snapshot{State: Vehicle{Status: StatusRegistered, Plate: "CA 123-456"}}
		snap.Fold = snap.Advance(42)

		body, err := json.Marshal(snap)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(body)).To(Equal(
			`{"state":{"status":"registered","plate":"CA 123-456"},"lastSeq":42}`))
	})

	It("reads a document written before Fold was embedded", func() {
		var snap Snapshot
		err := json.Unmarshal(
			[]byte(`{"state":{"status":"retired","plate":"CA 123-456"},"lastSeq":7}`), &snap)

		Expect(err).NotTo(HaveOccurred())
		Expect(snap.State.Status).To(Equal(StatusRetired))
		Expect(snap.LastSeq).To(Equal(uint64(7)))
	})

	It("keeps the read model flat, with lastSeq beside the totals", func() {
		entry := ReadEntry{Odometer: Odometer{}.Apply(Registered{Plate: "CA 123-456"}, time.Time{})}
		entry.Fold = entry.Advance(9)

		body, err := json.Marshal(entry)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(body)).To(ContainSubstring(`"lastSeq":9`))
		Expect(string(body)).To(ContainSubstring(`"totalKm":0`))
		Expect(string(body)).NotTo(ContainSubstring(`"Fold"`))
	})
})
