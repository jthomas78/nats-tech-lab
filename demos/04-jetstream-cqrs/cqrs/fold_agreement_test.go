package main

// 04.7.2 — do the two long-lived consumers agree?
//
// The snapshotter folds into KV odometer-write; the projector folds into KV
// odometer-read. They read the SAME log, event for event, and they are
// separate processes with separate durables. If they can disagree, every
// number this demo prints is suspect.
//
// The task was written as "same lastSeq, same totalKm". Only half of it is a
// question. Vehicle has no TotalKm at all — Travelled.applyToVehicle returns
// the aggregate unchanged — so the two sides CANNOT hold the same total, and
// that is deliberate, not a gap. What must agree is the POSITION, and the
// fields both sides actually carry.
//
// These specs pin both halves: the agreement, and the disagreement.

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("the two consumers over one log", func() {
	at := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)

	// One log, replayed the way each consumer replays it. This is what
	// snapshotter.go and read.go do per message, with the NATS parts removed.
	replay := func(events []Event) (Snapshot, ReadEntry) {
		var snap Snapshot
		var read ReadEntry
		for i, e := range events {
			seq := uint64(i + 1)

			if ok, err := snap.Next(seq); err == nil && ok {
				snap.State = snap.State.Apply(e)
				snap.Fold = snap.Advance(seq)
			}
			if ok, err := read.Next(seq); err == nil && ok {
				read.Odometer = read.Odometer.Apply(e, at)
				read.Fold = read.Advance(seq)
			}
		}
		return snap, read
	}

	log := []Event{
		Registered{Plate: "CA 123-456"},
		Travelled{Km: 120},
		Travelled{Km: 80},
		Retired{Reason: "sold"},
	}

	Context("what they must agree on", func() {
		It("reaches the same position in the log", func() {
			snap, read := replay(log)

			Expect(snap.LastSeq).To(Equal(uint64(len(log))))
			Expect(snap.LastSeq).To(Equal(read.LastSeq))
		})

		It("agrees on the fields both sides carry", func() {
			snap, read := replay(log)

			Expect(snap.State.Status).To(Equal(read.Status))
			Expect(snap.State.Plate).To(Equal(read.Plate))
		})

		// The position is one field on one type, shared by embedding. A
		// second copy of this rule is how two consumers drift.
		It("takes the same decision for the same sequence, because it is the same Fold", func() {
			snap := Snapshot{Fold: Fold{LastSeq: 10}}
			read := ReadEntry{Fold: Fold{LastSeq: 10}}

			for _, seq := range []uint64{9, 10, 11} {
				sOK, sErr := snap.Next(seq)
				rOK, rErr := read.Next(seq)

				Expect(sOK).To(Equal(rOK))
				Expect(sErr == nil).To(Equal(rErr == nil))
			}
		})

		It("still agrees after a redelivery hits one side only", func() {
			snap, read := replay(log)

			// The snapshotter is handed sequence 3 a second time (BR-OD07).
			ok, err := snap.Next(3)
			Expect(err).To(HaveOccurred())
			Expect(ok).To(BeFalse())

			Expect(snap.LastSeq).To(Equal(read.LastSeq))
			Expect(snap.State.Status).To(Equal(read.Status))
		})
	})

	Context("what they must NOT agree on", func() {
		// This is the half of the task that cannot be measured, and the
		// reason it cannot be is the demo. A trip is a fact the read model
		// needs and no command is ever judged against, so the write side
		// drops it -- which is exactly why a write-side snapshot stays tiny
		// however long the log gets.
		It("keeps the kilometres on the read side only", func() {
			_, read := replay(log)

			Expect(read.TotalKm).To(Equal(200.0))
			Expect(read.Trips).To(Equal(2))
			// There is no snap.State.TotalKm to compare it against, and
			// this is the assertion that says so: 10 000 trips move the
			// read model and leave the aggregate byte-for-byte identical.
			many := append([]Event{}, log[:1]...)
			for i := 0; i < 10000; i++ {
				many = append(many, Travelled{Km: 1})
			}
			big, bigRead := replay(many)
			oneTrip, _ := replay(log[:2])

			Expect(big.State).To(Equal(oneTrip.State))
			Expect(bigRead.TotalKm).To(Equal(10000.0))
		})

		It("still moves the write-side position for an event it ignores", func() {
			// The aggregate did not change, but the snapshot MUST advance
			// anyway. A snapshot that stopped at the last interesting event
			// would make rehydration replay the boring tail for ever.
			one, _ := replay(log[:2])
			Expect(one.LastSeq).To(Equal(uint64(2)))
		})
	})
})
