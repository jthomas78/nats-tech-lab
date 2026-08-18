package browserrpc

// Contract tests for shared/browserrpc, extracted in Phase 35 from four
// near-identical per-service copies (see browserrpc.go's package doc).
// These exercise the package over a real embedded NATS server and the
// actual nats.go/micro machinery, since the behavior that matters is what a
// wrapped endpoint actually publishes on the wire — the same integration
// style shared/natstrace's own tests use.

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/micro"
)

func newTestConn(t *testing.T) *nats.Conn {
	t.Helper()
	opts := &server.Options{Port: -1}
	srv, err := server.NewServer(opts)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	srv.Start()
	t.Cleanup(srv.Shutdown)
	if !srv.ReadyForConnections(10 * time.Second) {
		t.Fatal("nats server not ready")
	}

	nc, err := nats.Connect(srv.ClientURL(), nats.Name("browserrpc-test"))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(nc.Close)
	return nc
}

func newTestService(t *testing.T, nc *nats.Conn, subject string, handler micro.HandlerFunc) micro.Service {
	t.Helper()
	svc, err := micro.AddService(nc, micro.Config{Name: "browserrpc-test-svc", Version: "0.0.1"})
	if err != nil {
		t.Fatalf("add service: %v", err)
	}
	t.Cleanup(func() { _ = svc.Stop() })
	if err := svc.AddEndpoint("ep", handler, micro.WithEndpointSubject(subject)); err != nil {
		t.Fatalf("add endpoint: %v", err)
	}
	return svc
}

func TestContextFromSubject(t *testing.T) {
	cases := []struct {
		subject string
		want    string
	}{
		{"api.acme.widget.thing.action.v1", "acme"},
		{"api", ""},
		{"", ""},
		{"api.*.shipping.ship.arrive.v1", "*"},
	}
	for _, c := range cases {
		if got := ContextFromSubject(c.subject); got != c.want {
			t.Errorf("ContextFromSubject(%q) = %q, want %q", c.subject, got, c.want)
		}
	}
}

type echoResponse struct {
	Value string `json:"value"`
}

func TestRespondMarshalsResultAndStampsResponderHeader(t *testing.T) {
	nc := newTestConn(t)
	var svc micro.Service
	handler := func(req micro.Request) {
		Respond(req, svc, nil, req.Subject(), req.Reply(), echoResponse{Value: "ok"})
	}
	svc = newTestService(t, nc, "test.respond", handler)

	reply, err := nc.Request("test.respond", nil, 2*time.Second)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	var got echoResponse
	if err := json.Unmarshal(reply.Data, &got); err != nil {
		t.Fatalf("unmarshal reply: %v", err)
	}
	if got.Value != "ok" {
		t.Fatalf("expected value %q, got %q", "ok", got.Value)
	}
	if h := reply.Header.Get(ResponderHeader); h == "" {
		t.Fatal("expected Nats-Responder header to be set")
	}
}

func TestRespondErrorSetsNotFoundBodyAndErrorCodeHeader(t *testing.T) {
	nc := newTestConn(t)
	var svc micro.Service
	handler := func(req micro.Request) {
		RespondError(req, svc, nil, req.Subject(), req.Reply(), errors.New("no such thing"), true)
	}
	svc = newTestService(t, nc, "test.respond-error", handler)

	reply, err := nc.Request("test.respond-error", nil, 2*time.Second)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	var got ErrorResponse
	if err := json.Unmarshal(reply.Data, &got); err != nil {
		t.Fatalf("unmarshal reply: %v", err)
	}
	if !got.NotFound {
		t.Fatal("expected NotFound to be true")
	}
	if got.Error != "no such thing" {
		t.Fatalf("expected error %q, got %q", "no such thing", got.Error)
	}
	if code := reply.Header.Get(micro.ErrorCodeHeader); code != "404" {
		t.Fatalf("expected error code header 404, got %q", code)
	}
}

func TestRespondErrorDefaultsToServerErrorWhenNotNotFound(t *testing.T) {
	nc := newTestConn(t)
	var svc micro.Service
	handler := func(req micro.Request) {
		RespondError(req, svc, nil, req.Subject(), req.Reply(), errors.New("boom"), false)
	}
	svc = newTestService(t, nc, "test.respond-error-500", handler)

	reply, err := nc.Request("test.respond-error-500", nil, 2*time.Second)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if code := reply.Header.Get(micro.ErrorCodeHeader); code != "500" {
		t.Fatalf("expected error code header 500, got %q", code)
	}
	var got ErrorResponse
	if err := json.Unmarshal(reply.Data, &got); err != nil {
		t.Fatalf("unmarshal reply: %v", err)
	}
	if got.NotFound {
		t.Fatal("expected NotFound to be false")
	}
}

var errNotFound = errors.New("sentinel not found")

func isTestNotFound(err error) bool {
	return errors.Is(err, errNotFound)
}

func TestReplyRespondsWithResultWhenErrNil(t *testing.T) {
	nc := newTestConn(t)
	var svc micro.Service
	handler := func(req micro.Request) {
		Reply(req, svc, nil, isTestNotFound, echoResponse{Value: "ok"}, nil)
	}
	svc = newTestService(t, nc, "test.reply-ok", handler)

	reply, err := nc.Request("test.reply-ok", nil, 2*time.Second)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	var got echoResponse
	if err := json.Unmarshal(reply.Data, &got); err != nil {
		t.Fatalf("unmarshal reply: %v", err)
	}
	if got.Value != "ok" {
		t.Fatalf("expected value %q, got %q", "ok", got.Value)
	}
}

