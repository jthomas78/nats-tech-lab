package tradingpartner_test

// Wire-level round-trip specs for the api.* adapter (Phase 26h). Separate from
// browserrpc_test.go, which only asserts *registration* ($SRV discovery and
// subject shape) — that a subject is advertised says nothing about whether a
// request over it does the right thing.
//
// These exist for two claims in particular, both of which are the reason the
// api.* path is not merely a translation of the REST path:
//
//  1. {context} is taken from the subject, never from the request body, so a
//     client cannot reach another context by lying in JSON.
//  2. The BR-TP14 tenant is taken from the adapter's own connection, never from
//     the body — the thing REST cannot do, and the reason REST's
//     fleetAssetRequest has a `tenant` field this transport deliberately omits.
//
// Repositories are in-memory fakes: the point here is the transport boundary,
// and the domain rules behind it already have their own 37 specs.

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/nats-io/nats.go"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/trading-partner-service/tradingpartner/internal/application/commands"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/trading-partner-service/tradingpartner/internal/browserrpc"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/trading-partner-service/tradingpartner/internal/domain"
)

// --- in-memory fakes ---

type fakePartnerRepo struct {
	mu     sync.Mutex
	nextID int
	items  map[string]domain.TradingPartner
}

func newFakePartnerRepo() *fakePartnerRepo {
	return &fakePartnerRepo{items: map[string]domain.TradingPartner{}}
}

func (f *fakePartnerRepo) Register(_ context.Context, tp domain.TradingPartner) (domain.TradingPartner, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	tp.ID = fmt.Sprintf("tp-%d", f.nextID)
	f.items[tp.ID] = tp
	return tp, nil
}

func (f *fakePartnerRepo) Get(_ context.Context, id string) (domain.TradingPartner, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	tp, ok := f.items[id]
	if !ok {
		return domain.TradingPartner{}, domain.ErrTradingPartnerNotFound
	}
	return tp, nil
}

func (f *fakePartnerRepo) List(_ context.Context, contextKey string) ([]domain.TradingPartner, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []domain.TradingPartner
	for _, tp := range f.items {
		if tp.Context == contextKey {
			out = append(out, tp)
		}
	}
	return out, nil
}

// transition mirrors what the Postgres repository does: load, run the domain
// transition (value receiver, returns the next state), store the result.
func (f *fakePartnerRepo) transition(
	id string, fn func(domain.TradingPartner) (domain.TradingPartner, error),
) (domain.TradingPartner, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	tp, ok := f.items[id]
	if !ok {
		return domain.TradingPartner{}, domain.ErrTradingPartnerNotFound
	}
	next, err := fn(tp)
	if err != nil {
		return domain.TradingPartner{}, err
	}
	f.items[id] = next
	return next, nil
}

func (f *fakePartnerRepo) Activate(_ context.Context, id string) (domain.TradingPartner, error) {
	return f.transition(id, func(tp domain.TradingPartner) (domain.TradingPartner, error) { return tp.Activate() })
}

func (f *fakePartnerRepo) Suspend(_ context.Context, id, reason string) (domain.TradingPartner, error) {
	return f.transition(id, func(tp domain.TradingPartner) (domain.TradingPartner, error) { return tp.Suspend(reason) })
}

func (f *fakePartnerRepo) Reactivate(_ context.Context, id string) (domain.TradingPartner, error) {
	return f.transition(id, func(tp domain.TradingPartner) (domain.TradingPartner, error) { return tp.Reactivate() })
}

type fakeFleetRepo struct {
	mu     sync.Mutex
	byName map[string][]domain.FleetAsset
}

func newFakeFleetRepo() *fakeFleetRepo {
	return &fakeFleetRepo{byName: map[string][]domain.FleetAsset{}}
}

func (f *fakeFleetRepo) AddFleetAsset(_ context.Context, partnerID string, a domain.FleetAsset) (domain.FleetAsset, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.byName[partnerID] = append(f.byName[partnerID], a)
	return a, nil
}

func (f *fakeFleetRepo) ListFleetAssets(_ context.Context, partnerID string) ([]domain.FleetAsset, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.byName[partnerID], nil
}

// recordingValidator captures which tenant BR-TP14's lookup was performed as —
// the assertion target for claim (2) above.
type recordingValidator struct {
	mu           sync.Mutex
	seenTenants  []string
	seenContexts []string
	result       bool
}

func (v *recordingValidator) Exists(_ context.Context, tenant, contextKey, _ string) (bool, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.seenTenants = append(v.seenTenants, tenant)
	v.seenContexts = append(v.seenContexts, contextKey)
	return v.result, nil
}

func (v *recordingValidator) tenants() []string {
	v.mu.Lock()
	defer v.mu.Unlock()
	return append([]string(nil), v.seenTenants...)
}

