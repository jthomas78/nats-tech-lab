// Command otlp-bridge is Phase 28g's OTLP exporter — a JetStream consumer on
// TRACES, never an in-process SDK inside any of the five instrumented
// services (see ARCHITECTURE-COMMUNICATIONS.md § 6 for why: retroactive
// export, no-code toggling, one copy of the OTLP mapping, and a mapping bug
// or a slow/unreachable collector can never touch a business path — the
// span stays on TRACES, unacked, until this succeeds). Deliberately not a
// hexagonal service like the other five: this is a translation utility with
// no domain logic and no business rules of its own, so it stays one
// package plus the pure-function otlpmap package that owns the actual
// mapping.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/otlp-bridge/internal/otlpmap"
)

const (
	tracesStreamName      = "TRACES"
	tracesSubjectWildcard = "obs.trace.>"
	// consumerDurableName is the live-tailing mode's cursor — durable so a
	// bridge restart resumes exactly where it left off instead of
	// re-exporting everything already sent to Jaeger.
	consumerDurableName = "otlp-bridge"
	batchSize           = 100
	batchInterval       = 2 * time.Second
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	natsURL := envOr("NATS_URL", nats.DefaultURL)
	credsPath := envOr("NATS_CREDS_PATH", "/etc/nats/creds/platform.creds")
	endpoint := envOr("OTLP_ENDPOINT", "http://jaeger:4318/v1/traces")
	replay := os.Getenv("OTLP_BRIDGE_REPLAY") == "true"

	nc, err := nats.Connect(natsURL, nats.Name("otlp-bridge"), nats.UserCredentials(credsPath))
	if err != nil {
		log.Error("nats connect failed", "err", err)
		os.Exit(1)
	}
	defer nc.Close()

	js, err := jetstream.New(nc)
	if err != nil {
		log.Error("jetstream init failed", "err", err)
		os.Exit(1)
	}

	ctx := context.Background()
	cons, err := openConsumer(ctx, js, replay)
	if err != nil {
		log.Error("consumer setup failed", "err", err)
		os.Exit(1)
	}

	b := &batcher{endpoint: endpoint, client: &http.Client{Timeout: 10 * time.Second}, log: log}

	consumeCtx, err := cons.Consume(func(msg jetstream.Msg) {
		var w otlpmap.WireSpan
		if err := json.Unmarshal(msg.Data(), &w); err != nil {
			log.Error("drop malformed span", "subject", msg.Subject(), "err", err)
			_ = msg.Ack()
			return
		}
		b.add(msg, otlpmap.ToSpan(w))
	})
	if err != nil {
		log.Error("consume failed", "err", err)
		os.Exit(1)
	}
	defer consumeCtx.Stop()

	log.Info("otlp-bridge running", "endpoint", endpoint, "replay", replay)

	ticker := time.NewTicker(batchInterval)
	defer ticker.Stop()
	for range ticker.C {
		b.flush()
	}
}

// openConsumer picks the delivery mode ARCHITECTURE-COMMUNICATIONS.md § 6
// documents as "the only switch": replay=true is an ephemeral consumer over
// the whole retained window (an on-demand re-export, not a resumable
// cursor — each start replays from the beginning again); replay=false is
// the durable live-tailing mode, resuming from wherever it last left off.
func openConsumer(ctx context.Context, js jetstream.JetStream, replay bool) (jetstream.Consumer, error) {
	if replay {
		return js.CreateConsumer(ctx, tracesStreamName, jetstream.ConsumerConfig{
			FilterSubject: tracesSubjectWildcard,
			DeliverPolicy: jetstream.DeliverAllPolicy,
			AckPolicy:     jetstream.AckExplicitPolicy,
		})
	}
	return js.CreateOrUpdateConsumer(ctx, tracesStreamName, jetstream.ConsumerConfig{
		Durable:       consumerDurableName,
		FilterSubject: tracesSubjectWildcard,
		DeliverPolicy: jetstream.DeliverNewPolicy,
		AckPolicy:     jetstream.AckExplicitPolicy,
	})
}

// pending pairs a decoded span with the JetStream message it came from, so
// batcher can ack/nak the original message once the HTTP POST it fed
// resolves.
type pending struct {
	msg  jetstream.Msg
	span otlpmap.Span
}

// batcher accumulates spans until batchSize or batchInterval (whichever
// comes first — ARCHITECTURE-COMMUNICATIONS.md § 6's "batch: size or
// interval"), then POSTs one export request and acks or naks the whole
// batch together. A failed POST naks every message in it, so nothing is
// lost while the collector is down: each span stays on TRACES and is
// redelivered on the next attempt.
type batcher struct {
	mu       sync.Mutex
	items    []pending
	endpoint string
	client   *http.Client
	log      *slog.Logger
}

func (b *batcher) add(msg jetstream.Msg, span otlpmap.Span) {
	b.mu.Lock()
	b.items = append(b.items, pending{msg: msg, span: span})
	full := len(b.items) >= batchSize
	b.mu.Unlock()
	if full {
		b.flush()
	}
}

func (b *batcher) flush() {
	b.mu.Lock()
	items := b.items
	b.items = nil
	b.mu.Unlock()
	if len(items) == 0 {
		return
	}

	spans := make([]otlpmap.Span, len(items))
	for i, it := range items {
		spans[i] = it.span
	}

	body, err := otlpmap.MarshalExportRequest(spans)
	if err != nil {
		b.log.Error("marshal export request failed, will redeliver", "err", err)
		nakAll(items)
		return
	}

	resp, err := b.client.Post(b.endpoint, "application/json", bytes.NewReader(body))
	if err != nil {
		b.log.Error("otlp export failed, will redeliver", "err", err)
		nakAll(items)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		b.log.Error("otlp export rejected, will redeliver", "status", resp.StatusCode, "body", string(respBody))
		nakAll(items)
		return
	}

	for _, it := range items {
		_ = it.msg.Ack()
	}
	b.log.Info("exported spans", "count", len(items))
}

func nakAll(items []pending) {
	for _, it := range items {
		_ = it.msg.Nak()
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
