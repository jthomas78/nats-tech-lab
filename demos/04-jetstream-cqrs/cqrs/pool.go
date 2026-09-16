package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

// The worker pool: the same fold as read.go, run by many workers at once.
//
// This file is deliberately wrong, and it says so on screen. Everything else
// in this demo folds one event at a time, in order, because a fold is a fold.
// Here N workers bind to ONE durable consumer and race. Throughput goes up.
// Order goes away. BR-OD08 is what turns the second half of that sentence
// from a silence into a number.
//
// Three facts that make the whole thing behave the way it does:
//
//  1. The position belongs to the CONSUMER, not to a worker. Adding workers
//     adds hands, not places in the queue.
//  2. MaxAckPending is shared by the whole consumer. Eight workers and a cap
//     of three means five workers never get a message. That is starvation,
//     and it is a config answer, not a bug.
//  3. The server hands a stored event to exactly one worker, chosen by which
//     worker asked. Demand order is not log order, so worker 3 can be folding
//     #6 while worker 1 has already folded #7.

// PoolConfig is everything the pool subcommand can be told to do.
type PoolConfig struct {
	Workers    int
	MaxPending int
	AckWait    time.Duration

	// KillAt is real fault injection, not a label. The worker that fetches
	// this sequence stops fetching and never acks and never naks -- exactly
	// what the server observes when a process is killed. It must not nak:
	// a nak redelivers at once and hides the AckWait wait, which is the
	// only thing this flag exists to show.
	KillAt uint64

	// Drain rebuilds the pool's projection from sequence 1 and stops when
	// the consumer reports nothing left, printing the elapsed time. Every
	// run then answers the same question against the same seed, which is
	// what makes 1 / 2 / 4 / 8 workers comparable at all.
	Drain bool
}

// WorkerState is one key in KV odometer-pool-workers.
//
// The browser watches this bucket exactly the way it watches the other two.
// It is not a control channel: nothing here is read back by a worker, and
// nothing a viewer does can change it. The bucket has a TTL, so a killed
// worker leaves the screen on its own.
type WorkerState struct {
	Worker int    `json:"worker"`
	Status string `json:"status"` // working | waiting | killed
	// Holding is the stream sequence in flight, 0 when the worker is idle.
	Holding uint64 `json:"holding"`
	Acked   int    `json:"acked"`
	// Dropped counts events this worker refused under BR-OD08. They are
	// gone: the fold had already moved past them and never moves back.
	Dropped int       `json:"dropped"`
	Behind  uint64    `json:"behind"` // the position it was behind, when it dropped one
	At      time.Time `json:"at"`
}

// PoolResult is what a -drain run prints.
type PoolResult struct {
	Events  int
	Acked   int
	Dropped int
	Elapsed time.Duration
	Share   PoolShare
}

// PoolShare is where the work landed. Totals cannot show it: one worker doing
// everything and eight sharing it evenly produce the same Events and the same
// Acked, and only this tells them apart.
//
// It exists for the starvation lesson (04.7.6). MaxAckPending is one number on
// the CONSUMER, and the claim on that tab is that it does not divide up among
// the workers — eight workers on a cap of three is still three messages in
// flight, and the workers with nothing to fetch simply wait.
type PoolShare struct {
	Workers int
	// Busy acked at least one event. Idle acked none.
	Busy int
	Idle int
	// Acked per worker, worker 1 first, so the spread is visible and not
	// just the count.
	Acked []int
}

// shareOf judges on acks alone. "Did this worker do any work" has one honest
// answer and it is the ack count; a worker that was killed holding an event
// still did no work. The Redelivery tab reads status instead, because there
// the difference matters — but a drain run has no kill in it.
func shareOf(states []*WorkerState) PoolShare {
	out := PoolShare{Workers: len(states), Acked: make([]int, 0, len(states))}
	for _, s := range states {
		out.Acked = append(out.Acked, s.Acked)
		if s.Acked > 0 {
			out.Busy++
		} else {
			out.Idle++
		}
	}
	return out
}

