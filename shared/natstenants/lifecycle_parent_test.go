package natstenants

// Phase 57, decision 6 (regression) and BR-082: a tenant provisioned by a
// traced created delivery gets its lifecycle subscription from that
// delivery's ctx. Its later deliveries take their parent from their own
// traceparent, never from the provisioning span left on that ctx, and are
// never cancelled with the provisioning ctx.

import (
	"context"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/jthomas78/nats-tech-lab/shared/otelnats"
)

const (
	provisionTraceID = "11111111111111111111111111111111"
	provisionSpanID  = "1111111111111111"
	laterTraceID     = "22222222222222222222222222222222"
	laterSpanID      = "2222222222222222"
)

// consumerSpan waits for the one CONSUMER span named subject that arrived on
// tenant's connection, and returns it.
func consumerSpan(t *testing.T, p *otelnats.Provider, exp *tracetest.InMemoryExporter, subject, tenant string) tracetest.SpanStub {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if err := p.ForceFlush(context.Background()); err != nil {
			t.Fatal(err)
		}
		var found []tracetest.SpanStub
		for _, s := range exp.GetSpans() {
			if s.SpanKind != trace.SpanKindConsumer || s.Name != subject {
				continue
			}
			for _, kv := range s.Attributes {
				if kv.Key == otelnats.AttrReceivingAccount && kv.Value.AsString() == tenant {
					found = append(found, s)
				}
			}
		}
		if len(found) == 1 {
			return found[0]
		}
		if len(found) > 1 {
			t.Fatalf("want one %s span on %s's connection, got %d", subject, tenant, len(found))
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("no %s span on %s's connection", subject, tenant)
	return tracetest.SpanStub{}
}

func TestDecision6LaterLifecycleDeliveryOnADynamicTenantTakesItsParentFromTheHeader(t *testing.T) {
	srv := newTestServer(t)
	dir := t.TempDir()
	writeTestCreds(t, dir, "acme")

	exp := tracetest.NewInMemoryExporter()
	p, err := otelnats.Init(context.Background(), otelnats.Config{ServiceName: "refdata-service", Exporter: exp})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Shutdown(context.Background()) }()

	provisioned := make(chan string, 4)
	mgr := NewManager(srv.ClientURL(), dir, "refdata-service", nil,
		func(_ context.Context, _ *nats.Conn, tenant string) (*testResource, error) {
			provisioned <- tenant
			return &testResource{tenant: tenant}, nil
		},
		nil,
		WithTracing(p),
	)
	t.Cleanup(mgr.Close)
	if err := mgr.EnsureAll(context.Background()); err != nil {
		t.Fatalf("EnsureAll: %v", err)
	}
	<-provisioned // acme

	pub, err := nats.Connect(srv.ClientURL(), nats.Name("test"))
	if err != nil {
		t.Fatal(err)
	}
	defer pub.Close()
	send := func(subject, body, traceparent string) {
		t.Helper()
		msg := nats.NewMsg(subject)
		msg.Data = []byte(body)
		if traceparent != "" {
			msg.Header.Set("traceparent", traceparent)
		}
		if err := pub.PublishMsg(msg); err != nil {
			t.Fatal(err)
		}
	}

	// globex is provisioned by a traced created delivery on acme's
	// connection, so its subscription is made from that delivery's ctx.
	writeTestCreds(t, dir, "globex")
	send("notify.accounts.account.created", `{"name":"globex"}`, "00-"+provisionTraceID+"-"+provisionSpanID+"-01")
	select {
	case got := <-provisioned:
		if got != "globex" {
			t.Fatalf("provisioned %q, want globex", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("globex was not provisioned")
	}
	provisioning := consumerSpan(t, p, exp, "notify.accounts.account.created", "acme")
	if provisioning.SpanContext.TraceID().String() != provisionTraceID {
		t.Fatalf("provisioning span trace = %s, want %s", provisioning.SpanContext.TraceID(), provisionTraceID)
	}

	t.Run("with a traceparent, the parent is the header's span", func(t *testing.T) {
		send("notify.accounts.account.reactivated", `{"name":"acme"}`, "00-"+laterTraceID+"-"+laterSpanID+"-01")
		later := consumerSpan(t, p, exp, "notify.accounts.account.reactivated", "globex")
		if later.Parent.SpanID().String() != laterSpanID || later.SpanContext.TraceID().String() != laterTraceID {
			t.Errorf("globex delivery %s under parent %s, want trace %s under %s (not the provisioning span %s)",
				later.SpanContext.TraceID(), later.Parent.SpanID(), laterTraceID, laterSpanID, provisioning.SpanContext.SpanID())
		}
	})

	t.Run("with no traceparent, the span is a new root", func(t *testing.T) {
		exp.Reset()
		send("notify.accounts.account.reactivated", `{"name":"acme"}`, "")
		later := consumerSpan(t, p, exp, "notify.accounts.account.reactivated", "globex")
		if later.Parent.IsValid() {
			t.Errorf("globex delivery has parent %s, want a new root (not the provisioning span %s)",
				later.Parent.SpanID(), provisioning.SpanContext.SpanID())
		}
	})
}

// BR-082: a subscription made from a provisioning ctx keeps
// context.WithoutCancel, so cancelling that ctx never cancels a later
// delivery. The provisioning ctx here holds a span and a value, as ensure's
// does.
func TestBR082LifecycleSubscriptionIsNotCancelledWithItsProvisioningCtx(t *testing.T) {
	srv := newTestServer(t)
	nc, err := nats.Connect(srv.ClientURL(), nats.Name("test"))
	if err != nil {
		t.Fatal(err)
	}
	defer nc.Close()

	p, err := otelnats.Init(context.Background(), otelnats.Config{ServiceName: "refdata-service", Exporter: tracetest.NewInMemoryExporter()})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Shutdown(context.Background()) }()

	type key struct{}
	provisionCtx, cancel := context.WithCancel(context.WithValue(context.Background(), key{}, "provisioning"))
	provisionCtx, span := p.Tracer().Start(provisionCtx, "provisioning")
	defer span.End()

	seen := make(chan context.Context, 1)
	h := LifecycleHandlers{
		Ensure:   func(ctx context.Context, _ string) error { seen <- ctx; return nil },
		Teardown: func(context.Context, string) error { return nil },
	}
	if err := SubscribeLifecycle(provisionCtx, nc, nil, h, WithOTel(p, "globex")); err != nil {
		t.Fatal(err)
	}
	cancel()

	msg := nats.NewMsg("notify.accounts.account.created")
	msg.Data = []byte(`{"name":"globex"}`)
	if err := nc.PublishMsg(msg); err != nil {
		t.Fatal(err)
	}
	select {
	case ctx := <-seen:
		if ctx.Err() != nil || ctx.Done() != nil {
			t.Errorf("delivery ctx is cancellable (err %v), want WithoutCancel kept", ctx.Err())
		}
		if ctx.Value(key{}) != "provisioning" {
			t.Errorf("delivery ctx lost the provisioning ctx's values")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("handler was not called")
	}
}
