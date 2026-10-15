package otelnats_test

import (
	"context"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/micro"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/jthomas78/nats-tech-lab/shared/otelnats"
)

// mountMicro adds one micro endpoint, wrapped in p.Middleware, and returns
// the connection to call it on.
func mountMicro(p *otelnats.Provider, subject string, edge otelnats.Edge, trust otelnats.Trust, h micro.HandlerFunc) *nats.Conn {
	nc := startNATS("otelnats-micro-spec")
	svc, err := micro.AddService(nc, micro.Config{Name: "spec", Version: "0.0.1"})
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(func() { _ = svc.Stop() })
	err = svc.AddEndpoint("ep", p.Middleware(edge, func(micro.Request) otelnats.Trust { return trust }, h),
		micro.WithEndpointSubject(subject))
	Expect(err).NotTo(HaveOccurred())
	Expect(nc.Flush()).To(Succeed())
	return nc
}

var _ = Describe("BR-064: the micro middleware carries one span through a handler", func() {
	Context("BR-064: a micro endpoint wrapped in Middleware", func() {
		It("gives the handler a context whose span is the parent of its outbound CLIENT span", func() {
			exp := tracetest.NewInMemoryExporter()
			p := newProvider("organizations-service", exp)
			nc := mountMicro(p, "api.acme.organizations.fleet-asset.add.v1", otelnats.EdgeAPI,
				otelnats.Trust{ReceivingAccount: "ACME"},
				func(req micro.Request) {
					_, cspan := p.StartClient(otelnats.ContextFrom(req), "refdata.item.get.v1", "refdata")
					cspan.End()
					_ = req.Respond([]byte(`{}`))
				})

			_, err := nc.Request("api.acme.organizations.fleet-asset.add.v1", []byte(`{}`), 5*time.Second)
			Expect(err).NotTo(HaveOccurred())

			spans := flushed(exp, p)
			Expect(spans).To(HaveLen(2))
			server := byKind(spans, trace.SpanKindServer)
			client := byKind(spans, trace.SpanKindClient)
			Expect(client.Parent.SpanID()).To(Equal(server.SpanContext.SpanID()))

			got, ok := attr(server, string(otelnats.AttrReceivingAccount))
			Expect(ok).To(BeTrue())
			Expect(got).To(Equal("ACME"))
		})

		It("applies the edge policy: an EdgeAPI call keeps a sent traceparent as a link only (BR-071)", func() {
			exp := tracetest.NewInMemoryExporter()
			p := newProvider("organizations-service", exp)
			nc := mountMicro(p, "api.acme.organizations.fleet-asset.add.v1", otelnats.EdgeAPI,
				otelnats.Trust{ReceivingAccount: "ACME"},
				func(req micro.Request) { _ = req.Respond([]byte(`{}`)) })

			m := nats.NewMsg("api.acme.organizations.fleet-asset.add.v1")
			m.Header.Set("Traceparent", sentTraceparent("00"))
			_, err := nc.RequestMsg(m, 5*time.Second)
			Expect(err).NotTo(HaveOccurred())

			server := byKind(flushed(exp, p), trace.SpanKindServer)
			Expect(server.SpanContext.TraceID().String()).NotTo(Equal(sentTraceID))
			Expect(server.SpanContext.IsSampled()).To(BeTrue())
			Expect(server.Links).To(HaveLen(1))
		})

		It("continues a trusted caller's trace on an EdgeRPC endpoint", func() {
			exp := tracetest.NewInMemoryExporter()
			p := newProvider("refdata-service", exp)
			nc := mountMicro(p, "rpc.acme.refdata.item.get.v1", otelnats.EdgeRPC,
				otelnats.Trust{ReceivingAccount: "PLATFORM"},
				func(req micro.Request) { _ = req.Respond([]byte(`{}`)) })

			m := nats.NewMsg("rpc.acme.refdata.item.get.v1")
			m.Header.Set("Traceparent", sentTraceparent("01"))
			_, err := nc.RequestMsg(m, 5*time.Second)
			Expect(err).NotTo(HaveOccurred())

			server := byKind(flushed(exp, p), trace.SpanKindServer)
			Expect(server.SpanContext.TraceID().String()).To(Equal(sentTraceID))
			Expect(server.Parent.SpanID().String()).To(Equal(sentSpanID))
		})

		DescribeTable("marks the span ERROR when the reply is a micro service error (BR-055 carried over)",
			func(respond func(micro.Request)) {
				exp := tracetest.NewInMemoryExporter()
				p := newProvider("refdata-service", exp)
				nc := mountMicro(p, "rpc.acme.refdata.item.get.v1", otelnats.EdgeRPC,
					otelnats.Trust{ReceivingAccount: "PLATFORM"}, respond)

				_, err := nc.Request("rpc.acme.refdata.item.get.v1", []byte(`{}`), 5*time.Second)
				Expect(err).NotTo(HaveOccurred())

				server := byKind(flushed(exp, p), trace.SpanKindServer)
				Expect(server.Status.Code).To(Equal(codes.Error))
				Expect(server.Status.Description).To(Equal("dictionary item not found"))
			},
			Entry("through req.Error", func(req micro.Request) {
				_ = req.Error("404", "dictionary item not found", nil)
			}),
			Entry("through Respond with the error headers", func(req micro.Request) {
				_ = req.Respond(nil, micro.WithHeaders(micro.Headers{
					micro.ErrorHeader:     {"dictionary item not found"},
					micro.ErrorCodeHeader: {"404"},
				}))
			}),
		)

		It("leaves the status unset on a plain reply", func() {
			exp := tracetest.NewInMemoryExporter()
			p := newProvider("refdata-service", exp)
			nc := mountMicro(p, "rpc.acme.refdata.item.get.v1", otelnats.EdgeRPC,
				otelnats.Trust{ReceivingAccount: "PLATFORM"},
				func(req micro.Request) { _ = req.Respond([]byte(`{}`)) })

			_, err := nc.Request("rpc.acme.refdata.item.get.v1", []byte(`{}`), 5*time.Second)
			Expect(err).NotTo(HaveOccurred())

			server := byKind(flushed(exp, p), trace.SpanKindServer)
			Expect(server.Status.Code).To(Equal(codes.Unset))
		})
	})

	It("BR-064: ContextFrom gives a plain background context for a request it did not wrap", func() {
		var got context.Context
		nc := startNATS("otelnats-micro-spec-plain")
		svc, err := micro.AddService(nc, micro.Config{Name: "spec", Version: "0.0.1"})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = svc.Stop() })
		Expect(svc.AddEndpoint("ep", micro.HandlerFunc(func(req micro.Request) {
			got = otelnats.ContextFrom(req)
			_ = req.Respond(nil)
		}), micro.WithEndpointSubject("plain.v1"))).To(Succeed())

		_, err = nc.Request("plain.v1", nil, 5*time.Second)
		Expect(err).NotTo(HaveOccurred())
		Expect(got).NotTo(BeNil())
		Expect(trace.SpanContextFromContext(got).IsValid()).To(BeFalse())
	})
})
