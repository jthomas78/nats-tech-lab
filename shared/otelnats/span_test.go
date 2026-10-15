package otelnats_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

var _ = Describe("BR-065: an outbound CLIENT span belongs to the caller", func() {
	Context("BR-065: service.name and destination", func() {
		It("names the caller as service.name and records the destination as attributes", func() {
			exp := tracetest.NewInMemoryExporter()
			p := newProvider("organizations-service", exp)

			_, span := p.StartClient(context.Background(), "refdata.item.get.v1", "refdata")
			span.End()

			client := byKind(flushed(exp, p), trace.SpanKindClient)
			Expect(serviceName(client)).To(Equal("organizations-service"))

			peer, ok := attr(client, "peer.service")
			Expect(ok).To(BeTrue())
			Expect(peer).To(Equal("refdata"))

			dest, ok := attr(client, "messaging.destination.name")
			Expect(ok).To(BeTrue())
			Expect(dest).To(Equal("refdata.item.get.v1"))

			system, ok := attr(client, "messaging.system")
			Expect(ok).To(BeTrue())
			Expect(system).To(Equal("nats"))
		})

		It("is the child of the span already in the context", func() {
			exp := tracetest.NewInMemoryExporter()
			p := newProvider("organizations-service", exp)

			parentCtx, parent := p.Tracer().Start(context.Background(), "handler")
			_, span := p.StartClient(parentCtx, "refdata.item.get.v1", "refdata")
			span.End()
			parent.End()

			client := byKind(flushed(exp, p), trace.SpanKindClient)
			Expect(client.Parent.SpanID()).To(Equal(parent.SpanContext().SpanID()))
		})
	})
})
