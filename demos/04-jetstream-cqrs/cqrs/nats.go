package main

import (
	"context"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// connect opens one NATS connection and a JetStream handle.
//
// Every connection sets a name. An anonymous connection is indistinguishable
// from every other one in `nats server list connections`, and this demo is
// meant to be watched from the CLI while it runs.
func connect(name, url string) (*nats.Conn, jetstream.JetStream, error) {
	nc, err := nats.Connect(url, nats.Name(name))
	if err != nil {
		return nil, nil, fmt.Errorf("connect %s: %w", url, err)
	}
	js, err := jetstream.New(nc)
	if err != nil {
		nc.Close()
		return nil, nil, fmt.Errorf("jetstream: %w", err)
	}
	return nc, js, nil
}

// ensureStream creates ODOMETER if it is not there.
//
// LimitsPolicy, not InterestPolicy. This demo replays from sequence 1.
// InterestPolicy drops a message once every consumer has acked it, and the
// replay then returns nothing -- with no error at all. That silence is the
// whole reason the retention choice is written down here.
func ensureStream(ctx context.Context, js jetstream.JetStream) (jetstream.Stream, error) {
	return js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:      StreamName,
		Subjects:  []string{StreamSubject},
		Retention: jetstream.LimitsPolicy,
		Storage:   jetstream.FileStorage,
	})
}

// ensureKV creates a bucket if it is not there.
func ensureKV(ctx context.Context, js jetstream.JetStream, bucket string) (jetstream.KeyValue, error) {
	return js.CreateOrUpdateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: bucket})
}

// ensureExpiringKV creates a bucket whose values expire on their own.
//
// Only the worker heartbeat bucket uses this. A worker that is killed cannot
// tidy up after itself -- that is what being killed means -- so the bucket
// forgets it instead, and the browser sees the pool shrink with no cleanup
// code anywhere.
func ensureExpiringKV(ctx context.Context, js jetstream.JetStream, bucket string, ttl time.Duration) (jetstream.KeyValue, error) {
	return js.CreateOrUpdateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: bucket, TTL: ttl})
}
