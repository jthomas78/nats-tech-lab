/*
What rehydrate actually does, with the log faked and nothing else.

	This demo exists to answer one question: how much does a snapshot buy you?
	The two numbers on the screen -- events read, and time taken -- both come
	out of rehydrate(), and until these specs were written nothing drove
	rehydrate() at all. Its four call sites (main.go, seed.go, write.go,
	serve.go) each need a live JetStream, and the Go suite starts no server, so
	the demo's headline measurement was the one piece of it with no spec.

	THE COVERAGE BOUNDARY, stated plainly. These specs cover rehydrate: the
	ordering of snapshot-then-tail, where the replay starts, what is counted,
	which state wins, and what the two snapshot flags mean. They do NOT cover
	the production loadSnapshot (the KV read and its JSON decode) or the
	production replay (the ordered ephemeral consumer, its NumPending stop
	condition and its cleanup). Those sit behind logAccess and are faked here.
	They remain uncovered and are tracked as such in the plan -- proving them
	needs a real server, which is a different task with a different cost.

	What is NOT faked is just as deliberate: decode() and Vehicle.Apply() run
	for real inside every spec below. The bytes are produced by the real
	encode() too. So a spec that says "the warm side ends up retired" is a
	claim about the actual fold, not about a test double agreeing with itself.

	Two things these specs refuse to assert. They never claim the snapshot side
	is faster -- that is a property of a machine on a day, not of this code,
	and a suite that asserts it is a suite that goes red for no reason. And
	they never read the real clock; the one timing spec drives a fake one.
*/
package main

