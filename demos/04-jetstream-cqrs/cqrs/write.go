package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/rand/v2"
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
// The embedded Fold carries LastSeq: the stream sequence of the last event
// already folded into State. It is the join between the snapshot and the log,
// so rehydration continues from LastSeq+1 and never from the snapshot alone.
//
// Fold is embedded rather than copied so that the state and the position are
// one KV value and one write. A position stored apart from the state it
// describes can disagree with it after a crash. The stored JSON is unchanged
// by the embedding: {state, lastSeq}, exactly as before.
type Snapshot struct {
	State Vehicle `json:"state"`
	Fold
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
// The `src` argument is what lets the same code measure two different logs:
// Live for the demo, Bench for the fixture the Rehydrate panel seeds (04.7.16).
func rehydrate(ctx context.Context, js jetstream.JetStream, kv jetstream.KeyValue, src Source, id string, withSnapshot bool) (Rehydrated, error) {
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

	err := replay(ctx, js, src, src.VehicleFilter(id), out.FromSeq, func(seq uint64, subject string, data []byte) error {
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

// replayInactiveThreshold is how long the server keeps this demo's replay
// consumer after it stops hearing from us.
//
// It is a backstop, not the cleanup. The happy path deletes the consumer
// itself, below. This only catches the process that is killed mid-replay --
// and it must be set, because the client's own default is 5 MINUTES. A demo
// that rehydrates in a loop would leave a drift of dead consumers on the
// stream for five minutes each, and `nats consumer ls` is one of the things
// a reader of this demo is meant to look at.
const replayInactiveThreshold = 30 * time.Second

// replay reads every stream message matching filter, from fromSeq onward,
// in order, and hands each to fn.
//
// The consumer is ORDERED and EPHEMERAL, created for this one replay and
// deleted straight afterwards. A durable consumer here would remember an
// offset between commands, and rehydration must never depend on what a
// previous command happened to read.
//
// Messages(), NOT Next(). This is the single most expensive mistake available
// in this file, and the demo made it until 04.7.12.
//
// On an ordered consumer, Next() calls Fetch(1), and Fetch() calls reset() --
// which DELETES the server-side consumer and creates a new one, every call.
// So a Next()-per-event replay creates one consumer per event. On a 10k log
// that is 10k consumer creations and 10k deletions, and the measured
// "rehydration time" this demo exists to report was mostly that, not reading.
// It was visible on the server: the consumer name carries a serial, and it
// read `..._32469` while delivering stream sequence 32478.
//
// Messages() creates ONE consumer and pulls batches over it. The client says
// as much in its own doc comment on Next(). Believe it.
//
// Stopping is decided by the server, not by a timeout: every JetStream
// message reports how many are still pending, and pending==0 is the end of
// the history as it stood when the replay began.
func replay(ctx context.Context, js jetstream.JetStream, src Source, filter string, fromSeq uint64, fn func(seq uint64, subject string, data []byte) error) error {
	cfg := jetstream.OrderedConsumerConfig{
		FilterSubjects:    []string{filter},
		InactiveThreshold: replayInactiveThreshold,
	}
	if fromSeq <= 1 {
		cfg.DeliverPolicy = jetstream.DeliverAllPolicy
	} else {
		cfg.DeliverPolicy = jetstream.DeliverByStartSequencePolicy
		cfg.OptStartSeq = fromSeq
	}

	consumer, err := js.OrderedConsumer(ctx, src.Stream, cfg)
	if err != nil {
		return fmt.Errorf("replay consumer: %w", err)
	}
	// Registered first so it runs LAST: stop the iterator, then delete.
	defer deleteReplayConsumer(js, src, consumer)

	if consumer.CachedInfo().NumPending == 0 {
		return nil
	}

	msgs, err := consumer.Messages()
	if err != nil {
		return fmt.Errorf("replay messages: %w", err)
	}
	defer msgs.Stop()

	// Messages().Next() has no timeout of its own, so the caller's context is
	// what ends a replay against a server that has gone quiet. Without this a
	// cancelled command would block here for ever.
	defer context.AfterFunc(ctx, msgs.Stop)()

	for {
		msg, err := msgs.Next()
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

// deleteReplayConsumer removes the ephemeral consumer this replay created.
//
// It uses its own context on purpose. The caller's context is usually already
// cancelled by the time this runs -- that is what ended the replay -- and a
// cleanup that skips itself whenever it is needed most is not a cleanup.
//
// A failure here is logged and not returned. The rehydration succeeded; the
// InactiveThreshold above collects the consumer either way, and failing a
// command over tidying would be the wrong trade.
func deleteReplayConsumer(js jetstream.JetStream, src Source, consumer jetstream.Consumer) {
	name := consumer.CachedInfo().Name
	if name == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := js.DeleteConsumer(ctx, src.Stream, name); err != nil &&
		!errors.Is(err, jetstream.ErrConsumerNotFound) {
		log.Printf("replay: could not delete consumer %s: %v", name, err)
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

// Conflict retry timing. Five attempts, so four waits.
//
// The numbers are small on purpose. A conflict means another writer appended
// to this vehicle in the gap between our rehydrate and our append, and that
// writer has already finished -- we are not waiting for it, we are waiting to
// stop colliding with the OTHER losers of the same race.
const (
	retryBase = 2 * time.Millisecond
	retryCap  = 50 * time.Millisecond
)

// conflictBackoff is how long to wait before retry number `attempt`.
//
// Two properties matter and neither is the delay itself.
//
// CAPPED, so a long queue of writers on one vehicle does not turn into a long
// queue of sleepers. Doubling without a cap reaches seconds by attempt 10, and
// the command API would hold a request open for no reason.
//
// JITTERED, which is the part that is easy to leave out and is the whole
// point. A fixed delay makes every loser of a race wake at the same instant
// and collide again -- the sleep then does nothing except make the collision
// slower. The random half is what actually pulls the writers apart.
func conflictBackoff(attempt int) time.Duration {
	d := retryBase << (attempt - 1)
	if d > retryCap || d <= 0 {
		d = retryCap
	}
	// Half fixed, half random: never longer than d, never zero-spread.
	return d/2 + time.Duration(rand.Int64N(int64(d/2)+1))
}

// handleCommand is the whole write path: rehydrate, decide, append, retry.
//
// decide is the only place the domain is called. It gets the rehydrated
// aggregate and returns one event or one rule error. A rule error is final
// and is returned as-is; a conflict is not an error the user caused, so it is
// retried silently.
//
// A retry is NOT free and it is not a spin: each attempt rehydrates from the
// log and appends again, both over the network. It still waits between
// attempts, because without a wait every writer that lost one race re-enters
// the next one at the same moment as all the others.
func handleCommand(ctx context.Context, js jetstream.JetStream, kv jetstream.KeyValue, id string, withSnapshot bool, decide func(Vehicle) (Event, error)) (Rehydrated, uint64, error) {
	const attempts = 5
	for attempt := 1; ; attempt++ {
		state, err := rehydrate(ctx, js, kv, Live, id, withSnapshot)
		if err != nil {
			return state, 0, err
		}
		event, err := decide(state.Vehicle)
		if err != nil {
			return state, 0, err
		}
		seq, err := appendEvent(ctx, js, id, event, state.LastSeq)
		if errors.Is(err, ErrConflict) && attempt < attempts {
			// The caller's context ends the wait too. A cancelled
			// command that sleeps anyway is a command that answers
			// after nobody is listening.
			select {
			case <-time.After(conflictBackoff(attempt)):
				continue
			case <-ctx.Done():
				return state, 0, ctx.Err()
			}
		}
		if err != nil {
			return state, 0, err
		}
		return state, seq, nil
	}
}
