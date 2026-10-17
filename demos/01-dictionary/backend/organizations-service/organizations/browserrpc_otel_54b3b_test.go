package organizations_test

// Phase 54b-3b: every api.*.organizations.* endpoint on OTel, not just
// fleet-asset add (54a-4). With a Provider in Deps each endpoint is a SERVER
// span on the tenant's account (BR-069), started as a new root (BR-071), and
// nothing is published on obs.trace.* (Phase 54d retired the legacy tracer; these
// are now the module's api.* span specs). Decision 6's audit: the ctx a
// handler hands the command layer carries that SERVER span, not
// context.Background().

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/internal/application/commands"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/internal/browserrpc"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/internal/domain"
	"github.com/jthomas78/nats-tech-lab/shared/otelnats"
)

// ctxRecordingPartnerRepo records, per repository method, the span on the
// ctx the command layer passed it.
type ctxRecordingPartnerRepo struct {
	*fakePartnerRepo
	mu    sync.Mutex
	spans map[string]trace.SpanContext
}

func newCtxRecordingPartnerRepo() *ctxRecordingPartnerRepo {
	return &ctxRecordingPartnerRepo{fakePartnerRepo: newFakePartnerRepo(), spans: map[string]trace.SpanContext{}}
}

func (r *ctxRecordingPartnerRepo) record(method string, ctx context.Context) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.spans[method] = trace.SpanContextFromContext(ctx)
}

func (r *ctxRecordingPartnerRepo) span(method string) trace.SpanContext {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.spans[method]
}

func (r *ctxRecordingPartnerRepo) Register(ctx context.Context, tp domain.Organization) (domain.Organization, error) {
	r.record("Register", ctx)
	return r.fakePartnerRepo.Register(ctx, tp)
}

func (r *ctxRecordingPartnerRepo) Get(ctx context.Context, id string) (domain.Organization, error) {
	r.record("Get", ctx)
	return r.fakePartnerRepo.Get(ctx, id)
}

func (r *ctxRecordingPartnerRepo) List(ctx context.Context, contextKey string) ([]domain.Organization, error) {
	r.record("List", ctx)
	return r.fakePartnerRepo.List(ctx, contextKey)
}

func (r *ctxRecordingPartnerRepo) Suspend(ctx context.Context, id, reason string) (domain.Organization, error) {
	r.record("Suspend", ctx)
	return r.fakePartnerRepo.Suspend(ctx, id, reason)
}

