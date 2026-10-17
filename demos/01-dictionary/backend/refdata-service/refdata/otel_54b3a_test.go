package refdata_test

// Phase 54b-3a: with an otelnats Provider, every refdata-service rpc.* and
// api.* endpoint is traced with OTel, and the evt.* change pointer a mutation
// publishes is a PRODUCER span under the mutation's ctx. Since Phase 54d OTel
// is the only tracer: without a Provider the path is untraced, which the
// no-Provider specs in natsrpc_test.go and kvcache_test.go cover.

import (
	"context"
	"encoding/json"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata/internal/application/commands"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata/internal/browserrpc"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata/internal/domain"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata/internal/kvcache"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata/internal/kvstore"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata/internal/natsrpc"
	"github.com/jthomas78/nats-tech-lab/shared/jstream"
	"github.com/jthomas78/nats-tech-lab/shared/otelnats"
)

const (
	otelSentTraceID = "4bf92f3577b34da6a3ce929d0e0e4736"
	otelSentSpanID  = "00f067aa0ba902b7"
)

// newRefdataProvider is an OTel Provider that exports to exp.
func newRefdataProvider(exp *tracetest.InMemoryExporter) *otelnats.Provider {
	GinkgoHelper()
	p, err := otelnats.Init(context.Background(), otelnats.Config{ServiceName: "refdata-service", Exporter: exp})
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(func() { _ = p.Shutdown(context.Background()) })
	return p
}

// spansOfKind flushes p and returns the exported spans of kind.
func spansOfKind(p *otelnats.Provider, exp *tracetest.InMemoryExporter, kind trace.SpanKind) []tracetest.SpanStub {
	GinkgoHelper()
	Expect(p.ForceFlush(context.Background())).To(Succeed())
	var found []tracetest.SpanStub
	for _, s := range exp.GetSpans() {
		if s.SpanKind == kind {
			found = append(found, s)
		}
	}
	return found
}

func receivingAccount(s tracetest.SpanStub) string {
	for _, kv := range s.Attributes {
		if kv.Key == otelnats.AttrReceivingAccount {
			return kv.Value.AsString()
		}
	}
	return ""
}

// requestWithTraceparent sends body on subject with the sent traceparent and
// waits for the reply. The reply may be an error; the span is what counts.
func requestWithTraceparent(nc *nats.Conn, subject string, body any) {
	GinkgoHelper()
	data, err := json.Marshal(body)
	Expect(err).NotTo(HaveOccurred())
	m := nats.NewMsg(subject)
	m.Data = data
	m.Header.Set("traceparent", "00-"+otelSentTraceID+"-"+otelSentSpanID+"-01")
	_, err = nc.RequestMsg(m, 2*time.Second)
	Expect(err).NotTo(HaveOccurred())
}

var _ = Describe("Phase 54b-3a: the other rpc.*.refdata.* endpoints on OTel", func() {
	const itemCtx = "acme-test"

	var (
		nc  *nats.Conn
		exp *tracetest.InMemoryExporter
		p   *otelnats.Provider
	)

	BeforeEach(func() {
		items := newFakeItemRepo()
		itemH := commands.NewItemHandler(items, newFakeReferenceRepo(), nil)
		locH := commands.NewLocalizationHandler(items, newFakeLocalizationRepo(), newFakeLocaleRepo(), nil)

		exp = tracetest.NewInMemoryExporter()
		p = newRefdataProvider(exp)
		nc = newTestNATSConn()
		adapter, err := natsrpc.New(nc, natsrpc.Deps{Localizations: locH, Items: itemH, Tracing: p})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { Expect(adapter.Stop()).To(Succeed()) })
	})

	DescribeTable("BR-064 — a trusted rpc.* call continues the caller's trace",
		func(subject string, body any) {
			requestWithTraceparent(nc, subject, body)

			servers := spansOfKind(p, exp, trace.SpanKindServer)
			Expect(servers).To(HaveLen(1), "want one OTel SERVER span for %s", subject)
			s := servers[0]
			Expect(s.Name).To(Equal(subject))
			Expect(s.SpanContext.TraceID().String()).To(Equal(otelSentTraceID))
			Expect(s.Parent.SpanID().String()).To(Equal(otelSentSpanID))
			Expect(receivingAccount(s)).To(Equal("PLATFORM"))
		},
		Entry("type.list", "rpc."+itemCtx+".refdata.type.list.v1",
			map[string]string{"context": itemCtx, "typeKey": "currency", "locale": "en"}),
		Entry("item.get-versioned", "rpc."+itemCtx+".refdata.item.get-versioned.v1",
			map[string]any{"context": itemCtx, "version": 1, "typeKey": "currency", "code": "EUR"}),
		Entry("locales.list", "rpc."+itemCtx+".refdata.locales.list.v1",
			map[string]string{"context": itemCtx}),
		Entry("context.list", "rpc._platform.refdata.context.list.v1",
			map[string]string{}),
	)

	It("publishes nothing on obs.trace.* for any endpoint (the path is OTel only)", func() {
		legacy := make(chan *nats.Msg, 8)
		sub, err := nc.Subscribe("obs.trace.>", func(m *nats.Msg) { legacy <- m })
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = sub.Unsubscribe() })
		Expect(nc.Flush()).To(Succeed())

		requestWithTraceparent(nc, "rpc."+itemCtx+".refdata.locales.list.v1", map[string]string{"context": itemCtx})
		Expect(nc.Flush()).To(Succeed())
		Consistently(legacy, 200*time.Millisecond).ShouldNot(Receive())
	})
})

