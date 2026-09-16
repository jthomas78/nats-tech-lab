package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// Lesson 02's own log (plan 04.8, decisions D1, D2, D4, D9, D11).
//
// Until 04.8, lesson 02 bound its durable consumer to ODOMETER. It published
// nothing there, which is why nobody noticed -- the sharing was a CONSUMER,
// not events. But that consumer is deliberately broken: it is the whole point
// of the lesson that it reorders, starves and drops. Pointing it at the log
// every other screen is drawn from was one press away from a reader who had
// not read the warning.
//
// So lesson 02 gets a log it owns and may ruin. This file builds it.
//
// It does NOT build the correct fold -- that is 04.8.3, and it is a real fold
// by a real consumer, not a number written here. A fixture that computed the
// right answer would prove the arithmetic, not the fold.

// PoolSizes are the log lengths a reader may ask for, in events.
//
// The same three the benchmark offers, kept as its own list on purpose. The
// two fixtures answer different questions and may want to diverge later;
// sharing one variable would make that a surprise rather than a choice.
var PoolSizes = []int{10_000, 100_000, 1_000_000}

// DefaultPoolSize is what `cqrs pool -seed` writes with no number.
//
// The whole of lesson 02 is priced on this. A starvation set is four runs at
// 1 / 3 / 8 / 64 in-flight, and at 10 000 events that is about 60 seconds --
// inside what the screen tells a reader before they press. At 100 000 it is
// ten minutes of watching a bar, which is a different product.
const DefaultPoolSize = 10_000

// ErrUnknownPoolSize is returned for a size that is not on the list. Named so
// the HTTP shim can answer 400 without matching on text.
var ErrUnknownPoolSize = errors.New("not a pool size")

// PoolVehicles are the vehicles the log is spread over.
//
// Ten, and not one. Two reasons, both about what the lesson can show:
//
//	The odometer-pool tab lists the drift PER VEHICLE, because a single
//	total tells you kilometres were lost and not where. One vehicle would
//	make that tab a single row.
//
//	The damage IS two workers handling the same vehicle out of order. Ten
//	vehicles and up to eight workers means they collide constantly. A
//	vehicle each would let every worker be right.
//
// Zero padded so `nats kv ls` lists them in the order a reader expects.
var PoolVehicles = poolVehicleList(10)

func poolVehicleList(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("pool-%02d", i+1)
	}
	return out
}

// poolKm is the distance on every seeded trip. One kilometre, so a vehicle's
// total is its trip count and a reader can check the fold by eye.
const poolKm = 1.0

// poolPlan describes the log without building it. Everything here is a
// decision, which is why it is a pure function with specs.
type poolPlan struct {
	Size     int      // what the button said
	Vehicles []string // who the events belong to
	Events   int      // events in the log, registrations included
	Trips    int      // how many of those are trips
	TripsPer int      // trips each vehicle gets
	Extra    int      // trips left over, handed to the first Extra vehicles
}

// poolPlanFor turns a size into a plan, or refuses it.
func poolPlanFor(size int) (poolPlan, error) {
	for _, n := range PoolSizes {
		if n != size {
			continue
		}
		// The registrations ARE part of the size. The number on the
		// button is the length of the log, so a reader comparing two
		// runs is comparing what the screen says.
		trips := n - len(PoolVehicles)
		return poolPlan{
			Size:     n,
			Vehicles: PoolVehicles,
			Events:   n,
			Trips:    trips,
			TripsPer: trips / len(PoolVehicles),
			// The remainder is written, not dropped. Ten vehicles
			// into 9 990 trips divides cleanly, but a future
			// vehicle count that did not would quietly give a
			// "10 000 events" log holding 9 994.
			Extra: trips % len(PoolVehicles),
		}, nil
	}
	return poolPlan{}, fmt.Errorf("%w: %d", ErrUnknownPoolSize, size)
}

// poolPurgeFilter is what a re-seed removes: EVERYTHING.
//
// The benchmark purges one vehicle, because three fixtures share
// ODOMETER_BENCH and seeding one must not delete the others. This log has one
// tenant, so a per-vehicle purge would strand any vehicle that was dropped
// from PoolVehicles between two seeds.
func poolPurgeFilter() string { return Pool.StreamSubject }

// PoolState is what the screen and the CLI are told about the log.
//
// Events and Bytes travel together. Standing rule, set by the user
// 2026-09-16: a count is never shown without its bytes, because a length is a
// number nobody can price.
type PoolState struct {
	Stream    string   `json:"stream"`
	Subject   string   `json:"subject"`
	TruthKV   string   `json:"truthKv"`
	Exists    bool     `json:"exists"`
	Events    int      `json:"events"`
	Bytes     uint64   `json:"bytes"`
	Sizes     []int    `json:"sizes"`
	Vehicles  []string `json:"vehicles"`
	SeededAt  string   `json:"seededAt,omitempty"`
	ElapsedMs float64  `json:"elapsedMs,omitempty"`
}