var _ = Describe("Phase 54b-3b: every api.*.organizations.* endpoint on OTel", func() {
	var (
		nc       *nats.Conn
		exp      *tracetest.InMemoryExporter
		p        *otelnats.Provider
		partners *ctxRecordingPartnerRepo
	)

	BeforeEach(func() {
		nc = newTestNATSConn()
		exp = tracetest.NewInMemoryExporter()
		var err error
		p, err = otelnats.Init(context.Background(), otelnats.Config{ServiceName: "organizations-service", Exporter: exp})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = p.Shutdown(context.Background()) })

		partners = newCtxRecordingPartnerRepo()
		adapter, err := browserrpc.New(nc, browserrpc.Deps{
			Organizations: commands.NewOrganizationHandler(partners, &fakeAudit{}),
			Tenant:        "acme",
			Tracing:       p,
		})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = adapter.Stop() })
	})

	call := func(subject string, in any) []byte {
		GinkgoHelper()
		body, _ := json.Marshal(in)
		reply, err := nc.Request(subject, body, 2*time.Second)
		Expect(err).NotTo(HaveOccurred())
		return reply.Data
	}

	register := func() string {
		GinkgoHelper()
		var out struct {
			ID string `json:"id"`
		}
		Expect(json.Unmarshal(call("api.acme-north.organizations.organization.register.v1",
			map[string]any{"name": "Acme Trucking", "type": "TRANSPORTER"}), &out)).To(Succeed())
		return out.ID
	}

	serverSpan := func(name string) tracetest.SpanStub {
		GinkgoHelper()
		Expect(p.ForceFlush(context.Background())).To(Succeed())
		var found []tracetest.SpanStub
		for _, s := range exp.GetSpans() {
			if s.SpanKind == trace.SpanKindServer && s.Name == name {
				found = append(found, s)
			}
		}
		Expect(found).To(HaveLen(1), "want one SERVER span named %s", name)
		return found[0]
	}

	// Each entry: the endpoint, its request, and the repository method its
	// handler reaches. The spec checks the SERVER span, its account, and that
	// the repository saw that span on ctx.
	DescribeTable("BR-069 / decision 6 — a SERVER span on the tenant's account, and its ctx reaches the command layer",
		func(action, method string, body func(id string) any) {
			id := register()
			subject := "api.acme-north.organizations.organization." + action + ".v1"
			if action != "register" {
				call(subject, body(id))
			}

			s := serverSpan(subject)
			var account string
			for _, kv := range s.Attributes {
				if kv.Key == otelnats.AttrReceivingAccount {
					account = kv.Value.AsString()
				}
			}
			Expect(account).To(Equal("acme"))

			seen := partners.span(method)
			Expect(seen.IsValid()).To(BeTrue(), "the handler passed a ctx with no span")
			Expect(seen.SpanID()).To(Equal(s.SpanContext.SpanID()))
		},
		Entry("organization.register", "register", "Register", nil),
		Entry("organization.list", "list", "List", func(string) any { return map[string]any{} }),
		Entry("organization.get", "get", "Get", func(id string) any { return map[string]any{"id": id} }),
		Entry("organization.suspend", "suspend", "Suspend", func(id string) any { return map[string]any{"id": id, "reason": "audit"} }),
	)

	It("BR-071 — starts every api.* SERVER span as a new root, linking the sent traceparent", func() {
		const sentTraceID = "4bf92f3577b34da6a3ce929d0e0e4736"
		m := nats.NewMsg("api.acme-north.organizations.organization.list.v1")
		m.Data = []byte(`{}`)
		m.Header.Set("Traceparent", "00-"+sentTraceID+"-00f067aa0ba902b7-01")
		_, err := nc.RequestMsg(m, 2*time.Second)
		Expect(err).NotTo(HaveOccurred())

		s := serverSpan("api.acme-north.organizations.organization.list.v1")
		Expect(s.SpanContext.TraceID().String()).NotTo(Equal(sentTraceID))
		Expect(s.Links).To(HaveLen(1))
		Expect(s.Links[0].SpanContext.TraceID().String()).To(Equal(sentTraceID))
	})

	// Moved from browserrpc_roundtrip_test.go's obs.trace.* side-channel
	// context when Phase 54d retired the legacy tracer.
	It("BR-037 — marks a failed call's SERVER span ERROR with the error message", func() {
		m := nats.NewMsg("api.acme-north.organizations.organization.get.v1")
		m.Data = []byte(`{"id":"does-not-exist"}`)
		reply, err := nc.RequestMsg(m, 2*time.Second)
		Expect(err).NotTo(HaveOccurred())
		Expect(reply.Header.Get("Nats-Service-Error-Code")).To(Equal("404"))

		s := serverSpan("api.acme-north.organizations.organization.get.v1")
		Expect(s.Status.Code).To(Equal(codes.Error))
		Expect(s.Status.Description).NotTo(BeEmpty())
	})

	It("publishes nothing on obs.trace.> for any organizations endpoint (the path is OTel only)", func() {
		legacy := make(chan *nats.Msg, 8)
		sub, err := nc.Subscribe("obs.trace.>", func(m *nats.Msg) { legacy <- m })
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = sub.Unsubscribe() })
		Expect(nc.Flush()).To(Succeed())

		id := register()
		call("api.acme-north.organizations.organization.get.v1", map[string]any{"id": id})
		call("api.acme-north.organizations.organization.list.v1", map[string]any{})
		Expect(nc.Flush()).To(Succeed())
		Consistently(legacy, 200*time.Millisecond).ShouldNot(Receive())
	})
})
