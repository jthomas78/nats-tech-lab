package pricing_test

// Phase 54b-5: every api.*.pricing.* endpoint on OTel. With a Provider in
// Deps each endpoint is a SERVER span on the tenant's account (BR-069),
// started as a new root (BR-071), and no natstrace span is published.
// Decision 6's audit: the ctx a handler hands the command layer carries that
// SERVER span, not context.Background(). Without a Provider the adapter
// stays on natstrace (decision 16: the default stack keeps it).

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/pricing-service/pricing/internal/application/commands"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/pricing-service/pricing/internal/browserrpc"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/pricing-service/pricing/internal/domain"
	"github.com/jthomas78/nats-tech-lab/shared/otelnats"
)

// spanRecorder keeps the span on the ctx of the last repository call.
type spanRecorder struct {
	mu   sync.Mutex
	last trace.SpanContext
}

func (r *spanRecorder) record(ctx context.Context) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.last = trace.SpanContextFromContext(ctx)
}

func (r *spanRecorder) seen() trace.SpanContext {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.last
}

// The three fakes record ctx and return zero values. A handler that turns a
// zero value into an error still reached the repository, which is all the
// spec checks.
type recFeeScales struct{ *spanRecorder }

func (r recFeeScales) Register(ctx context.Context, _ domain.FeeScale) error { r.record(ctx); return nil }
func (r recFeeScales) Get(ctx context.Context, _, _ string) (domain.FeeScale, error) {
	r.record(ctx)
	return domain.FeeScale{}, nil
}
func (r recFeeScales) List(ctx context.Context, _ string) ([]domain.FeeScale, error) {
	r.record(ctx)
	return nil, nil
}
func (r recFeeScales) CreateDraft(ctx context.Context, _, _ string) (domain.FeeScaleVersion, error) {
	r.record(ctx)
	return domain.FeeScaleVersion{}, nil
}
func (r recFeeScales) AddRange(ctx context.Context, _, _ string, _ int, _ domain.FeeScaleRange) error {
	r.record(ctx)
	return nil
}
func (r recFeeScales) Publish(ctx context.Context, _, _ string) (domain.FeeScaleVersion, error) {
	r.record(ctx)
	return domain.FeeScaleVersion{}, nil
}
func (r recFeeScales) Rollback(ctx context.Context, _, _ string, _ int) (domain.FeeScaleVersion, error) {
	r.record(ctx)
	return domain.FeeScaleVersion{}, nil
}
func (r recFeeScales) Versions(ctx context.Context, _, _ string) ([]domain.FeeScaleVersion, error) {
	r.record(ctx)
	return nil, nil
}
func (r recFeeScales) ActiveVersion(ctx context.Context, _, _ string) (domain.FeeScaleVersion, error) {
	r.record(ctx)
	return domain.FeeScaleVersion{}, nil
}

type recRateSheets struct{ *spanRecorder }

func (r recRateSheets) Register(ctx context.Context, _ domain.RateSheet) error { r.record(ctx); return nil }
func (r recRateSheets) Get(ctx context.Context, _, _ string) (domain.RateSheet, error) {
	r.record(ctx)
	return domain.RateSheet{}, nil
}
func (r recRateSheets) List(ctx context.Context, _ string) ([]domain.RateSheet, error) {
	r.record(ctx)
	return nil, nil
}
func (r recRateSheets) CreateDraft(ctx context.Context, _, _ string) (domain.RateSheetVersion, error) {
	r.record(ctx)
	return domain.RateSheetVersion{}, nil
}
func (r recRateSheets) AddEntry(ctx context.Context, _, _ string, _ int, _ domain.RateSheetEntry) error {
	r.record(ctx)
	return nil
}
func (r recRateSheets) SetFeeScaleOverride(ctx context.Context, _, _ string, _ int, _ string) error {
	r.record(ctx)
	return nil
}
func (r recRateSheets) Publish(ctx context.Context, _, _ string) (domain.RateSheetVersion, error) {
	r.record(ctx)
	return domain.RateSheetVersion{}, nil
}
func (r recRateSheets) Rollback(ctx context.Context, _, _ string, _ int) (domain.RateSheetVersion, error) {
	r.record(ctx)
	return domain.RateSheetVersion{}, nil
}
func (r recRateSheets) Versions(ctx context.Context, _, _ string) ([]domain.RateSheetVersion, error) {
	r.record(ctx)
	return nil, nil
}
func (r recRateSheets) ActiveVersion(ctx context.Context, _, _ string) (domain.RateSheetVersion, error) {
	r.record(ctx)
	return domain.RateSheetVersion{}, nil
}
func (r recRateSheets) IndexDieselPrice(ctx context.Context, _ string, _ domain.DieselPrice) error {
	r.record(ctx)
	return nil
}
func (r recRateSheets) ListDieselPrices(ctx context.Context, _ string) ([]domain.DieselPrice, error) {
	r.record(ctx)
	return nil, nil
}
func (r recRateSheets) PersistDieselOverlay(ctx context.Context, _, _ string, _ domain.RateSheetVersion) error {
	r.record(ctx)
	return nil
}

