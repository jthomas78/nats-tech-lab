package refdataclient_test

// Phase 54a-4: with an otelnats Provider, the client makes OTel CLIENT spans
// instead of natstrace ones. The natstrace specs in client_test.go cover the
// path with no Provider, which is still the default stack's.

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/nats-io/nats.go"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/internal/refdataclient"
	"github.com/jthomas78/nats-tech-lab/shared/otelnats"
)

var traceparentFormat = `^00-[0-9a-f]{32}-[0-9a-f]{16}-01$`

func otelProvider(exp *tracetest.InMemoryExporter) *otelnats.Provider {
	GinkgoHelper()
	p, err := otelnats.Init(context.Background(), otelnats.Config{
		ServiceName: "organizations-service",
		Exporter:    exp,
	})
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(func() { _ = p.Shutdown(context.Background()) })
	return p
}

func clientSpans(p *otelnats.Provider, exp *tracetest.InMemoryExporter) tracetest.SpanStubs {
	GinkgoHelper()
	Expect(p.ForceFlush(context.Background())).To(Succeed())
	return exp.GetSpans()
}

func intAttr(s tracetest.SpanStub, key string) (int64, bool) {
	for _, kv := range s.Attributes {
		if string(kv.Key) == key && kv.Value.Type() == attribute.INT64 {
			return kv.Value.AsInt64(), true
		}
	}
	return 0, false
}

