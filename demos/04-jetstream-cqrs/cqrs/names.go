package main

import (
	"fmt"
	"time"
)

// Names of everything this demo creates in NATS, in one place.
//
// Root rule, and it holds here: streams are SCREAMING_SNAKE, KV buckets are
// lowercase-kebab.
const (
	StreamName    = "ODOMETER"
	StreamSubject = "evt.odometer.>"

	// WriteKV holds the write-side snapshot: {state, lastSeq}. It is an
	// optimisation and never the truth. Deleting it costs correctness
	// nothing and speed everything.
	WriteKV = "odometer-write"

	// ReadKV holds the read model. Phase 04.4 fills it.
	ReadKV = "odometer-read"

	// SnapshotConsumer folds new events into WriteKV, asynchronously. That
	// is why the snapshot always trails the stream.
	SnapshotConsumer = "odometer-snapshotter"

	// ReadConsumer folds every event into ReadKV. It reads the same log as
	// SnapshotConsumer and does a completely different job with it, which
	// is the whole of CQRS in one sentence.
	ReadConsumer = "vehicle-projector"

	// PoolKV holds the worker pool's own fold. It is a THIRD projection of
	// the same log, kept apart from ReadKV on purpose: the pool is
	// deliberately wrong, and a demo that damaged the read model to show
	// that would have nothing correct left to compare against.
	PoolKV = "odometer-pool"

	// PoolWorkersKV holds one key per worker: heartbeat and counters. The
	// browser watches this bucket the same way it watches the other two,
	// so the panel needs no new transport and no consumer-info API call.
	PoolWorkersKV = "odometer-pool-workers"

	// PoolConsumer is ONE durable consumer with many workers bound to it.
	// That is the whole lesson: the position belongs to the consumer, not
	// to a worker, so adding workers adds throughput and removes order.
	//
	// It shares its name with PoolKV deliberately -- one pool, one
	// position, one damaged projection.
	PoolConsumer = "odometer-pool"

	// PoolStream is lesson 02's own log (plan 04.8, D1 and D2).
	//
	// The pool used to bind its durable consumer to ODOMETER, so the
	// deliberately broken consumer in the lesson was misbehaving on the
	// demo's own log. It published nothing there, which is why nobody
	// noticed -- the sharing was a CONSUMER, not events.
	//
	// Named for what it holds, not for the lesson it is shown on. Every
	// other stream here is named that way, and a lesson can be renumbered.
	PoolStream = "ODOMETER_POOL"

	// PoolStreamSubject does NOT overlap StreamSubject, because the second
	// token differs. Hyphen, not a dot: `evt.odometer.pool.>` keeps
	// `odometer` as the second token, so `evt.odometer.>` would match every
	// event of it and the isolation would be a comment rather than a fact.
	PoolStreamSubject = "evt.odometer-pool.>"

	// PoolTruthKV is the CORRECT fold of PoolStream, and the reason 04.8 is
	// a phase and not a flag.
	//
	// The screen measures the pool's damage by subtracting a correct fold
	// from the pool's own. That correct fold used to be ReadKV -- fine
	// while both folded ODOMETER, and nonsense the moment the pool moved to
	// a log of its own. Without this bucket the screen would subtract two
	// unrelated logs and print the difference as damage.
	//
	// `-truth` and not `-read`: a "read model" is lesson 01's idea, shaped
	// for a question. This is not shaped for anything. It is the answer.
	PoolTruthKV = "odometer-pool-truth"

	// PoolTruthConsumer folds PoolStream correctly: one worker, and
	// MaxAckPending 1 so it cannot reorder itself. It is the control the
	// whole lesson is measured against, so it is built to be slow and
	// right, which is the opposite of PoolConsumer beside it.
	PoolTruthConsumer = "odometer-pool-truth"

	// PoolWorkerTTL expires a worker's heartbeat key. A worker that is
	// killed stops appearing on screen without anything having to delete
	// its key, which is what makes -kill-at visible in the browser.
	PoolWorkerTTL = 10 * time.Second

	// PoolHeartbeat is how often a worker republishes its counters. Often
	// enough to watch, rare enough that the heartbeat is not the workload.
	PoolHeartbeat = 500 * time.Millisecond

	defaultURL = "nats://127.0.0.1:4422"

	// Host ports for this demo follow 20<demo number><increment>, so they
	// never collide with demo 01's 7100-7299 bands. 20401 is the frontend,
	// 20402 the command API, 20403 the NATS WebSocket.
	defaultServeAddr = "127.0.0.1:20402"

	// The frontend's origin. Only a page served from here may send commands.
	// Loopback on purpose: this demo has no accounts and no auth, so the
	// bind address is what keeps it off the network.
	defaultOrigin = "http://localhost:20401,http://127.0.0.1:20401"
)

