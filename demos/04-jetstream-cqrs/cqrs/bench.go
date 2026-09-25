package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// The rehydrate benchmark fixture (plan 04.7.16).
//
// One size proves one dot. The claim this demo makes is that the gap between
// rehydrating WITH a snapshot and WITHOUT one grows with history, and a single
// 10 000-event vehicle cannot show a slope. So the Rehydrate panel can build
// three fixtures and measure all three.
//
// It builds them on ODOMETER_BENCH, never on ODOMETER. That is the whole
// reason this file exists: every other tab in the demo is drawn from
// ODOMETER, and a million events dropped into it would bury them.

// BenchSizes are the fixtures a reader may ask for, in events.
//
// A fixed list IS the cap. A typed length would need a validator, an error
// state and a ceiling, and none of that makes the slope any clearer. Three
// points, each ten times the last, is enough to see a straight line.
//
// 100 000 000 was asked for on 2026-09-16 and is deliberately absent. The
// time would have been fine -- about four minutes -- but at roughly 76 bytes
// an event it is about 7.6 GB on disk, and a demo fixture must not be able to
// fill the disk it runs on.
var BenchSizes = []int{10_000, 100_000, 1_000_000}

// ErrUnknownBenchSize is returned for a size that is not on the list. It is a
// named error so the HTTP shim can answer 400 without matching on text.
var ErrUnknownBenchSize = errors.New("not a benchmark size")

// benchTail is how many events are left OUT of the snapshot.
//
// CLAUDE.md: "The snapshot is always stale." In the real demo that is a fact
// about an asynchronous consumer; here it has to be arranged on purpose,
// because the seeder writes the snapshot itself and could trivially make it
// perfectly current. A perfectly current snapshot would measure a single KV
// read and call it a rehydration, which flatters the snapshot side and is not
// what this demo claims.
//
// It is the SAME at every size. The tail is what the snapshot side pays, so
// holding it still is what makes 10 000 and 1 000 000 comparable: the only
// thing that changed between them is the work the no-snapshot side must do.
const benchTail = 50

// benchKm is the distance on every seeded trip. One kilometre, so the total
// on screen is the trip count and a reader can check the fold by eye.
const benchKm = 1.0

// benchPlan describes one fixture without building it. Everything here is a
// decision, which is why it is a pure function with specs.
type benchPlan struct {
	Size    int    // what the button said
	Vehicle string // the vehicle this size lives on
	Events  int    // events in the log, registration included
	Trips   int    // how many of those are trips
	Tail    int    // events deliberately left out of the snapshot
	Folded  int    // events the snapshot does contain
}

// benchPlanFor turns a size into a plan, or refuses it.
func benchPlanFor(size int) (benchPlan, error) {
	for _, n := range BenchSizes {
		if n != size {
			continue
		}
		return benchPlan{
			Size: n,
			// One vehicle per size, so all three fixtures can exist
			// at once and a reader can walk up the three dots
			// without rebuilding anything.
			Vehicle: benchVehicle(n),
			// The registration IS one of the events. The number on
			// the button is the length of the log, so a reader
			// comparing two sizes is comparing what the screen says.
			Events: n,
			Trips:  n - 1,
			Tail:   benchTail,
			Folded: n - benchTail,
		}, nil
	}
	return benchPlan{}, fmt.Errorf("%w: %d", ErrUnknownBenchSize, size)
}

// benchVehicle names the vehicle one size lives on. Short enough to read in
// `nats kv ls`, and it says its own size.
func benchVehicle(n int) string {
	switch {
	case n%1_000_000 == 0:
		return fmt.Sprintf("bench-%dm", n/1_000_000)
	case n%1_000 == 0:
		return fmt.Sprintf("bench-%dk", n/1_000)
	default:
		return fmt.Sprintf("bench-%d", n)
	}
}

// BenchFixture is what the panel is told about one seeded size.
type BenchFixture struct {
	Size     int    `json:"size"`
	Vehicle  string `json:"vehicle"`
	Events   int    `json:"events"`   // events actually in the log now
	SnapSeq  uint64 `json:"snapSeq"`  // where the snapshot stops
	TailLeft int    `json:"tailLeft"` // events the snapshot side must replay
}

