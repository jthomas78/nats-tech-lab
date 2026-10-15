package otelnats_test

import (
	"context"

	"github.com/nats-io/nats.go"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/jthomas78/nats-tech-lab/shared/otelnats"
)

var _ = Describe("BR-037: evt.* and notify.* carry trace context as PRODUCER and CONSUMER spans (decision 5)", func() {
	const subject = "evt.acme.shipping.ship.s1.arrived"

	Context("BR-037: StartProducer", func() {
		It("starts a PRODUCER span named for the subject, as the child of the span in ctx", func() {
			exp := tracetest.NewInMemoryExporter()
			p := newProvider("shipping-service", exp)

			ctx, parent := p.Tracer().Start(context.Background(), "api.acme.shipping.ship.arrive.v1")
			_, span := p.StartProducer(ctx, subject)
			span.End()
			parent.End()

			producer := byKind(flushed(exp, p), trace.SpanKindProducer)
			Expect(producer.Name).To(Equal(subject))
			Expect(producer.Parent.SpanID()).To(Equal(parent.SpanContext().SpanID()))
			Expect(producer.SpanContext.TraceID()).To(Equal(parent.SpanContext().TraceID()))
			got, _ := attr(producer, "messaging.destination.name")
			Expect(got).To(Equal(subject))
		})

		It("starts a new root when ctx holds no span", func() {
			exp := tracetest.NewInMemoryExporter()
			p := newProvider("shipping-service", exp)

			_, span := p.StartProducer(context.Background(), subject)
			span.End()

			producer := byKind(flushed(exp, p), trace.SpanKindProducer)
			Expect(producer.SpanContext.IsValid()).To(BeTrue())
			Expect(producer.Parent.IsValid()).To(BeFalse())
		})

		It("returns a context that Inject turns into the producer's own traceparent", func() {
			exp := tracetest.NewInMemoryExporter()
			p := newProvider("shipping-service", exp)

			ctx, span := p.StartProducer(context.Background(), subject)
			msg := nats.NewMsg(subject)
			otelnats.Inject(ctx, msg)
			span.End()

			producer := byKind(flushed(exp, p), trace.SpanKindProducer)
			Expect(msg.Header.Get("traceparent")).To(ContainSubstring(producer.SpanContext.SpanID().String()))
		})
	})

	Context("BR-037: StartConsumer", func() {
		It("starts a CONSUMER span that continues the traceparent in the message", func() {
			exp := tracetest.NewInMemoryExporter()
			p := newProvider("organizations-service", exp)

			msg := nats.NewMsg("notify.accounts.account.created")
			msg.Header.Set("traceparent", sentTraceparent("01"))
			_, span := p.StartConsumer(context.Background(), msg, otelnats.Trust{ReceivingAccount: "PLATFORM"})
			span.End()

			consumer := byKind(flushed(exp, p), trace.SpanKindConsumer)
			Expect(consumer.Name).To(Equal("notify.accounts.account.created"))
			Expect(consumer.SpanContext.TraceID().String()).To(Equal(sentTraceID))
			Expect(consumer.Parent.SpanID().String()).To(Equal(sentSpanID))
		})

		It("starts a new root when the message carries no traceparent", func() {
			exp := tracetest.NewInMemoryExporter()
			p := newProvider("organizations-service", exp)

			_, span := p.StartConsumer(context.Background(), nats.NewMsg("notify.accounts.account.created"),
				otelnats.Trust{ReceivingAccount: "PLATFORM"})
			span.End()

			consumer := byKind(flushed(exp, p), trace.SpanKindConsumer)
			Expect(consumer.SpanContext.IsValid()).To(BeTrue())
			Expect(consumer.Parent.IsValid()).To(BeFalse())
		})

		It("sets the tenant attributes from trust only (BR-069)", func() {
			exp := tracetest.NewInMemoryExporter()
			p := newProvider("shipping-service", exp)

			msg := nats.NewMsg("evt.acme.shipping.ship.s1.arrived")
			msg.Header.Set("tenant", "globex")
			_, span := p.StartConsumer(context.Background(), msg, otelnats.Trust{ReceivingAccount: "acme"})
			span.End()

			consumer := byKind(flushed(exp, p), trace.SpanKindConsumer)
			got, ok := attr(consumer, string(otelnats.AttrReceivingAccount))
			Expect(ok).To(BeTrue())
			Expect(got).To(Equal("acme"))
			_, has := attr(consumer, string(otelnats.AttrOriginatingTenant))
			Expect(has).To(BeFalse())
		})
	})
})
