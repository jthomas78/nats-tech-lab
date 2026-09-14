package main

import (
	"context"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// seed writes a long history, so the replay cost is big enough to measure.
//
// It is a FIXTURE, not a command path, and it deliberately skips two things
// handleCommand does:
//
//   - it rehydrates once, not once per event. Per-event rehydration would be
//     O(n^2) and would take longer than the measurement it sets up.
//   - it publishes without the expected-sequence header. Only one writer runs
//     here, so there is no race to lose.
//
// The rule still holds: the aggregate is rehydrated and checked ONCE before
// the first event, so a seed on an unregistered or retired vehicle is refused
// instead of writing 10000 events nothing can ever fold.
func seed(ctx context.Context, js jetstream.JetStream, kv jetstream.KeyValue, id string, n int, km float64) (time.Duration, error) {
	if n <= 0 {
		return 0, fmt.Errorf("-n must be greater than 0")
	}

	state, err := rehydrate(ctx, js, kv, id, true)
	if err != nil {
		return 0, err
	}
	if _, err := state.Vehicle.Travel(RecordTrip{Km: km}); err != nil {
		return 0, err
	}

	body, err := encode(Travelled{Km: km})
	if err != nil {
		return 0, err
	}
	subject := vehicleSubject(id, "travelled")

	// Async publish. Each event still gets an ack from the server; the acks
	// are just collected at the end instead of one round trip per event.
	start := time.Now()
	for i := 0; i < n; i++ {
		if _, err := js.PublishMsgAsync(&nats.Msg{Subject: subject, Data: body}); err != nil {
			return 0, err
		}
	}
	select {
	case <-js.PublishAsyncComplete():
	case <-time.After(60 * time.Second):
		return 0, fmt.Errorf("timed out waiting for publish acks")
	case <-ctx.Done():
		return 0, ctx.Err()
	}
	return time.Since(start), nil
}
