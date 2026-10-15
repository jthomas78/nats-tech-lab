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

// browserMsg is an api.* request whose sender chose its own traceparent.
func browserMsg(traceparent string) *nats.Msg {
	m := nats.NewMsg("api.acme.organizations.fleet-asset.add.v1")
	m.Header.Set("Traceparent", traceparent)
	return m
}

// serveOnce runs StartServer the way a receiving adapter does: extract, then
// start, then end.
func serveOnce(p *otelnats.Provider, m *nats.Msg, edge otelnats.Edge) {
	ctx := otelnats.Extract(context.Background(), m.Header)
	_, span := p.StartServer(ctx, m, edge, otelnats.Trust{ReceivingAccount: "ACME"})
	span.End()
}

var _ = Describe("BR-071: a browser traceparent is a link, never a parent", func() {
	Context("BR-071: EdgeAPI", func() {
		DescribeTable("starts a new, sampled trace with one link to what the browser sent",
			func(flags string) {
				exp := tracetest.NewInMemoryExporter()
				p := newProvider("organizations-service", exp)

				serveOnce(p, browserMsg(sentTraceparent(flags)), otelnats.EdgeAPI)

				server := byKind(flushed(exp, p), trace.SpanKindServer)
				Expect(server.SpanContext.IsSampled()).To(BeTrue(), "the browser cannot turn sampling off")
				Expect(server.SpanContext.TraceID().String()).NotTo(Equal(sentTraceID))
				Expect(server.Parent.IsValid()).To(BeFalse(), "a new root has no parent")
				Expect(server.Links).To(HaveLen(1))
				Expect(server.Links[0].SpanContext.TraceID().String()).To(Equal(sentTraceID))
				Expect(server.Links[0].SpanContext.SpanID().String()).To(Equal(sentSpanID))
			},
			Entry("sampled", "01"),
			Entry("not sampled", "00"),
		)

		It("starts a new trace with no link when the traceparent is malformed", func() {
			exp := tracetest.NewInMemoryExporter()
			p := newProvider("organizations-service", exp)

			serveOnce(p, browserMsg("not-a-traceparent"), otelnats.EdgeAPI)

			server := byKind(flushed(exp, p), trace.SpanKindServer)
			Expect(server.SpanContext.IsValid()).To(BeTrue())
			Expect(server.SpanContext.IsSampled()).To(BeTrue())
			Expect(server.Parent.IsValid()).To(BeFalse())
			Expect(server.Links).To(BeEmpty())
		})

		It("starts a sampled trace when no traceparent is sent at all", func() {
			exp := tracetest.NewInMemoryExporter()
			p := newProvider("organizations-service", exp)

			serveOnce(p, nats.NewMsg("api.acme.organizations.fleet-asset.add.v1"), otelnats.EdgeAPI)

			server := byKind(flushed(exp, p), trace.SpanKindServer)
			Expect(server.SpanContext.IsSampled()).To(BeTrue())
			Expect(server.Links).To(BeEmpty())
		})
	})

	Context("BR-071: EdgeRPC, for contrast", func() {
		It("continues a trusted caller's trace as its parent", func() {
			exp := tracetest.NewInMemoryExporter()
			p := newProvider("refdata-service", exp)

			m := nats.NewMsg("rpc.acme.refdata.item.get.v1")
			m.Header.Set("Traceparent", sentTraceparent("01"))
			serveOnce(p, m, otelnats.EdgeRPC)

			server := byKind(flushed(exp, p), trace.SpanKindServer)
			Expect(server.SpanContext.TraceID().String()).To(Equal(sentTraceID))
			Expect(server.Parent.SpanID().String()).To(Equal(sentSpanID))
			Expect(server.Links).To(BeEmpty())
		})
	})
})
