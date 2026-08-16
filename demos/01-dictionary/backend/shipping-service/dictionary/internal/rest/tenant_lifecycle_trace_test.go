package rest

// BR-037 (Phase 28e): subscribeTenantLifecycle's notify.accounts.* consumers
// continue whatever traceparent accounts-service's publishAccountEvent
// attached, rather than each starting an unrelated root span.

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/internal/natstrace"
)

type fullSpan struct {
	TraceID      string `json:"traceId"`
	SpanID       string `json:"spanId"`
	ParentSpanID string `json:"parentSpanId,omitempty"`
	Service      string `json:"service"`
	Entity       string `json:"entity"`
	Action       string `json:"action"`
	StatusCode   string `json:"statusCode"`
}

func TestSubscribeTenantLifecycleContinuesInboundTraceparent(t *testing.T) {
	opts := &server.Options{Port: -1}
	srv, err := server.NewServer(opts)
	if err != nil {
		t.Fatal(err)
	}
	srv.Start()
	if !srv.ReadyForConnections(10 * time.Second) {
		t.Fatal("nats server not ready")
	}
	defer srv.Shutdown()

	nc, err := nats.Connect(srv.ClientURL(), nats.Name("tenant-lifecycle-trace-test"))
	if err != nil {
		t.Fatal(err)
	}
	defer nc.Close()

	spans := make(chan *nats.Msg, 4)
	spanSub, err := nc.Subscribe("obs.trace.>", func(m *nats.Msg) { spans <- m })
	if err != nil {
		t.Fatal(err)
	}
	defer spanSub.Unsubscribe() //nolint:errcheck

	h := NewHandlers(Deps{CredsDir: t.TempDir(), Log: slog.New(slog.DiscardHandler)})
	if err := h.subscribeTenantLifecycle(context.Background(), nc); err != nil {
		t.Fatal(err)
	}
	if err := nc.Flush(); err != nil {
		t.Fatal(err)
	}

	parentTraceID := strings.Repeat("a", 32)
	parentSpanID := strings.Repeat("b", 16)
	payload, err := json.Marshal(struct {
		Name string `json:"name"`
	}{Name: "ghost-tenant"})
	if err != nil {
		t.Fatal(err)
	}
	msg := &nats.Msg{
		Subject: "notify.accounts.account.created",
		Data:    payload,
		Header:  nats.Header{natstrace.TraceparentHeader: []string{"00-" + parentTraceID + "-" + parentSpanID + "-01"}},
	}
	if err := nc.PublishMsg(msg); err != nil {
		t.Fatal(err)
	}

	select {
	case got := <-spans:
		var span fullSpan
		if err := json.Unmarshal(got.Data, &span); err != nil {
			t.Fatal(err)
		}
		if span.TraceID != parentTraceID {
			t.Fatalf("expected traceId %q, got %q", parentTraceID, span.TraceID)
		}
		if span.ParentSpanID != parentSpanID {
			t.Fatalf("expected parentSpanId %q, got %q", parentSpanID, span.ParentSpanID)
		}
		if span.SpanID == parentSpanID {
			t.Fatal("expected a fresh child span id, not the inbound span id")
		}
		if span.Service != "accounts" || span.Entity != "account" || span.Action != "created" {
			t.Fatalf("expected service/entity/action accounts/account/created, got %s/%s/%s", span.Service, span.Entity, span.Action)
		}
		if span.StatusCode != "OK" {
			t.Fatalf("expected statusCode OK (an unknown tenant name is a silent no-op, not an error), got %s", span.StatusCode)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for obs.trace.* span")
	}
}