type recFixedRates struct{ *spanRecorder }

func (r recFixedRates) Register(ctx context.Context, _ domain.FixedRate) error { r.record(ctx); return nil }
func (r recFixedRates) Get(ctx context.Context, _, _ string) (domain.FixedRate, error) {
	r.record(ctx)
	return domain.FixedRate{}, nil
}
func (r recFixedRates) List(ctx context.Context, _ string) ([]domain.FixedRate, error) {
	r.record(ctx)
	return nil, nil
}
func (r recFixedRates) CreateDraft(ctx context.Context, _, _ string, _ int64, _ int, _ int64) (domain.FixedRateVersion, error) {
	r.record(ctx)
	return domain.FixedRateVersion{}, nil
}
func (r recFixedRates) Publish(ctx context.Context, _, _ string) (domain.FixedRateVersion, error) {
	r.record(ctx)
	return domain.FixedRateVersion{}, nil
}
func (r recFixedRates) Rollback(ctx context.Context, _, _ string, _ int) (domain.FixedRateVersion, error) {
	r.record(ctx)
	return domain.FixedRateVersion{}, nil
}
func (r recFixedRates) Versions(ctx context.Context, _, _ string) ([]domain.FixedRateVersion, error) {
	r.record(ctx)
	return nil, nil
}
func (r recFixedRates) ActiveVersion(ctx context.Context, _, _ string) (domain.FixedRateVersion, error) {
	r.record(ctx)
	return domain.FixedRateVersion{}, nil
}

// startNATS runs an embedded NATS server for one spec.
func startNATS() *nats.Conn {
	GinkgoHelper()
	srv, err := server.NewServer(&server.Options{Port: -1})
	Expect(err).NotTo(HaveOccurred())
	srv.Start()
	Expect(srv.ReadyForConnections(10 * time.Second)).To(BeTrue())
	nc, err := nats.Connect(srv.ClientURL(), nats.Name("pricing-service"))
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(func() { nc.Close(); srv.Shutdown() })
	return nc
}

// startAdapter starts one tenant's adapter over recording fakes.
func startAdapter(nc *nats.Conn, rec *spanRecorder, tracing *otelnats.Provider) {
	GinkgoHelper()
	adapter, err := browserrpc.New(nc, browserrpc.Deps{
		FeeScales:  commands.NewFeeScaleHandler(recFeeScales{rec}),
		RateSheets: commands.NewRateSheetHandler(recRateSheets{rec}),
		FixedRates: commands.NewFixedRateHandler(recFixedRates{rec}),
		Tenant:     "acme",
		Tracing:    tracing,
	})
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(func() { _ = adapter.Stop() })
}

