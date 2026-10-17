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

// BR-041 (Phase 54d): the caller's self-declared Nats-Requestor header is
// kept on the SERVER span as lab.requester, for display only. It replaces
// the retired legacy tracer's Requester field. It never feeds the tenant
// attributes (BR-069).
var _ = Describe("BR-041: a self-declared requester is carried for observability only", func() {
	Context("BR-041: lab.requester on the SERVER span", func() {
		It("copies Nats-Requestor onto the span as lab.requester", func() {
			exp := tracetest.NewInMemoryExporter()
			p := newProvider("refdata-service", exp)

			msg := &nats.Msg{Subject: "api.acme.refdata.item.get.v1", Header: nats.Header{}}
			msg.Header.Set(otelnats.RequestorHeader, "admin-app/abc123")
			_, span := p.StartServer(context.Background(), msg, otelnats.EdgeAPI, otelnats.Trust{ReceivingAccount: "acme"})
			span.End()

			server := byKind(flushed(exp, p), trace.SpanKindServer)
			got, ok := attr(server, string(otelnats.AttrRequester))
			Expect(ok).To(BeTrue())
			Expect(got).To(Equal("admin-app/abc123"))
		})

		It("sets no lab.requester when the header is absent", func() {
			exp := tracetest.NewInMemoryExporter()
			p := newProvider("refdata-service", exp)

			msg := &nats.Msg{Subject: "rpc.acme.refdata.item.get.v1", Header: nats.Header{}}
			_, span := p.StartServer(context.Background(), msg, otelnats.EdgeRPC, otelnats.Trust{ReceivingAccount: "acme"})
			span.End()

			server := byKind(flushed(exp, p), trace.SpanKindServer)
			_, ok := attr(server, string(otelnats.AttrRequester))
			Expect(ok).To(BeFalse())
		})

		It("never takes the tenant from the requester header", func() {
			exp := tracetest.NewInMemoryExporter()
			p := newProvider("refdata-service", exp)

			msg := &nats.Msg{Subject: "api.acme.refdata.item.get.v1", Header: nats.Header{}}
			msg.Header.Set(otelnats.RequestorHeader, "browser-globex/1")
			_, span := p.StartServer(context.Background(), msg, otelnats.EdgeAPI, otelnats.Trust{ReceivingAccount: "acme"})
			span.End()

			server := byKind(flushed(exp, p), trace.SpanKindServer)
			_, ok := attr(server, string(otelnats.AttrOriginatingTenant))
			Expect(ok).To(BeFalse())
		})
	})

	It("names the identity headers once, for every service (BR-027)", func() {
		Expect(otelnats.RequestorHeader).To(Equal("Nats-Requestor"))
		Expect(otelnats.ResponderHeader).To(Equal("Nats-Responder"))
	})
})
