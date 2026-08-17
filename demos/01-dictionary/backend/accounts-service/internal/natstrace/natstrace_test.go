package natstrace_test

// Contract tests for accounts-service's Phase 28e natstrace copy — same
// BR-036 wire-shape/redaction/truncation coverage the other four services'
// natstrace_test.go pins, plus HTTPMiddleware, this copy's one genuine
// divergence (no micro.Service in this service, so tracing wraps net/http
// instead of nats.go/micro).

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/accounts-service/internal/natstrace"
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

var _ = Describe("natstrace HTTPMiddleware (Phase 28e)", func() {
	var nc *nats.Conn

	BeforeEach(func() {
		nc = newTestConn()
	})

	It("publishes one OK span per request, decodable both as the old envelope shape and the new one, with no parent span when no inbound traceparent is present", func() {
		spans := make(chan *nats.Msg, 4)
		sub, err := nc.Subscribe("obs.trace.>", func(m *nats.Msg) { spans <- m })
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = sub.Unsubscribe() })
		Expect(nc.Flush()).To(Succeed())

		tracer := natstrace.New(nc)
		mux := http.NewServeMux()
		mux.HandleFunc("POST /api/accounts", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusCreated)
		})
		srv := httptest.NewServer(tracer.HTTPMiddleware(mux))
		DeferCleanup(srv.Close)

		resp, err := http.Post(srv.URL+"/api/accounts", "application/json", strings.NewReader(`{}`))
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusCreated))

		var msg *nats.Msg
		Eventually(spans).Should(Receive(&msg))
		Expect(msg.Subject).To(Equal("obs.trace._platform.accounts.accounts.post"))

		var legacy legacyEnvelope
		Expect(json.Unmarshal(msg.Data, &legacy)).To(Succeed(), "an old-shape obsEnvelope consumer must still decode this")

		var span fullSpan
		Expect(json.Unmarshal(msg.Data, &span)).To(Succeed())
		Expect(span.TraceID).NotTo(BeEmpty())
		Expect(span.SpanID).NotTo(BeEmpty())
		Expect(span.ParentSpanID).To(BeEmpty(), "no inbound traceparent means this is a root span")
		Expect(span.Service).To(Equal("accounts"))
		Expect(span.Entity).To(Equal("accounts"))
		Expect(span.Action).To(Equal("post"))
		Expect(span.StatusCode).To(Equal("OK"))
		Expect(span.Attributes["http.status_code"]).To(Equal("201"))
	})

	It("continues an inbound Traceparent header as a child span", func() {
		spans := make(chan *nats.Msg, 4)
		sub, err := nc.Subscribe("obs.trace.>", func(m *nats.Msg) { spans <- m })
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = sub.Unsubscribe() })
		Expect(nc.Flush()).To(Succeed())

		tracer := natstrace.New(nc)
		mux := http.NewServeMux()
		mux.HandleFunc("GET /api/auth/tenants", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
		srv := httptest.NewServer(tracer.HTTPMiddleware(mux))
		DeferCleanup(srv.Close)

		parentTraceID := strings.Repeat("a", 32)
		parentSpanID := strings.Repeat("b", 16)
		req, err := http.NewRequest(http.MethodGet, srv.URL+"/api/auth/tenants", nil)
		Expect(err).NotTo(HaveOccurred())
		req.Header.Set("traceparent", "00-"+parentTraceID+"-"+parentSpanID+"-01")

		resp, err := http.DefaultClient.Do(req)
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusOK))

		var msg *nats.Msg
		Eventually(spans).Should(Receive(&msg))
		var span fullSpan
		Expect(json.Unmarshal(msg.Data, &span)).To(Succeed())
		Expect(span.TraceID).To(Equal(parentTraceID))
		Expect(span.ParentSpanID).To(Equal(parentSpanID))
		Expect(span.SpanID).NotTo(Equal(parentSpanID), "a continued span mints its own child span id")
		Expect(span.Entity).To(Equal("auth"))
	})

	It("marks a >=400 response as an ERROR span", func() {
		spans := make(chan *nats.Msg, 4)
		sub, err := nc.Subscribe("obs.trace.>", func(m *nats.Msg) { spans <- m })
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = sub.Unsubscribe() })
		Expect(nc.Flush()).To(Succeed())

		tracer := natstrace.New(nc)
		mux := http.NewServeMux()
		mux.HandleFunc("GET /api/accounts/does-not-exist", func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "not found", http.StatusNotFound)
		})
		srv := httptest.NewServer(tracer.HTTPMiddleware(mux))
		DeferCleanup(srv.Close)

		resp, err := http.Get(srv.URL + "/api/accounts/does-not-exist")
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusNotFound))

		var msg *nats.Msg
		Eventually(spans).Should(Receive(&msg))
		var span fullSpan
		Expect(json.Unmarshal(msg.Data, &span)).To(Succeed())
		Expect(span.StatusCode).To(Equal("ERROR"))
		Expect(span.Attributes["http.status_code"]).To(Equal("404"))
	})

	It("captures the raw HTTP request body into requestPayload while still letting the handler read the real bytes (Phase 28h)", func() {
		spans := make(chan *nats.Msg, 4)
		sub, err := nc.Subscribe("obs.trace.>", func(m *nats.Msg) { spans <- m })
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = sub.Unsubscribe() })
		Expect(nc.Flush()).To(Succeed())

		tracer := natstrace.New(nc)
		mux := http.NewServeMux()
		var handlerSawBody []byte
		mux.HandleFunc("POST /api/accounts", func(w http.ResponseWriter, r *http.Request) {
			handlerSawBody, _ = io.ReadAll(r.Body)
			w.WriteHeader(http.StatusCreated)
		})
		srv := httptest.NewServer(tracer.HTTPMiddleware(mux))
		DeferCleanup(srv.Close)

		resp, err := http.Post(srv.URL+"/api/accounts", "application/json", strings.NewReader(`{"name":"acme"}`))
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusCreated))
		Expect(handlerSawBody).To(MatchJSON(`{"name":"acme"}`), "buffering the body for the span must not consume it out from under the real handler")

		var msg *nats.Msg
		Eventually(spans).Should(Receive(&msg))
		var span fullSpan
		Expect(json.Unmarshal(msg.Data, &span)).To(Succeed())
		Expect(span.RequestPayload).To(MatchJSON(`{"name":"acme"}`))
	})
})

