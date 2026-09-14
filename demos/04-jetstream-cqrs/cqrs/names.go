package main

import "fmt"

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

	defaultURL = "nats://127.0.0.1:4422"
)

// vehicleSubject is the subject one event is published on.
//
// The first token is the fixed literal `evt`, never a wildcard: an open first
// token textually overlaps $SYS.> and $JS.API.>, and JetStream refuses such a
// stream without NoAck.
func vehicleSubject(id, eventType string) string {
	return fmt.Sprintf("evt.odometer.vehicle.%s.%s", id, eventType)
}

// vehicleFilter matches every event of one vehicle. It is both the replay
// filter and the scope of the optimistic-append header.
func vehicleFilter(id string) string {
	return fmt.Sprintf("evt.odometer.vehicle.%s.>", id)
}

// snapshotKey is the KV key for one vehicle, in either bucket.
func snapshotKey(id string) string { return "vehicle." + id }
