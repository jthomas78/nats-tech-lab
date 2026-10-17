package accounts_test

// Phase 54b-5: accounts-service on OTel. The Users api.* endpoints are SERVER
// spans on the PLATFORM account that start a new root (BR-069, BR-071).
// TraceHTTP picks the REST tracing: the OTel SERVER span when a Provider is
// set (BR-073), untraced otherwise (Phase 54d: the old tracer is gone, and
// nothing is published on obs.trace.*). The REST-to-publish trace itself is
// in handler_test.go, where it now carries BR-037's rule alone.

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/nats-io/nats.go"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/accounts-service/accounts"
	"github.com/jthomas78/nats-tech-lab/shared/otelnats"
)

func otelProvider(exp *tracetest.InMemoryExporter) *otelnats.Provider {
	GinkgoHelper()
	p, err := otelnats.Init(context.Background(), otelnats.Config{ServiceName: "accounts-service", Exporter: exp})
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(func() { _ = p.Shutdown(context.Background()) })
	return p
}

func obsTraceMsgs(nc *nats.Conn) chan *nats.Msg {
	GinkgoHelper()
	ch := make(chan *nats.Msg, 8)
	sub, err := nc.Subscribe("obs.trace.>", func(m *nats.Msg) { ch <- m })
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(func() { _ = sub.Unsubscribe() })
	Expect(nc.Flush()).To(Succeed())
	return ch
}

var _ = Describe("Phase 54b-5: the Users api.* adapter on OTel", func() {
	It("BR-069 / BR-071 — .list.v1 is a new-root SERVER span on PLATFORM, and nothing on obs.trace.*", func() {
		nc := startEmbeddedNATS("accounts-service-platform")
		exp := tracetest.NewInMemoryExporter()
		p := otelProvider(exp)
		legacy := obsTraceMsgs(nc)

		adapter, err := accounts.NewUsersAdapter(nc,
			accounts.NewUserClaimsReader(&fakeUserReadStore{}, &fakeAccountLookup{}, nil),
			accounts.NewUserRevoker(&fakeUserReadStore{}, &fakeNATSRevoker{}, nil),
			slog.New(slog.NewTextHandler(io.Discard, nil)),
			accounts.WithUsersTracing(p))
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = adapter.Stop() })

		const sentTraceID = "4bf92f3577b34da6a3ce929d0e0e4736"
		m := nats.NewMsg(accounts.UserListSubject)
		m.Header.Set("Traceparent", "00-"+sentTraceID+"-00f067aa0ba902b7-01")
		_, err = nc.RequestMsg(m, 2*time.Second)
		Expect(err).NotTo(HaveOccurred())

		Expect(p.ForceFlush(context.Background())).To(Succeed())
		var found []tracetest.SpanStub
		for _, s := range exp.GetSpans() {
			if s.SpanKind == trace.SpanKindServer && s.Name == accounts.UserListSubject {
				found = append(found, s)
			}
		}
		Expect(found).To(HaveLen(1))
		var account string
		for _, kv := range found[0].Attributes {
			if kv.Key == otelnats.AttrReceivingAccount {
				account = kv.Value.AsString()
			}
		}
		Expect(account).To(Equal("PLATFORM"))
		Expect(found[0].SpanContext.TraceID().String()).NotTo(Equal(sentTraceID))
		Expect(found[0].Links).To(HaveLen(1))
		Consistently(legacy, 200*time.Millisecond).ShouldNot(Receive())
	})

	It("Phase 54d — without WithUsersTracing the request still works, and nothing on obs.trace.*", func() {
		nc := startEmbeddedNATS("accounts-service-platform")
		legacy := obsTraceMsgs(nc)

		adapter, err := accounts.NewUsersAdapter(nc,
			accounts.NewUserClaimsReader(&fakeUserReadStore{}, &fakeAccountLookup{}, nil),
			accounts.NewUserRevoker(&fakeUserReadStore{}, &fakeNATSRevoker{}, nil),
			slog.New(slog.NewTextHandler(io.Discard, nil)))
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = adapter.Stop() })

		_, err = nc.Request(accounts.UserListSubject, nil, 2*time.Second)
		Expect(err).NotTo(HaveOccurred())
		Consistently(legacy, 200*time.Millisecond).ShouldNot(Receive())
	})
})

var _ = Describe("Phase 54b-5: TraceHTTP picks the REST tracing", func() {
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	It("BR-073 — with a Provider, a SERVER span on PLATFORM and nothing on obs.trace.*", func() {
		nc := startEmbeddedNATS("accounts-service")
		exp := tracetest.NewInMemoryExporter()
		p := otelProvider(exp)
		legacy := obsTraceMsgs(nc)

		accounts.TraceHTTP(ok, p).ServeHTTP(httptest.NewRecorder(),
			httptest.NewRequest(http.MethodGet, "/api/accounts", nil))

		Expect(p.ForceFlush(context.Background())).To(Succeed())
		spans := exp.GetSpans()
		Expect(spans).To(HaveLen(1))
		Expect(spans[0].SpanKind).To(Equal(trace.SpanKindServer))
		Expect(spans[0].Name).To(Equal("GET /api/accounts"))
		var account string
		for _, kv := range spans[0].Attributes {
			if kv.Key == otelnats.AttrReceivingAccount {
				account = kv.Value.AsString()
			}
		}
		Expect(account).To(Equal("PLATFORM"))
		Consistently(legacy, 200*time.Millisecond).ShouldNot(Receive())
	})

	It("Phase 54d — with no Provider, the request is served untraced and nothing on obs.trace.*", func() {
		nc := startEmbeddedNATS("accounts-service")
		legacy := obsTraceMsgs(nc)

		rec := httptest.NewRecorder()
		accounts.TraceHTTP(ok, nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/accounts", nil))

		Expect(rec.Code).To(Equal(http.StatusOK))
		Consistently(legacy, 200*time.Millisecond).ShouldNot(Receive())
	})
})
