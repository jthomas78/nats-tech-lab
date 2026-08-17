package natstrace_test

// Contract tests for the Phase 28b natstrace copy (BR-036/BR-037/BR-P25).
// These exercise the package end to end over a real embedded NATS server and
// the actual nats.go/micro machinery — the same integration style
// browserrpc_roundtrip_test.go uses — rather than calling unexported helpers
// directly, since the behaviour that matters is what a wrapped endpoint
// actually publishes on the wire.

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/micro"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/pricing-service/pricing/internal/natstrace"
)

func newTestConn() *nats.Conn {
	GinkgoHelper()
	opts := &server.Options{Port: -1}
	srv, err := server.NewServer(opts)
	Expect(err).NotTo(HaveOccurred())
	srv.Start()
	DeferCleanup(srv.Shutdown)
	Expect(srv.ReadyForConnections(10 * time.Second)).To(BeTrue())

	nc, err := nats.Connect(srv.ClientURL(), nats.Name("natstrace-test"))
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(nc.Close)
	return nc
}

// legacyEnvelope is the pre-Phase-28 obsEnvelope shape (BR-D26/BR-026):
// decoding a traceSpan into this must still succeed with no error, per BR-036.
type legacyEnvelope struct {
	Direction     string              `json:"direction"`
	CorrelationID string              `json:"correlationId"`
	Subject       string              `json:"subject"`
	Payload       json.RawMessage     `json:"payload,omitempty"`
	Error         string              `json:"error,omitempty"`
	Headers       map[string][]string `json:"headers,omitempty"`
	Timestamp     time.Time           `json:"timestamp"`
	PayloadBytes  int                 `json:"payloadBytes"`
}

type fullSpan struct {
	legacyEnvelope
	Requester     string            `json:"requester,omitempty"`
	TraceID       string            `json:"traceId,omitempty"`
	SpanID        string            `json:"spanId,omitempty"`
	ParentSpanID  string            `json:"parentSpanId,omitempty"`
	Service       string            `json:"service,omitempty"`
	Entity        string            `json:"entity,omitempty"`
	Action        string            `json:"action,omitempty"`
	StatusCode    string            `json:"statusCode,omitempty"`
	StatusMessage string            `json:"statusMessage,omitempty"`
	Attributes    map[string]string `json:"attributes,omitempty"`
	Redacted      []string          `json:"redacted,omitempty"`
	Truncated     bool              `json:"truncated,omitempty"`
	DurationMs    int64             `json:"durationMs"`

	RequestPayload      json.RawMessage `json:"requestPayload,omitempty"`
	RequestPayloadBytes int             `json:"requestPayloadBytes,omitempty"`
	RequestRedacted     []string        `json:"requestRedacted,omitempty"`
	RequestTruncated    bool            `json:"requestTruncated,omitempty"`
}