// Source is one log a vehicle can be rehydrated from: a stream, the subjects
// it holds, and the KV bucket its snapshots live in.
//
// There are three, and they exist for one reason. `ODOMETER` is the demo -- the
// Overview lane, both bucket tabs and the event log are all drawn from it, and
// a million-event fixture dropped into it would bury every one of them.
// `ODOMETER_BENCH` is a disposable fixture nothing else reads, so the
// rehydrate measurement can be as big as it likes. `ODOMETER_POOL` is lesson
// 02's own log, for the same reason in reverse: the consumer that reads it is
// deliberately broken, and it must not be broken on the demo's own log.
//
// Before phase 04.7.16, rehydrate() spelled StreamName and vehicleFilter()
// into itself and could therefore only ever read one log. Handing it a Source
// is the whole of that change.
type Source struct {
	Stream        string // the stream to replay
	StreamSubject string // everything that stream holds
	WriteKV       string // where {state, lastSeq} snapshots live
	prefix        string // subject prefix for one vehicle's events
}

// Live is the demo's own log. Every tab except Rehydrate is drawn from it.
var Live = Source{
	Stream:        StreamName,
	StreamSubject: StreamSubject,
	WriteKV:       WriteKV,
	prefix:        "evt.odometer.vehicle",
}

// Bench is the rehydrate fixture. Disposable: purging it costs the demo
// nothing, because nothing else on the screen reads it.
//
// `evt.odometer-bench.>` does NOT overlap `evt.odometer.>` -- the second
// token differs, so the two streams may share a server. Hyphen, not a dot:
// a dot would split the token and put the fixture inside the demo's filter.
var Bench = Source{
	Stream:        "ODOMETER_BENCH",
	StreamSubject: "evt.odometer-bench.>",
	WriteKV:       "odometer-bench-write",
	prefix:        "evt.odometer-bench.vehicle",
}

// Pool is lesson 02's log. Disposable in the same way Bench is: lesson 01
// never reads it, and lesson 02 never reads anything else (D7).
//
// Its WriteKV is the correct fold. That is not a shortcut -- for this log the
// snapshot of the truth and the correct fold are the same object, and a fourth
// bucket holding a second copy of the right answer would be one more thing
// that can disagree.
var Pool = Source{
	Stream:        PoolStream,
	StreamSubject: PoolStreamSubject,
	WriteKV:       PoolTruthKV,
	prefix:        "evt.odometer-pool.vehicle",
}

// VehicleSubject is the subject one event is published on.
//
// The first token is the fixed literal `evt`, never a wildcard: an open first
// token textually overlaps $SYS.> and $JS.API.>, and JetStream refuses such a
// stream without NoAck.
func (s Source) VehicleSubject(id, eventType string) string {
	return fmt.Sprintf("%s.%s.%s", s.prefix, id, eventType)
}

// VehicleFilter matches every event of one vehicle. It is both the replay
// filter and the scope of the optimistic-append header.
func (s Source) VehicleFilter(id string) string {
	return fmt.Sprintf("%s.%s.>", s.prefix, id)
}

// vehicleSubject and vehicleFilter are the LIVE source's subjects, kept as
// plain functions because the write path only ever writes to the demo's own
// log. They delegate so that there is one spelling of the subject, not two
// that can drift apart.
func vehicleSubject(id, eventType string) string { return Live.VehicleSubject(id, eventType) }

func vehicleFilter(id string) string { return Live.VehicleFilter(id) }

// workerKey is the KV key for one pool worker in PoolWorkersKV. Zero padded
// so `nats kv ls` lists ten workers in the order a reader expects.
func workerKey(n int) string { return fmt.Sprintf("worker.%02d", n) }

// snapshotKey is the KV key for one vehicle, in either bucket.
func snapshotKey(id string) string { return "vehicle." + id }