var _ = Describe("Phase 54b-5: every api.*.pricing.* endpoint on OTel", func() {
	var (
		nc  *nats.Conn
		exp *tracetest.InMemoryExporter
		p   *otelnats.Provider
		rec *spanRecorder
	)

	BeforeEach(func() {
		nc = startNATS()
		exp = tracetest.NewInMemoryExporter()
		var err error
		p, err = otelnats.Init(context.Background(), otelnats.Config{ServiceName: "pricing-service", Exporter: exp})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = p.Shutdown(context.Background()) })
		rec = &spanRecorder{}
		startAdapter(nc, rec, p)
	})

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

	DescribeTable("BR-069 / decision 6 — a SERVER span on the tenant's account, and its ctx reaches the repository",
		func(action string, in any) {
			subject := "api.acme-north.pricing." + action + ".v1"
			body, _ := json.Marshal(in)
			_, err := nc.Request(subject, body, 2*time.Second)
			Expect(err).NotTo(HaveOccurred())

			s := serverSpan(subject)
			var account string
			for _, kv := range s.Attributes {
				if kv.Key == otelnats.AttrReceivingAccount {
					account = kv.Value.AsString()
				}
			}
			Expect(account).To(Equal("acme"))

			seen := rec.seen()
			Expect(seen.IsValid()).To(BeTrue(), "the handler passed a ctx with no span")
			Expect(seen.SpanID()).To(Equal(s.SpanContext.SpanID()))
		},
		Entry(nil, "fee-scale.register", map[string]any{"name": "std"}),
		Entry(nil, "fee-scale.list", map[string]any{}),
		Entry(nil, "fee-scale.get", map[string]any{"name": "std"}),
		Entry(nil, "fee-scale.create-draft", map[string]any{"name": "std"}),
		Entry(nil, "fee-scale.add-range", map[string]any{"name": "std", "version": 1}),
		Entry(nil, "fee-scale.publish", map[string]any{"name": "std"}),
		Entry(nil, "fee-scale.rollback", map[string]any{"name": "std", "version": 1}),
		Entry(nil, "fee-scale.versions", map[string]any{"name": "std"}),
		Entry(nil, "fee-scale.active", map[string]any{"name": "std"}),
		Entry(nil, "fee-scale.calculate-fee", map[string]any{"name": "std", "centBid": 100}),
		Entry(nil, "rate-sheet.register", map[string]any{"name": "rs"}),
		Entry(nil, "rate-sheet.list", map[string]any{}),
		Entry(nil, "rate-sheet.get", map[string]any{"name": "rs"}),
		Entry(nil, "rate-sheet.create-draft", map[string]any{"name": "rs"}),
		Entry(nil, "rate-sheet.add-entry", map[string]any{"name": "rs", "version": 1}),
		Entry(nil, "rate-sheet.set-fee-scale-override", map[string]any{"name": "rs", "version": 1, "feeScaleName": "std"}),
		Entry(nil, "rate-sheet.publish", map[string]any{"name": "rs"}),
		Entry(nil, "rate-sheet.rollback", map[string]any{"name": "rs", "version": 1}),
		Entry(nil, "rate-sheet.versions", map[string]any{"name": "rs"}),
		Entry(nil, "rate-sheet.active", map[string]any{"name": "rs"}),
		Entry(nil, "rate-sheet.drops-charge", map[string]any{"name": "rs", "routeKey": "r", "vehicleType": "v", "addressCount": 2}),
		Entry(nil, "fixed-rate.register", map[string]any{"name": "fr"}),
		Entry(nil, "fixed-rate.list", map[string]any{}),
		Entry(nil, "fixed-rate.get", map[string]any{"name": "fr"}),
		Entry(nil, "fixed-rate.create-draft", map[string]any{"name": "fr"}),
		Entry(nil, "fixed-rate.publish", map[string]any{"name": "fr"}),
		Entry(nil, "fixed-rate.rollback", map[string]any{"name": "fr", "version": 1}),
		Entry(nil, "fixed-rate.versions", map[string]any{"name": "fr"}),
		Entry(nil, "fixed-rate.active", map[string]any{"name": "fr"}),
		Entry(nil, "fixed-rate.drops-charge", map[string]any{"name": "fr", "addressCount": 2}),
		Entry(nil, "diesel-price.index", map[string]any{"activeDate": "2026-10-01T00:00:00Z"}),
		Entry(nil, "diesel-price.list", map[string]any{}),
		Entry(nil, "rate-sheet.apply-overlay", map[string]any{"name": "rs", "activeDate": "2026-10-01T00:00:00Z"}),
	)

	It("BR-071 — starts every api.* SERVER span as a new root, linking the sent traceparent", func() {
		const sentTraceID = "4bf92f3577b34da6a3ce929d0e0e4736"
		m := nats.NewMsg("api.acme-north.pricing.fee-scale.list.v1")
		m.Data = []byte(`{}`)
		m.Header.Set("Traceparent", "00-"+sentTraceID+"-00f067aa0ba902b7-01")
		_, err := nc.RequestMsg(m, 2*time.Second)
		Expect(err).NotTo(HaveOccurred())

		s := serverSpan("api.acme-north.pricing.fee-scale.list.v1")
		Expect(s.SpanContext.TraceID().String()).NotTo(Equal(sentTraceID))
		Expect(s.Links).To(HaveLen(1))
		Expect(s.Links[0].SpanContext.TraceID().String()).To(Equal(sentTraceID))
	})

	It("publishes no natstrace span for any pricing endpoint (the path is OTel only)", func() {
		legacy := make(chan *nats.Msg, 8)
		sub, err := nc.Subscribe("obs.trace.>", func(m *nats.Msg) { legacy <- m })
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = sub.Unsubscribe() })
		Expect(nc.Flush()).To(Succeed())

		_, err = nc.Request("api.acme-north.pricing.fee-scale.list.v1", []byte(`{}`), 2*time.Second)
		Expect(err).NotTo(HaveOccurred())
		Expect(nc.Flush()).To(Succeed())
		Consistently(legacy, 200*time.Millisecond).ShouldNot(Receive())
	})
})

var _ = Describe("Phase 54b-5: no Provider keeps pricing on natstrace (decision 16)", func() {
	It("publishes a natstrace span when Deps.Tracing is nil", func() {
		nc := startNATS()
		startAdapter(nc, &spanRecorder{}, nil)

		legacy := make(chan *nats.Msg, 8)
		sub, err := nc.Subscribe("obs.trace.>", func(m *nats.Msg) { legacy <- m })
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = sub.Unsubscribe() })
		Expect(nc.Flush()).To(Succeed())

		_, err = nc.Request("api.acme-north.pricing.fee-scale.list.v1", []byte(`{}`), 2*time.Second)
		Expect(err).NotTo(HaveOccurred())
		Eventually(legacy, 2*time.Second).Should(Receive())
	})
})
