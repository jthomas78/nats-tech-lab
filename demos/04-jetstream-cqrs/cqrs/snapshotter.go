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
		if err := foldIntoSnapshot(ctx, kv, msg); err != nil {
			log.Printf("snapshot: %v", err)
			_ = msg.Nak()
			return
		}
		_ = msg.Ack()
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

	entry, err := kv.Get(ctx, snapshotKey(id))
	switch {
	case errors.Is(err, jetstream.ErrKeyNotFound):
		snap := Snapshot{State: Vehicle{}.Apply(event), LastSeq: meta.Sequence.Stream}
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
	// Already folded. Redelivery is normal in JetStream, and a fold that is
	// not idempotent turns an at-least-once delivery into a wrong total.
	if meta.Sequence.Stream <= snap.LastSeq {
		return nil
	}
	snap.State = snap.State.Apply(event)
	snap.LastSeq = meta.Sequence.Stream
	body, _ := json.Marshal(snap)
	_, err = kv.Update(ctx, snapshotKey(id), body, entry.Revision())
	return err
}

// vehicleIDFrom pulls the id out of evt.odometer.vehicle.{id}.{event}.
// Fixed arity, read by position -- the id is never split on.
func vehicleIDFrom(subject string) (string, error) {
	parts := strings.Split(subject, ".")
	if len(parts) != 5 {
		return "", fmt.Errorf("subject %q is not a vehicle event", subject)
	}
	return parts[3], nil
}
