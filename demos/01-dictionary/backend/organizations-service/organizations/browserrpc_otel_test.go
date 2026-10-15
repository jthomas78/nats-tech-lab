package organizations_test

// Phase 54a-4: with an otelnats Provider in Deps, the fleet-asset add
// endpoint is traced with OTel instead of natstrace. Every other endpoint
// keeps natstrace until 54b. Without a Provider nothing changes — the
// natstrace specs in browserrpc_roundtrip_test.go cover that path.

import (
	"context"
	"encoding/json"
	"time"

	"github.com/nats-io/nats.go"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/internal/browserrpc"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/internal/application/commands"
	"github.com/jthomas78/nats-tech-lab/shared/otelnats"
)

// spanningValidator stands in for refdataclient: it starts the CLIENT span
// the real client would, from the ctx the command layer hands it, so the
// spec proves the handler's ctx reaches the outbound call.
type spanningValidator struct{ p *otelnats.Provider }

func (v spanningValidator) Exists(ctx context.Context, _, _, _ string) (bool, error) {
	_, span := v.p.StartClient(ctx, "refdata.item.get.v1", "refdata")
	span.End()
	return true, nil
}

var _ = Describe("fleet-asset add with an otelnats Provider (Phase 54a-4)", func() {
	var (
		nc  *nats.Conn
		exp *tracetest.InMemoryExporter
		p   *otelnats.Provider
	)

	BeforeEach(func() {
		nc = newTestNATSConn()
		exp = tracetest.NewInMemoryExporter()
		var err error
		p, err = otelnats.Init(context.Background(), otelnats.Config{ServiceName: "organizations-service", Exporter: exp})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = p.Shutdown(context.Background()) })

		partners := newFakePartnerRepo()
		adapter, err := browserrpc.New(nc, browserrpc.Deps{
			Organizations: commands.NewOrganizationHandler(partners, &fakeAudit{}),
			FleetAssets:   commands.NewFleetAssetHandler(partners, newFakeFleetRepo(), spanningValidator{p}),
			Tenant:        "acme",
			Tracing:       p,
		})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = adapter.Stop() })
	})

	request := func(m *nats.Msg) {
		GinkgoHelper()
		_, err := nc.RequestMsg(m, 2*time.Second)
		Expect(err).NotTo(HaveOccurred())
	}

	addFleetAsset := func(traceparent string) {
		GinkgoHelper()
		body, _ := json.Marshal(map[string]any{"name": "Acme Trucking", "type": "TRANSPORTER"})
		reply, err := nc.Request("api.acme-north.organizations.organization.register.v1", body, 2*time.Second)
		Expect(err).NotTo(HaveOccurred())
		var registered struct {
			ID string `json:"id"`
		}
		Expect(json.Unmarshal(reply.Data, &registered)).To(Succeed())

		body, _ = json.Marshal(map[string]any{
			"id": registered.ID, "registrationNo": "ABC123", "vin": "VIN1",
			"make": "Volvo", "model": "FH16", "vehicleTypeCode": "TAUTLINER",
		})
		m := nats.NewMsg("api.acme-north.organizations.fleet-asset.add.v1")
		m.Data = body
		if traceparent != "" {
			m.Header.Set("Traceparent", traceparent)
		}
		request(m)
	}

	spans := func() tracetest.SpanStubs {
		GinkgoHelper()
		Expect(p.ForceFlush(context.Background())).To(Succeed())
		return exp.GetSpans()
	}

	Context("BR-064 — the api.* SERVER span is the parent of the outbound CLIENT span", func() {
		It("makes one SERVER and one CLIENT span in one trace", func() {
			addFleetAsset("")

			var server, client []tracetest.SpanStub
			for _, s := range spans() {
				switch s.SpanKind {
				case trace.SpanKindServer:
					server = append(server, s)
				case trace.SpanKindClient:
					client = append(client, s)
				}
			}
			Expect(server).To(HaveLen(1), "only fleet-asset add is on OTel; organization.register stays on natstrace")
			Expect(server[0].Name).To(Equal("api.acme-north.organizations.fleet-asset.add.v1"))
			Expect(client).To(HaveLen(1))
			Expect(client[0].SpanContext.TraceID()).To(Equal(server[0].SpanContext.TraceID()))
			Expect(client[0].Parent.SpanID()).To(Equal(server[0].SpanContext.SpanID()))
		})
	})

	Context("BR-069 — the receiving account is the adapter's tenant", func() {
		It("sets lab.tenant.receiving_account from Deps.Tenant, and no originating tenant", func() {
			addFleetAsset("")

			for _, s := range spans() {
				if s.SpanKind != trace.SpanKindServer {
					continue
				}
				var account string
				var hasOrigin bool
				for _, kv := range s.Attributes {
					switch kv.Key {
					case otelnats.AttrReceivingAccount:
						account = kv.Value.AsString()
					case otelnats.AttrOriginatingTenant:
						hasOrigin = true
					}
				}
				Expect(account).To(Equal("acme"))
				Expect(hasOrigin).To(BeFalse())
			}
		})
	})

	Context("BR-071 — a browser traceparent is a link, never a parent", func() {
		It("starts a new trace and keeps the sent context as one link", func() {
			const sentTraceID = "4bf92f3577b34da6a3ce929d0e0e4736"
			addFleetAsset("00-" + sentTraceID + "-00f067aa0ba902b7-00")

			for _, s := range spans() {
				if s.SpanKind != trace.SpanKindServer {
					continue
				}
				Expect(s.SpanContext.TraceID().String()).NotTo(Equal(sentTraceID))
				Expect(s.SpanContext.IsSampled()).To(BeTrue())
				Expect(s.Links).To(HaveLen(1))
				Expect(s.Links[0].SpanContext.TraceID().String()).To(Equal(sentTraceID))
			}
		})
	})

	It("publishes no natstrace span for fleet-asset add (the path is OTel only)", func() {
		legacy := make(chan *nats.Msg, 8)
		sub, err := nc.Subscribe("obs.trace.*.organizations.fleet-asset.>", func(m *nats.Msg) { legacy <- m })
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = sub.Unsubscribe() })
		Expect(nc.Flush()).To(Succeed())

		addFleetAsset("")
		Expect(nc.Flush()).To(Succeed())
		Consistently(legacy, 200*time.Millisecond).ShouldNot(Receive())
	})
})