// BenchState is the whole fixture stream, as the panel shows it.
//
// Bytes is the stream's own state.bytes, not an estimate of ours. A reader
// pressing "1 000 000" is spending disk, and the screen has to say how much.
type BenchState struct {
	Stream    string         `json:"stream"`
	Subject   string         `json:"subject"`
	WriteKV   string         `json:"writeKv"`
	Exists    bool           `json:"exists"`
	Events    int            `json:"events"`
	Bytes     uint64         `json:"bytes"`
	Sizes     []int          `json:"sizes"`
	Fixtures  []BenchFixture `json:"fixtures"`
	SeededAt  string         `json:"seededAt,omitempty"`
	ElapsedMs float64        `json:"elapsedMs,omitempty"`
}

// ensureBench creates the fixture stream and its snapshot bucket if they are
// not there yet. Both are disposable: `nats stream rm ODOMETER_BENCH` costs
// the demo nothing.
func ensureBench(ctx context.Context, js jetstream.JetStream) (jetstream.KeyValue, error) {
	_, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:     Bench.Stream,
		Subjects: []string{Bench.StreamSubject},
		// LimitsPolicy, like the demo's own log, and for the same
		// reason: this stream exists to be replayed from sequence 1.
		// InterestPolicy would discard each message once it was acked
		// and the replay would return nothing, with no error.
		Retention: jetstream.LimitsPolicy,
		Storage:   jetstream.FileStorage,
	})
	if err != nil {
		return nil, fmt.Errorf("create %s: %w", Bench.Stream, err)
	}
	kv, err := ensureKV(ctx, js, Bench.WriteKV)
	if err != nil {
		return nil, err
	}
	return kv, nil
}

// seedBench builds one fixture and returns the whole stream's new state.
//
// It is NOT the command path and does not pretend to be. handleCommand
// rehydrates before every single append, which is right for a command and
// would be O(n^2) here. This writes a history that is correct by
// construction: one registration, then n-1 identical trips, in order, from
// one writer, so there is no race to lose and no rule that can be broken
// halfway through.
func seedBench(ctx context.Context, js jetstream.JetStream, kv jetstream.KeyValue, size int) (BenchState, error) {
	plan, err := benchPlanFor(size)
	if err != nil {
		return BenchState{}, err
	}

	// Re-seeding a size REPLACES it. Without the purge, pressing the
	// button twice would silently give a 20 000-event "10 000" fixture and
	// the measurement would stop matching its own label.
	if err := purgeBenchVehicle(ctx, js, kv, plan.Vehicle); err != nil {
		return BenchState{}, err
	}

	start := time.Now()
	snapSeq, err := publishFixture(ctx, js, plan)
	if err != nil {
		return BenchState{}, err
	}
	if err := writeBenchSnapshot(ctx, kv, plan, snapSeq); err != nil {
		return BenchState{}, err
	}
	elapsed := time.Since(start)

	state, err := benchState(ctx, js, kv)
	if err != nil {
		return BenchState{}, err
	}
	state.SeededAt = time.Now().UTC().Format(time.RFC3339)
	state.ElapsedMs = float64(elapsed.Microseconds()) / 1000
	return state, nil
}

