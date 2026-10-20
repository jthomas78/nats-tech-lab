package otelnats_test

import (
	"context"
	"errors"
	"time"

	"github.com/nats-io/nats.go"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/jthomas78/nats-tech-lab/shared/otelnats"
)

// Phase 57: the span wrappers Call (CLIENT), Consume (CONSUMER) and Produce
// (PRODUCER). Service code passes the work; the wrapper owns the span.

const (
	wrapSubject = "evt.acme.shipping.ship.s1.arrived"
	rpcSubject  = "refdata.item.get.v1"
)

// ctxKey is a value a caller puts on its ctx.
type ctxKey struct{}

// attrValue is attr without its presence flag.
func attrValue(s tracetest.SpanStub, key string) string {
	v, _ := attr(s, key)
	return v
}

// nop is a Consume or Produce callback that succeeds.
func nop(context.Context) error { return nil }

// statusOf returns the status of the one span of kind k.
func statusOf(exp *tracetest.InMemoryExporter, p *otelnats.Provider, k trace.SpanKind) (codes.Code, string) {
	s := byKind(flushed(exp, p), k)
	return s.Status.Code, s.Status.Description
}

var _ = Describe("Phase 57: span wrappers", func() {
	var (
		exp *tracetest.InMemoryExporter
		p   *otelnats.Provider
	)
	BeforeEach(func() {
		exp = tracetest.NewInMemoryExporter()
		p = newProvider("shipping-service", exp)
	})

	Context("BR-078: Call owns one CLIENT span for the whole retry loop", func() {
		It("makes one CLIENT span, child of the span in ctx, with peer.service and rpc.retry_count", func() {
			ctx, parent := p.Tracer().Start(context.Background(), "api.acme.shipping.ship.get.v1")
			calls := 0
			_, err := p.Call(ctx, nats.NewMsg(rpcSubject), "refdata", func(context.Context) (*nats.Msg, int, error) {
				calls++
				return &nats.Msg{}, 2, nil
			})
			parent.End()
			Expect(err).NotTo(HaveOccurred())
			Expect(calls).To(Equal(1), "the wrapper never retries; the service owns the retry policy")

			client := byKind(flushed(exp, p), trace.SpanKindClient)
			Expect(client.Name).To(Equal(rpcSubject))
			Expect(client.Parent.SpanID()).To(Equal(parent.SpanContext().SpanID()))
			Expect(attrValue(client, "peer.service")).To(Equal("refdata"))
			Expect(attrValue(client, "rpc.retry_count")).To(Equal("2"))
			Expect(attrValue(client, "messaging.destination.name")).To(Equal(rpcSubject))
		})

		It("writes the CLIENT span's traceparent into the request before the callback runs", func() {
			msg := nats.NewMsg(rpcSubject)
			var sent string
			_, err := p.Call(context.Background(), msg, "refdata", func(context.Context) (*nats.Msg, int, error) {
				sent = msg.Header.Get("traceparent")
				return &nats.Msg{}, 0, nil
			})
			Expect(err).NotTo(HaveOccurred())

			client := byKind(flushed(exp, p), trace.SpanKindClient)
			Expect(sent).To(ContainSubstring(client.SpanContext.SpanID().String()))
		})

		It("returns the callback's reply", func() {
			want := &nats.Msg{Data: []byte("ok")}
			got, err := p.Call(context.Background(), nats.NewMsg(rpcSubject), "refdata", func(context.Context) (*nats.Msg, int, error) {
				return want, 0, nil
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(BeIdenticalTo(want))
		})
	})

	Context("BR-078: Consume and Produce", func() {
		It("Consume makes one CONSUMER span named for the subject, with the tenant attributes from Trust", func() {
			err := p.Consume(context.Background(), wrapSubject, nil,
				otelnats.Trust{ReceivingAccount: "acme", OriginatingTenant: "globex"}, nop)
			Expect(err).NotTo(HaveOccurred())

			consumer := byKind(flushed(exp, p), trace.SpanKindConsumer)
			Expect(consumer.Name).To(Equal(wrapSubject))
			Expect(attrValue(consumer, "lab.tenant.receiving_account")).To(Equal("acme"))
			Expect(attrValue(consumer, "lab.tenant.originating")).To(Equal("globex"))
		})

		It("Consume gives the callback the CONSUMER span's ctx, so nested work is its child", func() {
			err := p.Consume(context.Background(), wrapSubject, nil, otelnats.Trust{ReceivingAccount: "acme"},
				func(ctx context.Context) error {
					return p.Produce(ctx, nats.NewMsg("notify.acme.shipping.ship.changed"), nop)
				})
			Expect(err).NotTo(HaveOccurred())

			spans := flushed(exp, p)
			consumer := byKind(spans, trace.SpanKindConsumer)
			producer := byKind(spans, trace.SpanKindProducer)
			Expect(producer.Parent.SpanID()).To(Equal(consumer.SpanContext.SpanID()))
		})

		It("Produce makes one PRODUCER span, child of the span in ctx, and writes its traceparent before the publish runs", func() {
			ctx, parent := p.Tracer().Start(context.Background(), "api.acme.shipping.ship.arrive.v1")
			msg := nats.NewMsg(wrapSubject)
			var sent string
			err := p.Produce(ctx, msg, func(context.Context) error {
				sent = msg.Header.Get("traceparent")
				return nil
			})
			parent.End()
			Expect(err).NotTo(HaveOccurred())

			producer := byKind(flushed(exp, p), trace.SpanKindProducer)
			Expect(producer.Name).To(Equal(wrapSubject))
			Expect(producer.Parent.SpanID()).To(Equal(parent.SpanContext().SpanID()))
			Expect(sent).To(ContainSubstring(producer.SpanContext.SpanID().String()))
		})
	})

	Context("Decision 6a: a CONSUMER span's parent comes from the message header only", func() {
		It("takes the header's span as parent even when ctx already holds a span", func() {
			ctx, stale := p.Tracer().Start(context.Background(), "provisioning")
			h := nats.Header{"traceparent": []string{sentTraceparent("01")}}
			Expect(p.Consume(ctx, wrapSubject, h, otelnats.Trust{ReceivingAccount: "acme"}, nop)).To(Succeed())
			stale.End()

			consumer := byKind(flushed(exp, p), trace.SpanKindConsumer)
			Expect(consumer.Parent.SpanID().String()).To(Equal(sentSpanID))
			Expect(consumer.SpanContext.TraceID().String()).To(Equal(sentTraceID))
		})

		It("starts a new root with no header, not a child of the span in ctx", func() {
			ctx, stale := p.Tracer().Start(context.Background(), "provisioning")
			Expect(p.Consume(ctx, wrapSubject, nil, otelnats.Trust{ReceivingAccount: "acme"}, nop)).To(Succeed())
			stale.End()

			consumer := byKind(flushed(exp, p), trace.SpanKindConsumer)
			Expect(consumer.Parent.IsValid()).To(BeFalse())
			Expect(consumer.SpanContext.TraceID()).NotTo(Equal(stale.SpanContext().TraceID()))
		})
	})

	// wrappers runs each wrapper once on ctx with a callback that records
	// the ctx it got. Every wrapper is one entry, so a spec covers all three.
	type wrapper struct {
		name string
		run  func(p *otelnats.Provider, ctx context.Context, got *context.Context) error
	}
	wrappers := []wrapper{
		{"Call", func(p *otelnats.Provider, ctx context.Context, got *context.Context) error {
			_, err := p.Call(ctx, nats.NewMsg(rpcSubject), "refdata", func(c context.Context) (*nats.Msg, int, error) {
				*got = c
				return &nats.Msg{}, 0, nil
			})
			return err
		}},
		{"Consume", func(p *otelnats.Provider, ctx context.Context, got *context.Context) error {
			return p.Consume(ctx, wrapSubject, nil, otelnats.Trust{ReceivingAccount: "acme"}, func(c context.Context) error {
				*got = c
				return nil
			})
		}},
		{"Produce", func(p *otelnats.Provider, ctx context.Context, got *context.Context) error {
			return p.Produce(ctx, nats.NewMsg(wrapSubject), func(c context.Context) error {
				*got = c
				return nil
			})
		}},
	}

	Context("Decision 6b / BR-082: the callback's ctx keeps the caller's cancellation, deadline and values", func() {
		for _, w := range wrappers {
			It(w.name+" keeps them", func() {
				deadline := time.Now().Add(time.Hour)
				base, cancel := context.WithDeadline(context.WithValue(context.Background(), ctxKey{}, "caller"), deadline)
				defer cancel()
				// A span already on ctx: Consume removes it and must keep the rest.
				base, stale := p.Tracer().Start(base, "caller")
				defer stale.End()

				var got context.Context
				Expect(w.run(p, base, &got)).To(Succeed())

				d, ok := got.Deadline()
				Expect(ok).To(BeTrue())
				Expect(d).To(Equal(deadline))
				Expect(got.Value(ctxKey{})).To(Equal("caller"))
				Expect(got.Err()).NotTo(HaveOccurred())
				cancel()
				Eventually(got.Done()).Should(BeClosed())
				Expect(got.Err()).To(MatchError(context.Canceled))
			})
		}
	})

	Context("Decision 6c / BR-079: with a nil provider a wrapper only runs the operation", func() {
		var nilP *otelnats.Provider

		for _, w := range wrappers {
			It(w.name+" passes the caller's ctx unchanged", func() {
				ctx := context.WithValue(context.Background(), ctxKey{}, "caller")
				var got context.Context
				Expect(w.run(nilP, ctx, &got)).To(Succeed())
				Expect(got).To(BeIdenticalTo(ctx))
			})
		}

		It("Call runs the callback once, returns its reply and error unchanged, and adds no header", func() {
			msg := nats.NewMsg(rpcSubject)
			msg.Header = nil
			want := &nats.Msg{Data: []byte("r")}
			boom := errors.New("boom")
			calls := 0
			got, err := nilP.Call(context.Background(), msg, "refdata", func(context.Context) (*nats.Msg, int, error) {
				calls++
				return want, 3, boom
			})
			Expect(calls).To(Equal(1))
			Expect(got).To(BeIdenticalTo(want))
			Expect(err).To(BeIdenticalTo(boom))
			Expect(msg.Header).To(BeNil())
		})

		It("Consume runs the callback once and returns its error unchanged", func() {
			boom := errors.New("boom")
			calls := 0
			err := nilP.Consume(context.Background(), wrapSubject, nil, otelnats.Trust{}, func(context.Context) error {
				calls++
				return boom
			})
			Expect(calls).To(Equal(1))
			Expect(err).To(BeIdenticalTo(boom))
		})

		It("Produce runs the callback once, returns its error unchanged, and adds no header", func() {
			msg := &nats.Msg{Subject: wrapSubject}
			boom := errors.New("boom")
			calls := 0
			err := nilP.Produce(context.Background(), msg, func(context.Context) error {
				calls++
				return boom
			})
			Expect(calls).To(Equal(1))
			Expect(err).To(BeIdenticalTo(boom))
			Expect(msg.Header).To(BeNil())
		})

		It("lets a panic propagate unchanged", func() {
			Expect(func() {
				_, _ = nilP.Call(context.Background(), nats.NewMsg(rpcSubject), "refdata", func(context.Context) (*nats.Msg, int, error) { panic("call") })
			}).To(PanicWith("call"))
			Expect(func() {
				_ = nilP.Consume(context.Background(), wrapSubject, nil, otelnats.Trust{}, func(context.Context) error { panic("consume") })
			}).To(PanicWith("consume"))
			Expect(func() {
				_ = nilP.Produce(context.Background(), nats.NewMsg(wrapSubject), func(context.Context) error { panic("produce") })
			}).To(PanicWith("produce"))
		})
	})

	Context("BR-080: a span is Error when its operation fails", func() {
		boom := errors.New("boom")

		It("Call: an error is Error, and the error is returned", func() {
			_, err := p.Call(context.Background(), nats.NewMsg(rpcSubject), "refdata", func(context.Context) (*nats.Msg, int, error) {
				return nil, 2, boom
			})
			Expect(err).To(BeIdenticalTo(boom))
			code, desc := statusOf(exp, p, trace.SpanKindClient)
			Expect(code).To(Equal(codes.Error))
			Expect(desc).To(Equal("boom"))
			client := byKind(flushed(exp, p), trace.SpanKindClient)
			Expect(attrValue(client, "rpc.retry_count")).To(Equal("2"))
		})

		It("Call: a reply carrying Nats-Service-Error is Error (BR-055)", func() {
			reply := &nats.Msg{Header: nats.Header{"Nats-Service-Error": []string{"not found"}}}
			_, err := p.Call(context.Background(), nats.NewMsg(rpcSubject), "refdata", func(context.Context) (*nats.Msg, int, error) {
				return reply, 0, nil
			})
			Expect(err).NotTo(HaveOccurred())
			code, desc := statusOf(exp, p, trace.SpanKindClient)
			Expect(code).To(Equal(codes.Error))
			Expect(desc).To(Equal("not found"))
		})

		It("Consume: a failed attempt is Error and records the error", func() {
			err := p.Consume(context.Background(), wrapSubject, nil, otelnats.Trust{ReceivingAccount: "acme"}, func(context.Context) error { return boom })
			Expect(err).To(BeIdenticalTo(boom))
			consumer := byKind(flushed(exp, p), trace.SpanKindConsumer)
			Expect(consumer.Status.Code).To(Equal(codes.Error))
			Expect(consumer.Events).NotTo(BeEmpty(), "the error is recorded as an event")
		})

		It("Produce: a failed publish is Error", func() {
			err := p.Produce(context.Background(), nats.NewMsg(wrapSubject), func(context.Context) error { return boom })
			Expect(err).To(BeIdenticalTo(boom))
			code, _ := statusOf(exp, p, trace.SpanKindProducer)
			Expect(code).To(Equal(codes.Error))
		})

		It("leaves a successful or skipped operation Unset", func() {
			Expect(p.Consume(context.Background(), wrapSubject, nil, otelnats.Trust{ReceivingAccount: "acme"}, nop)).To(Succeed())
			code, _ := statusOf(exp, p, trace.SpanKindConsumer)
			Expect(code).To(Equal(codes.Unset))
		})

		for _, w := range wrappers {
			It(w.name+": a panic marks the span Error, ends it, and panics again with the same value", func() {
				var kind trace.SpanKind
				run := func() {}
				switch w.name {
				case "Call":
					kind = trace.SpanKindClient
					run = func() {
						_, _ = p.Call(context.Background(), nats.NewMsg(rpcSubject), "refdata", func(context.Context) (*nats.Msg, int, error) { panic("bang") })
					}
				case "Consume":
					kind = trace.SpanKindConsumer
					run = func() {
						_ = p.Consume(context.Background(), wrapSubject, nil, otelnats.Trust{ReceivingAccount: "acme"}, func(context.Context) error { panic("bang") })
					}
				case "Produce":
					kind = trace.SpanKindProducer
					run = func() {
						_ = p.Produce(context.Background(), nats.NewMsg(wrapSubject), func(context.Context) error { panic("bang") })
					}
				}
				Expect(run).To(PanicWith("bang"))
				code, _ := statusOf(exp, p, kind)
				Expect(code).To(Equal(codes.Error))
			})
		}
	})

	Context("BR-081: a caller label never replaces an attribute the wrapper sets", func() {
		reserved := []otelnats.Label{
			{Key: "lab.tenant.receiving_account", Value: "evil"},
			{Key: "lab.tenant.originating", Value: "globex"},
			{Key: "messaging.destination.name", Value: "evil.subject"},
			{Key: "messaging.system", Value: "kafka"},
			{Key: "peer.service", Value: "evil"},
			{Key: "rpc.retry_count", Value: "99"},
			{Key: "lab.requester", Value: "evil"},
		}
		labels := append([]otelnats.Label{{Key: "entity_id", Value: "s1"}}, reserved...)

		It("Consume keeps the trusted tenant and messaging attributes, and the unreserved label", func() {
			Expect(p.Consume(context.Background(), wrapSubject, nil, otelnats.Trust{ReceivingAccount: "acme"}, nop, labels...)).To(Succeed())
			consumer := byKind(flushed(exp, p), trace.SpanKindConsumer)
			Expect(attrValue(consumer, "lab.tenant.receiving_account")).To(Equal("acme"))
			_, present := attr(consumer, "lab.tenant.originating")
			Expect(present).To(BeFalse(), "an empty trusted originating tenant stays absent")
			Expect(attrValue(consumer, "messaging.destination.name")).To(Equal(wrapSubject))
			Expect(attrValue(consumer, "messaging.system")).To(Equal("nats"))
			_, present = attr(consumer, "peer.service")
			Expect(present).To(BeFalse())
			_, present = attr(consumer, "lab.requester")
			Expect(present).To(BeFalse())
			Expect(attrValue(consumer, "entity_id")).To(Equal("s1"))
		})

		It("Call keeps rpc.retry_count, peer.service and the destination", func() {
			_, err := p.Call(context.Background(), nats.NewMsg(rpcSubject), "refdata", func(context.Context) (*nats.Msg, int, error) {
				return &nats.Msg{}, 1, nil
			}, labels...)
			Expect(err).NotTo(HaveOccurred())
			client := byKind(flushed(exp, p), trace.SpanKindClient)
			Expect(attrValue(client, "rpc.retry_count")).To(Equal("1"))
			Expect(attrValue(client, "peer.service")).To(Equal("refdata"))
			Expect(attrValue(client, "messaging.destination.name")).To(Equal(rpcSubject))
			_, present := attr(client, "lab.tenant.originating")
			Expect(present).To(BeFalse())
			Expect(attrValue(client, "entity_id")).To(Equal("s1"))
		})

		It("Produce keeps the destination and adds no tenant attribute", func() {
			Expect(p.Produce(context.Background(), nats.NewMsg(wrapSubject), nop, labels...)).To(Succeed())
			producer := byKind(flushed(exp, p), trace.SpanKindProducer)
			Expect(attrValue(producer, "messaging.destination.name")).To(Equal(wrapSubject))
			_, present := attr(producer, "lab.tenant.receiving_account")
			Expect(present).To(BeFalse())
			_, present = attr(producer, "rpc.retry_count")
			Expect(present).To(BeFalse())
			Expect(attrValue(producer, "entity_id")).To(Equal("s1"))
		})
	})
})

