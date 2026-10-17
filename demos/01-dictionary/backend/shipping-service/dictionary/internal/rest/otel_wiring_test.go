package rest

// Phase 54b-4: Deps.Tracing reaches every per-tenant resource that
// buildTenantResources builds — the api.* adapter, the evt.* publisher and
// the projectors — with the tenant as the receiving account. The span shapes
// themselves are specified in dictionary/browserrpc_otel_test.go; this file
// proves only the wiring.

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/application/commands"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/domain"
	"github.com/jthomas78/nats-tech-lab/shared/otelnats"
)

type wiringShipRepo struct{}

func (wiringShipRepo) Upsert(_ context.Context, s domain.ShipState) (domain.ShipState, error) {
	return s, nil
}
func (wiringShipRepo) Find(context.Context, string, string) (domain.ShipState, error) {
	return domain.ShipState{}, domain.ErrNotFound
}
func (wiringShipRepo) List(context.Context, string) ([]domain.ShipState, error) { return nil, nil }

type wiringContainerRepo struct{}

func (wiringContainerRepo) Upsert(_ context.Context, s domain.ContainerState) (domain.ContainerState, error) {
	return s, nil
}

type wiringPortRepo struct{}

func (wiringPortRepo) Exists(context.Context, string, string) (bool, error) { return true, nil }
func (wiringPortRepo) Register(context.Context, string, string) error      { return nil }
func (wiringPortRepo) List(context.Context, string) ([]string, error)      { return nil, nil }
func (wiringPortRepo) ListRecords(context.Context, string) ([]domain.PortRecord, error) {
	return nil, nil
}

func TestBuildTenantResourcesPutsTheTenantsTrafficOnOTel(t *testing.T) {
	nc, _, cleanup := newTestNATSJS(t)
	defer cleanup()

	exp := tracetest.NewInMemoryExporter()
	p, err := otelnats.Init(context.Background(), otelnats.Config{ServiceName: "shipping-service", Exporter: exp})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Shutdown(context.Background()) //nolint:errcheck

	ports := wiringPortRepo{}
	res, err := buildTenantResources(context.Background(), nc, "acme", Deps{
		Ports:         commands.NewPortHandler(ports),
		Log:           discardLogger(),
		ShipRepo:      wiringShipRepo{},
		ContainerRepo: wiringContainerRepo{},
		PortRepo:      ports,
		Tracing:       p,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer teardownTenantResources("acme", res, discardLogger()) //nolint:errcheck

	body, _ := json.Marshal(commands.ContainerInput{ContainerID: "TCKU1234567", Cargo: "steel", OriginPort: "Hamburg", DestPort: "Rotterdam"})
	reply, err := nc.Request("api.acme-pacific-fleet.shipping.container.register.v1", body, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if e := reply.Header.Get("Nats-Service-Error"); e != "" {
		t.Fatalf("register failed: %s %s", e, reply.Data)
	}

	kinds := func() map[trace.SpanKind][]tracetest.SpanStub {
		if err := p.ForceFlush(context.Background()); err != nil {
			t.Fatal(err)
		}
		out := map[trace.SpanKind][]tracetest.SpanStub{}
		for _, s := range exp.GetSpans() {
			out[s.SpanKind] = append(out[s.SpanKind], s)
		}
		return out
	}

	// The two container projectors (container, meta) consume asynchronously.
	deadline := time.Now().Add(5 * time.Second)
	for len(kinds()[trace.SpanKindConsumer]) < 2 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}

	got := kinds()
	if n := len(got[trace.SpanKindServer]); n != 1 {
		t.Fatalf("expected one api.* SERVER span (the adapter), got %d", n)
	}
	if len(got[trace.SpanKindProducer]) == 0 {
		t.Fatal("expected PRODUCER spans (the evt.* publisher and notify.*)")
	}
	if n := len(got[trace.SpanKindConsumer]); n != 2 {
		t.Fatalf("expected two CONSUMER spans (the container and meta projectors), got %d", n)
	}
	for _, s := range append(got[trace.SpanKindServer], got[trace.SpanKindConsumer]...) {
		var account string
		for _, kv := range s.Attributes {
			if kv.Key == otelnats.AttrReceivingAccount {
				account = kv.Value.AsString()
			}
		}
		if account != "acme" {
			t.Fatalf("span %q: expected receiving account acme, got %q", s.Name, account)
		}
	}
}

// Phase 54d: with no Provider the tenant is served untraced. The request
// still works, and nothing goes out on the retired obs.trace.* subjects.
func TestBuildTenantResourcesWithoutTracingIsUntraced(t *testing.T) {
	nc, _, cleanup := newTestNATSJS(t)
	defer cleanup()

	spans := make(chan *nats.Msg, 16)
	sub, err := nc.Subscribe("obs.trace.>", func(m *nats.Msg) { spans <- m })
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Unsubscribe() //nolint:errcheck

	ports := wiringPortRepo{}
	res, err := buildTenantResources(context.Background(), nc, "acme", Deps{
		Ports:         commands.NewPortHandler(ports),
		Log:           discardLogger(),
		ShipRepo:      wiringShipRepo{},
		ContainerRepo: wiringContainerRepo{},
		PortRepo:      ports,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer teardownTenantResources("acme", res, discardLogger()) //nolint:errcheck

	if _, err := nc.Request("api.acme-pacific-fleet.shipping.port.list.v1", []byte("{}"), 2*time.Second); err != nil {
		t.Fatal(err)
	}
	select {
	case m := <-spans:
		t.Fatalf("expected no obs.trace.* message with no Provider, got %s", m.Subject)
	case <-time.After(300 * time.Millisecond):
	}
}
