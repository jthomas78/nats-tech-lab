package registry_test

// Phase 5d — pushed health snapshots (BR-AS64, BR-AS65).
//
// Unlike the catalogue hint, this message is the central checker's completed
// observation. A full timestamped snapshot after every pass keeps stable
// healthy results fresh without every browser asking for the same data.

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

type recordingPublisher struct {
	mu        sync.Mutex
	snapshots []application.HealthSnapshot
}

func (h *recordingPublisher) HealthChanged(_ context.Context, snapshot application.HealthSnapshot) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.snapshots = append(h.snapshots, snapshot)
}

func (h *recordingPublisher) updates() []application.HealthSnapshot {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]application.HealthSnapshot(nil), h.snapshots...)
}

var _ = Describe("BR-AS64 — the central checker pushes health observations", func() {
	var (
		publisher *recordingPublisher
		front     *stubProber
		checker   *application.HealthChecker
		clock     time.Time
	)

	allowed := domain.NewAllowlist([]string{"http://localhost:7110"})

	BeforeEach(func() {
		publisher = &recordingPublisher{}
		front = &stubProber{ok: true}
		clock = time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

		origins, _ := domain.NewHealthOrigins(allowed, map[string]string{
			"http://localhost:7110": "http://plugins.internal:7110",
		})
		targets, _ := domain.NewHealthTargets(map[string][]string{"fleet-ops": {}})
		catalog := stubCatalog{doc: domain.Document{Entries: []domain.Entry{{
			ID: "fleet-ops", Name: "fleet-ops", Enabled: true,
			Remote: domain.Remote{Kind: "federated", URL: "http://localhost:7110/fleet-ops.js", Module: "./plugin"},
		}}}}

		checker = application.NewHealthChecker(catalog, origins, targets, front, front, publisher)
	})

	// The clock is the spec's, not the machine's (BR-AS63). Each pass is one
	// probe interval later, which is what makes a target due again — waiting
	// five real seconds for that would be the same assertion, slowly.
	onePass := func() {
		checker.Step(context.Background(), clock)
		clock = clock.Add(domain.HealthProbeInterval)
	}

	It("pushes a complete snapshot when a plugin's health first becomes known", func() {
		onePass()

		updates := publisher.updates()
		Expect(updates).To(HaveLen(1))
		Expect(updates[0].OK).To(BeTrue())
		Expect(updates[0].AsOf).To(Equal(time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC).UnixMilli()))
		Expect(updates[0].Plugins["fleet-ops"].Frontend.State).To(Equal(domain.HealthHealthy))
	})

	It("pushes a newer observation when the state stays healthy", func() {
		onePass()
		onePass()

		updates := publisher.updates()
		Expect(updates).To(HaveLen(2))
		Expect(updates[1].AsOf).To(BeNumerically(">", updates[0].AsOf))
		Expect(updates[1].Plugins["fleet-ops"].Frontend.LastCheckAt).
			To(BeTemporally(">", updates[0].Plugins["fleet-ops"].Frontend.LastCheckAt))
	})

	It("includes the unavailable transition after the failure threshold", func() {
		onePass()

		front.set(false)
		onePass() // one failure — below the threshold, still healthy
		onePass() // second consecutive failure — now unavailable

		updates := publisher.updates()
		Expect(updates).To(HaveLen(3))
		Expect(updates[2].Plugins["fleet-ops"].Frontend.State).To(Equal(domain.HealthUnavailable))
	})

	It("names a subject a shell may only subscribe to", func() {
		subject := notify.HealthChanged()

		Expect(subject.Name).To(Equal("notify._platform.mfe-registry.frontend-plugins.health"))
		Expect(subject.Tokens.Context).To(Equal("_platform"))
		Expect(subject.Tokens.Action).To(Equal("health"))
	})
})
