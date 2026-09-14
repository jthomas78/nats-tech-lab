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

// The write side.
//
// One command does three things and nothing else:
//
//	1. rehydrate the aggregate from the log  (rehydrate)
//	2. ask the aggregate to decide           (domain.go -- not here)
//	3. append the one event it produced      (appendEvent)
//
// No rule is enforced in this file. A rule here would be a rule no spec can
// reach, and BUSINESS_RULES-ODOMETER.md would stop being true.

// Snapshot is what the write-side KV bucket holds for one vehicle.
//
// LastSeq is the stream sequence of the last event already folded into State.
// It is the join between the snapshot and the log: rehydration continues from
// LastSeq+1, never from the snapshot alone.
type Snapshot struct {
	State   Vehicle `json:"state"`
	LastSeq uint64  `json:"lastSeq"`
}

// Rehydrated is one rebuilt aggregate plus the numbers that make the
// snapshot claim measurable.
type Rehydrated struct {
	Vehicle      Vehicle
	LastSeq      uint64 // stream sequence of its last event; 0 = no events
	EventsRead   int    // how many events this rehydration actually read
	FromSeq      uint64 // where the replay started
	UsedSnapshot bool
	Elapsed      time.Duration
}

// rehydrate rebuilds one vehicle from the log.
//
// withSnapshot=false replays every event from sequence 1. Always right, and
// the cost grows with history forever.
//
// withSnapshot=true loads {state, lastSeq} from KV and replays only the tail
// after lastSeq. The snapshot is ALWAYS stale -- the snapshotter is
// asynchronous, so it trails the stream. Code that read the snapshot and
// stopped there would be a bug, not a shortcut, which is why the tail replay
// below is unconditional.
func rehydrate(ctx context.Context, js jetstream.JetStream, kv jetstream.KeyValue, id string, withSnapshot bool) (Rehydrated, error) {
	start := time.Now()
	out := Rehydrated{UsedSnapshot: withSnapshot, FromSeq: 1}

	if withSnapshot {
		snap, err := loadSnapshot(ctx, kv, id)
		if err != nil {
			return out, err
		}
		out.Vehicle = snap.State
		out.LastSeq = snap.LastSeq
		out.FromSeq = snap.LastSeq + 1
	}

	err := replay(ctx, js, vehicleFilter(id), out.FromSeq, func(seq uint64, subject string, data []byte) error {
		e, err := decode(subject, data)
		if err != nil {
			return err
		}
		out.Vehicle = out.Vehicle.Apply(e)
		out.LastSeq = seq
		out.EventsRead++
		return nil
	})
	out.Elapsed = time.Since(start)
	return out, err
}

// loadSnapshot reads one vehicle's snapshot. A missing key is not an error:
// it means the snapshotter has not caught up yet, or was never run, and the
// caller then simply replays the whole history.
func loadSnapshot(ctx context.Context, kv jetstream.KeyValue, id string) (Snapshot, error) {
	var snap Snapshot
	entry, err := kv.Get(ctx, snapshotKey(id))
	if errors.Is(err, jetstream.ErrKeyNotFound) {
		return snap, nil
	}
	if err != nil {
		return snap, fmt.Errorf("read snapshot %s: %w", id, err)
	}
	if err := json.Unmarshal(entry.Value(), &snap); err != nil {
		return Snapshot{}, fmt.Errorf("decode snapshot %s: %w", id, err)
	}
	return snap, nil
}

