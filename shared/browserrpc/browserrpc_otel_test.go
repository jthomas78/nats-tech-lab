package browserrpc

// Phase 54b: browserrpc on a handler wrapped by otelnats.Middleware. The
// helpers here are the seam every browser adapter replies through, so they
// must hand the handler the OTel span and end it with the right status.

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/nats-io/nats.go/micro"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/jthomas78/nats-tech-lab/shared/otelnats"
)

// serveOTel mounts h behind otelnats.Middleware, calls it once, and returns
// the one SERVER span.
func serveOTel(t *testing.T, h func(svc *micro.Service) micro.HandlerFunc) tracetest.SpanStub {
	t.Helper()
	nc := newTestConn(t)
	exp := tracetest.NewInMemoryExporter()
	p, err := otelnats.Init(context.Background(), otelnats.Config{ServiceName: "shipping-service", Exporter: exp})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Shutdown(context.Background()) })

	const subject = "api.acme.shipping.ship.get.v1"
	var svc micro.Service
	trust := func(micro.Request) otelnats.Trust { return otelnats.Trust{ReceivingAccount: "acme"} }
	svc = newTestService(t, nc, subject, p.Middleware(otelnats.EdgeAPI, trust, h(&svc)))

	if _, err := nc.Request(subject, []byte(`{}`), 5*time.Second); err != nil {
		t.Fatal(err)
	}
	if err := p.ForceFlush(context.Background()); err != nil {
		t.Fatal(err)
	}
	var servers []tracetest.SpanStub
	for _, s := range exp.GetSpans() {
		if s.SpanKind == trace.SpanKindServer {
			servers = append(servers, s)
		}
	}
	if len(servers) != 1 {
		t.Fatalf("want exactly one SERVER span, got %d", len(servers))
	}
	return servers[0]
}

func TestSpanContextCarriesTheOTelSpanOfAWrappedRequest(t *testing.T) {
	var got trace.SpanContext
	server := serveOTel(t, func(svc *micro.Service) micro.HandlerFunc {
		return func(req micro.Request) {
			got = trace.SpanContextFromContext(SpanContext(req))
			Respond(req, *svc, slog.Default(), req.Subject(), "c1", map[string]string{})
		}
	})

	if got.SpanID() != server.SpanContext.SpanID() {
		t.Fatalf("SpanContext span = %s, want the SERVER span %s", got.SpanID(), server.SpanContext.SpanID())
	}
}

func TestRespondOnAWrappedRequestLeavesTheStatusUnset(t *testing.T) {
	server := serveOTel(t, func(svc *micro.Service) micro.HandlerFunc {
		return func(req micro.Request) {
			Respond(req, *svc, slog.Default(), req.Subject(), "c1", map[string]string{"id": "s1"})
		}
	})

	if server.Status.Code != codes.Unset {
		t.Fatalf("status = %v, want Unset", server.Status.Code)
	}
}

func TestRespondErrorOnAWrappedRequestMarksTheSpanError(t *testing.T) {
	server := serveOTel(t, func(svc *micro.Service) micro.HandlerFunc {
		return func(req micro.Request) {
			RespondError(req, *svc, slog.Default(), req.Subject(), "c1", errors.New("ship not found"), true)
		}
	})

	if server.Status.Code != codes.Error || server.Status.Description != "ship not found" {
		t.Fatalf("status = %v %q, want Error \"ship not found\"", server.Status.Code, server.Status.Description)
	}
}