// runPool starts the workers and blocks until ctx is cancelled, or until the
// consumer is drained when cfg.Drain is set.
func runPool(ctx context.Context, js jetstream.JetStream, poolKV, workersKV jetstream.KeyValue, cfg PoolConfig) (PoolResult, error) {
	var out PoolResult

	if cfg.Drain {
		// A drain run must start from nothing, or the second run of a
		// comparison has no work left and reports a speed-up that is
		// really an empty queue.
		if err := resetPool(ctx, js, poolKV); err != nil {
			return out, err
		}
	}

	consumer, err := js.CreateOrUpdateConsumer(ctx, StreamName, jetstream.ConsumerConfig{
		Durable:       PoolConsumer,
		FilterSubject: StreamSubject,
		AckPolicy:     jetstream.AckExplicitPolicy,
		DeliverPolicy: jetstream.DeliverAllPolicy,
		// Shared by every worker. This one number is the difference
		// between four busy workers and five idle ones.
		MaxAckPending: cfg.MaxPending,
		// How long the server waits for an ack before it decides the
		// holder is dead. It is a guess about how long real work takes.
		AckWait: cfg.AckWait,
	})
	if err != nil {
		return out, fmt.Errorf("pool consumer: %w", err)
	}

	runCtx, stop := context.WithCancel(ctx)
	defer stop()

	states := make([]*WorkerState, cfg.Workers)
	var mu sync.Mutex
	var wg sync.WaitGroup

	// One clock for the whole pool: the worker that loses an event is never
	// the worker that gets it back.
	clock := newKillClock()

	start := time.Now()
	for i := 0; i < cfg.Workers; i++ {
		state := &WorkerState{Worker: i + 1, Status: "waiting"}
		states[i] = state
		wg.Add(1)
		go func() {
			defer wg.Done()
			work(runCtx, consumer, poolKV, workersKV, state, &mu, cfg, clock)
		}()
	}

	if cfg.Drain {
		if err := waitDrained(runCtx, consumer); err != nil && !errors.Is(err, context.Canceled) {
			return out, err
		}
		stop()
	}
	wg.Wait()
	out.Elapsed = time.Since(start)

	mu.Lock()
	for _, s := range states {
		out.Acked += s.Acked
		out.Dropped += s.Dropped
	}
	out.Share = shareOf(states)
	mu.Unlock()
	out.Events = out.Acked + out.Dropped
	return out, nil
}

// work is one worker: fetch one, fold it, ack it, say so. Forever.
//
// killClock remembers when a worker went silent, so the redelivery can be
// measured instead of estimated.
//
// Nothing else in the system knows. The server does not say "this is a
// redelivery after 30 seconds"; it says only that the delivery count is 2, and
// the worker that receives it was never the worker that lost it. The clock
// lives in the pool process because that is the one place that saw both ends.
//
// The first kill wins. A second kill of the same sequence is a different
// silence, and overwriting would shorten the wait being measured.
type killClock struct {
	mu sync.Mutex
	at map[uint64]time.Time
}

func newKillClock() *killClock {
	return &killClock{at: map[uint64]time.Time{}}
}

func (c *killClock) killed(seq uint64, when time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, seen := c.at[seq]; !seen {
		c.at[seq] = when
	}
}

// since reports how long the sequence has been silent. The false answer means
// nobody killed it, and that is reported rather than returned as zero -- a
// zero wait and no wait at all are different facts.
func (c *killClock) since(seq uint64, now time.Time) (time.Duration, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	at, ok := c.at[seq]
	if !ok {
		return 0, false
	}
	return now.Sub(at), true
}

