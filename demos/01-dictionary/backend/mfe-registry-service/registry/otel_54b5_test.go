package registry_test

// Phase 54b-5: mfe-registry-service on OTel. With registry.WithTracing every
// api.* endpoint is a SERVER span on the PLATFORM account that starts a new
// root (BR-071), every rpc.* endpoint is a SERVER span that continues a
// trusted caller's trace (BR-071 EdgeRPC), and a notify.* hint a write
// publishes is a PRODUCER child of that SERVER span (BR-037). Without it the
// adapters stay on natstrace (decision 16).

import (
	"context"
	"log/slog"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/browserrpc"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/domain"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/notify"
	"github.com/jthomas78/nats-tech-lab/shared/mferegistry"
	"github.com/jthomas78/nats-tech-lab/shared/otelnats"
)

const (
	otelSentTraceID = "4bf92f3577b34da6a3ce929d0e0e4736"
	otelSentSpanID  = "00f067aa0ba902b7"
)

// otelRegistry composes the module on an embedded NATS server, traced into
// exp when tracing is set.
func otelRegistry(ctx context.Context, opts ...registry.Option) *nats.Conn {
	GinkgoHelper()
	if pgUnavailable != "" {
		Skip(pgUnavailable)
	}
	GinkgoT().Setenv("REGISTRY_PRELOAD_FILE", "")
	_, err := pgDB.ExecContext(ctx, `TRUNCATE registry.entries, registry.audit; UPDATE registry.revision SET revision = 0`)
	Expect(err).NotTo(HaveOccurred())
	srv, err := server.NewServer(&server.Options{Host: "127.0.0.1", Port: -1})
	Expect(err).NotTo(HaveOccurred())
	srv.Start()
	DeferCleanup(srv.Shutdown)
	Expect(srv.ReadyForConnections(5 * time.Second)).To(BeTrue())
	nc, err := nats.Connect(srv.ClientURL(), nats.Name("mfe-registry-service"))
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(nc.Close)
	module, err := registry.Startup(ctx, pgDB, nil, nc, domain.NewAllowlist([]string{"http://localhost:7110"}), slog.Default(), opts...)
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(module.Stop)
	return nc
}

const otelUpsertBody = `{"ifRevision":0,"entryId":"fleet","entry":{"id":"fleet","enabled":true,"name":"fixture","contributions":[{"kind":"shell-footer","id":"status"}],"remote":{"kind":"federated","url":"http://localhost:7110/r.js","module":"./plugin"}}}`

