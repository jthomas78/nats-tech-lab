package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/nats-io/nats.go/jetstream"
)

// The snapshotter: the write-side projector.
//
// It is a DURABLE, long-lived consumer, and it is a different thing from the
// ephemeral consumer rehydration uses. This one remembers where it got to and
// keeps folding new events into KV odometer-write forever. Rehydration's one
// is created per command and thrown away.
//
// It is also asynchronous, and that is not a flaw to be fixed. It is the
// reason rehydration always replays the tail after lastSeq.

// runSnapshotter folds every event into the write-side snapshot until ctx is
// cancelled.
func runSnapshotter(ctx context.Context, js jetstream.JetStream, kv jetstream.KeyValue) error {
	consumer, err := js.CreateOrUpdateConsumer(ctx, StreamName, jetstream.ConsumerConfig{
		Durable:       SnapshotConsumer,
		FilterSubject: StreamSubject,
		AckPolicy:     jetstream.AckExplicitPolicy,
		DeliverPolicy: jetstream.DeliverAllPolicy,
		// One at a time, in order. A snapshot is a fold, and a fold that
		// runs out of order is just a wrong answer arrived at quickly.
		MaxAckPending: 1,
	})
	if err != nil {
		return fmt.Errorf("snapshot consumer: %w", err)
	}

	sub, err := consumer.Consume(func(msg jetstream.Msg) {
		switch err := foldIntoSnapshot(ctx, kv, msg); {
		case Permanent(err):
			// BR-OD09, and the same reasoning as the projector's. One
			// unreadable event must not stop the snapshot: a snapshot
			// that stops trailing the stream makes every rehydration
			// after it replay a tail that only grows.
			_ = msg.Term()
			log.Printf("snapshot: DROPPED %s — %v", msg.Subject(), err)
		case err != nil:
			log.Printf("snapshot: %v", err)
			_ = msg.Nak()
		default:
			_ = msg.Ack()
		}
	})
	if err != nil {
		return fmt.Errorf("snapshot consume: %w", err)
	}
	defer sub.Stop()

	<-ctx.Done()
	return nil
}

// foldIntoSnapshot applies one event to one vehicle's snapshot.
//
// The write is a compare-and-swap on the KV revision. Two snapshotters would
// otherwise overwrite each other and produce a snapshot that skipped an
// event -- and a snapshot that skipped an event makes a command approve what
// it should refuse.
func foldIntoSnapshot(ctx context.Context, kv jetstream.KeyValue, msg jetstream.Msg) error {
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
		snap := Snapshot{State: Vehicle{}.Apply(event), Fold: Fold{}.Advance(seq)}
		body, _ := json.Marshal(snap)
		_, err = kv.Create(ctx, snapshotKey(id), body)
		return err
	case err != nil:
		return err
	}

	var snap Snapshot
	if err := json.Unmarshal(entry.Value(), &snap); err != nil {
		return err
	}
	// BR-OD06..08. Redelivery is normal in JetStream and is ignored; an event
	// that arrived behind the fold is refused out loud instead of acked in
	// silence. This consumer holds MaxAckPending 1, so the refusal cannot
	// fire here today -- it is the pool in phase 04.7 that makes it speak.
	apply, err := snap.Next(seq)
	if err != nil {
		return err
	}
	if !apply {
		return nil // BR-OD07 — a redelivery, not a new fact
	}
	snap.State = snap.State.Apply(event)
	snap.Fold = snap.Advance(seq)
	body, _ := json.Marshal(snap)
	_, err = kv.Update(ctx, snapshotKey(id), body, entry.Revision())
	return err
}

// vehicleIDFrom pulls the id out of evt.odometer.vehicle.{id}.{event}.
// Fixed arity, read by position -- the id is never split on.
//
// A subject of the wrong shape is ErrUndecodable, BR-OD09. The stream filter
// is `evt.odometer.>`, wider than anything this code writes, so a stray
// `nats pub evt.odometer.oops` really does arrive here -- and a subject does
// not change shape between deliveries.
func vehicleIDFrom(subject string) (string, error) {
	parts := strings.Split(subject, ".")
	if len(parts) != 5 {
		return "", fmt.Errorf("%w: subject %q is not a vehicle event", ErrUndecodable, subject)
	}
	return parts[3], nil
}