var _ = Describe("natstrace (Phase 28b — pricing-service copy — BR-036/BR-037/BR-P25)", func() {
	var nc *nats.Conn

	// registerEcho wires one endpoint through tracer.Middleware; the handler
	// finishes the span exactly the way browserrpc.Adapter's respond/
	// respondError do (End on success, Fail on a "fail" request), the real
	// shape this package will be used in once copied to the four adapters.
	registerEcho := func(subject string) {
		tracer := natstrace.New(nc)
		svc, err := micro.AddService(nc, micro.Config{Name: "natstrace-test-svc", Version: "0.0.1"})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = svc.Stop() })

		handler := func(req micro.Request) {
			sp := natstrace.SpanFrom(req)
			if strings.Contains(string(req.Data()), `"fail":true`) {
				failErr := errors.New("boom")
				sp.Fail(failErr, req.Data(), nil)
				_ = req.Respond([]byte(`{"error":"boom"}`))
				return
			}
			sp.End(req.Data(), nil)
			_ = req.Respond(req.Data())
		}
		Expect(svc.AddEndpoint("echo", tracer.Middleware(handler), micro.WithEndpointSubject(subject))).To(Succeed())
	}

	BeforeEach(func() {
		nc = newTestConn()
	})

	Context("BR-036 — traceSpan is a strict superset of obsEnvelope", func() {
		It("publishes one span per call, decodable as both the old envelope shape and the new one", func() {
			registerEcho("api.acme.pricing.thing.action.v1")

			spans := make(chan *nats.Msg, 4)
			sub, err := nc.Subscribe("obs.trace.>", func(m *nats.Msg) { spans <- m })
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { _ = sub.Unsubscribe() })
			Expect(nc.Flush()).To(Succeed())

			_, err = nc.Request("api.acme.pricing.thing.action.v1", []byte(`{"ok":true}`), 2*time.Second)
			Expect(err).NotTo(HaveOccurred())

			var msg *nats.Msg
			Eventually(spans).Should(Receive(&msg))
			Expect(msg.Subject).To(Equal("obs.trace.acme.pricing.thing.action"))

			var legacy legacyEnvelope
			Expect(json.Unmarshal(msg.Data, &legacy)).To(Succeed(), "an old-shape obsEnvelope consumer must still decode this")
			Expect(legacy.Subject).To(Equal("api.acme.pricing.thing.action.v1"))
			Expect(legacy.PayloadBytes).To(BeNumerically(">", 0))

			var span fullSpan
			Expect(json.Unmarshal(msg.Data, &span)).To(Succeed())
			Expect(span.TraceID).To(HaveLen(32))
			Expect(span.SpanID).To(HaveLen(16))
			Expect(span.ParentSpanID).To(BeEmpty())
			Expect(span.Service).To(Equal("pricing"))
			Expect(span.Entity).To(Equal("thing"))
			Expect(span.Action).To(Equal("action"))
			Expect(span.StatusCode).To(Equal("OK"))
		})

		It("records the span's own measured duration (Phase 28g), not derivable from Timestamp alone", func() {
			tracer := natstrace.New(nc)
			svc, err := micro.AddService(nc, micro.Config{Name: "natstrace-test-svc", Version: "0.0.1"})
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { _ = svc.Stop() })

			const handlerDelay = 40 * time.Millisecond
			handler := func(req micro.Request) {
				time.Sleep(handlerDelay)
				natstrace.SpanFrom(req).End(req.Data(), nil)
				_ = req.Respond(req.Data())
			}
			Expect(svc.AddEndpoint("echo", tracer.Middleware(handler), micro.WithEndpointSubject("api.acme.pricing.thing.slow.v1"))).To(Succeed())

			spans := make(chan *nats.Msg, 4)
			sub, err := nc.Subscribe("obs.trace.>", func(m *nats.Msg) { spans <- m })
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { _ = sub.Unsubscribe() })
			Expect(nc.Flush()).To(Succeed())

			_, err = nc.Request("api.acme.pricing.thing.slow.v1", []byte(`{"ok":true}`), 2*time.Second)
			Expect(err).NotTo(HaveOccurred())

			var msg *nats.Msg
			Eventually(spans).Should(Receive(&msg))

			var span fullSpan
			Expect(json.Unmarshal(msg.Data, &span)).To(Succeed())
			Expect(span.DurationMs).To(BeNumerically(">=", handlerDelay.Milliseconds()), "duration must reflect the handler's own elapsed time, not read as 0 or a value shorter than the deliberate delay")
		})

		It("marks a failed call with statusCode ERROR and the error message, never blocking the reply", func() {
			registerEcho("api.acme.pricing.thing.action.v1")

			spans := make(chan *nats.Msg, 4)
			sub, err := nc.Subscribe("obs.trace.>", func(m *nats.Msg) { spans <- m })
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { _ = sub.Unsubscribe() })
			Expect(nc.Flush()).To(Succeed())

			reply, err := nc.Request("api.acme.pricing.thing.action.v1", []byte(`{"fail":true}`), 2*time.Second)
			Expect(err).NotTo(HaveOccurred(), "a span publish failure must never block the real reply")
			Expect(string(reply.Data)).To(ContainSubstring("boom"))

			var msg *nats.Msg
			Eventually(spans).Should(Receive(&msg))
			var span fullSpan
			Expect(json.Unmarshal(msg.Data, &span)).To(Succeed())
			Expect(span.StatusCode).To(Equal("ERROR"))
			Expect(span.StatusMessage).To(Equal("boom"))
		})

		It("redacts a denylisted field before applying the 4 KiB cap, and flags truncation with the pre-truncation length", func() {
			registerEcho("api.acme.pricing.thing.action.v1")

			spans := make(chan *nats.Msg, 4)
			sub, err := nc.Subscribe("obs.trace.>", func(m *nats.Msg) { spans <- m })
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { _ = sub.Unsubscribe() })
			Expect(nc.Flush()).To(Succeed())

			big := struct {
				Password string `json:"password"`
				Blob     string `json:"blob"`
			}{Password: "s3cr3t", Blob: strings.Repeat("x", 5000)}
			body, err := json.Marshal(big)
			Expect(err).NotTo(HaveOccurred())

			_, err = nc.Request("api.acme.pricing.thing.action.v1", body, 2*time.Second)
			Expect(err).NotTo(HaveOccurred())

			var msg *nats.Msg
			Eventually(spans).Should(Receive(&msg))
			var span fullSpan
			Expect(json.Unmarshal(msg.Data, &span)).To(Succeed())

			Expect(span.Redacted).To(ContainElement("password"))
			Expect(string(span.Payload)).NotTo(ContainSubstring("s3cr3t"))
			Expect(span.Truncated).To(BeTrue())
			Expect(span.PayloadBytes).To(BeNumerically(">", 4096), "payloadBytes holds the pre-truncation length")

			// Truncated payload is re-represented as a quoted JSON string of
			// the cut bytes (a raw object/array truncated mid-byte would no
			// longer be valid JSON), so decode it as a string and check its
			// content length rather than the serialized field's byte length.
			var truncatedContent string
			Expect(json.Unmarshal(span.Payload, &truncatedContent)).To(Succeed())
			Expect(len(truncatedContent)).To(BeNumerically("<=", 4096))
		})

		It("captures the request payload independently of the reply payload, each with its own redaction/truncation (Phase 28h)", func() {
			tracer := natstrace.New(nc)
			svc, err := micro.AddService(nc, micro.Config{Name: "natstrace-test-svc", Version: "0.0.1"})
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { _ = svc.Stop() })

			handler := func(req micro.Request) {
				sp := natstrace.SpanFrom(req)
				sp.End([]byte(`{"status":"ok"}`), nil)
				_ = req.Respond([]byte(`{"status":"ok"}`))
			}
			Expect(svc.AddEndpoint("echo", tracer.Middleware(handler), micro.WithEndpointSubject("api.acme.pricing.thing.action.v1"))).To(Succeed())

			spans := make(chan *nats.Msg, 4)
			sub, err := nc.Subscribe("obs.trace.>", func(m *nats.Msg) { spans <- m })
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { _ = sub.Unsubscribe() })
			Expect(nc.Flush()).To(Succeed())

			reqBody := struct {
				Password string `json:"password"`
				Blob     string `json:"blob"`
			}{Password: "s3cr3t", Blob: strings.Repeat("x", 5000)}
			body, err := json.Marshal(reqBody)
			Expect(err).NotTo(HaveOccurred())

			_, err = nc.Request("api.acme.pricing.thing.action.v1", body, 2*time.Second)
			Expect(err).NotTo(HaveOccurred())

			var msg *nats.Msg
			Eventually(spans).Should(Receive(&msg))
			var span fullSpan
			Expect(json.Unmarshal(msg.Data, &span)).To(Succeed())

			Expect(span.RequestRedacted).To(ContainElement("password"))
			Expect(string(span.RequestPayload)).NotTo(ContainSubstring("s3cr3t"))
			Expect(span.RequestTruncated).To(BeTrue())
			Expect(span.RequestPayloadBytes).To(BeNumerically(">", 4096), "requestPayloadBytes holds the pre-truncation length")

			Expect(span.Redacted).To(BeEmpty(), "the reply payload has no password field to redact")
			Expect(span.Truncated).To(BeFalse(), "the reply payload is well under the cap")
			Expect(string(span.Payload)).To(ContainSubstring("ok"))
		})
	})

	Context("BR-037 — trace context propagates and continues a parent span", func() {
		It("continues an inbound traceparent header as a child span rather than minting a new root", func() {
			registerEcho("api.acme.pricing.thing.action.v1")

			spans := make(chan *nats.Msg, 4)
			sub, err := nc.Subscribe("obs.trace.>", func(m *nats.Msg) { spans <- m })
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { _ = sub.Unsubscribe() })
			Expect(nc.Flush()).To(Succeed())

			parentTraceID := strings.Repeat("a", 32)
			parentSpanID := strings.Repeat("b", 16)
			msg := &nats.Msg{
				Subject: "api.acme.pricing.thing.action.v1",
				Reply:   nats.NewInbox(),
				Data:    []byte(`{"ok":true}`),
				Header:  nats.Header{"Traceparent": []string{"00-" + parentTraceID + "-" + parentSpanID + "-01"}},
			}
			replySub, err := nc.SubscribeSync(msg.Reply)
			Expect(err).NotTo(HaveOccurred())
			Expect(nc.PublishMsg(msg)).To(Succeed())
			_, err = replySub.NextMsg(2 * time.Second)
			Expect(err).NotTo(HaveOccurred())

			var received *nats.Msg
			Eventually(spans).Should(Receive(&received))
			var span fullSpan
			Expect(json.Unmarshal(received.Data, &span)).To(Succeed())
			Expect(span.TraceID).To(Equal(parentTraceID))
			Expect(span.ParentSpanID).To(Equal(parentSpanID))
			Expect(span.SpanID).NotTo(Equal(parentSpanID), "a continued span mints its own child span id")
		})
	})

	Context("Phase 28h — every span records who requested it, not just who answered", func() {
		It("retains the inbound headers StartFromHeaders is handed, so an evt.*/notify.* span keeps its requestor", func() {
			spans := make(chan *nats.Msg, 4)
			sub, err := nc.Subscribe("obs.trace.>", func(m *nats.Msg) { spans <- m })
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { _ = sub.Unsubscribe() })
			Expect(nc.Flush()).To(Succeed())

			inbound := nats.Header{"Nats-Requestor": []string{"some-service/abc123"}}
			sp := natstrace.New(nc).StartFromHeaders(inbound, "evt.acme.thing.1.happened", nil, "acme", "thing", "thing", "happened")
			sp.End(nil, nil)

			var msg *nats.Msg
			Eventually(spans).Should(Receive(&msg))
			var span fullSpan
			Expect(json.Unmarshal(msg.Data, &span)).To(Succeed())
			Expect(span.Headers).To(HaveKeyWithValue("Nats-Requestor", []string{"some-service/abc123"}),
				"StartFromHeaders receives the inbound headers and must retain them, not read only traceparent out of them")
		})

		It("records an outbound span's own request headers via SetRequestHeaders, the identity it puts on the wire", func() {
			spans := make(chan *nats.Msg, 4)
			sub, err := nc.Subscribe("obs.trace.>", func(m *nats.Msg) { spans <- m })
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { _ = sub.Unsubscribe() })
			Expect(nc.Flush()).To(Succeed())

			sp := natstrace.New(nc).StartOutbound(nil, "refdata.type.list.v1", nil, "acme", "refdata", "type", "list")
			sp.SetRequestHeaders(map[string][]string{"Nats-Requestor": {"caller-service/xyz789"}})
			sp.End([]byte(`{}`), map[string][]string{"Nats-Responder": {"callee-service/def456"}})

			var msg *nats.Msg
			Eventually(spans).Should(Receive(&msg))
			var span fullSpan
			Expect(json.Unmarshal(msg.Data, &span)).To(Succeed())
			Expect(span.Headers).To(HaveKeyWithValue("Nats-Requestor", []string{"caller-service/xyz789"}))
			Expect(span.Headers).To(HaveKeyWithValue("Nats-Responder", []string{"callee-service/def456"}))
		})

		It("strips a denylisted header name before publishing, recording it as a headers.* redaction", func() {
			spans := make(chan *nats.Msg, 4)
			sub, err := nc.Subscribe("obs.trace.>", func(m *nats.Msg) { spans <- m })
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { _ = sub.Unsubscribe() })
			Expect(nc.Flush()).To(Succeed())

			inbound := nats.Header{
				"Nats-Requestor": []string{"some-service/abc123"},
				"Authorization":  []string{"Bearer super-secret-jwt"},
			}
			sp := natstrace.New(nc).StartFromHeaders(inbound, "evt.acme.thing.1.happened", nil, "acme", "thing", "thing", "happened")
			sp.End(nil, nil)

			var msg *nats.Msg
			Eventually(spans).Should(Receive(&msg))
			Expect(string(msg.Data)).NotTo(ContainSubstring("super-secret-jwt"))
			var span fullSpan
			Expect(json.Unmarshal(msg.Data, &span)).To(Succeed())
			Expect(span.Headers).NotTo(HaveKey("Authorization"))
			Expect(span.Headers).To(HaveKey("Nats-Requestor"), "only the denylisted header is stripped")
			Expect(span.Redacted).To(ContainElement("headers.Authorization"))
		})

		It("lifts an inbound Nats-Requestor header onto span.Requester (BR-041/Phase 34.3)", func() {
			spans := make(chan *nats.Msg, 4)
			sub, err := nc.Subscribe("obs.trace.>", func(m *nats.Msg) { spans <- m })
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { _ = sub.Unsubscribe() })
			Expect(nc.Flush()).To(Succeed())

			inbound := nats.Header{"Nats-Requestor": []string{"some-service/abc123"}}
			sp := natstrace.New(nc).StartFromHeaders(inbound, "evt.acme.thing.1.happened", nil, "acme", "thing", "thing", "happened")
			sp.End(nil, nil)

			var msg *nats.Msg
			Eventually(spans).Should(Receive(&msg))
			var span fullSpan
			Expect(json.Unmarshal(msg.Data, &span)).To(Succeed())
			Expect(span.Requester).To(Equal("some-service/abc123"))
		})

		It("leaves span.Requester empty when no Nats-Requestor header was present (BR-041/Phase 34.3)", func() {
			spans := make(chan *nats.Msg, 4)
			sub, err := nc.Subscribe("obs.trace.>", func(m *nats.Msg) { spans <- m })
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { _ = sub.Unsubscribe() })
			Expect(nc.Flush()).To(Succeed())

			sp := natstrace.New(nc).StartOutbound(nil, "refdata.type.list.v1", nil, "acme", "refdata", "type", "list")
			sp.End([]byte(`{}`), nil)

			var msg *nats.Msg
			Eventually(spans).Should(Receive(&msg))
			var span fullSpan
			Expect(json.Unmarshal(msg.Data, &span)).To(Succeed())
			Expect(span.Requester).To(Equal(""))
		})
	})

	Context("nil-safety — a Span from an unwrapped request never panics", func() {
		It("tolerates End/Fail/SetAttribute/Traceparent on a nil *Span", func() {
			var sp *natstrace.Span
			Expect(func() {
				sp.End([]byte(`{}`), nil)
				sp.Fail(errors.New("x"), []byte(`{}`), nil)
				sp.SetAttribute("k", "v")
				_ = sp.Traceparent()
			}).NotTo(Panic())
			Expect(sp.Traceparent()).To(Equal(""))
		})
	})
})
