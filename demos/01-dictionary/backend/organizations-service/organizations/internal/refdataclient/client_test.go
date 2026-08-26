package refdataclient_test

// Wire-level specs for refdataclient.Client. This package had none before
// Phase 28c. Wiring BR-037's traceparent injection here first assumed the
// bare "refdata.item.get.v1" subject was a bug (it looks nothing like
// refdata-service's real registered "rpc.*.refdata.item.get.v1") and
// "fixed" it to construct the full subject directly — which turned out to
// be wrong and worse than the assumed bug: "refdata.item.get.v1" is this
// tenant account's *local* alias, remapped server-side by an account import
// (accounts-service's provisioner.go, jwt.RenamingSubject) to
// "rpc.{tenant}.refdata.item.get.v1" — the account's own identity lands at
// that token, never a value the client supplies. Publishing the full
// subject directly doesn't match the account's LocalSubject (so it doesn't
// route) and would have been a security regression had it routed (a
// business-unit context, not the tenant, ending up in the token the import
// exists specifically to keep caller-uncontrolled). These specs pin the
// correct local-alias subject and the trace-propagation behavior together so
// neither regresses silently again.

import (
	"context"
	"encoding/json"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/internal/refdataclient"
	"github.com/jthomas78/nats-tech-lab/shared/natstrace"
)

func newTestConn() *nats.Conn {
	GinkgoHelper()
	opts := &server.Options{Port: -1}
	srv, err := server.NewServer(opts)
	Expect(err).NotTo(HaveOccurred())
	srv.Start()
	DeferCleanup(srv.Shutdown)
	Expect(srv.ReadyForConnections(10 * time.Second)).To(BeTrue())

	nc, err := nats.Connect(srv.ClientURL(), nats.Name("refdataclient-test"))
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(nc.Close)
	return nc
}

type fullSpan struct {
	CorrelationID string            `json:"correlationId"`
	Subject       string            `json:"subject"`
	TraceID       string            `json:"traceId"`
	SpanID        string            `json:"spanId"`
	ParentSpanID  string            `json:"parentSpanId,omitempty"`
	Service       string            `json:"service"`
	Entity        string            `json:"entity"`
	Action        string            `json:"action"`
	StatusCode    string            `json:"statusCode"`
	StatusMessage string            `json:"statusMessage,omitempty"`
	Attributes    map[string]string `json:"attributes,omitempty"`
}