// One message at a time, on purpose. A batch of ten would hide which worker
// holds which sequence, and holding is the thing the panel draws.
func work(ctx context.Context, consumer jetstream.Consumer, poolKV, workersKV jetstream.KeyValue,
	state *WorkerState, mu *sync.Mutex, cfg PoolConfig, clock *killClock) {

	beat := time.NewTicker(PoolHeartbeat)
	defer beat.Stop()
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-beat.C:
				publishWorker(ctx, workersKV, state, mu)
			}
		}
	}()
	publishWorker(ctx, workersKV, state, mu)

	for ctx.Err() == nil {
		batch, err := consumer.Fetch(1, jetstream.FetchMaxWait(time.Second))
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf("worker %d: fetch: %v", state.Worker, err)
			continue
		}
		for msg := range batch.Messages() {
			meta, err := msg.Metadata()
			if err != nil {
				log.Printf("worker %d: metadata: %v", state.Worker, err)
				continue
			}
			seq := meta.Sequence.Stream

			// The kill. No ack, no nak, no error, no return path. The
			// worker simply stops existing as far as the pool is
			// concerned, and the server finds out the slow way -- when
			// AckWait expires. That wait is the lesson.
			if cfg.KillAt != 0 && seq == cfg.KillAt && meta.NumDelivered == 1 {
				clock.killed(seq, time.Now())
				mu.Lock()
				state.Status, state.Holding = "killed", seq
				mu.Unlock()
				publishWorker(ctx, workersKV, state, mu)
				log.Printf("worker %d: killed while holding #%d — no ack, no nak", state.Worker, seq)
				return
			}

			// The measurement 04.7.5 exists for. A redelivery is the
			// server admitting it waited AckWait out; the worker that
			// gets it never saw the silence, so the clock supplies it.
			if meta.NumDelivered > 1 {
				if waited, ok := clock.since(seq, time.Now()); ok {
					log.Printf("worker %d: REDELIVERED #%d after %s — delivery %d, AckWait %s",
						state.Worker, seq, waited.Round(time.Millisecond), meta.NumDelivered, cfg.AckWait)
				} else {
					log.Printf("worker %d: redelivered #%d — delivery %d", state.Worker, seq, meta.NumDelivered)
				}
			}

			mu.Lock()
			state.Status, state.Holding = "working", seq
			mu.Unlock()
			publishWorker(ctx, workersKV, state, mu)

			switch err := foldIntoPool(ctx, poolKV, msg, meta); {
			case errors.Is(err, ErrUndecodable):
				// BR-OD09. Term, like a late event -- and counted
				// apart from one, because Dropped on screen means
				// "the pool lost this to racing" and an unreadable
				// event was never the pool's fault.
				_ = msg.Term()
				log.Printf("worker %d: DROPPED #%d — %v", state.Worker, seq, err)
			case errors.Is(err, ErrOutOfOrder):
				// BR-OD08. Term, not nak. The fold's position never
				// moves backwards, so a retry can only be refused
				// again -- a nak here loops for ever. Terminating
				// makes the loss counted, logged and drawn instead
				// of silent.
				_ = msg.Term()
				mu.Lock()
				state.Dropped++
				state.Behind = seq
				mu.Unlock()
				log.Printf("worker %d: dropped #%d — %v", state.Worker, seq, err)
			case err != nil:
				_ = msg.Nak()
				log.Printf("worker %d: #%d: %v", state.Worker, seq, err)
			default:
				_ = msg.Ack()
				mu.Lock()
				state.Acked++
				mu.Unlock()
			}

			mu.Lock()
			state.Status, state.Holding = "waiting", 0
			mu.Unlock()
			publishWorker(ctx, workersKV, state, mu)
		}
	}
}