var _ = Describe("natstrace payload redaction/truncation (BR-036)", func() {
	// HTTPMiddleware now buffers the real HTTP request body into requestPayload
	// (Phase 28h, covered above), so both this copy's payload-bearing paths —
	// HTTPMiddleware and accounts/handler.go's publishAccountEvent (which
	// calls StartOutbound then End/Fail with a real payload) — reach finish().
	// This test exercises finish() mechanics directly via StartFromHeaders,
	// matching the other four services' natstrace_test.go coverage of this
	// BR-036 guarantee, since this copy's finish() is byte-identical to
	// theirs.
	It("redacts a denylisted field before applying the 4 KiB cap, and flags truncation with the pre-truncation length", func() {
		nc := newTestConn()

		spans := make(chan *nats.Msg, 4)
		sub, err := nc.Subscribe("obs.trace.>", func(m *nats.Msg) { spans <- m })
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = sub.Unsubscribe() })
		Expect(nc.Flush()).To(Succeed())

		tracer := natstrace.New(nc)
		sp := tracer.StartFromHeaders(nil, "/api/accounts", nil, "_platform", "accounts", "account", "created")

		big := struct {
			Password string `json:"password"`
			Blob     string `json:"blob"`
		}{Password: "s3cr3t", Blob: strings.Repeat("x", 5000)}
		body, err := json.Marshal(big)
		Expect(err).NotTo(HaveOccurred())
		sp.End(body, nil)

		var msg *nats.Msg
		Eventually(spans).Should(Receive(&msg))
		var span fullSpan
		Expect(json.Unmarshal(msg.Data, &span)).To(Succeed())

		Expect(span.Redacted).To(ContainElement("password"))
		Expect(string(span.Payload)).NotTo(ContainSubstring("s3cr3t"))
		Expect(span.Truncated).To(BeTrue())
		Expect(span.PayloadBytes).To(BeNumerically(">", 4096), "payloadBytes holds the pre-truncation length")

		var truncatedContent string
		Expect(json.Unmarshal(span.Payload, &truncatedContent)).To(Succeed())
		Expect(len(truncatedContent)).To(BeNumerically("<=", 4096))
	})

	It("captures the request payload independently of the reply payload, each with its own redaction/truncation (Phase 28h)", func() {
		nc := newTestConn()

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

		tracer := natstrace.New(nc)
		sp := tracer.StartFromHeaders(nil, "/api/accounts", body, "_platform", "accounts", "account", "created")
		sp.End([]byte(`{"status":"ok"}`), nil)

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

var _ = Describe("natstrace requestor identity (Phase 28h)", func() {
	It("retains the inbound HTTP headers HTTPMiddleware hands over, so a span shows what the caller sent", func() {
		nc := newTestConn()

		spans := make(chan *nats.Msg, 4)
		sub, err := nc.Subscribe("obs.trace.>", func(m *nats.Msg) { spans <- m })
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = sub.Unsubscribe() })
		Expect(nc.Flush()).To(Succeed())

		tracer := natstrace.New(nc)
		mux := http.NewServeMux()
		mux.HandleFunc("GET /api/accounts", func(w http.ResponseWriter, r *http.Request) {})
		srv := httptest.NewServer(tracer.HTTPMiddleware(mux))
		DeferCleanup(srv.Close)

		req, err := http.NewRequest(http.MethodGet, srv.URL+"/api/accounts", nil)
		Expect(err).NotTo(HaveOccurred())
		req.Header.Set("X-Actor", "jeremy")
		resp, err := http.DefaultClient.Do(req)
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusOK))

		var msg *nats.Msg
		Eventually(spans).Should(Receive(&msg))
		var span fullSpan
		Expect(json.Unmarshal(msg.Data, &span)).To(Succeed())
		Expect(span.Headers).To(HaveKeyWithValue("X-Actor", []string{"jeremy"}))
	})

	It("strips a denylisted header before publishing — an HTTP entry point must never leak an Authorization value into a trace", func() {
		nc := newTestConn()

		spans := make(chan *nats.Msg, 4)
		sub, err := nc.Subscribe("obs.trace.>", func(m *nats.Msg) { spans <- m })
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = sub.Unsubscribe() })
		Expect(nc.Flush()).To(Succeed())

		tracer := natstrace.New(nc)
		mux := http.NewServeMux()
		mux.HandleFunc("GET /api/accounts", func(w http.ResponseWriter, r *http.Request) {})
		srv := httptest.NewServer(tracer.HTTPMiddleware(mux))
		DeferCleanup(srv.Close)

		req, err := http.NewRequest(http.MethodGet, srv.URL+"/api/accounts", nil)
		Expect(err).NotTo(HaveOccurred())
		req.Header.Set("Authorization", "Bearer super-secret-jwt")
		resp, err := http.DefaultClient.Do(req)
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusOK))

		var msg *nats.Msg
		Eventually(spans).Should(Receive(&msg))
		Expect(string(msg.Data)).NotTo(ContainSubstring("super-secret-jwt"))
		var span fullSpan
		Expect(json.Unmarshal(msg.Data, &span)).To(Succeed())
		Expect(span.Headers).NotTo(HaveKey("Authorization"))
		Expect(span.Redacted).To(ContainElement("headers.Authorization"))
	})

	It("records an outbound span's own request headers via SetRequestHeaders", func() {
		nc := newTestConn()

		spans := make(chan *nats.Msg, 4)
		sub, err := nc.Subscribe("obs.trace.>", func(m *nats.Msg) { spans <- m })
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = sub.Unsubscribe() })
		Expect(nc.Flush()).To(Succeed())

		sp := natstrace.New(nc).StartOutbound(nil, "notify.accounts.account.created", nil, "_platform", "accounts", "account", "created")
		sp.SetRequestHeaders(map[string][]string{"Nats-Requestor": {"accounts-service/abc"}})
		sp.End([]byte(`{}`), nil)

		var msg *nats.Msg
		Eventually(spans).Should(Receive(&msg))
		var span fullSpan
		Expect(json.Unmarshal(msg.Data, &span)).To(Succeed())
		Expect(span.Headers).To(HaveKeyWithValue("Nats-Requestor", []string{"accounts-service/abc"}))
	})

	// Phase 34.3 (BR-041): traceSpan.Requester lifts the self-declared
	// Nats-Requestor header onto its own field, populated from the same
	// merged headers Headers itself already carries — purely so the Admin
	// UI can read it directly instead of digging through Headers, and
	// never as an authorization signal (BR-041 forbids that outright).
	It("populates Requester from an inbound Nats-Requestor header", func() {
		nc := newTestConn()

		spans := make(chan *nats.Msg, 4)
		sub, err := nc.Subscribe("obs.trace.>", func(m *nats.Msg) { spans <- m })
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = sub.Unsubscribe() })
		Expect(nc.Flush()).To(Succeed())

		tracer := natstrace.New(nc)
		mux := http.NewServeMux()
		mux.HandleFunc("GET /api/accounts", func(w http.ResponseWriter, r *http.Request) {})
		srv := httptest.NewServer(tracer.HTTPMiddleware(mux))
		DeferCleanup(srv.Close)

		req, err := http.NewRequest(http.MethodGet, srv.URL+"/api/accounts", nil)
		Expect(err).NotTo(HaveOccurred())
		req.Header.Set("Nats-Requestor", "browser/session-123")
		resp, err := http.DefaultClient.Do(req)
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusOK))

		var msg *nats.Msg
		Eventually(spans).Should(Receive(&msg))
		var span fullSpan
		Expect(json.Unmarshal(msg.Data, &span)).To(Succeed())
		Expect(span.Requester).To(Equal("browser/session-123"))
	})

	It("leaves Requester empty when no Nats-Requestor header was captured — never a placeholder that could be mistaken for a real identity", func() {
		nc := newTestConn()

		spans := make(chan *nats.Msg, 4)
		sub, err := nc.Subscribe("obs.trace.>", func(m *nats.Msg) { spans <- m })
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = sub.Unsubscribe() })
		Expect(nc.Flush()).To(Succeed())

		tracer := natstrace.New(nc)
		mux := http.NewServeMux()
		mux.HandleFunc("GET /api/accounts", func(w http.ResponseWriter, r *http.Request) {})
		srv := httptest.NewServer(tracer.HTTPMiddleware(mux))
		DeferCleanup(srv.Close)

		resp, err := http.Get(srv.URL + "/api/accounts")
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusOK))

		var msg *nats.Msg
		Eventually(spans).Should(Receive(&msg))
		var span fullSpan
		Expect(json.Unmarshal(msg.Data, &span)).To(Succeed())
		Expect(span.Requester).To(BeEmpty())
	})
})