type fakeAudit struct {
	mu      sync.Mutex
	entries []domain.AuditEntry
}

func (f *fakeAudit) Record(_ context.Context, e domain.AuditEntry) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.entries = append(f.entries, e)
	return nil
}

func (f *fakeAudit) ListByPartner(_ context.Context, partnerID string) ([]domain.AuditEntry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []domain.AuditEntry
	for _, e := range f.entries {
		if e.TradingPartnerID == partnerID {
			out = append(out, e)
		}
	}
	return out, nil
}

func (f *fakeAudit) all() []domain.AuditEntry {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]domain.AuditEntry(nil), f.entries...)
}

var _ = Describe("api.* round trips (Phase 26h)", func() {
	const requestTimeout = 2 * time.Second

	var (
		nc        *nats.Conn
		partners  *fakePartnerRepo
		fleet     *fakeFleetRepo
		validator *recordingValidator
		audit     *fakeAudit
	)

	// request sends data on subject and decodes the reply into out.
	request := func(subject string, in any, out any) {
		GinkgoHelper()
		body, err := json.Marshal(in)
		Expect(err).NotTo(HaveOccurred())
		reply, err := nc.Request(subject, body, requestTimeout)
		Expect(err).NotTo(HaveOccurred())
		if out != nil {
			Expect(json.Unmarshal(reply.Data, out)).To(Succeed())
		}
	}

	BeforeEach(func() {
		nc = newTestNATSConn()
		partners = newFakePartnerRepo()
		fleet = newFakeFleetRepo()
		validator = &recordingValidator{result: true}
		audit = &fakeAudit{}

		adapter, err := browserrpc.New(nc, browserrpc.Deps{
			TradingPartners: commands.NewTradingPartnerHandler(partners, audit),
			Documents:       commands.NewComplianceDocumentHandler(partners, nil),
			FleetAssets:     commands.NewFleetAssetHandler(partners, fleet, validator),
			Audit:           audit,
			// The tenant this adapter's connection authenticated into.
			Tenant: "acme",
		})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = adapter.Stop() })
	})

	Context("context scoping", func() {
		It("takes {context} from the subject, not the request body", func() {
			// The security property: a body field must not be able to redirect a
			// write into a context the subject didn't name.
			var registered struct {
				ID      string `json:"id"`
				Context string `json:"context"`
			}
			request("api.acme-north.trading-partner.partner.register.v1",
				map[string]any{"name": "Initech", "type": "SHIPPER", "context": "acme-south"},
				&registered)

			Expect(registered.Context).To(Equal("acme-north"))
			Expect(registered.Context).NotTo(Equal("acme-south"))
		})

		It("lists only partners in the subject's context", func() {
			request("api.acme-north.trading-partner.partner.register.v1",
				map[string]any{"name": "North Co", "type": "SHIPPER"}, nil)
			request("api.acme-south.trading-partner.partner.register.v1",
				map[string]any{"name": "South Co", "type": "SHIPPER"}, nil)

			var listed struct {
				TradingPartners []struct {
					Name string `json:"name"`
				} `json:"tradingPartners"`
			}
			request("api.acme-north.trading-partner.partner.list.v1", map[string]any{}, &listed)

			Expect(listed.TradingPartners).To(HaveLen(1))
			Expect(listed.TradingPartners[0].Name).To(Equal("North Co"))
		})
	})

	Context("BR-TP14 tenant resolution", func() {
		It("validates vehicleTypeCode as the connection's tenant, ignoring any body-supplied tenant", func() {
			// This is the improvement over REST, and the reason the api.*
			// fleetAssetRequest has no `tenant` field: HTTP had to trust the body.
			var registered struct {
				ID string `json:"id"`
			}
			request("api.acme-north.trading-partner.partner.register.v1",
				map[string]any{"name": "Acme Trucking", "type": "TRANSPORTER"}, &registered)
			Expect(registered.ID).NotTo(BeEmpty())

			request("api.acme-north.trading-partner.fleet-asset.add.v1", map[string]any{
				"id":              registered.ID,
				"registrationNo":  "ABC123",
				"vin":             "VIN1",
				"make":            "Volvo",
				"model":           "FH16",
				"vehicleTypeCode": "TAUTLINER",
				// A caller trying to borrow another tenant's refdata corpus.
				"tenant": "globex",
			}, nil)

			Expect(validator.tenants()).To(ConsistOf("acme"))
			Expect(validator.tenants()).NotTo(ContainElement("globex"))
		})
	})

	Context("lifecycle + audit (BR-TP03/BR-TP04/BR-TP06)", func() {
		It("activates and suspends over api.*, recording audit rows with the actor", func() {
			var registered struct {
				ID string `json:"id"`
			}
			request("api.acme.trading-partner.partner.register.v1",
				map[string]any{"name": "Globex", "type": "SHIPPER"}, &registered)

			request("api.acme.trading-partner.partner.activate.v1",
				map[string]any{"id": registered.ID}, nil)

			var suspended struct {
				Status string `json:"status"`
			}
			request("api.acme.trading-partner.partner.suspend.v1",
				map[string]any{"id": registered.ID, "reason": "docs expired"}, &suspended)

			Expect(suspended.Status).To(Equal("SUSPENDED"))

			actions := make([]string, 0)
			for _, e := range audit.all() {
				actions = append(actions, e.Action)
				Expect(e.Actor).NotTo(BeEmpty(), "BR-TP06 requires an actor on every row")
			}
			Expect(actions).To(ContainElements(
				domain.AuditActionRegistered, domain.AuditActionActivated, domain.AuditActionSuspended,
			))
		})

		It("returns a 404-coded error for an unknown partner rather than hanging", func() {
			reply, err := nc.Request("api.acme.trading-partner.partner.get.v1",
				[]byte(`{"id":"does-not-exist"}`), requestTimeout)
			Expect(err).NotTo(HaveOccurred())

			Expect(reply.Header.Get("Nats-Service-Error-Code")).To(Equal("404"))
			var out struct {
				Error    string `json:"error"`
				NotFound bool   `json:"notFound"`
			}
			Expect(json.Unmarshal(reply.Data, &out)).To(Succeed())
			Expect(out.NotFound).To(BeTrue())
			Expect(out.Error).To(ContainSubstring("not found"))
		})

		It("identifies itself in the Nats-Responder header (Phase 18 invariant)", func() {
			reply, err := nc.Request("api.acme.trading-partner.partner.list.v1", []byte(`{}`), requestTimeout)
			Expect(err).NotTo(HaveOccurred())
			Expect(reply.Header.Get("Nats-Responder")).To(HavePrefix(browserrpc.ServiceName + "/"))
		})
	})

	Context("obs.trace.* side-channel (BR-036/BR-037/BR-TP15)", func() {
		// traceSpan is a strict superset of the pre-Phase-28 obsEnvelope: this
		// decodes it into the old shape (ignoring every new field) to assert
		// backward compatibility, then again into the full shape for the new
		// tracing fields.
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
		type traceSpan struct {
			legacyEnvelope
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
		}

		It("publishes one span per call to obs.trace.{context}.trading-partner.{entity}.{action}, decodable both as the old envelope shape and the new one", func() {
			spans := make(chan *nats.Msg, 8)
			sub, err := nc.Subscribe("obs.trace.>", func(m *nats.Msg) { spans <- m })
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { _ = sub.Unsubscribe() })
			Expect(nc.Flush()).To(Succeed())

			request("api.acme.trading-partner.partner.list.v1", map[string]any{}, nil)

			var msg *nats.Msg
			Eventually(spans).Should(Receive(&msg))
			Expect(msg.Subject).To(Equal("obs.trace.acme.trading-partner.partner.list"))

			var legacy legacyEnvelope
			Expect(json.Unmarshal(msg.Data, &legacy)).To(Succeed())
			Expect(legacy.Subject).To(Equal("api.acme.trading-partner.partner.list.v1"))
			Expect(legacy.PayloadBytes).To(BeNumerically(">", 0))

			var span traceSpan
			Expect(json.Unmarshal(msg.Data, &span)).To(Succeed())
			Expect(span.TraceID).NotTo(BeEmpty())
			Expect(span.SpanID).NotTo(BeEmpty())
			Expect(span.ParentSpanID).To(BeEmpty(), "a call with no inbound traceparent is a root span")
			Expect(span.Service).To(Equal("trading-partner"))
			Expect(span.Entity).To(Equal("partner"))
			Expect(span.Action).To(Equal("list"))
			Expect(span.StatusCode).To(Equal("OK"))
		})

		It("marks a failed call with statusCode ERROR and the error message", func() {
			spans := make(chan *nats.Msg, 8)
			sub, err := nc.Subscribe("obs.trace.>", func(m *nats.Msg) { spans <- m })
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { _ = sub.Unsubscribe() })
			Expect(nc.Flush()).To(Succeed())

			request("api.acme.trading-partner.partner.get.v1", map[string]any{"id": "does-not-exist"}, nil)

			var msg *nats.Msg
			Eventually(spans).Should(Receive(&msg))

			var span traceSpan
			Expect(json.Unmarshal(msg.Data, &span)).To(Succeed())
			Expect(span.StatusCode).To(Equal("ERROR"))
			Expect(span.StatusMessage).NotTo(BeEmpty())
		})
	})
})
