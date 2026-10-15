package otelnats_test

import (
	"context"
	"regexp"
	"time"

	"github.com/nats-io/nats.go"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/jthomas78/nats-tech-lab/shared/otelnats"
)

var traceparentFormat = regexp.MustCompile(`^00-[0-9a-f]{32}-[0-9a-f]{16}-01$`)

var _ = Describe("BR-064: one request across services is one trace", func() {
	Context("BR-064: SERVER → CLIENT → SERVER over NATS", func() {
		It("links a browser api.* call, its outbound request and the remote handler into one trace", func() {
			nc := startNATS("otelnats-spec")
			exp := tracetest.NewInMemoryExporter()
			org := newProvider("organizations-service", exp)
			refdata := newProvider("refdata-service", exp)

			// refdata-service: the trusted rpc.* side.
			_, err := nc.Subscribe("rpc.acme.refdata.item.get.v1", func(m *nats.Msg) {
				ctx := otelnats.Extract(context.Background(), m.Header)
				_, span := refdata.StartServer(ctx, m, otelnats.EdgeRPC, otelnats.Trust{ReceivingAccount: "PLATFORM"})
				span.End()
				_ = m.Respond([]byte(`{}`))
			})
			Expect(err).NotTo(HaveOccurred())

			// organizations-service: the browser api.* side, which calls refdata.
			_, err = nc.Subscribe("api.acme.organizations.fleet-asset.add.v1", func(m *nats.Msg) {
				defer GinkgoRecover()
				ctx, span := org.StartServer(context.Background(), m, otelnats.EdgeAPI, otelnats.Trust{ReceivingAccount: "ACME"})
				cctx, cspan := org.StartClient(ctx, "refdata.item.get.v1", "refdata")
				out := nats.NewMsg("rpc.acme.refdata.item.get.v1")
				otelnats.Inject(cctx, out)
				_, rerr := nc.RequestMsg(out, 2*time.Second)
				Expect(rerr).NotTo(HaveOccurred())
				cspan.End()
				span.End()
				_ = m.Respond([]byte(`{}`))
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(nc.Flush()).To(Succeed())

			_, err = nc.Request("api.acme.organizations.fleet-asset.add.v1", []byte(`{}`), 5*time.Second)
			Expect(err).NotTo(HaveOccurred())

			spans := flushed(exp, org, refdata)
			Expect(spans).To(HaveLen(3))

			var servers []tracetest.SpanStub
			for _, s := range spans {
				if s.SpanKind == trace.SpanKindServer {
					servers = append(servers, s)
				}
			}
			Expect(servers).To(HaveLen(2))
			client := byKind(spans, trace.SpanKindClient)

			var apiServer, rpcServer tracetest.SpanStub
			for _, s := range servers {
				if serviceName(s) == "organizations-service" {
					apiServer = s
				} else {
					rpcServer = s
				}
			}

			By("sharing one trace ID")
			traceID := apiServer.SpanContext.TraceID()
			Expect(client.SpanContext.TraceID()).To(Equal(traceID))
			Expect(rpcServer.SpanContext.TraceID()).To(Equal(traceID))

			By("making the CLIENT span the child of the api.* SERVER span")
			Expect(client.Parent.SpanID()).To(Equal(apiServer.SpanContext.SpanID()))

			By("making the rpc.* SERVER span the remote child of the CLIENT span")
			Expect(rpcServer.Parent.SpanID()).To(Equal(client.SpanContext.SpanID()))
			Expect(rpcServer.Parent.IsRemote()).To(BeTrue())
		})
	})

	Context("BR-064: the NATS header carrier", func() {
		It("writes a W3C traceparent for the current span", func() {
			exp := tracetest.NewInMemoryExporter()
			p := newProvider("organizations-service", exp)
			ctx, span := p.StartClient(context.Background(), "refdata.item.get.v1", "refdata")
			defer span.End()

			msg := nats.NewMsg("rpc.acme.refdata.item.get.v1")
			otelnats.Inject(ctx, msg)

			got := otelnats.HeaderCarrier(msg.Header).Get("traceparent")
			Expect(got).To(MatchRegexp(traceparentFormat.String()))
			Expect(got).To(ContainSubstring(span.SpanContext().TraceID().String()))
		})

		It("reads traceparent whatever the header key's case", func() {
			for _, key := range []string{"traceparent", "Traceparent", "TRACEPARENT"} {
				h := nats.Header{}
				h[key] = []string{sentTraceparent("01")}
				sc := trace.SpanContextFromContext(otelnats.Extract(context.Background(), h))
				Expect(sc.IsValid()).To(BeTrue(), "key %q", key)
				Expect(sc.TraceID().String()).To(Equal(sentTraceID), "key %q", key)
				Expect(sc.IsRemote()).To(BeTrue(), "key %q", key)
			}
		})

		It("lists the keys it holds", func() {
			c := otelnats.HeaderCarrier(nats.Header{})
			c.Set("traceparent", sentTraceparent("01"))
			Expect(c.Keys()).To(HaveLen(1))
			Expect(c.Get("traceparent")).To(Equal(sentTraceparent("01")))
		})
	})
})
