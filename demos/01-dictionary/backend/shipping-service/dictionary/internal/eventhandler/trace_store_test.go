// Integration tests (Phase 28f/28g) for RegisterTraceStore's full pipeline:
// TRACES stream + trace-request-reply KV bucket provisioning, and the durable consumer
// that projects obs.trace.* spans into it. Uses the same embedded
// JetStream-enabled server helper platform_notify_test.go's specs share.
package eventhandler_test

import (
	"context"
	"encoding/json"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/eventhandler"
)

var _ = Describe("RegisterTraceStore (Phase 28f/28g)", func() {
	It("does not provision anything or panic when platformFullJS or platformNC is nil", func() {
		nc, js := newPlatformNotifyTestNATS()
		ctx, cancel := context.WithCancel(context.Background())
		DeferCleanup(cancel)

		Expect(func() {
			consumeCtx, err := eventhandler.RegisterTraceStore(ctx, nil, nc, discardLogger())
			Expect(err).NotTo(HaveOccurred())
			Expect(consumeCtx).To(BeNil())
		}).NotTo(Panic())

		Expect(func() {
			consumeCtx, err := eventhandler.RegisterTraceStore(ctx, js, nil, discardLogger())
			Expect(err).NotTo(HaveOccurred())
			Expect(consumeCtx).To(BeNil())
		}).NotTo(Panic())
	})

	It("projects a published obs.trace.* span into the trace-request-reply KV bucket, keyed by traceId", func() {
		nc, js := newPlatformNotifyTestNATS()
		ctx, cancel := context.WithCancel(context.Background())
		DeferCleanup(cancel)

		consumeCtx, err := eventhandler.RegisterTraceStore(ctx, js, nc, discardLogger())
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(consumeCtx.Stop)

		span := []byte(`{"traceId":"abc123","spanId":"span1","service":"shipping","entity":"ship","action":"arrived","statusCode":"OK"}`)
		Expect(nc.Publish("obs.trace.acme.shipping.ship.arrived", span)).To(Succeed())

		Eventually(func() (int, error) {
			kv, err := js.KeyValue(ctx, "trace-request-reply")
			if err != nil {
				return 0, err
			}
			entry, err := kv.Get(ctx, "_platform.trace.abc123")
			if err != nil {
				return 0, err
			}
			var record struct {
				TraceID string            `json:"traceId"`
				Spans   []json.RawMessage `json:"spans"`
			}
			if err := json.Unmarshal(entry.Value(), &record); err != nil {
				return 0, err
			}
			return len(record.Spans), nil
		}, 5*time.Second, 100*time.Millisecond).Should(Equal(1))
	})

	It("merges a second span of the same trace into the existing KV entry instead of overwriting it", func() {
		nc, js := newPlatformNotifyTestNATS()
		ctx, cancel := context.WithCancel(context.Background())
		DeferCleanup(cancel)

		consumeCtx, err := eventhandler.RegisterTraceStore(ctx, js, nc, discardLogger())
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(consumeCtx.Stop)

		span1 := []byte(`{"traceId":"xyz789","spanId":"span1","service":"trading-partner","action":"fleet-asset-add"}`)
		span2 := []byte(`{"traceId":"xyz789","spanId":"span2","service":"refdata","action":"item-get"}`)
		Expect(nc.Publish("obs.trace.acme.trading-partner.fleet.asset-add", span1)).To(Succeed())
		Expect(nc.Publish("obs.trace._platform.refdata.item.get", span2)).To(Succeed())

		Eventually(func() (int, error) {
			kv, err := js.KeyValue(ctx, "trace-request-reply")
			if err != nil {
				return 0, err
			}
			entry, err := kv.Get(ctx, "_platform.trace.xyz789")
			if err != nil {
				return 0, err
			}
			var record struct {
				Spans []json.RawMessage `json:"spans"`
			}
			if err := json.Unmarshal(entry.Value(), &record); err != nil {
				return 0, err
			}
			return len(record.Spans), nil
		}, 5*time.Second, 100*time.Millisecond).Should(Equal(2), "both hops of the same cross-service trace must be assembled under one KV entry")
	})

	It("publishes notify._platform.kv.trace-request-reply.{key}.changed after each write, reusing kvstore.Store.EnableNotify", func() {
		nc, js := newPlatformNotifyTestNATS()
		ctx, cancel := context.WithCancel(context.Background())
		DeferCleanup(cancel)

		consumeCtx, err := eventhandler.RegisterTraceStore(ctx, js, nc, discardLogger())
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(consumeCtx.Stop)

		notifyCh := subscribeSync(nc, "notify._platform.kv.trace-request-reply.>")

		span := []byte(`{"traceId":"notif1","spanId":"span1","service":"shipping"}`)
		Expect(nc.Publish("obs.trace.acme.shipping.ship.arrived", span)).To(Succeed())

		Eventually(notifyCh, 5*time.Second, 100*time.Millisecond).Should(Receive(ContainSubstring("notif1")))
	})
})