func TestReplyMapsIsNotFoundPredicateToA404(t *testing.T) {
	nc := newTestConn(t)
	var svc micro.Service
	handler := func(req micro.Request) {
		Reply(req, svc, nil, isTestNotFound, nil, errNotFound)
	}
	svc = newTestService(t, nc, "test.reply-404", handler)

	reply, err := nc.Request("test.reply-404", nil, 2*time.Second)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if code := reply.Header.Get(micro.ErrorCodeHeader); code != "404" {
		t.Fatalf("expected error code header 404, got %q", code)
	}
}

func TestReplyDefaultsToA500ForAnUnmappedError(t *testing.T) {
	nc := newTestConn(t)
	var svc micro.Service
	handler := func(req micro.Request) {
		Reply(req, svc, nil, isTestNotFound, nil, errors.New("something else"))
	}
	svc = newTestService(t, nc, "test.reply-500", handler)

	reply, err := nc.Request("test.reply-500", nil, 2*time.Second)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if code := reply.Header.Get(micro.ErrorCodeHeader); code != "500" {
		t.Fatalf("expected error code header 500, got %q", code)
	}
}

func TestReplyTreatsNilIsNotFoundAsNeverNotFound(t *testing.T) {
	nc := newTestConn(t)
	var svc micro.Service
	handler := func(req micro.Request) {
		Reply(req, svc, nil, nil, nil, errors.New("boom"))
	}
	svc = newTestService(t, nc, "test.reply-nil-predicate", handler)

	reply, err := nc.Request("test.reply-nil-predicate", nil, 2*time.Second)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if code := reply.Header.Get(micro.ErrorCodeHeader); code != "500" {
		t.Fatalf("expected error code header 500 with a nil isNotFound predicate, got %q", code)
	}
}

func TestDecodeToleratesEmptyBody(t *testing.T) {
	nc := newTestConn(t)
	type in struct {
		Name string `json:"name"`
	}
	var decoded in
	var decodeErr error
	handler := func(req micro.Request) {
		decoded, decodeErr = Decode[in](req)
		_ = req.Respond(nil)
	}
	newTestService(t, nc, "test.decode-empty", handler)

	if _, err := nc.Request("test.decode-empty", nil, 2*time.Second); err != nil {
		t.Fatalf("request: %v", err)
	}
	if decodeErr != nil {
		t.Fatalf("expected no decode error for an empty body, got %v", decodeErr)
	}
	if decoded.Name != "" {
		t.Fatalf("expected zero value for an empty body, got %q", decoded.Name)
	}
}

func TestDecodeUnmarshalsANonEmptyBody(t *testing.T) {
	nc := newTestConn(t)
	type in struct {
		Name string `json:"name"`
	}
	var decoded in
	var decodeErr error
	handler := func(req micro.Request) {
		decoded, decodeErr = Decode[in](req)
		_ = req.Respond(nil)
	}
	newTestService(t, nc, "test.decode-body", handler)

	body, err := json.Marshal(in{Name: "acme"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if _, err := nc.Request("test.decode-body", body, 2*time.Second); err != nil {
		t.Fatalf("request: %v", err)
	}
	if decodeErr != nil {
		t.Fatalf("expected no decode error, got %v", decodeErr)
	}
	if decoded.Name != "acme" {
		t.Fatalf("expected decoded name %q, got %q", "acme", decoded.Name)
	}
}

func TestDecodeReturnsErrorForMalformedJSON(t *testing.T) {
	nc := newTestConn(t)
	type in struct {
		Name string `json:"name"`
	}
	var decodeErr error
	handler := func(req micro.Request) {
		_, decodeErr = Decode[in](req)
		_ = req.Respond(nil)
	}
	newTestService(t, nc, "test.decode-malformed", handler)

	if _, err := nc.Request("test.decode-malformed", []byte("not json"), 2*time.Second); err != nil {
		t.Fatalf("request: %v", err)
	}
	if decodeErr == nil {
		t.Fatal("expected a decode error for malformed JSON")
	}
}

func TestSpanContextIsNilSafeWithoutTracerMiddleware(t *testing.T) {
	nc := newTestConn(t)
	var panicked bool
	handler := func(req micro.Request) {
		func() {
			defer func() {
				if recover() != nil {
					panicked = true
				}
			}()
			ctx := SpanContext(req)
			if ctx == nil {
				panicked = true
			}
		}()
		_ = req.Respond(nil)
	}
	newTestService(t, nc, "test.span-context", handler)

	if _, err := nc.Request("test.span-context", nil, 2*time.Second); err != nil {
		t.Fatalf("request: %v", err)
	}
	if panicked {
		t.Fatal("SpanContext must be nil-safe when no natstrace.Tracer.Middleware wrapped the handler")
	}
}

func TestResponderIdentityCombinesServiceNameAndInstanceID(t *testing.T) {
	nc := newTestConn(t)
	svc, err := micro.AddService(nc, micro.Config{Name: "identity-svc", Version: "0.0.1"})
	if err != nil {
		t.Fatalf("add service: %v", err)
	}
	t.Cleanup(func() { _ = svc.Stop() })

	got := ResponderIdentity(svc)
	info := svc.Info()
	want := info.Name + "/" + info.ID
	if got != want {
		t.Fatalf("ResponderIdentity() = %q, want %q", got, want)
	}
}