var _ = Describe("natstrace.StartOutbound / ContextWithSpan / SpanFromContext (Phase 28e)", func() {
	It("mints a child span continuing a parent's traceId, and propagates via ctx", func() {
		nc := newTestConn()
		tracer := natstrace.New(nc)
		parent := tracer.StartOutbound(nil, "notify.accounts.account.created", nil, "_platform", "accounts", "account", "created")

		ctx := natstrace.ContextWithSpan(context.Background(), parent)
		got := natstrace.SpanFromContext(ctx)
		Expect(got.Traceparent()).To(Equal(parent.Traceparent()))
	})

	It("SpanFromContext returns nil when no span was attached", func() {
		Expect(natstrace.SpanFromContext(context.Background())).To(BeNil())
	})
})

var _ = Describe("natstrace span duration (Phase 28g)", func() {
	It("records the span's own measured duration, not derivable from Timestamp alone", func() {
		nc := newTestConn()

		spans := make(chan *nats.Msg, 4)
		sub, err := nc.Subscribe("obs.trace.>", func(m *nats.Msg) { spans <- m })
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = sub.Unsubscribe() })
		Expect(nc.Flush()).To(Succeed())

		tracer := natstrace.New(nc)
		sp := tracer.StartFromHeaders(nil, "/api/accounts", nil, "_platform", "accounts", "account", "created")

		const delay = 40 * time.Millisecond
		time.Sleep(delay)
		sp.End(nil, nil)

		var msg *nats.Msg
		Eventually(spans).Should(Receive(&msg))

		var span fullSpan
		Expect(json.Unmarshal(msg.Data, &span)).To(Succeed())
		Expect(span.DurationMs).To(BeNumerically(">=", delay.Milliseconds()), "duration must reflect real elapsed time, not read as 0")
	})
})