// ensurePoolStream creates the log if it is not there. Disposable: `cqrs pool
// -rm` costs the demo nothing, which is the point of it being separate.
func ensurePoolStream(ctx context.Context, js jetstream.JetStream) error {
	_, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:     Pool.Stream,
		Subjects: []string{Pool.StreamSubject},
		// LimitsPolicy, like every other stream here, and for the same
		// reason: this log is replayed from sequence 1 on every run.
		// InterestPolicy would discard each message once it was acked,
		// and the second run would drain an empty log with no error.
		Retention: jetstream.LimitsPolicy,
		Storage:   jetstream.FileStorage,
	})
	if err != nil {
		return fmt.Errorf("create %s: %w", Pool.Stream, err)
	}
	return nil
}

// seedPool builds the log and returns its new state.
//
// Like seedBench, this is not the command path and does not pretend to be. It
// writes a history that is correct by construction: one registration per
// vehicle, then identical trips, in order, from one writer. There is no race
// to lose and no rule that can be broken halfway through.
func seedPool(ctx context.Context, js jetstream.JetStream, size int) (PoolState, error) {
	plan, err := poolPlanFor(size)
	if err != nil {
		return PoolState{}, err
	}
	if err := ensurePoolStream(ctx, js); err != nil {
		return PoolState{}, err
	}

	// Re-seeding REPLACES. Without this, pressing the button twice would
	// give a 20 000-event "10 000" log, and every number the lesson
	// measures would stop matching its own label. Worse, it would stop
	// matching the same label from yesterday.
	if err := purgePool(ctx, js); err != nil {
		return PoolState{}, err
	}

	start := time.Now()
	if err := publishPoolFixture(ctx, js, plan); err != nil {
		return PoolState{}, err
	}
	elapsed := time.Since(start)

	state, err := poolState(ctx, js)
	if err != nil {
		return PoolState{}, err
	}
	state.SeededAt = time.Now().UTC().Format(time.RFC3339)
	state.ElapsedMs = float64(elapsed.Microseconds()) / 1000
	return state, nil
}

// publishPoolFixture appends the history.
//
// Async publish: every event is still acked by the server, the acks are just
// collected at the end instead of one round trip each. That is the difference
// between a million appends taking seconds and taking minutes.
func publishPoolFixture(ctx context.Context, js jetstream.JetStream, plan poolPlan) error {
	travelled, err := encode(Travelled{Km: poolKm})
	if err != nil {
		return err
	}

	for _, v := range plan.Vehicles {
		registered, err := encode(Registered{Plate: v})
		if err != nil {
			return err
		}
		subject := Pool.VehicleSubject(v, Registered{}.EventType())
		if _, err := js.PublishMsgAsync(&nats.Msg{Subject: subject, Data: registered}); err != nil {
			return fmt.Errorf("seed %s: %w", v, err)
		}
	}

	// Round robin, not vehicle by vehicle. A log grouped by vehicle would
	// let a worker take a whole vehicle's history in one contiguous run
	// and fold it in perfect order, which is the one thing this lesson
	// must not accidentally arrange.
	for i := 0; i < plan.TripsPer; i++ {
		for _, v := range plan.Vehicles {
			subject := Pool.VehicleSubject(v, Travelled{}.EventType())
			if _, err := js.PublishMsgAsync(&nats.Msg{Subject: subject, Data: travelled}); err != nil {
				return fmt.Errorf("seed %s at trip %d: %w", v, i+1, err)
			}
		}
	}
	for i := 0; i < plan.Extra; i++ {
		v := plan.Vehicles[i]
		subject := Pool.VehicleSubject(v, Travelled{}.EventType())
		if _, err := js.PublishMsgAsync(&nats.Msg{Subject: subject, Data: travelled}); err != nil {
			return fmt.Errorf("seed %s remainder: %w", v, err)
		}
	}

	select {
	case <-js.PublishAsyncComplete():
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// purgePool empties the log. A stream that is not there is the expected state
// before the first seed, not a failure.
func purgePool(ctx context.Context, js jetstream.JetStream) error {
	stream, err := js.Stream(ctx, Pool.Stream)
	if errors.Is(err, jetstream.ErrStreamNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open %s: %w", Pool.Stream, err)
	}
	if err := stream.Purge(ctx, jetstream.WithPurgeSubject(poolPurgeFilter())); err != nil {
		return fmt.Errorf("purge %s: %w", Pool.Stream, err)
	}
	return nil
}

// poolState reads what is in the log now.
//
// A stream that is not there is not an error: nobody has to seed, and the
// screen has to be able to say "not seeded yet" rather than show a failure.
func poolState(ctx context.Context, js jetstream.JetStream) (PoolState, error) {
	state := PoolState{
		Stream:   Pool.Stream,
		Subject:  Pool.StreamSubject,
		TruthKV:  PoolTruthKV,
		Sizes:    PoolSizes,
		Vehicles: PoolVehicles,
	}
	stream, err := js.Stream(ctx, Pool.Stream)
	if errors.Is(err, jetstream.ErrStreamNotFound) {
		return state, nil
	}
	if err != nil {
		return PoolState{}, fmt.Errorf("open %s: %w", Pool.Stream, err)
	}
	info, err := stream.Info(ctx)
	if err != nil {
		return PoolState{}, fmt.Errorf("info %s: %w", Pool.Stream, err)
	}
	state.Exists = true
	state.Events = int(info.State.Msgs)
	// The stream's OWN bytes, not an estimate. A reader pressing
	// "1 000 000" is spending disk, and the screen has to say how much.
	state.Bytes = info.State.Bytes
	return state, nil
}
