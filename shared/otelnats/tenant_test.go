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

// forgedMsg carries every client-controllable claim to be another tenant:
// BR-041's self-declared requester, a server-style request-info header the
// client wrote itself, and a tenant field in the body.
func forgedMsg() *nats.Msg {
	m := nats.NewMsg("rpc.acme.refdata.item.get.v1")
	m.Header.Set("Nats-Requestor", "globex-service")
	m.Header.Set("Nats-Request-Info", `{"acc":"GLOBEX","rtt":"1ms"}`)
	m.Data = []byte(`{"tenant":"globex","account":"GLOBEX"}`)
	return m
}

var _ = Describe("BR-069: tenant attributes come from trusted context only", func() {
	Context("BR-069: StartServer reads Trust, never the message", func() {
		It("sets both attributes from Trust, ignoring forged headers and body", func() {
			exp := tracetest.NewInMemoryExporter()
			p := newProvider("refdata-service", exp)

			_, span := p.StartServer(context.Background(), forgedMsg(), otelnats.EdgeRPC, otelnats.Trust{
				ReceivingAccount:  "PLATFORM",
				OriginatingTenant: "acme",
			})
			span.End()

			server := byKind(flushed(exp, p), trace.SpanKindServer)
			got, ok := attr(server, string(otelnats.AttrReceivingAccount))
			Expect(ok).To(BeTrue())
			Expect(got).To(Equal("PLATFORM"))
			got, ok = attr(server, string(otelnats.AttrOriginatingTenant))
			Expect(ok).To(BeTrue())
			Expect(got).To(Equal("acme"))
		})

		It("leaves the originating tenant unset when Trust has none, even though the message names one", func() {
			exp := tracetest.NewInMemoryExporter()
			p := newProvider("refdata-service", exp)

			_, span := p.StartServer(context.Background(), forgedMsg(), otelnats.EdgeRPC, otelnats.Trust{
				ReceivingAccount: "PLATFORM",
			})
			span.End()

			server := byKind(flushed(exp, p), trace.SpanKindServer)
			got, ok := attr(server, string(otelnats.AttrReceivingAccount))
			Expect(ok).To(BeTrue())
			Expect(got).To(Equal("PLATFORM"))
			_, ok = attr(server, string(otelnats.AttrOriginatingTenant))
			Expect(ok).To(BeFalse(), "an unknown originating tenant is absent (shown unattributed), never guessed")
		})
	})

	Context("BR-069: the import-renamed subject", func() {
		DescribeTable("reads the tenant token from rpc.{tenant}.{service}.… only",
			func(subject, want string) {
				Expect(otelnats.OriginatingTenantFromRPCSubject(subject)).To(Equal(want))
			},
			Entry("the renamed form", "rpc.acme.refdata.item.get.v1", "acme"),
			Entry("a hyphenated tenant", "rpc.acme-north.refdata.item.get.v1", "acme-north"),
			Entry("the unrenamed alias", "refdata.item.get.v1", ""),
			Entry("an api.* subject", "api.acme.refdata.item.get.v1", ""),
			Entry("an empty token", "rpc..refdata.item.get.v1", ""),
			Entry("a wildcard token", "rpc.*.refdata.item.get.v1", ""),
			Entry("a full wildcard", "rpc.>", ""),
			Entry("too few tokens", "rpc.acme", ""),
			Entry("empty", "", ""),
		)
	})
})