// replay reads every stream message matching filter, from fromSeq onward,
// in order, and hands each to fn.
//
// The consumer is ORDERED and EPHEMERAL, created for this one replay and
// deleted straight afterwards. A durable consumer here would remember an
// offset between commands, and rehydration must never depend on what a
// previous command happened to read.
//
// Stopping is decided by the server, not by a timeout: every JetStream
// message reports how many are still pending, and pending==0 is the end of
// the history as it stood when the replay began.
func replay(ctx context.Context, js jetstream.JetStream, filter string, fromSeq uint64, fn func(seq uint64, subject string, data []byte) error) error {
	cfg := jetstream.OrderedConsumerConfig{FilterSubjects: []string{filter}}
	if fromSeq <= 1 {
		cfg.DeliverPolicy = jetstream.DeliverAllPolicy
	} else {
		cfg.DeliverPolicy = jetstream.DeliverByStartSequencePolicy
		cfg.OptStartSeq = fromSeq
	}

	consumer, err := js.OrderedConsumer(ctx, StreamName, cfg)
	if err != nil {
		return fmt.Errorf("replay consumer: %w", err)
	}
	if consumer.CachedInfo().NumPending == 0 {
		return nil
	}

	for {
		msg, err := consumer.Next(jetstream.FetchMaxWait(5 * time.Second))
		if err != nil {
			return fmt.Errorf("replay %s: %w", filter, err)
		}
		meta, err := msg.Metadata()
		if err != nil {
			return fmt.Errorf("replay metadata: %w", err)
		}
		if err := fn(meta.Sequence.Stream, msg.Subject(), msg.Data()); err != nil {
			return err
		}
		if meta.NumPending == 0 {
			return nil
		}
	}
}

// ErrConflict means another writer appended to this vehicle between our
// rehydration and our append. The command is retried from the top, which is
// the only safe answer: our decision was made against state that is now old.
var ErrConflict = errors.New("another writer got there first")

// appendEvent writes one event, but only if the vehicle's history still ends
// where rehydration said it did.
//
// Two headers do the work:
//
//	Nats-Expected-Last-Subject-Sequence          the sequence we expect
//	Nats-Expected-Last-Subject-Sequence-Subject  the subjects that applies to
//
// Without the second header the check is scoped to the exact subject of the
// message being written, which is useless here: a `travelled` event must be
// guarded against a `retired` event on a sibling subject. The scoped form
// needs NATS 2.11+; this demo runs 2.14.3.
func appendEvent(ctx context.Context, js jetstream.JetStream, id string, e Event, expectedLastSeq uint64) (uint64, error) {
	body, err := encode(e)
	if err != nil {
		return 0, err
	}
	msg := &nats.Msg{
		Subject: vehicleSubject(id, e.EventType()),
		Data:    body,
		Header:  nats.Header{},
	}
	msg.Header.Set(jetstream.ExpectedLastSubjSeqHeader, fmt.Sprintf("%d", expectedLastSeq))
	msg.Header.Set("Nats-Expected-Last-Subject-Sequence-Subject", vehicleFilter(id))

	ack, err := js.PublishMsg(ctx, msg)
	if err != nil {
		var apiErr *jetstream.APIError
		if errors.As(err, &apiErr) && apiErr.ErrorCode == jetstream.JSErrCodeStreamWrongLastSequence {
			return 0, ErrConflict
		}
		return 0, fmt.Errorf("append %s: %w", e.EventType(), err)
	}
	return ack.Sequence, nil
}

// handleCommand is the whole write path: rehydrate, decide, append, retry.
//
// decide is the only place the domain is called. It gets the rehydrated
// aggregate and returns one event or one rule error. A rule error is final
// and is returned as-is; a conflict is not an error the user caused, so it is
// retried silently.
func handleCommand(ctx context.Context, js jetstream.JetStream, kv jetstream.KeyValue, id string, withSnapshot bool, decide func(Vehicle) (Event, error)) (Rehydrated, uint64, error) {
	const attempts = 5
	for attempt := 1; ; attempt++ {
		state, err := rehydrate(ctx, js, kv, id, withSnapshot)
		if err != nil {
			return state, 0, err
		}
		event, err := decide(state.Vehicle)
		if err != nil {
			return state, 0, err
		}
		seq, err := appendEvent(ctx, js, id, event, state.LastSeq)
		if errors.Is(err, ErrConflict) && attempt < attempts {
			continue
		}
		if err != nil {
			return state, 0, err
		}
		return state, seq, nil
	}
}
