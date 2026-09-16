package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

// The read side.
//
// Compare this file with write.go. That is the exercise.
//
//	write.go  rehydrates an aggregate, checks a rule, appends one event.
//	read.go   folds every event into a flat document, and reads one key.
//
// Nothing here replays a history to answer a question, and nothing here
// refuses anything. A rule on this side would be a rule enforced twice and
// agreed on never.

// ReadEntry is what KV odometer-read holds for one vehicle.
//
// Odometer is embedded, so the stored JSON stays flat and built for reading:
// {status, plate, totalKm, trips, lastTripAt, lastSeq}.
//
// The embedded Fold is not for the reader. It is how the projector decides
// whether an event may be applied at all -- BR-OD06..08. JetStream delivers
// at least once, and a fold that is not idempotent turns a redelivery into a
// wrong total.
type ReadEntry struct {
	Odometer
	Fold
}

// runProjector folds every event into the read store until ctx is cancelled.
//
// The consumer is DURABLE and long-lived, the opposite of the ephemeral one
// rehydration uses. It remembers its place, so restarting the projector
// resumes; deleting the bucket and the consumer together rebuilds the whole
// read model from the log, which is the point of keeping the log forever.
func runProjector(ctx context.Context, js jetstream.JetStream, kv jetstream.KeyValue) error {
	consumer, err := js.CreateOrUpdateConsumer(ctx, StreamName, jetstream.ConsumerConfig{
		Durable:       ReadConsumer,
		FilterSubject: StreamSubject,
		AckPolicy:     jetstream.AckExplicitPolicy,
		DeliverPolicy: jetstream.DeliverAllPolicy,
		// A projection is a fold. One at a time, in order.
		MaxAckPending: 1,
	})
	if err != nil {
		return fmt.Errorf("read consumer: %w", err)
	}

	sub, err := consumer.Consume(func(msg jetstream.Msg) {
		switch err := project(ctx, kv, msg); {
		case Permanent(err):
			// BR-OD09. Term, not nak. These bytes are in the log for
			// good and the next delivery reads exactly like this one,
			// so a nak here redelivers the same event for ever -- and
			// with MaxAckPending 1 the whole read model stops behind
			// it. Terminating drops ONE event, loudly, and lets the
			// rest of the log through.
			_ = msg.Term()
			log.Printf("projector: DROPPED %s — %v", msg.Subject(), err)
		case err != nil:
			// A bad moment, not a bad event. Retry.
			log.Printf("projector: %v", err)
			_ = msg.Nak()
		default:
			_ = msg.Ack()
		}
	})
	if err != nil {
		return fmt.Errorf("read consume: %w", err)
	}
	defer sub.Stop()

	<-ctx.Done()
	return nil
}

// project applies one event to one vehicle's read document.
//
// The event's own stream timestamp is used, never the wall clock. Rebuilding
// the read model next year must produce the same lastTripAt it produced
// today, or the read model is a record of when it was built rather than of
// what happened.
func project(ctx context.Context, kv jetstream.KeyValue, msg jetstream.Msg) error {
	id, err := vehicleIDFrom(msg.Subject())
	if err != nil {
		return err
	}
	meta, err := msg.Metadata()
	if err != nil {
		return err
	}
	event, err := decode(msg.Subject(), msg.Data())
	if err != nil {
		return err
	}

	seq := meta.Sequence.Stream

	entry, err := kv.Get(ctx, snapshotKey(id))
	switch {
	case errors.Is(err, jetstream.ErrKeyNotFound):
		first := ReadEntry{
			Odometer: Odometer{}.Apply(event, meta.Timestamp),
			Fold:     Fold{}.Advance(seq),
		}
		body, _ := json.Marshal(first)
		_, err = kv.Create(ctx, snapshotKey(id), body)
		return err
	case err != nil:
		return err
	}

	var current ReadEntry
	if err := json.Unmarshal(entry.Value(), &current); err != nil {
		return err
	}
	// BR-OD06..08. Three answers, not two: apply it, ignore a redelivery, or
	// refuse an event that arrived behind the fold. The refusal is what the
	// old `<=` used to swallow.
	apply, err := current.Next(seq)
	if err != nil {
		return err
	}
	if !apply {
		return nil // BR-OD07 — a redelivery, not a new fact
	}
	current.Odometer = current.Odometer.Apply(event, meta.Timestamp)
	current.Fold = current.Advance(seq)
	body, _ := json.Marshal(current)
	// Compare-and-swap on the revision. Two projectors must not overwrite
	// each other into a read model that skipped an event.
	_, err = kv.Update(ctx, snapshotKey(id), body, entry.Revision())
	return err
}

// ErrNoReadEntry means the read store has no document for this vehicle.
//
// There is deliberately no fallback to the log. A query that quietly replayed
// the stream would hide exactly the thing this demo measures, and would make
// the read side as slow as the write side on the one day it mattered.
var ErrNoReadEntry = errors.New("no read entry — the projector has not seen this vehicle")

// queryVehicle is the entire read path: one KV Get.
func queryVehicle(ctx context.Context, kv jetstream.KeyValue, id string) (ReadEntry, time.Duration, error) {
	var out ReadEntry
	start := time.Now()
	entry, err := kv.Get(ctx, snapshotKey(id))
	elapsed := time.Since(start)
	if errors.Is(err, jetstream.ErrKeyNotFound) {
		return out, elapsed, ErrNoReadEntry
	}
	if err != nil {
		return out, elapsed, err
	}
	return out, elapsed, json.Unmarshal(entry.Value(), &out)
}
