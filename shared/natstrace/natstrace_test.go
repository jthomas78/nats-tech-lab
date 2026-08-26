package natstrace_test

// Contract tests for shared/natstrace (BR-036/BR-037), extracted in Phase 35
// from the five near-identical per-service copies (see natstrace.go's package
// doc). These exercise the package end to end over a real embedded NATS
// server and the actual nats.go/micro machinery — the same integration style
// browserrpc's own tests use — rather than calling unexported helpers
// directly, since the behaviour that matters is what a wrapped endpoint
// actually publishes on the wire.

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/micro"

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

var _ = Describe("natstrace (Phase 35 — shared package — BR-036/BR-037)", func() {
	var nc *nats.Conn

	// registerEcho wires one endpoint through tracer.Middleware; the handler
	// finishes the span exactly the way browserrpc.Adapter's respond/
	// respondError do (End on success, Fail on a "fail" request) — the real
	// shape this package is used in once wired into an adapter.
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
			registerEcho("api.acme.widget.thing.action.v1")

			spans := make(chan *nats.Msg, 4)
			sub, err := nc.Subscribe("obs.trace.>", func(m *nats.Msg) { spans <- m })
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { _ = sub.Unsubscribe() })
			Expect(nc.Flush()).To(Succeed())

			_, err = nc.Request("api.acme.widget.thing.action.v1", []byte(`{"ok":true}`), 2*time.Second)
			Expect(err).NotTo(HaveOccurred())

			var msg *nats.Msg
			Eventually(spans).Should(Receive(&msg))
			Expect(msg.Subject).To(Equal("obs.trace.acme.widget.thing.action"))

			var legacy legacyEnvelope
			Expect(json.Unmarshal(msg.Data, &legacy)).To(Succeed(), "an old-shape obsEnvelope consumer must still decode this")
			Expect(legacy.Subject).To(Equal("api.acme.widget.thing.action.v1"))
			Expect(legacy.PayloadBytes).To(BeNumerically(">", 0))

			var span fullSpan
			Expect(json.Unmarshal(msg.Data, &span)).To(Succeed())
			Expect(span.TraceID).To(HaveLen(32))
			Expect(span.SpanID).To(HaveLen(16))
			Expect(span.ParentSpanID).To(BeEmpty())
			Expect(span.Service).To(Equal("widget"))
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
			Expect(svc.AddEndpoint("echo", tracer.Middleware(handler), micro.WithEndpointSubject("api.acme.widget.thing.slow.v1"))).To(Succeed())

			spans := make(chan *nats.Msg, 4)
			sub, err := nc.Subscribe("obs.trace.>", func(m *nats.Msg) { spans <- m })
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { _ = sub.Unsubscribe() })
			Expect(nc.Flush()).To(Succeed())

			_, err = nc.Request("api.acme.widget.thing.slow.v1", []byte(`{"ok":true}`), 2*time.Second)
			Expect(err).NotTo(HaveOccurred())

			var msg *nats.Msg
			Eventually(spans).Should(Receive(&msg))

			var span fullSpan
			Expect(json.Unmarshal(msg.Data, &span)).To(Succeed())
			Expect(span.DurationMs).To(BeNumerically(">=", handlerDelay.Milliseconds()), "duration must reflect the handler's own elapsed time, not read as 0 or a value shorter than the deliberate delay")
		})

		It("marks a failed call with statusCode ERROR and the error message, never blocking the reply", func() {
			registerEcho("api.acme.widget.thing.action.v1")

			spans := make(chan *nats.Msg, 4)
			sub, err := nc.Subscribe("obs.trace.>", func(m *nats.Msg) { spans <- m })
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { _ = sub.Unsubscribe() })
			Expect(nc.Flush()).To(Succeed())

			reply, err := nc.Request("api.acme.widget.thing.action.v1", []byte(`{"fail":true}`), 2*time.Second)
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
			registerEcho("api.acme.widget.thing.action.v1")

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

			_, err = nc.Request("api.acme.widget.thing.action.v1", body, 2*time.Second)
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

		It("redacts actor PII — actorName and actorSourceIP — at any nesting depth (BR-046, Phase 43a)", func() {
			registerEcho("api.acme.widget.actor.action.v1")

			spans := make(chan *nats.Msg, 4)
			sub, err := nc.Subscribe("obs.trace.>", func(m *nats.Msg) { spans <- m })
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { _ = sub.Unsubscribe() })
			Expect(nc.Flush()).To(Succeed())

			// Shaped after organizations-service's transporter-profile event
			// (BR-TP75): actor fields sit beside benign ones, and the snake_case
			// spellings must match too since the denylist is case- and
			// separator-explicit rather than normalizing.
			body, err := json.Marshal(map[string]any{
				"organizationID": "01J0ABCDEF",
				"actorName":      "Jane Wilkinson",
				"actorSourceIP":  "203.0.113.47",
				"nested": map[string]any{
					"actor_name":      "Ravi Chandrasekaran",
					"actor_source_ip": "198.51.100.12",
				},
			})
			Expect(err).NotTo(HaveOccurred())

			_, err = nc.Request("api.acme.widget.actor.action.v1", body, 2*time.Second)
			Expect(err).NotTo(HaveOccurred())

			var msg *nats.Msg
			Eventually(spans).Should(Receive(&msg))
			var span fullSpan
			Expect(json.Unmarshal(msg.Data, &span)).To(Succeed())

			payload := string(span.Payload)
			Expect(payload).NotTo(ContainSubstring("Jane Wilkinson"))
			Expect(payload).NotTo(ContainSubstring("203.0.113.47"))
			Expect(payload).NotTo(ContainSubstring("Ravi Chandrasekaran"))
			Expect(payload).NotTo(ContainSubstring("198.51.100.12"))

			Expect(span.Redacted).To(ContainElement("actorName"))
			Expect(span.Redacted).To(ContainElement("actorSourceIP"))

			// The benign identifier survives — redaction is targeted, not a
			// blanket drop of the payload.
			Expect(payload).To(ContainSubstring("01J0ABCDEF"))
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
			Expect(svc.AddEndpoint("echo", tracer.Middleware(handler), micro.WithEndpointSubject("api.acme.widget.thing.action.v1"))).To(Succeed())

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

			_, err = nc.Request("api.acme.widget.thing.action.v1", body, 2*time.Second)
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
			registerEcho("api.acme.widget.thing.action.v1")

			spans := make(chan *nats.Msg, 4)
			sub, err := nc.Subscribe("obs.trace.>", func(m *nats.Msg) { spans <- m })
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { _ = sub.Unsubscribe() })
			Expect(nc.Flush()).To(Succeed())

			parentTraceID := strings.Repeat("a", 32)
			parentSpanID := strings.Repeat("b", 16)
			msg := &nats.Msg{
				Subject: "api.acme.widget.thing.action.v1",
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

		It("lifts an inbound Nats-Requestor header onto span.Requester (BR-041)", func() {
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

		It("leaves span.Requester empty when no Nats-Requestor header was present (BR-041)", func() {
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

	// BR-055 (Phase 48j) — micro's Nats-Service-Error header *is* the error
	// channel on a reply, so a span holding one when it finishes is not OK,
	// whichever of End/Fail the caller reached for. Before this, a
	// request/reply hop that got a 404 back published statusCode OK — the
	// transport had, after all, succeeded — while the very same span's stored
	// headers rendered red in the Admin UI's detail pane, so one panel
	// disagreed with itself. Fixing it here rather than at either end means
	// every outbound span in the repo agrees at once and the stored KV record
	// is right, instead of being repaired at render time.
	Context("BR-055 — a reply carrying Nats-Service-Error finishes the span as ERROR", func() {
		// subscribeSpans is the same obs.trace.> tap the specs above build
		// inline; the four cases here differ only in how the span finishes.
		subscribeSpans := func() chan *nats.Msg {
			GinkgoHelper()
			spans := make(chan *nats.Msg, 4)
			sub, err := nc.Subscribe("obs.trace.>", func(m *nats.Msg) { spans <- m })
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { _ = sub.Unsubscribe() })
			Expect(nc.Flush()).To(Succeed())
			return spans
		}

		receiveSpan := func(spans chan *nats.Msg) fullSpan {
			GinkgoHelper()
			var msg *nats.Msg
			Eventually(spans).Should(Receive(&msg))
			var span fullSpan
			Expect(json.Unmarshal(msg.Data, &span)).To(Succeed())
			return span
		}

		It("marks an End'd outbound hop ERROR with the header as the status message, though the transport itself succeeded", func() {
			spans := subscribeSpans()

			// The exact shape organizations-service's refdataclient produces:
			// a reply arrived, so it calls End, and the refusal rides in the
			// headers it hands over without ever being inspected.
			sp := natstrace.New(nc).StartOutbound(nil, "rpc.globex.refdata.item.get.v1", []byte(`{"typeKey":"vehicle-type"}`), "globex", "refdata", "item", "get")
			sp.End([]byte(`{"error":"dictionary item not found","notFound":true}`), map[string][]string{
				"Nats-Responder":          {"refdata-service/rdfBdp7UUVgdcir14CkjjR"},
				"Nats-Service-Error":      {"dictionary item not found"},
				"Nats-Service-Error-Code": {"404"},
			})

			span := receiveSpan(spans)
			Expect(span.StatusCode).To(Equal("ERROR"), "a reply carrying Nats-Service-Error is not a successful call, even though the request/reply hop completed")
			Expect(span.StatusMessage).To(Equal("dictionary item not found"), "the header value is the status message — the caller has no error object to supply one")
			Expect(span.Error).To(Equal("dictionary item not found"), "the legacy obsEnvelope error field carries it too, so a pre-Phase-28 consumer sees the failure")
		})

		It("leaves a clean reply OK, including one carrying unrelated headers", func() {
			spans := subscribeSpans()

			sp := natstrace.New(nc).StartOutbound(nil, "rpc.globex.refdata.item.get.v1", []byte(`{"typeKey":"vehicle-type"}`), "globex", "refdata", "item", "get")
			sp.End([]byte(`{"value":"rigid"}`), map[string][]string{
				"Nats-Responder": {"refdata-service/rdfBdp7UUVgdcir14CkjjR"},
			})

			span := receiveSpan(spans)
			Expect(span.StatusCode).To(Equal("OK"))
			Expect(span.StatusMessage).To(BeEmpty())
		})

		It("reads only the reply's headers, so an inbound request that carried the header does not turn its own span red", func() {
			spans := subscribeSpans()

			// A handler span retains the inbound headers (Phase 28h). If those
			// were consulted, a service relaying a failed upstream call's
			// headers would report its own successful work as ERROR.
			inbound := nats.Header{
				natstrace.RequestorHeader: []string{"organizations-service/uB4O77dOFo1YpkRTz2CpS9"},
				"Nats-Service-Error":      {"dictionary item not found"},
			}
			sp := natstrace.New(nc).StartFromHeaders(inbound, "api.globex.organizations.fleet-asset.add.v1", []byte(`{"ok":true}`), "globex", "organizations", "fleet-asset", "add")
			sp.End([]byte(`{"id":"01J..."}`), nil)

			span := receiveSpan(spans)
			Expect(span.StatusCode).To(Equal("OK"), "the header belongs to the request this span received, not to the reply it sent")
		})

		It("keeps Fail's own error as the status message when the reply also carries the header", func() {
			spans := subscribeSpans()

			sp := natstrace.New(nc).StartOutbound(nil, "rpc.globex.refdata.item.get.v1", []byte(`{"typeKey":"vehicle-type"}`), "globex", "refdata", "item", "get")
			sp.Fail(errors.New("refdata rpc unavailable"), nil, map[string][]string{
				"Nats-Service-Error": {"dictionary item not found"},
			})

			span := receiveSpan(spans)
			Expect(span.StatusCode).To(Equal("ERROR"))
			Expect(span.StatusMessage).To(Equal("refdata rpc unavailable"), "an explicit failure the caller diagnosed outranks a header it merely relayed")
		})
	})

	Context("HTTPMiddleware — the REST-transport symmetric counterpart of Middleware", func() {
		It("wraps an http.Handler, publishing an OK span for a 2xx response", func() {
			spans := make(chan *nats.Msg, 4)
			sub, err := nc.Subscribe("obs.trace.>", func(m *nats.Msg) { spans <- m })
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { _ = sub.Unsubscribe() })
			Expect(nc.Flush()).To(Succeed())

			tracer := natstrace.New(nc)
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"ok":true}`))
			})
			ts := httptest.NewServer(tracer.HTTPMiddleware("_platform", "accounts", next))
			DeferCleanup(ts.Close)

			resp, err := http.Get(ts.URL + "/api/accounts/acme/suspend")
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { _ = resp.Body.Close() })
			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			var msg *nats.Msg
			Eventually(spans).Should(Receive(&msg))
			Expect(msg.Subject).To(Equal("obs.trace._platform.accounts.accounts.get"))
			var span fullSpan
			Expect(json.Unmarshal(msg.Data, &span)).To(Succeed())
			Expect(span.StatusCode).To(Equal("OK"))
		})

		It("finishes the span as Fail for a >=400 response, without altering the real reply", func() {
			spans := make(chan *nats.Msg, 4)
			sub, err := nc.Subscribe("obs.trace.>", func(m *nats.Msg) { spans <- m })
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { _ = sub.Unsubscribe() })
			Expect(nc.Flush()).To(Succeed())

			tracer := natstrace.New(nc)
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":"bad"}`))
			})
			ts := httptest.NewServer(tracer.HTTPMiddleware("_platform", "accounts", next))
			DeferCleanup(ts.Close)

			resp, err := http.Get(ts.URL + "/api/auth/connectInfo")
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { _ = resp.Body.Close() })
			Expect(resp.StatusCode).To(Equal(http.StatusBadRequest), "HTTPMiddleware must never alter the real reply")

			var msg *nats.Msg
			Eventually(spans).Should(Receive(&msg))
			var span fullSpan
			Expect(json.Unmarshal(msg.Data, &span)).To(Succeed())
			Expect(span.StatusCode).To(Equal("ERROR"))
			Expect(span.Entity).To(Equal("auth"))
		})

		// BR-AC35: a REST span records the same identity pair an api.*/rpc.*
		// span does — the caller's self-declared Nats-Requestor (lifted onto
		// the envelope's own Requester field by BR-041) and the answering
		// instance's Nats-Responder, derived here from the connection's
		// nats.Name since a REST entry point has no micro.Service to read an
		// identity off.
		It("records the requestor from the inbound HTTP header and the responder from the connection's nats.Name (BR-AC35)", func() {
			spans := make(chan *nats.Msg, 4)
			sub, err := nc.Subscribe("obs.trace.>", func(m *nats.Msg) { spans <- m })
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { _ = sub.Unsubscribe() })
			Expect(nc.Flush()).To(Succeed())

			tracer := natstrace.New(nc)
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
			ts := httptest.NewServer(tracer.HTTPMiddleware("_platform", "accounts", next))
			DeferCleanup(ts.Close)

			req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/accounts", nil)
			Expect(err).NotTo(HaveOccurred())
			req.Header.Set(natstrace.RequestorHeader, "admin-app/deadbeefdeadbeef")
			resp, err := http.DefaultClient.Do(req)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { _ = resp.Body.Close() })

			var msg *nats.Msg
			Eventually(spans).Should(Receive(&msg))
			var span fullSpan
			Expect(json.Unmarshal(msg.Data, &span)).To(Succeed())
			Expect(span.Requester).To(Equal("admin-app/deadbeefdeadbeef"))
			Expect(span.Headers).To(HaveKeyWithValue(natstrace.RequestorHeader, []string{"admin-app/deadbeefdeadbeef"}))
			Expect(span.Headers[natstrace.ResponderHeader]).To(HaveLen(1))
			Expect(span.Headers[natstrace.ResponderHeader][0]).To(HavePrefix("natstrace-test/"),
				"the name half comes from the publishing connection, never a hardcoded service string")
		})

		It("still records the responder on a failed (>=400) REST span (BR-AC35)", func() {
			spans := make(chan *nats.Msg, 4)
			sub, err := nc.Subscribe("obs.trace.>", func(m *nats.Msg) { spans <- m })
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { _ = sub.Unsubscribe() })
			Expect(nc.Flush()).To(Succeed())

			tracer := natstrace.New(nc)
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusConflict) })
			ts := httptest.NewServer(tracer.HTTPMiddleware("_platform", "accounts", next))
			DeferCleanup(ts.Close)

			resp, err := http.Get(ts.URL + "/api/accounts")
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { _ = resp.Body.Close() })

			var msg *nats.Msg
			Eventually(spans).Should(Receive(&msg))
			var span fullSpan
			Expect(json.Unmarshal(msg.Data, &span)).To(Succeed())
			Expect(span.StatusCode).To(Equal("ERROR"))
			Expect(span.Headers[natstrace.ResponderHeader]).To(HaveLen(1),
				"a failed request answered by this instance is exactly when knowing which instance matters")
		})
	})

	Context("ResponderIdentity / ResponderHeaders (BR-AC35)", func() {
		It("is <nats.Name>/<instance> and stable for the process, so two Tracers on one connection agree", func() {
			first := natstrace.New(nc).ResponderHeaders()
			second := natstrace.New(nc).ResponderHeaders()
			Expect(first).To(HaveKey(natstrace.ResponderHeader))
			Expect(first).To(Equal(second),
				"the instance half is per-process, not per-Tracer — shipping's httpTraceMiddleware builds one Tracer per request")
			Expect(first[natstrace.ResponderHeader][0]).To(HavePrefix("natstrace-test/"))
		})

		It("yields no identity — and so no header at all — for a nil or unnamed connection", func() {
			Expect(natstrace.ResponderIdentity(nil)).To(Equal(""))
			Expect(natstrace.New(nil).ResponderHeaders()).To(BeNil(),
				"a half-formed \"/abc123\" identity is worse than none: it reads as a real service named \"\"")
			var nilTracer *natstrace.Tracer
			Expect(nilTracer.ResponderHeaders()).To(BeNil())
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

// parentTraceID/parentSpanID read a span's ids off the traceparent header it
// would attach, since the fields themselves are unexported.
func parentTraceID(sp *natstrace.Span) string {
	GinkgoHelper()
	parts := strings.Split(sp.Traceparent(), "-")
	Expect(parts).To(HaveLen(4))
	return parts[1]
}

func parentSpanID(sp *natstrace.Span) string {
	GinkgoHelper()
	parts := strings.Split(sp.Traceparent(), "-")
	Expect(parts).To(HaveLen(4))
	return parts[2]
}

// ---------------------------------------------------------------------------
// BR-045 / BR-047 (Phase 43a) — obs.pubsub.*, the fire-and-forget sibling of
// obs.trace.*. These cover the emit primitive and its subject derivation; the
// per-service wiring (the evt.* seam, the notify.* call sites) is tested in
// each service alongside its own publisher.
// ---------------------------------------------------------------------------

var _ = Describe("obs.pubsub.* publish observation (Phase 43a, BR-045)", func() {
	var nc *nats.Conn
	var envelopes chan *nats.Msg

	BeforeEach(func() {
		nc = newTestConn()
		envelopes = make(chan *nats.Msg, 8)
		sub, err := nc.Subscribe("obs.pubsub.>", func(m *nats.Msg) { envelopes <- m })
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = sub.Unsubscribe() })
		Expect(nc.Flush()).To(Succeed())
	})

	receive := func() (*nats.Msg, fullSpan) {
		GinkgoHelper()
		var msg *nats.Msg
		Eventually(envelopes).Should(Receive(&msg))
		var env fullSpan
		Expect(json.Unmarshal(msg.Data, &env)).To(Succeed())
		return msg, env
	}

	Context("subject derivation (ObservePublish)", func() {
		It("derives context/service/entity/action from a 6-token evt.* subject, taking action from the last token", func() {
			natstrace.New(nc).ObservePublish(nil, "evt.acme.shipping.ship.SHIP-1.arrived", []byte(`{"shipID":"SHIP-1"}`))

			msg, env := receive()
			Expect(msg.Subject).To(Equal("obs.pubsub.acme.shipping.ship.arrived"))
			Expect(env.Subject).To(Equal("evt.acme.shipping.ship.SHIP-1.arrived"),
				"the envelope records the real published subject, ids included")
		})

		It("derives from a 5-token notify.* subject", func() {
			natstrace.New(nc).ObservePublish(nil, "notify.acme.shipping.ship.changed", []byte(`{}`))

			msg, _ := receive()
			Expect(msg.Subject).To(Equal("obs.pubsub.acme.shipping.ship.changed"))
		})

		It("derives from a 6-token notify.*.raw.* subject", func() {
			natstrace.New(nc).ObservePublish(nil, "notify.acme.shipping.raw.container.loaded", []byte(`{}`))

			msg, _ := receive()
			Expect(msg.Subject).To(Equal("obs.pubsub.acme.shipping.raw.loaded"))
		})

		It("marks direction as publish — there is no reply half to pair", func() {
			natstrace.New(nc).ObservePublish(nil, "evt.acme.shipping.ship.SHIP-1.arrived", []byte(`{}`))

			_, env := receive()
			Expect(env.Direction).To(Equal("publish"))
		})
	})

	Context("trace continuation (BR-045)", func() {
		It("continues the causing span's trace rather than minting an unrelated one", func() {
			parent := natstrace.New(nc).StartOutbound(nil, "api.acme.shipping.ship.arrive.v1", []byte(`{}`), "acme", "shipping", "ship", "arrive")

			natstrace.New(nc).ObservePublish(parent, "evt.acme.shipping.ship.SHIP-1.arrived", []byte(`{}`))

			_, env := receive()
			Expect(env.TraceID).To(Equal(parentTraceID(parent)))
			Expect(env.ParentSpanID).To(Equal(parentSpanID(parent)))
			Expect(env.SpanID).NotTo(BeEmpty())
			Expect(env.SpanID).NotTo(Equal(parentSpanID(parent)), "the observation is its own span")
		})

		It("mints a root trace when no span was reachable at the call site", func() {
			natstrace.New(nc).ObservePublish(nil, "evt.acme.shipping.ship.SHIP-1.arrived", []byte(`{}`))

			_, env := receive()
			Expect(env.TraceID).NotTo(BeEmpty())
			Expect(env.ParentSpanID).To(BeEmpty())
		})
	})

	Context("dedup and redaction (BR-045/BR-046/BR-047)", func() {
		It("sets Nats-Msg-Id to the envelope's spanId, which is what makes BR-047's dedup enforceable", func() {
			natstrace.New(nc).ObservePublish(nil, "evt.acme.shipping.ship.SHIP-1.arrived", []byte(`{}`))

			msg, env := receive()
			Expect(msg.Header.Get(nats.MsgIdHdr)).To(Equal(env.SpanID))
			Expect(env.SpanID).NotTo(BeEmpty())
		})

		It("redacts denylisted fields before the 4 KiB cap, exactly as obs.trace.* does", func() {
			body, err := json.Marshal(map[string]any{
				"shipID":   "SHIP-1",
				"password": "s3cr3t",
				"blob":     strings.Repeat("x", 5000),
			})
			Expect(err).NotTo(HaveOccurred())

			natstrace.New(nc).ObservePublish(nil, "evt.acme.shipping.ship.SHIP-1.arrived", body)

			_, env := receive()
			Expect(env.Redacted).To(ContainElement("password"))
			Expect(string(env.Payload)).NotTo(ContainSubstring("s3cr3t"))
			Expect(env.Truncated).To(BeTrue())
			Expect(env.PayloadBytes).To(BeNumerically(">", 4096), "payloadBytes holds the pre-truncation length")
		})
	})

	Context("self-observation guard (BR-045)", func() {
		It("never observes a publish that is itself on an obs.* subject", func() {
			natstrace.New(nc).ObservePublish(nil, "obs.pubsub.acme.shipping.ship.arrived", []byte(`{}`))
			natstrace.New(nc).ObservePublish(nil, "obs.trace.acme.shipping.ship.arrive", []byte(`{}`))

			Expect(nc.Flush()).To(Succeed())
			Consistently(envelopes).ShouldNot(Receive())
		})

		It("ignores a subject too short to carry the four tokens rather than publishing a malformed obs subject", func() {
			natstrace.New(nc).ObservePublish(nil, "evt.acme.shipping", []byte(`{}`))

			Expect(nc.Flush()).To(Succeed())
			Consistently(envelopes).ShouldNot(Receive())
		})
	})

	Context("the explicit primitive (ObservePublishAs)", func() {
		It("takes the four tokens as given, for subject families whose arity the deriver cannot read", func() {
			// notify.accounts.account.created carries no {context} token
			// (CLAUDE.md: accounts-service subjects administer the tenant
			// axis itself), so deriving by position would read "accounts"
			// as the context.
			natstrace.New(nc).ObservePublishAs(nil, "notify.accounts.account.created", []byte(`{"name":"acme"}`),
				"_platform", "accounts", "account", "created")

			msg, env := receive()
			Expect(msg.Subject).To(Equal("obs.pubsub._platform.accounts.account.created"))
			Expect(env.Subject).To(Equal("notify.accounts.account.created"))
		})
	})
})
