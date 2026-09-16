package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/nats-io/nats.go/jetstream"
)

// `cqrs pool -rm` (plan 04.8.4).
//
// Lesson 02 is the part of this demo a reader is invited to break. The way
// back to nothing has to be as easy to find as the way in, and it has to be
// complete -- a removal that left the durable consumer behind would have the
// next seed fold an empty log and report success.
//
// The list is a value, not a sequence of calls, because the list is the part
// that can be wrong in a way nobody notices until it has already run. ODOMETER
// is a PREFIX of ODOMETER_POOL. A drop written against a prefix, or one name
// pasted from the wrong constant, deletes the demo and prints "dropped".

// poolRemovalPlan is everything `pool -rm` deletes, and nothing else.
type poolRemovalPlan struct {
	Streams []string
	// ConsumerStream is the log the consumers hang off. Named separately
	// because deleting a consumer needs the stream that owns it.
	ConsumerStream string
	Consumers      []string
	Buckets        []string
}

func poolRemovalPlanFor() poolRemovalPlan {
	return poolRemovalPlan{
		Streams:        []string{Pool.Stream},
		ConsumerStream: Pool.Stream,
		// Both of them. The pool's own consumer is the deliberately
		// broken one; the truth consumer is the right answer beside it.
		Consumers: []string{PoolConsumer, PoolTruthConsumer},
		// Three. The wrong fold, the worker heartbeats the screen
		// watches, and the right answer. A bucket left behind draws a
		// screen full of yesterday's run.
		Buckets: []string{PoolKV, PoolWorkersKV, PoolTruthKV},
	}
}

// poolAlreadyGone lists the errors that mean "there was nothing to delete".
//
// Written out so the tolerance is a decision with a spec, and not a guess
// buried in an if. A second `-rm` is a no-op: a reader who presses Delete
// twice has done nothing wrong.
func poolAlreadyGone() []error {
	return []error{
		jetstream.ErrStreamNotFound,
		jetstream.ErrConsumerNotFound,
		jetstream.ErrBucketNotFound,
		jetstream.ErrKeyNotFound,
	}
}

// poolDropTolerates says whether an error is an already-gone. nil is not: a
// caller asking "may I ignore this" about nothing has lost its place.
func poolDropTolerates(err error) bool {
	if err == nil {
		return false
	}
	for _, gone := range poolAlreadyGone() {
		if errors.Is(err, gone) {
			return true
		}
	}
	return false
}

// dropPool deletes lesson 02 and leaves the demo standing.
//
// Consumers first, then the stream, then the buckets. The order is not
// cosmetic: deleting the stream takes its consumers with it, and a
// consumer-delete afterwards would return not-found for a reason that has
// nothing to do with the consumer having been there.
func dropPool(ctx context.Context, js jetstream.JetStream) error {
	plan := poolRemovalPlanFor()

	for _, name := range plan.Consumers {
		if err := js.DeleteConsumer(ctx, plan.ConsumerStream, name); err != nil && !poolDropTolerates(err) {
			return fmt.Errorf("delete consumer %s: %w", name, err)
		}
	}
	for _, name := range plan.Streams {
		if err := js.DeleteStream(ctx, name); err != nil && !poolDropTolerates(err) {
			return fmt.Errorf("delete %s: %w", name, err)
		}
	}
	for _, name := range plan.Buckets {
		if err := js.DeleteKeyValue(ctx, name); err != nil && !poolDropTolerates(err) {
			return fmt.Errorf("delete %s: %w", name, err)
		}
	}

	fmt.Printf("dropped     %s, %d consumers and %d buckets\n",
		Pool.Stream, len(plan.Consumers), len(plan.Buckets))
	return nil
}