// foldIntoPool applies one event to the pool's own projection.
//
// It is read.go's project() with one difference: this one can be handed an
// event that arrived behind the fold, and it says so. The three answers are
// BR-OD06, BR-OD07 and BR-OD08, and they come from domain.go, not from here.
//
// Two workers folding the same vehicle at the same time both read the same
// document and both try to write it. The compare-and-swap lets exactly one
// of them win. The loser re-reads and asks the rules again -- and by then the
// position has usually moved past its event, so the answer is BR-OD08. That
// is not a lock failure dressed up as a domain error: a fold that lost the
// race genuinely is holding an event the projection has already moved past.
func foldIntoPool(ctx context.Context, kv jetstream.KeyValue, msg jetstream.Msg, meta *jetstream.MsgMetadata) error {
	id, err := vehicleIDFrom(msg.Subject())
	if err != nil {
		return err
	}
	event, err := decode(msg.Subject(), msg.Data())
	if err != nil {
		return err
	}
	seq := meta.Sequence.Stream

	// Bounded, because an unbounded retry on a hot vehicle is a spin. A
	// worker that cannot win in ten tries naks and lets the server decide
	// when to try again.
	for attempt := 0; attempt < 10; attempt++ {
		entry, err := kv.Get(ctx, snapshotKey(id))
		switch {
		case errors.Is(err, jetstream.ErrKeyNotFound):
			first := ReadEntry{
				Odometer: Odometer{}.Apply(event, meta.Timestamp),
				Fold:     Fold{}.Advance(seq),
			}
			body, _ := json.Marshal(first)
			if _, err := kv.Create(ctx, snapshotKey(id), body); err != nil {
				continue // someone else created it first; read it and decide again
			}
			return nil
		case err != nil:
			return err
		}

		var current ReadEntry
		if err := json.Unmarshal(entry.Value(), &current); err != nil {
			return err
		}
		apply, err := current.Next(seq)
		if err != nil {
			return err // BR-OD08 — the caller terminates it
		}
		if !apply {
			return nil // BR-OD07 — a redelivery
		}
		current.Odometer = current.Odometer.Apply(event, meta.Timestamp)
		current.Fold = current.Advance(seq)
		body, _ := json.Marshal(current)
		if _, err := kv.Update(ctx, snapshotKey(id), body, entry.Revision()); err != nil {
			continue // lost the compare-and-swap; re-read and ask the rules again
		}
		return nil
	}
	return fmt.Errorf("fold #%d: lost the compare-and-swap ten times", seq)
}

// publishWorker writes one worker's key. One writer per key, so no CAS.
func publishWorker(ctx context.Context, kv jetstream.KeyValue, state *WorkerState, mu *sync.Mutex) {
	mu.Lock()
	snap := *state
	mu.Unlock()
	snap.At = time.Now()
	body, err := json.Marshal(snap)
	if err != nil {
		return
	}
	if _, err := kv.Put(ctx, workerKey(snap.Worker), body); err != nil && ctx.Err() == nil {
		log.Printf("worker %d: heartbeat: %v", snap.Worker, err)
	}
}

// waitDrained blocks until the consumer has nothing left to hand out and
// nothing outstanding.
func waitDrained(ctx context.Context, consumer jetstream.Consumer) error {
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
			info, err := consumer.Info(ctx)
			if err != nil {
				return err
			}
			if info.NumPending == 0 && info.NumAckPending == 0 {
				return nil
			}
		}
	}
}

// resetPool throws away the pool's position and its projection.
//
// Only -drain does this. The stream is untouched -- that is the point of
// keeping the log forever, and it is why a damaged projection is a nuisance
// here and a catastrophe in a system that kept no log.
func resetPool(ctx context.Context, js jetstream.JetStream, poolKV jetstream.KeyValue) error {
	if err := js.DeleteConsumer(ctx, StreamName, PoolConsumer); err != nil &&
		!errors.Is(err, jetstream.ErrConsumerNotFound) {
		return fmt.Errorf("reset pool consumer: %w", err)
	}
	keys, err := poolKV.Keys(ctx)
	if errors.Is(err, jetstream.ErrNoKeysFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("reset pool bucket: %w", err)
	}
	for _, k := range keys {
		if err := poolKV.Purge(ctx, k); err != nil {
			return fmt.Errorf("reset pool bucket: %w", err)
		}
	}
	return nil
}