import (
	"context"
	"errors"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

/* ---------- the fixed history ---------- */

// plate is the one the fixture registers with. Every spec that proves the
// two modes agree proves it against this exact string.
const plate = "CA 123-456"

// history is the fixed log both modes are rebuilt from: ten events at
// sequences 1..10. The first registers, the rest are trips.
//
// Trips are the point. Travelled leaves the Vehicle unchanged (domain.go), so
// nine of these ten events cost the cold side real work and change nothing.
// That is the shape the snapshot claim is made against.
func history() []Event {
	events := []Event{Registered{Plate: plate}}
	for i := 0; i < 9; i++ {
		events = append(events, Travelled{Km: 10})
	}
	return events
}

// retiringHistory is the same log with the last event retiring the vehicle.
// It exists for one spec: the stale-snapshot case, where the snapshot says
// registered and the tail says otherwise.
func retiringHistory() []Event {
	events := history()
	events[len(events)-1] = Retired{Reason: "sold"}
	return events
}

// foldOf applies the first n events by hand, using the real Apply, to build
// the state a snapshot at sequence n would have held.
func foldOf(events []Event, n int) Vehicle {
	var v Vehicle
	for _, e := range events[:n] {
		v = v.Apply(e)
	}
	return v
}

/* ---------- the fake log ---------- */

// replayCall is one invocation of the log, recorded.
//
// It is recorded because a returned answer cannot tell the two implementations
// apart. An implementation that skipped the tail replay whenever the snapshot
// was already at the head would return the right Vehicle every time, and would
// still be the bug this demo's comments warn about -- the snapshot is always
// stale, so the tail must always be read.
type replayCall struct {
	filter  string
	fromSeq uint64
}

// fakeLog is a logAccess over a slice of events held in memory.
type fakeLog struct {
	src    Source
	id     string
	events []Event

	// subjectOverride, when set, is the subject every replayed event is
	// delivered on. Only the decode spec uses it.
	subjectOverride string

	snap      Snapshot
	snapFound bool
	snapErr   error

	replayErr error

	// what happened, for the specs that must look
	replays   []replayCall
	snapshots int

	// clock, if a spec wants one. tick is added on every read.
	clock *time.Time
	tick  time.Duration
}

// access builds the logAccess rehydrate will run against.
func (f *fakeLog) access() logAccess {
	a := logAccess{
		snapshot: func(_ context.Context, _ string) (Snapshot, bool, error) {
			f.snapshots++
			return f.snap, f.snapFound, f.snapErr
		},
		replay: func(_ context.Context, _ Source, filter string, fromSeq uint64, fn foldFunc) error {
			f.replays = append(f.replays, replayCall{filter: filter, fromSeq: fromSeq})
			if f.replayErr != nil {
				return f.replayErr
			}
			// Real bytes, real subjects, and only the tail the caller asked
			// for -- the same slice a JetStream consumer with a start
			// sequence would deliver.
			for i, e := range f.events {
				seq := uint64(i + 1)
				if seq < fromSeq {
					continue
				}
				data, err := encode(e)
				if err != nil {
					return err
				}
				subject := f.src.VehicleSubject(f.id, e.EventType())
				if f.subjectOverride != "" {
					subject = f.subjectOverride
				}
				if err := fn(seq, subject, data); err != nil {
					return err
				}
			}
			return nil
		},
	}
	if f.clock != nil {
		a.now = func() time.Time {
			t := *f.clock
			*f.clock = t.Add(f.tick)
			return t
		}
	}
	return a
}

// withSnapshotAt gives the fake a snapshot holding the state after n events.
func (f *fakeLog) withSnapshotAt(n int) *fakeLog {
	f.snap = Snapshot{State: foldOf(f.events, n), Fold: Fold{LastSeq: uint64(n)}}
	f.snapFound = true
	return f
}

func newLog(events []Event) *fakeLog {
	return &fakeLog{src: Bench, id: "V1", events: events}
}

// run is the call under test.
func (f *fakeLog) run(withSnapshot bool) (Rehydrated, error) {
	return rehydrate(context.Background(), f.access(), f.src, f.id, withSnapshot)
}

/* ---------- the specs ---------- */

var _ = Describe("rehydrating one vehicle", func() {
	Context("the cold side, with no snapshot asked for", func() {
		It("starts at the first sequence and reads the whole history", func() {
			log := newLog(history()).withSnapshotAt(6)

			out, err := log.run(false)

			Expect(err).NotTo(HaveOccurred())
			Expect(out.FromSeq).To(Equal(uint64(1)))
			Expect(out.EventsRead).To(Equal(10))
			Expect(out.LastSeq).To(Equal(uint64(10)))
		})

		/* Even with a snapshot sitting right there. "No snapshot" is a
		   measurement condition, not a hint, and a cold side that quietly
		   read the snapshot would flatter every comparison on the screen. */
		It("does not read the snapshot at all", func() {
			log := newLog(history()).withSnapshotAt(6)

			_, err := log.run(false)

			Expect(err).NotTo(HaveOccurred())
			Expect(log.snapshots).To(Equal(0))
		})

		It("reports that no snapshot was asked for, and none was used", func() {
			out, err := newLog(history()).withSnapshotAt(6).run(false)

			Expect(err).NotTo(HaveOccurred())
			Expect(out.SnapshotRequested).To(BeFalse())
			Expect(out.SnapshotFound).To(BeFalse())
		})
	})

	Context("the warm side, with a snapshot at sequence 6", func() {
		It("starts after the snapshot and reads only the tail", func() {
			out, err := newLog(history()).withSnapshotAt(6).run(true)

			Expect(err).NotTo(HaveOccurred())
			Expect(out.FromSeq).To(Equal(uint64(7)))
			Expect(out.EventsRead).To(Equal(4))
			Expect(out.LastSeq).To(Equal(uint64(10)))
		})

		It("reports the snapshot as both asked for and found", func() {
			out, err := newLog(history()).withSnapshotAt(6).run(true)

			Expect(err).NotTo(HaveOccurred())
			Expect(out.SnapshotRequested).To(BeTrue())
			Expect(out.SnapshotFound).To(BeTrue())
		})

		It("asks the log only for this vehicle's events", func() {
			log := newLog(history()).withSnapshotAt(6)

			_, err := log.run(true)

			Expect(err).NotTo(HaveOccurred())
			Expect(log.replays).To(HaveLen(1))
			Expect(log.replays[0].filter).To(Equal(Bench.VehicleFilter("V1")))
		})
	})

	/* The claim the whole demo rests on: the shortcut is not a different
	   answer. Both sides are run over one fixed history and compared. */
	Context("the two sides, over the same fixed history", func() {
		It("ends at the same state", func() {
			cold, err := newLog(history()).withSnapshotAt(6).run(false)
			Expect(err).NotTo(HaveOccurred())
			warm, err := newLog(history()).withSnapshotAt(6).run(true)
			Expect(err).NotTo(HaveOccurred())

			Expect(warm.Vehicle).To(Equal(cold.Vehicle))
			Expect(warm.Vehicle.Status).To(Equal(StatusRegistered))
			Expect(warm.Vehicle.Plate).To(Equal(plate))
		})

		It("ends at the same sequence", func() {
			cold, _ := newLog(history()).withSnapshotAt(6).run(false)
			warm, _ := newLog(history()).withSnapshotAt(6).run(true)

			Expect(warm.LastSeq).To(Equal(cold.LastSeq))
			Expect(warm.LastSeq).To(Equal(uint64(10)))
		})

		/* The saving the panel prints is exactly this subtraction. Six is
		   the snapshot's sequence, and that is not a coincidence: the
		   snapshot is worth precisely the events it stands in for. */
		It("differs by the six events the snapshot stood in for", func() {
			cold, _ := newLog(history()).withSnapshotAt(6).run(false)
			warm, _ := newLog(history()).withSnapshotAt(6).run(true)

			Expect(cold.EventsRead - warm.EventsRead).To(Equal(6))
		})
	})

	Context("when the snapshot is already at the head", func() {
		/* An empty tail must still be asked for. The snapshot trails the
		   stream by an unknown amount, so "nothing new" is an answer only
		   the log can give -- it is never something the caller may assume. */
		It("still asks the log for the tail", func() {
			log := newLog(history()).withSnapshotAt(10)

			out, err := log.run(true)

			Expect(err).NotTo(HaveOccurred())
			Expect(log.replays).To(HaveLen(1))
			Expect(log.replays[0].fromSeq).To(Equal(uint64(11)))
			Expect(out.EventsRead).To(Equal(0))
		})

		It("keeps the snapshot's state and sequence", func() {
			out, err := newLog(history()).withSnapshotAt(10).run(true)

			Expect(err).NotTo(HaveOccurred())
			Expect(out.LastSeq).To(Equal(uint64(10)))
			Expect(out.Vehicle.Plate).To(Equal(plate))
		})
	})

	Context("when the snapshot is stale", func() {
		/* The snapshot is ALWAYS stale -- the snapshotter is asynchronous.
		   Here the tail carries a retirement the snapshot never saw, so a
		   rehydration that trusted the snapshot would hand the write side a
		   vehicle that is still on the road. */
		It("lets the tail overwrite what the snapshot said", func() {
			log := newLog(retiringHistory())
			log.snap = Snapshot{State: foldOf(retiringHistory(), 6), Fold: Fold{LastSeq: 6}}
			log.snapFound = true

			out, err := log.run(true)

			Expect(err).NotTo(HaveOccurred())
			Expect(log.snap.State.Status).To(Equal(StatusRegistered))
			Expect(out.Vehicle.Status).To(Equal(StatusRetired))
		})

		It("agrees with the cold side, which never saw the snapshot", func() {
			warmLog := newLog(retiringHistory())
			warmLog.snap = Snapshot{State: foldOf(retiringHistory(), 6), Fold: Fold{LastSeq: 6}}
			warmLog.snapFound = true

			cold, _ := newLog(retiringHistory()).run(false)
			warm, _ := warmLog.run(true)

			Expect(warm.Vehicle).To(Equal(cold.Vehicle))
		})
	})

	/* Asked for, and not there. This is normal: the snapshotter runs behind,
	   so a young vehicle has no snapshot yet. It is not an error and it is
	   not a wrong answer -- it is the full replay, which is always right. */
	Context("when no snapshot exists yet", func() {
		It("replays the whole history instead, with no error", func() {
			log := newLog(history()) // snapFound stays false

			out, err := log.run(true)

			Expect(err).NotTo(HaveOccurred())
			Expect(out.FromSeq).To(Equal(uint64(1)))
			Expect(out.EventsRead).To(Equal(10))
			Expect(out.Vehicle.Plate).To(Equal(plate))
		})

		/* The two flags are why they are two flags. "I asked" stays true so
		   the panel can still label this card the snapshot side; "I found
		   one" is false so nobody credits a saving that never happened. */
		It("reports the snapshot as asked for but not found", func() {
			out, err := newLog(history()).run(true)

			Expect(err).NotTo(HaveOccurred())
			Expect(out.SnapshotRequested).To(BeTrue())
			Expect(out.SnapshotFound).To(BeFalse())
		})

		It("reads exactly what the cold side reads", func() {
			cold, _ := newLog(history()).run(false)
			warm, _ := newLog(history()).run(true)

			Expect(warm.EventsRead).To(Equal(cold.EventsRead))
			Expect(warm.Vehicle).To(Equal(cold.Vehicle))
		})
	})

	Context("when the log cannot be read", func() {
		It("reports a broken snapshot read and does not replay", func() {
			log := newLog(history())
			log.snapErr = errors.New("kv down")

			_, err := log.run(true)

			Expect(err).To(MatchError(ContainSubstring("kv down")))
			Expect(log.replays).To(BeEmpty())
		})

		It("reports a broken tail read", func() {
			log := newLog(history()).withSnapshotAt(6)
			log.replayErr = errors.New("stream down")

			_, err := log.run(true)

			Expect(err).To(MatchError(ContainSubstring("stream down")))
		})

		/* An event the fold cannot read stops the rehydration. A partial
		   fold looks exactly like a complete one, and this is the only
		   place that can tell the difference. */
		It("stops on an event it cannot decode", func() {
			log := newLog(history())
			log.subjectOverride = "evt.odometer-bench.vehicle.V1.nonsense"

			out, err := log.run(false)

			Expect(err).To(MatchError(ErrUndecodable))
			Expect(out.EventsRead).To(Equal(0))
		})
	})

	/* Elapsed is measured, not asserted against a real clock. A spec that
	   said "elapsed > 0" would be asserting that two calls to time.Now on a
	   loaded machine differ, which is not this code's promise and is not
	   always true. With the clock held still the promise is exact. */
	Context("the number the panel divides", func() {
		It("is the span the fake clock advances by", func() {
			base := time.Unix(0, 0)
			log := newLog(history()).withSnapshotAt(6)
			log.clock = &base
			log.tick = 7 * time.Millisecond

			out, err := log.run(true)

			Expect(err).NotTo(HaveOccurred())
			// One read to start, one to finish: exactly one tick apart.
			Expect(out.Elapsed).To(Equal(7 * time.Millisecond))
		})
	})
})