var _ = Describe("refdataclient.Client with an otelnats Provider (Phase 54a-4)", func() {
	var nc *nats.Conn

	BeforeEach(func() {
		nc = newTestConn()
	})

	Context("BR-064 — the outbound request carries a W3C traceparent for its CLIENT span", func() {
		It("writes traceparent, and the CLIENT span is the child of the span in ctx", func() {
			received := make(chan *nats.Msg, 1)
			sub, err := nc.Subscribe("refdata.item.get.v1", func(m *nats.Msg) {
				received <- m
				_ = m.Respond([]byte(`{"item":{"code":"TAUTLINER","status":"active"}}`))
			})
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { _ = sub.Unsubscribe() })
			Expect(nc.Flush()).To(Succeed())

			exp := tracetest.NewInMemoryExporter()
			p := otelProvider(exp)
			parentCtx, parent := p.Tracer().Start(context.Background(), "handler")

			client := refdataclient.New(nc, refdataclient.WithTracing(p))
			ok, err := client.Exists(parentCtx, "acme-north", "TAUTLINER")
			parent.End()
			Expect(err).NotTo(HaveOccurred())
			Expect(ok).To(BeTrue())

			var msg *nats.Msg
			Eventually(received).Should(Receive(&msg))
			Expect(otelnats.HeaderCarrier(msg.Header).Get("traceparent")).To(MatchRegexp(traceparentFormat))
			Expect(msg.Header.Get("Nats-Requestor")).NotTo(BeEmpty(), "BR-041's requester header is unchanged")

			var cspan tracetest.SpanStub
			for _, s := range clientSpans(p, exp) {
				if s.SpanKind == trace.SpanKindClient {
					cspan = s
				}
			}
			Expect(cspan.Parent.SpanID()).To(Equal(parent.SpanContext().SpanID()))
			Expect(otelnats.HeaderCarrier(msg.Header).Get("traceparent")).To(ContainSubstring(cspan.SpanContext.SpanID().String()))
			n, ok := intAttr(cspan, "rpc.retry_count")
			Expect(ok).To(BeTrue())
			Expect(n).To(BeZero())
		})
	})

	Context("BR-065 — the CLIENT span belongs to the caller", func() {
		It("has the caller's service.name and refdata as peer.service", func() {
			sub, err := nc.Subscribe("refdata.item.get.v1", func(m *nats.Msg) {
				_ = m.Respond([]byte(`{"item":{"code":"TAUTLINER","status":"active"}}`))
			})
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { _ = sub.Unsubscribe() })
			Expect(nc.Flush()).To(Succeed())

			exp := tracetest.NewInMemoryExporter()
			p := otelProvider(exp)
			_, err = refdataclient.New(nc, refdataclient.WithTracing(p)).Exists(context.Background(), "acme-north", "TAUTLINER")
			Expect(err).NotTo(HaveOccurred())

			spans := clientSpans(p, exp)
			Expect(spans).To(HaveLen(1))
			Expect(spans[0].SpanKind).To(Equal(trace.SpanKindClient))
			var svc, peer string
			for _, kv := range spans[0].Resource.Attributes() {
				if kv.Key == "service.name" {
					svc = kv.Value.AsString()
				}
			}
			for _, kv := range spans[0].Attributes {
				if kv.Key == "peer.service" {
					peer = kv.Value.AsString()
				}
			}
			Expect(svc).To(Equal("organizations-service"))
			Expect(peer).To(Equal("refdata"))
		})
	})

	Context("BR-037 — one span per logical call, never one per retry", func() {
		It("makes exactly one ERROR CLIENT span with rpc.retry_count=2 when nothing answers", func() {
			exp := tracetest.NewInMemoryExporter()
			p := otelProvider(exp)

			_, err := refdataclient.New(nc, refdataclient.WithTracing(p)).Exists(context.Background(), "acme-north", "TAUTLINER")
			Expect(err).To(MatchError(refdataclient.ErrRPCUnavailable))

			spans := clientSpans(p, exp)
			Expect(spans).To(HaveLen(1))
			Expect(spans[0].Status.Code).To(Equal(codes.Error))
			n, ok := intAttr(spans[0], "rpc.retry_count")
			Expect(ok).To(BeTrue())
			Expect(n).To(Equal(int64(2)))
		})
	})

	Context("BR-055 — a micro service-error reply marks the CLIENT span ERROR", func() {
		It("sets ERROR with the Nats-Service-Error text", func() {
			sub, err := nc.Subscribe("refdata.item.get.v1", func(m *nats.Msg) {
				reply := nats.NewMsg(m.Reply)
				reply.Header.Set("Nats-Service-Error", "dictionary item not found")
				reply.Header.Set("Nats-Service-Error-Code", "404")
				reply.Data = []byte(`{"error":"dictionary item not found","notFound":true}`)
				_ = m.RespondMsg(reply)
			})
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { _ = sub.Unsubscribe() })
			Expect(nc.Flush()).To(Succeed())

			exp := tracetest.NewInMemoryExporter()
			p := otelProvider(exp)
			ok, err := refdataclient.New(nc, refdataclient.WithTracing(p)).Exists(context.Background(), "acme-north", "NOPE")
			Expect(err).NotTo(HaveOccurred())
			Expect(ok).To(BeFalse())

			spans := clientSpans(p, exp)
			Expect(spans).To(HaveLen(1))
			Expect(spans[0].Status.Code).To(Equal(codes.Error))
			Expect(spans[0].Status.Description).To(Equal("dictionary item not found"))
		})
	})

	It("publishes no natstrace span on obs.trace.> when a Provider is set (the path is OTel only)", func() {
		legacy := make(chan *nats.Msg, 4)
		spanSub, err := nc.Subscribe("obs.trace.>", func(m *nats.Msg) { legacy <- m })
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = spanSub.Unsubscribe() })
		sub, err := nc.Subscribe("refdata.item.get.v1", func(m *nats.Msg) {
			_ = m.Respond([]byte(`{"item":{"code":"TAUTLINER","status":"active"}}`))
		})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = sub.Unsubscribe() })
		Expect(nc.Flush()).To(Succeed())

		exp := tracetest.NewInMemoryExporter()
		p := otelProvider(exp)
		_, err = refdataclient.New(nc, refdataclient.WithTracing(p)).Exists(context.Background(), "acme-north", "TAUTLINER")
		Expect(err).NotTo(HaveOccurred())
		Expect(nc.Flush()).To(Succeed())
		Consistently(legacy, 200*time.Millisecond).ShouldNot(Receive())
	})
})
