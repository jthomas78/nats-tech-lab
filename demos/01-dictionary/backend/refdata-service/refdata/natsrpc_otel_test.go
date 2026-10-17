package refdata_test

// Phase 54a-4: with an otelnats Provider in Deps, rpc.*.refdata.item.get is
// traced with OTel (the other endpoints followed in 54b-3a). Since Phase 54d
// OTel is the only tracer: without a Provider the call is untraced, which
// natsrpc_test.go's BR-D39 spec covers.

import (
	"context"
	"encoding/json"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/nats-io/nats.go"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata/internal/application/commands"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata/internal/natsrpc"
	"github.com/jthomas78/nats-tech-lab/shared/otelnats"
)

var _ = Describe("rpc.*.refdata.item.get with an otelnats Provider (Phase 54a-4)", func() {
	const (
		itemCtx     = "acme-test"
		sentTraceID = "4bf92f3577b34da6a3ce929d0e0e4736"
		sentSpanID  = "00f067aa0ba902b7"
	)
	rpcSubject := "rpc." + itemCtx + ".refdata.item.get.v1"

	var (
		nc  *nats.Conn
		exp *tracetest.InMemoryExporter
		p   *otelnats.Provider
	)

	BeforeEach(func() {
		ctx := context.Background()
		items := newFakeItemRepo()
		itemH := commands.NewItemHandler(items, newFakeReferenceRepo(), nil)
		locH := commands.NewLocalizationHandler(items, newFakeLocalizationRepo(), newFakeLocaleRepo(), nil)
		_, err := itemH.RegisterItem(ctx, commands.ItemInput{TypeKey: "currency", Code: "EUR", Context: itemCtx})
		Expect(err).NotTo(HaveOccurred())

		exp = tracetest.NewInMemoryExporter()
		p, err = otelnats.Init(ctx, otelnats.Config{ServiceName: "refdata-service", Exporter: exp})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = p.Shutdown(context.Background()) })

		nc = newTestNATSConn()
		adapter, err := natsrpc.New(nc, natsrpc.Deps{Localizations: locH, Items: itemH, Tracing: p})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { Expect(adapter.Stop()).To(Succeed()) })
	})

	itemGet := func(code string) {
		GinkgoHelper()
		body, err := json.Marshal(natsrpc.ItemGetRequest{Context: itemCtx, TypeKey: "currency", Code: code, Locale: "en"})
		Expect(err).NotTo(HaveOccurred())
		m := nats.NewMsg(rpcSubject)
		m.Data = body
		m.Header.Set("traceparent", "00-"+sentTraceID+"-"+sentSpanID+"-01")
		_, err = nc.RequestMsg(m, 2*time.Second)
		Expect(err).NotTo(HaveOccurred())
	}

	server := func() tracetest.SpanStub {
		GinkgoHelper()
		Expect(p.ForceFlush(context.Background())).To(Succeed())
		var found []tracetest.SpanStub
		for _, s := range exp.GetSpans() {
			if s.SpanKind == trace.SpanKindServer {
				found = append(found, s)
			}
		}
		Expect(found).To(HaveLen(1))
		return found[0]
	}

	Context("BR-064 — a trusted rpc.* call continues the caller's trace", func() {
		It("makes the SERVER span a child of the sent traceparent", func() {
			itemGet("EUR")
			s := server()
			Expect(s.Name).To(Equal(rpcSubject))
			Expect(s.SpanContext.TraceID().String()).To(Equal(sentTraceID))
			Expect(s.Parent.SpanID().String()).To(Equal(sentSpanID))
			Expect(s.Status.Code).To(Equal(codes.Unset))
		})
	})

	Context("BR-065 — the SERVER span belongs to refdata-service", func() {
		It("has refdata-service as its service.name", func() {
			itemGet("EUR")
			var svc string
			for _, kv := range server().Resource.Attributes() {
				if kv.Key == "service.name" {
					svc = kv.Value.AsString()
				}
			}
			Expect(svc).To(Equal("refdata-service"))
		})
	})

	Context("BR-069 — the receiving account is PLATFORM; the originating tenant stays empty until 54a-6", func() {
		It("sets lab.tenant.receiving_account to PLATFORM and no originating tenant", func() {
			itemGet("EUR")
			var account string
			var hasOrigin bool
			for _, kv := range server().Attributes {
				switch kv.Key {
				case otelnats.AttrReceivingAccount:
					account = kv.Value.AsString()
				case otelnats.AttrOriginatingTenant:
					hasOrigin = true
				}
			}
			Expect(account).To(Equal("PLATFORM"))
			Expect(hasOrigin).To(BeFalse())
		})
	})

	Context("BR-055 carried over — a not-found reply marks the SERVER span ERROR", func() {
		It("sets ERROR with the error text", func() {
			itemGet("does-not-exist")
			s := server()
			Expect(s.Status.Code).To(Equal(codes.Error))
			Expect(s.Status.Description).NotTo(BeEmpty())
		})
	})

	It("publishes nothing on obs.trace.* for item.get (the path is OTel only)", func() {
		legacy := make(chan *nats.Msg, 8)
		sub, err := nc.Subscribe("obs.trace.*.refdata.item.get", func(m *nats.Msg) { legacy <- m })
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = sub.Unsubscribe() })
		Expect(nc.Flush()).To(Succeed())

		itemGet("EUR")
		Expect(nc.Flush()).To(Succeed())
		Consistently(legacy, 200*time.Millisecond).ShouldNot(Receive())
	})
})
