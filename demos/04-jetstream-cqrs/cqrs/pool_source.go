package main

// Which log the pool folds (04.8.6).
//
// Until this task runPool spelled StreamName and StreamSubject into itself,
// so lesson 02's deliberately broken consumer hung off ODOMETER -- the log
// every other screen in this demo is drawn from. It never PUBLISHED there,
// which is why it looked harmless, but a consumer is state on the stream: it
// starves, it redelivers and, under -kill-at, it abandons messages unacked.
// One press on lesson 02 could leave the demo's own log carrying the wreckage.
//
// The fix is the one rehydrate() already had in 04.7.16: take a Source, never
// name a stream. These two functions are the whole of it, kept pure so the
// specs can read the answer without a server.

import "github.com/nats-io/nats.go/jetstream"

// poolStreamFor says which stream the pool hangs its consumer on.
func poolStreamFor(src Source) string { return src.Stream }

// poolConsumerConfig builds the pool's consumer from the source it is given
// and the settings the reader typed.
//
// Everything the reader chose is passed through untouched. MaxAckPending in
// particular: it is ONE number on the consumer, shared by every worker, and
// it is the difference between four busy workers and five idle ones. A pool
// that quietly adjusted it would turn the starvation tab into a chart of this
// function's opinions instead of the reader's.
func poolConsumerConfig(src Source, cfg PoolConfig) jetstream.ConsumerConfig {
	return jetstream.ConsumerConfig{
		Durable:       PoolConsumer,
		FilterSubject: src.StreamSubject,
		AckPolicy:     jetstream.AckExplicitPolicy,
		// Every run replays the whole log. A run that started where the
		// last one stopped would report a speed-up that was really an
		// empty queue.
		DeliverPolicy: jetstream.DeliverAllPolicy,
		MaxAckPending: cfg.MaxPending,
		// How long the server waits for an ack before it decides the
		// holder is dead. It is a guess about how long real work takes.
		AckWait: cfg.AckWait,
	}
}
