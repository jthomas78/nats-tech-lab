package registry_test

// Phase 5d — the health hint (BR-AS64, BR-AS65).
//
// The hint is the same promise the catalogue's notification makes: it says
// "look again", never "here is the answer". A delivery proves a message
// arrived and never proves a service was alive, so a hint that carried state
// would be an observation nothing re-checked. These specs pin that, plus the
// quieter half: a pass where nothing moved says nothing at all, because a
// heartbeat on a five-second timer would train every shell to ignore it.

import (
	"context"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/application"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/domain"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/notify"
)

type stubCatalog struct{ doc domain.Document }

func (s stubCatalog) Curated(context.Context) (domain.Document, error) { return s.doc, nil }

type stubProber struct {
	mu    sync.Mutex
	ok    bool
	calls int
}

func (p *stubProber) Probe(_ context.Context, _ string, at time.Time) domain.HealthProbe {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	if p.ok {
		return domain.HealthProbeOK(at)
	}
	return domain.HealthProbeFailed("unreachable", at)
}

func (p *stubProber) set(ok bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.ok = ok
}

type recordingHinter struct {
	mu    sync.Mutex
	count int
}

func (h *recordingHinter) HealthChanged(context.Context) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.count++
}

func (h *recordingHinter) hints() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.count
}

var _ = Describe("BR-AS64 — the health hint says look again, and nothing else", func() {
	var (
		hinter  *recordingHinter
		front   *stubProber
		checker *application.HealthChecker
	)

	allowed := domain.NewAllowlist([]string{"http://localhost:7110"})

	BeforeEach(func() {
		hinter = &recordingHinter{}
		front = &stubProber{ok: true}

		origins, _ := domain.NewHealthOrigins(allowed, map[string]string{
			"http://localhost:7110": "http://plugins.internal:7110",
		})
		targets, _ := domain.NewHealthTargets(map[string][]string{"fleet-ops": {}})
		catalog := stubCatalog{doc: domain.Document{Entries: []domain.Entry{{
			ID: "fleet-ops", Name: "fleet-ops", Enabled: true,
			Remote: domain.Remote{Kind: "federated", URL: "http://localhost:7110/fleet-ops.js", Module: "./plugin"},
		}}}}

		checker = application.NewHealthChecker(catalog, origins, targets, front, front, hinter)
	})

	// The clock is the spec's, not the machine's (BR-AS63). Each pass is one
	// probe interval later, which is what makes a target due again — waiting
	// five real seconds for that would be the same assertion, slowly.
	clock := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	onePass := func() {
		checker.Step(context.Background(), clock)
		clock = clock.Add(domain.HealthProbeInterval)
	}

	It("hints once when a plugin's health first becomes known", func() {
		onePass()

		Expect(hinter.hints()).To(Equal(1))
	})

	It("stays quiet on a pass where nothing moved", func() {
		// A hint every five seconds regardless would teach every shell to
		// ignore it, which is the opposite of what a hint is for.
		onePass()
		onePass()

		Expect(hinter.hints()).To(Equal(1))
	})

	It("hints again when a signal actually changes", func() {
		onePass()

		front.set(false)
		onePass() // one failure — below the threshold, still healthy
		onePass() // second consecutive failure — now unavailable

		Expect(hinter.hints()).To(Equal(2))
	})

	It("names a subject a shell may only subscribe to", func() {
		subject := notify.HealthChanged()

		Expect(subject.Name).To(Equal("notify._platform.registry.frontend-plugins.health"))
		Expect(subject.Tokens.Context).To(Equal("_platform"))
		Expect(subject.Tokens.Action).To(Equal("health"))
	})
})