var _ = Describe("refdataclient.Client (BR-TP14/BR-037)", func() {
	var nc *nats.Conn

	BeforeEach(func() {
		nc = newTestConn()
	})

	Context("BR-TP14 — the rpc.* call rides the tenant account's local alias, not a directly-constructed subject", func() {
		It("requests the local alias refdata.item.get.v1 (the real cross-account subject is a NATS-server-side remap this client never constructs)", func() {
			received := make(chan *nats.Msg, 1)
			sub, err := nc.Subscribe("refdata.item.get.v1", func(m *nats.Msg) {
				received <- m
				_ = m.Respond([]byte(`{"item":{"code":"TAUTLINER","status":"active"}}`))
			})
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { _ = sub.Unsubscribe() })
			Expect(nc.Flush()).To(Succeed())

			client := refdataclient.New(nc)
			ok, err := client.Exists(context.Background(), "acme-north", "TAUTLINER")
			Expect(err).NotTo(HaveOccurred())
			Expect(ok).To(BeTrue())

			var msg *nats.Msg
			Eventually(received).Should(Receive(&msg))
			Expect(msg.Subject).To(Equal("refdata.item.get.v1"))
		})

		It("returns false, no error, when refdata-service reports the code not found", func() {
			sub, err := nc.Subscribe("refdata.item.get.v1", func(m *nats.Msg) {
				_ = m.Respond([]byte(`{"error":"item not found","notFound":true}`))
			})
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { _ = sub.Unsubscribe() })
			Expect(nc.Flush()).To(Succeed())

			client := refdataclient.New(nc)
			ok, err := client.Exists(context.Background(), "acme-north", "NOT-A-TYPE")
			Expect(err).NotTo(HaveOccurred())
			Expect(ok).To(BeFalse())
		})
	})

	Context("BR-037 — trace context propagates on the outbound rpc.* call", func() {
		It("mints a root span (no parentSpanId), labeled from contextKey (not parsed off the local-alias subject), when ctx carries no natstrace span", func() {
			spans := make(chan *nats.Msg, 4)
			spanSub, err := nc.Subscribe("obs.trace.>", func(m *nats.Msg) { spans <- m })
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { _ = spanSub.Unsubscribe() })

			var gotTraceparent string
			sub, err := nc.Subscribe("refdata.item.get.v1", func(m *nats.Msg) {
				gotTraceparent = m.Header.Get(natstrace.TraceparentHeader)
				_ = m.Respond([]byte(`{"item":{"code":"TAUTLINER","status":"active"}}`))
			})
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { _ = sub.Unsubscribe() })
			Expect(nc.Flush()).To(Succeed())

			client := refdataclient.New(nc)
			_, err = client.Exists(context.Background(), "acme-north", "TAUTLINER")
			Expect(err).NotTo(HaveOccurred())

			Expect(gotTraceparent).NotTo(BeEmpty(), "the outbound request must carry a traceparent header")

			var msg *nats.Msg
			Eventually(spans).Should(Receive(&msg))
			Expect(msg.Subject).To(Equal("obs.trace.acme-north.refdata.item.get"))
			var span fullSpan
			Expect(json.Unmarshal(msg.Data, &span)).To(Succeed())
			Expect(span.ParentSpanID).To(BeEmpty(), "no parent span in ctx means this is a root span")
			Expect(span.Service).To(Equal("refdata"))
			Expect(span.Entity).To(Equal("item"))
			Expect(span.Action).To(Equal("get"))
			Expect(span.StatusCode).To(Equal("OK"))
			Expect(span.Attributes["rpc.retry_count"]).To(Equal("0"))
		})

		It("continues the parent span attached via natstrace.ContextWithSpan", func() {
			spans := make(chan *nats.Msg, 4)
			spanSub, err := nc.Subscribe("obs.trace.>", func(m *nats.Msg) { spans <- m })
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { _ = spanSub.Unsubscribe() })

			sub, err := nc.Subscribe("refdata.item.get.v1", func(m *nats.Msg) {
				_ = m.Respond([]byte(`{"item":{"code":"TAUTLINER","status":"active"}}`))
			})
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { _ = sub.Unsubscribe() })
			Expect(nc.Flush()).To(Succeed())

			// Simulate the inbound span a browserrpc handler would have
			// started (natstrace.Start requires a real micro.Request to
			// build one, so a StartOutbound-minted span stands in for it —
			// what matters here is only that it has a traceId/spanId to
			// continue, not which API happened to mint it).
			parentTracer := natstrace.New(nc)
			parentSpan := parentTracer.StartOutbound(nil, "api.acme-north.organizations.fleet-asset.add.v1", nil,
				"acme-north", "organizations", "fleet-asset", "add")

			client := refdataclient.New(nc)
			ctx := natstrace.ContextWithSpan(context.Background(), parentSpan)
			_, err = client.Exists(ctx, "acme-north", "TAUTLINER")
			Expect(err).NotTo(HaveOccurred())

			var msg *nats.Msg
			Eventually(spans).Should(Receive(&msg))
			var span fullSpan
			Expect(json.Unmarshal(msg.Data, &span)).To(Succeed())
			Expect(span.ParentSpanID).NotTo(BeEmpty())
		})
	})

	// BR-055 (Phase 48j) — the regression net at the call site that produced
	// the defect: this client calls sp.End on *any* reply that arrives, and
	// only sp.Fail when the transport itself gave up after retries. A 404 is a
	// reply, so a trace of an organizations -> refdata lookup for a missing
	// item drew this hop blue between two red rows. The fix lives in
	// natstrace.End (so every outbound span agrees at once), but the assertion
	// belongs here too — nothing else pins the behaviour of this client's own
	// End/Fail choice against a refusing responder.
	Context("BR-055 — a refused reply is an ERROR span, not an OK one", func() {
		It("publishes an ERROR span carrying the responder's Nats-Service-Error, though End was called and no retry was needed", func() {
			spans := make(chan *nats.Msg, 4)
			spanSub, err := nc.Subscribe("obs.trace.>", func(m *nats.Msg) { spans <- m })
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { _ = spanSub.Unsubscribe() })

			// micro's own refusal shape: the error rides in the headers with a
			// numeric code beside it, and the reply is otherwise ordinary.
			sub, err := nc.Subscribe("refdata.item.get.v1", func(m *nats.Msg) {
				reply := nats.NewMsg(m.Reply)
				reply.Header.Set(natstrace.ServiceErrorHeader, "dictionary item not found")
				reply.Header.Set(natstrace.ServiceErrorCodeHeader, "404")
				_ = m.RespondMsg(reply)
			})
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { _ = sub.Unsubscribe() })
			Expect(nc.Flush()).To(Succeed())

			client := refdataclient.New(nc)
			_, _ = client.Exists(context.Background(), "globex", "NOT-A-TYPE")

			var msg *nats.Msg
			Eventually(spans).Should(Receive(&msg))
			var span fullSpan
			Expect(json.Unmarshal(msg.Data, &span)).To(Succeed())
			Expect(span.StatusCode).To(Equal("ERROR"), "the hop got a refusal back; a completed request/reply is not by itself a success")
			Expect(span.StatusMessage).To(Equal("dictionary item not found"))
			Expect(span.Attributes["rpc.retry_count"]).To(Equal("0"), "nothing went wrong at the transport level — that is precisely why this used to read OK")
		})
	})

	Context("BR-037 — one span per logical call, not one per retry attempt", func() {
		It("publishes a single ERROR span with rpc.retry_count reflecting every failed attempt, when no responder ever exists", func() {
			spans := make(chan *nats.Msg, 4)
			spanSub, err := nc.Subscribe("obs.trace.>", func(m *nats.Msg) { spans <- m })
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { _ = spanSub.Unsubscribe() })

			// Exists blocks until the whole retry loop (all attempts + the
			// span it publishes) is done, so by the time it returns there is
			// exactly one span already on the wire — not a partial/growing
			// stream of per-attempt spans.
			client := refdataclient.New(nc)
			_, err = client.Exists(context.Background(), "acme-north", "TAUTLINER")
			Expect(err).To(HaveOccurred())

			var msg *nats.Msg
			Eventually(spans, "5s").Should(Receive(&msg))

			var span fullSpan
			Expect(json.Unmarshal(msg.Data, &span)).To(Succeed())
			Expect(span.StatusCode).To(Equal("ERROR"))
			Expect(span.StatusMessage).NotTo(BeEmpty())
			Expect(span.Attributes["rpc.retry_count"]).To(Equal("2"), "defaultRPCRetries=2, so a total failure means 2 retries after the first attempt")

			// Only one span total for this whole call, not one per attempt.
			Consistently(spans, "100ms").Should(HaveLen(0))
		})
	})
})