// publishFixture appends the history and returns the stream sequence the
// snapshot should stop at.
//
// Async publish: each event is still acked by the server, the acks are just
// collected at the end instead of one round trip per event. That is what
// makes a million appends take about two seconds instead of minutes.
func publishFixture(ctx context.Context, js jetstream.JetStream, plan benchPlan) (uint64, error) {
	registered, err := encode(Registered{Plate: plan.Vehicle})
	if err != nil {
		return 0, err
	}
	travelled, err := encode(Travelled{Km: benchKm})
	if err != nil {
		return 0, err
	}

	regSubject := Bench.VehicleSubject(plan.Vehicle, Registered{}.EventType())
	tripSubject := Bench.VehicleSubject(plan.Vehicle, Travelled{}.EventType())

	// Only ONE ack future is kept: the one at the snapshot boundary. Keeping
	// a million of them would hold a million futures in memory to learn one
	// sequence number.
	var boundary jetstream.PubAckFuture

	first, err := js.PublishMsgAsync(&nats.Msg{Subject: regSubject, Data: registered})
	if err != nil {
		return 0, err
	}
	if plan.Folded == 1 {
		boundary = first
	}
	for i := 0; i < plan.Trips; i++ {
		f, err := js.PublishMsgAsync(&nats.Msg{Subject: tripSubject, Data: travelled})
		if err != nil {
			return 0, fmt.Errorf("seed %s at event %d: %w", plan.Vehicle, i+2, err)
		}
		if i+2 == plan.Folded {
			boundary = f
		}
	}

	select {
	case <-js.PublishAsyncComplete():
	case <-ctx.Done():
		return 0, ctx.Err()
	}
	if boundary == nil {
		return 0, fmt.Errorf("seed %s: no snapshot boundary", plan.Vehicle)
	}
	select {
	case ack := <-boundary.Ok():
		return ack.Sequence, nil
	case err := <-boundary.Err():
		return 0, fmt.Errorf("seed %s: %w", plan.Vehicle, err)
	default:
		return 0, fmt.Errorf("seed %s: boundary event was never acked", plan.Vehicle)
	}
}

// writeBenchSnapshot writes the {state, lastSeq} the snapshot side will read.
//
// The state is built by folding the same events that were just published,
// through the same Vehicle.Apply the real snapshotter uses. Computing the
// numbers directly would be faster and would let the fixture disagree with
// the domain.
func writeBenchSnapshot(ctx context.Context, kv jetstream.KeyValue, plan benchPlan, snapSeq uint64) error {
	v := Vehicle{}.Apply(Registered{Plate: plan.Vehicle})
	for i := 1; i < plan.Folded; i++ {
		v = v.Apply(Travelled{Km: benchKm})
	}
	snap := Snapshot{State: v, Fold: Fold{LastSeq: snapSeq}}
	body, err := json.Marshal(snap)
	if err != nil {
		return err
	}
	if _, err := kv.Put(ctx, snapshotKey(plan.Vehicle), body); err != nil {
		return fmt.Errorf("write bench snapshot %s: %w", plan.Vehicle, err)
	}
	return nil
}

// purgeBenchVehicle removes one fixture: its events and its snapshot. A
// missing stream, bucket or key is the expected state before the first seed,
// not a failure.
func purgeBenchVehicle(ctx context.Context, js jetstream.JetStream, kv jetstream.KeyValue, vehicle string) error {
	stream, err := js.Stream(ctx, Bench.Stream)
	if err != nil && !errors.Is(err, jetstream.ErrStreamNotFound) {
		return fmt.Errorf("open %s: %w", Bench.Stream, err)
	}
	if err == nil {
		if err := stream.Purge(ctx, jetstream.WithPurgeSubject(Bench.VehicleFilter(vehicle))); err != nil {
			return fmt.Errorf("purge %s: %w", vehicle, err)
		}
	}
	if err := kv.Delete(ctx, snapshotKey(vehicle)); err != nil &&
		!errors.Is(err, jetstream.ErrKeyNotFound) {
		return fmt.Errorf("delete bench snapshot %s: %w", vehicle, err)
	}
	return nil
}

// benchState reads what is in the fixture stream now.
//
// A stream that is not there is not an error: nobody has to seed, and the
// panel's job is to say "no fixture yet" and offer the button.
func benchState(ctx context.Context, js jetstream.JetStream, kv jetstream.KeyValue) (BenchState, error) {
	out := BenchState{
		Stream:   Bench.Stream,
		Subject:  Bench.StreamSubject,
		WriteKV:  Bench.WriteKV,
		Sizes:    BenchSizes,
		Fixtures: []BenchFixture{},
	}

	stream, err := js.Stream(ctx, Bench.Stream)
	if errors.Is(err, jetstream.ErrStreamNotFound) {
		return out, nil
	}
	if err != nil {
		return out, fmt.Errorf("open %s: %w", Bench.Stream, err)
	}
	info, err := stream.Info(ctx)
	if err != nil {
		return out, fmt.Errorf("info %s: %w", Bench.Stream, err)
	}
	out.Exists = true
	out.Events = int(info.State.Msgs)
	out.Bytes = info.State.Bytes

	// One entry per size that actually has events. The panel needs to know
	// which of the three buttons has already been pressed.
	for _, n := range BenchSizes {
		plan, err := benchPlanFor(n)
		if err != nil {
			continue
		}
		fixture, ok, err := benchFixture(ctx, js, kv, plan)
		if err != nil {
			return out, err
		}
		if ok {
			out.Fixtures = append(out.Fixtures, fixture)
		}
	}
	return out, nil
}

