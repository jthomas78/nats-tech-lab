package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/nats-io/nats.go/jetstream"
)

// The correct fold for lesson 02 (plan 04.8.3, decision D3).
//
// Lesson 02 measures DAMAGE, and damage is a difference: the pool's total
// subtracted from the right total. Before 04.8 the right total came from
// odometer-read, which folds a different log. Giving lesson 02 its own log
// without also giving it its own right answer would leave the screen
// subtracting two unrelated histories and printing the remainder as a defect.
//
// So this file folds ODOMETER_POOL the boring way -- one consumer, one worker,
// one event at a time, in order -- into odometer-pool-truth.
//
// It does NOT compute the total. It could: the seed knows how many trips it
// wrote and every trip is one kilometre, so the answer is a multiplication.
// But then the number lesson 02 compares against would be arithmetic, and the
// claim on that screen is about FOLDING. A computed total would prove the
// seed can multiply, and would go on being right even if the fold broke.

// poolTruthStream is the log the correct fold reads. Named as a function so a
// spec can state the thing that matters -- that it is not ODOMETER.
func poolTruthStream() string { return Pool.Stream }

// poolTruthConsumerConfig is the consumer that feeds the correct fold.
//
// MaxAckPending 1 is the whole design. It is the same cap read.go uses, and
// for the same reason: a projection is a fold, and a fold is one at a time.
// Lesson 02's OWN consumer raises that cap on purpose -- that is the defect
// being taught -- so this one must not, or there would be nothing correct left
// to compare against.
func poolTruthConsumerConfig() jetstream.ConsumerConfig {
	return jetstream.ConsumerConfig{
		Durable:       PoolTruthConsumer,
		FilterSubject: Pool.StreamSubject,
		AckPolicy:     jetstream.AckExplicitPolicy,
		DeliverPolicy: jetstream.DeliverAllPolicy,
		MaxAckPending: 1,
	}
}

// foldPoolTruth builds the right answer and returns when the log is folded.
//
// It runs to completion INSIDE the seed. The alternative -- folding lazily the
// first time a screen asks -- would put the right answer's own delay inside
// the measurement of the wrong one.
//
// The fold per message is project(), unchanged: the same function the read
// model uses, pointed at a different bucket. Two folds that were meant to
// agree and were written twice would eventually stop agreeing.
func foldPoolTruth(ctx context.Context, js jetstream.JetStream, kv jetstream.KeyValue) error {
	consumer, err := js.CreateOrUpdateConsumer(ctx, poolTruthStream(), poolTruthConsumerConfig())
	if err != nil {
		return fmt.Errorf("pool truth consumer: %w", err)
	}

	runCtx, stop := context.WithCancel(ctx)
	defer stop()

	sub, err := consumer.Consume(func(msg jetstream.Msg) {
		switch err := project(runCtx, kv, msg); {
		case Permanent(err):
			// BR-OD09, same as the projector: these bytes are in the
			// log for good, so a nak would redeliver the same event
			// for ever and, at a cap of 1, stop the whole fold behind
			// it. Terminate drops one event and lets the rest through.
			_ = msg.Term()
		case err != nil:
			_ = msg.Nak()
		default:
			_ = msg.Ack()
		}
	})
	if err != nil {
		return fmt.Errorf("pool truth consume: %w", err)
	}
	defer sub.Stop()

	if err := waitDrained(runCtx, consumer); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}

// resetPoolTruth throws the old answer away before a re-seed.
//
// Both halves, and both are needed. Keeping the bucket would fold a new log
// on top of an old answer; keeping the consumer would leave it parked at the
// end of the log that has just been purged, so it would fold nothing and
// report success.
func resetPoolTruth(ctx context.Context, js jetstream.JetStream, kv jetstream.KeyValue) error {
	if err := js.DeleteConsumer(ctx, Pool.Stream, PoolTruthConsumer); err != nil &&
		!errors.Is(err, jetstream.ErrConsumerNotFound) &&
		!errors.Is(err, jetstream.ErrStreamNotFound) {
		return fmt.Errorf("reset pool truth consumer: %w", err)
	}
	keys, err := kv.Keys(ctx)
	if errors.Is(err, jetstream.ErrNoKeysFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("reset pool truth bucket: %w", err)
	}
	for _, k := range keys {
		if err := kv.Purge(ctx, k); err != nil {
			return fmt.Errorf("reset pool truth bucket: %w", err)
		}
	}
	return nil
}