var _ = Describe("Phase 54b-5: mfe-registry-service on OTel", func() {
	var (
		ctx context.Context
		nc  *nats.Conn
		exp *tracetest.InMemoryExporter
		p   *otelnats.Provider
	)

	BeforeEach(func() {
		ctx = context.Background()
		exp = tracetest.NewInMemoryExporter()
		var err error
		p, err = otelnats.Init(ctx, otelnats.Config{ServiceName: "mfe-registry-service", Exporter: exp})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = p.Shutdown(context.Background()) })
		nc = otelRegistry(ctx, registry.WithTracing(p))
	})

	spans := func() tracetest.SpanStubs {
		GinkgoHelper()
		Expect(p.ForceFlush(context.Background())).To(Succeed())
		return exp.GetSpans()
	}

	one := func(kind trace.SpanKind, name string) tracetest.SpanStub {
		GinkgoHelper()
		var found []tracetest.SpanStub
		for _, s := range spans() {
			if s.SpanKind == kind && s.Name == name {
				found = append(found, s)
			}
		}
		Expect(found).To(HaveLen(1), "want one %s span named %s", kind, name)
		return found[0]
	}

	account := func(s tracetest.SpanStub) string {
		for _, kv := range s.Attributes {
			if kv.Key == otelnats.AttrReceivingAccount {
				return kv.Value.AsString()
			}
		}
		return ""
	}

	It("BR-069 / BR-071 — an api.* write is a new-root SERVER span on PLATFORM, linking the sent traceparent", func() {
		m := nats.NewMsg(browserrpc.UpsertSubject)
		m.Data = []byte(otelUpsertBody)
		m.Header.Set("Traceparent", "00-"+otelSentTraceID+"-"+otelSentSpanID+"-01")
		_, err := nc.RequestMsg(m, 2*time.Second)
		Expect(err).NotTo(HaveOccurred())

		s := one(trace.SpanKindServer, browserrpc.UpsertSubject)
		Expect(account(s)).To(Equal("PLATFORM"))
		Expect(s.SpanContext.TraceID().String()).NotTo(Equal(otelSentTraceID))
		Expect(s.Parent.IsValid()).To(BeFalse())
		Expect(s.Links).To(HaveLen(1))
		Expect(s.Links[0].SpanContext.TraceID().String()).To(Equal(otelSentTraceID))
	})

	It("BR-037 — the write's notify.* hint is a PRODUCER child of the api.* SERVER span", func() {
		_, err := nc.Request(browserrpc.UpsertSubject, []byte(otelUpsertBody), 2*time.Second)
		Expect(err).NotTo(HaveOccurred())

		server := one(trace.SpanKindServer, browserrpc.UpsertSubject)
		var producer tracetest.SpanStub
		Eventually(func() int {
			n := 0
			for _, s := range spans() {
				if s.SpanKind == trace.SpanKindProducer && s.Name == notify.Changed().Name {
					producer, n = s, n+1
				}
			}
			return n
		}, 2*time.Second).Should(Equal(1))
		Expect(producer.SpanContext.TraceID()).To(Equal(server.SpanContext.TraceID()))
		Expect(producer.Parent.SpanID()).To(Equal(server.SpanContext.SpanID()))
	})

	It("BR-071 EdgeRPC — an rpc.* announcement continues the trusted caller's trace", func() {
		m := nats.NewMsg(mferegistry.Announce)
		m.Data = []byte(`{}`)
		m.Header.Set("Traceparent", "00-"+otelSentTraceID+"-"+otelSentSpanID+"-01")
		_, err := nc.RequestMsg(m, 2*time.Second)
		Expect(err).NotTo(HaveOccurred())

		s := one(trace.SpanKindServer, mferegistry.Announce)
		Expect(account(s)).To(Equal("PLATFORM"))
		Expect(s.SpanContext.TraceID().String()).To(Equal(otelSentTraceID))
		Expect(s.Parent.SpanID().String()).To(Equal(otelSentSpanID))
	})

	It("publishes no natstrace span on the OTel path", func() {
		legacy := make(chan *nats.Msg, 8)
		sub, err := nc.Subscribe("obs.trace.>", func(m *nats.Msg) { legacy <- m })
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = sub.Unsubscribe() })
		Expect(nc.Flush()).To(Succeed())

		_, err = nc.Request(browserrpc.ShellReadSubject, []byte(`{"heldRevision":null}`), 2*time.Second)
		Expect(err).NotTo(HaveOccurred())
		_, err = nc.Request(mferegistry.Announce, []byte(`{}`), 2*time.Second)
		Expect(err).NotTo(HaveOccurred())
		Expect(nc.Flush()).To(Succeed())
		Consistently(legacy, 200*time.Millisecond).ShouldNot(Receive())
	})
})

var _ = Describe("Phase 54b-5: no Provider keeps mfe-registry on natstrace (decision 16)", func() {
	It("publishes a natstrace span for an api.* read when WithTracing is not given", func() {
		nc := otelRegistry(context.Background())

		legacy := make(chan *nats.Msg, 8)
		sub, err := nc.Subscribe("obs.trace.>", func(m *nats.Msg) { legacy <- m })
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = sub.Unsubscribe() })
		Expect(nc.Flush()).To(Succeed())

		_, err = nc.Request(browserrpc.ShellReadSubject, []byte(`{"heldRevision":null}`), 2*time.Second)
		Expect(err).NotTo(HaveOccurred())
		Eventually(legacy, 2*time.Second).Should(Receive())
	})
})