// benchFixture counts one vehicle's events and reads where its snapshot stops.
func benchFixture(ctx context.Context, js jetstream.JetStream, kv jetstream.KeyValue, plan benchPlan) (BenchFixture, bool, error) {
	stream, err := js.Stream(ctx, Bench.Stream)
	if err != nil {
		return BenchFixture{}, false, fmt.Errorf("open %s: %w", Bench.Stream, err)
	}
	// A per-subject count, not a replay. Asking the server how many
	// messages match a filter costs one API call; replaying a million
	// events to count them would cost a million.
	info, err := stream.Info(ctx, jetstream.WithSubjectFilter(Bench.VehicleFilter(plan.Vehicle)))
	if err != nil {
		return BenchFixture{}, false, fmt.Errorf("count %s: %w", plan.Vehicle, err)
	}
	events := 0
	for _, n := range info.State.Subjects {
		events += int(n)
	}
	if events == 0 {
		return BenchFixture{}, false, nil
	}

	snap, _, err := loadSnapshot(ctx, kv, plan.Vehicle)
	if err != nil {
		return BenchFixture{}, false, err
	}
	return BenchFixture{
		Size:     plan.Size,
		Vehicle:  plan.Vehicle,
		Events:   events,
		SnapSeq:  snap.LastSeq,
		TailLeft: plan.Tail,
	}, true, nil
}

// dropBench deletes the fixture, both halves. It is offered because the
// fixture is the one thing in this demo a reader can make a gigabyte of, and
// the way to undo that must be as easy to find as the way to do it.
func dropBench(ctx context.Context, js jetstream.JetStream) error {
	if err := js.DeleteStream(ctx, Bench.Stream); err != nil &&
		!errors.Is(err, jetstream.ErrStreamNotFound) {
		return fmt.Errorf("delete %s: %w", Bench.Stream, err)
	}
	if err := js.DeleteKeyValue(ctx, Bench.WriteKV); err != nil &&
		!errors.Is(err, jetstream.ErrBucketNotFound) {
		return fmt.Errorf("delete %s: %w", Bench.WriteKV, err)
	}
	fmt.Printf("dropped     %s and %s\n", Bench.Stream, Bench.WriteKV)
	return nil
}

// humanBytes prints a byte count a reader can price at a glance.
//
// A length is never printed without one of these beside it (plan 04.7.16).
func humanBytes(b uint64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := uint64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(b)/float64(div), "KMGTPE"[exp])
}

// printBench reports the fixture. Length and bytes together, always.
func printBench(s BenchState) {
	fmt.Printf("stream      %s (%s)\n", s.Stream, s.Subject)
	if !s.Exists {
		fmt.Printf("fixture     none yet — seed one with: cqrs bench -size %d\n", BenchSizes[0])
		return
	}
	fmt.Printf("holds       %d events, %s on disk\n", s.Events, humanBytes(s.Bytes))
	fmt.Printf("snapshots   KV %s\n", s.WriteKV)
	if len(s.Fixtures) == 0 {
		fmt.Printf("fixture     none yet — seed one with: cqrs bench -size %d\n", BenchSizes[0])
		return
	}
	for _, f := range s.Fixtures {
		fmt.Printf("fixture     %-12s %9d events, snapshot stops at seq %d, %d to replay\n",
			f.Vehicle, f.Events, f.SnapSeq, f.TailLeft)
	}
}