var _ = Describe("Phase 54b-3a: api.*.refdata.* on OTel", func() {
	var (
		nc  *nats.Conn
		exp *tracetest.InMemoryExporter
		p   *otelnats.Provider
	)

	mount := func(tenant string) {
		GinkgoHelper()
		items := newFakeItemRepo()
		itemH := commands.NewItemHandler(items, newFakeReferenceRepo(), nil)
		locH := commands.NewLocalizationHandler(items, newFakeLocalizationRepo(), newFakeLocaleRepo(), nil)
		adapter, err := browserrpc.New(nc, browserrpc.Deps{Items: itemH, Localizations: locH, Tenant: tenant, Tracing: p})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { Expect(adapter.Stop()).To(Succeed()) })
	}

	BeforeEach(func() {
		exp = tracetest.NewInMemoryExporter()
		p = newRefdataProvider(exp)
		nc = newTestNATSConn()
	})

	server := func() tracetest.SpanStub {
		GinkgoHelper()
		servers := spansOfKind(p, exp, trace.SpanKindServer)
		Expect(servers).To(HaveLen(1), "want one OTel SERVER span")
		return servers[0]
	}

	Context("BR-071 — a browser call starts a new root and only links the sent traceparent", func() {
		It("gives the SERVER span a new trace ID and one link to the sent span", func() {
			mount("acme")
			requestWithTraceparent(nc, "api.acme.refdata.item.get.v1", browserrpc.ItemGetRequest{TypeKey: "currency", Code: "EUR", Locale: "en"})

			s := server()
			Expect(s.Name).To(Equal("api.acme.refdata.item.get.v1"))
			Expect(s.SpanContext.TraceID().String()).NotTo(Equal(otelSentTraceID))
			Expect(s.Parent.IsValid()).To(BeFalse())
			Expect(s.Links).To(HaveLen(1))
			Expect(s.Links[0].SpanContext.TraceID().String()).To(Equal(otelSentTraceID))
		})
	})

	Context("BR-069 — the receiving account is the account the adapter's connection lives in", func() {
		It("is the tenant on a tenant's own connection", func() {
			mount("acme")
			requestWithTraceparent(nc, "api.acme.refdata.item.get.v1", browserrpc.ItemGetRequest{TypeKey: "currency", Code: "EUR", Locale: "en"})
			Expect(receivingAccount(server())).To(Equal("acme"))
		})

		It("is PLATFORM on the _platform mount", func() {
			mount("_platform")
			requestWithTraceparent(nc, "api.acme.refdata.item.get.v1", browserrpc.ItemGetRequest{TypeKey: "currency", Code: "EUR", Locale: "en"})
			Expect(receivingAccount(server())).To(Equal("PLATFORM"))
		})
	})

	It("publishes nothing on obs.trace.* (the path is OTel only)", func() {
		mount("acme")
		legacy := make(chan *nats.Msg, 8)
		sub, err := nc.Subscribe("obs.trace.>", func(m *nats.Msg) { legacy <- m })
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = sub.Unsubscribe() })
		Expect(nc.Flush()).To(Succeed())

		requestWithTraceparent(nc, "api.acme.refdata.item.get.v1", browserrpc.ItemGetRequest{TypeKey: "currency", Code: "EUR", Locale: "en"})
		Expect(nc.Flush()).To(Succeed())
		Consistently(legacy, 200*time.Millisecond).ShouldNot(Receive())
	})
})

var _ = Describe("Phase 54b-3a: the evt.* change pointer on OTel", func() {
	const itemCtx = "acme-test"

	It("BR-037 — is a PRODUCER span under the mutation's ctx, and carries its traceparent", func() {
		ctx := context.Background()
		js := newRefdataJetStream()
		exp := tracetest.NewInMemoryExporter()
		p := newRefdataProvider(exp)

		items := newFakeItemRepo()
		refs := newFakeReferenceRepo()
		projector := kvcache.NewProjector(kvstore.New(js, "refdata"), items, newFakeLocalizationRepo(), refs, newFakeVersionRepo(),
			newTestNamespaces(domain.DictionaryType{TypeKey: "currency", Category: domain.CategoryStandards}),
			jstream.NewPublisher(js, jstream.WithOTel(p)))
		itemH := commands.NewItemHandler(items, refs, projector)

		consumer, err := js.CreateOrUpdateConsumer(ctx, kvcache.ChangeStreamName, jetstream.ConsumerConfig{DeliverPolicy: jetstream.DeliverAllPolicy})
		Expect(err).NotTo(HaveOccurred())

		// Stands in for the SERVER span an rpc.*/api.* handler puts in ctx.
		parentCtx, parent := p.Tracer().Start(ctx, "rpc.acme-test.refdata.item.register.v1")
		_, err = itemH.RegisterItem(parentCtx, commands.ItemInput{TypeKey: "currency", Code: "EUR", Context: itemCtx})
		Expect(err).NotTo(HaveOccurred())
		parent.End()

		producers := spansOfKind(p, exp, trace.SpanKindProducer)
		Expect(producers).To(HaveLen(1), "want one PRODUCER span for the evt.* publish")
		pr := producers[0]
		Expect(pr.Name).To(Equal(kvcache.ChangeSubject(itemCtx, "currency")))
		Expect(pr.SpanContext.TraceID()).To(Equal(parent.SpanContext().TraceID()))
		Expect(pr.Parent.SpanID()).To(Equal(parent.SpanContext().SpanID()))

		msgs, err := consumer.Fetch(1, jetstream.FetchMaxWait(5*time.Second))
		Expect(err).NotTo(HaveOccurred())
		var msg jetstream.Msg
		for m := range msgs.Messages() {
			msg = m
		}
		Expect(msg).NotTo(BeNil())
		Expect(msg.Headers().Get("traceparent")).To(Equal(
			"00-" + pr.SpanContext.TraceID().String() + "-" + pr.SpanContext.SpanID().String() + "-01"))
	})
})
