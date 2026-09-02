package registry_test

// Pushed health snapshots (BR-AS64, BR-AS65).
//
// Unlike the catalogue hint, this message is the central checker's completed
// observation. A full timestamped snapshot after every pass keeps stable
// healthy results fresh without every browser asking for the same data.
//
// Phase 15 decision 14 changed where the frontend half of a snapshot comes
// from, and NOT what a snapshot is: the checker used to probe a plugin and
// now reads what the plugin last said about itself. The browser plane below
// is unchanged, which is the point — one pass, one full snapshot, one
// broadcast, and no vocabulary a shell has to learn.

import (
	"context"
	"reflect"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/application"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/domain"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/notify"
	"github.com/jthomas78/nats-tech-lab/shared/mferegistry"
)

type stubCatalog struct{ doc domain.Document }

func (s *stubCatalog) Curated(context.Context) (domain.Document, error) { return s.doc, nil }

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

func (p *stubProber) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
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
		backend   *stubProber
		checker   *application.HealthChecker
		catalog   *stubCatalog
		clock     time.Time
	)

	BeforeEach(func() {
		publisher = &recordingPublisher{}
		backend = &stubProber{ok: true}
		clock = time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

		targets, _ := domain.NewHealthTargets(map[string][]string{"fleet-ops": {}})
		catalog = &stubCatalog{doc: domain.Document{Revision: 17, Entries: []domain.Entry{{
			ID: "fleet-ops", Name: "fleet-ops", Enabled: true,
			Remote: domain.Remote{Kind: "federated", URL: "http://localhost:7110/fleet-ops.js", Module: "./plugin"},
		}}}}

		checker = application.NewHealthChecker(catalog, targets, backend, publisher)
	})

	// The clock is the spec's, not the machine's (BR-AS63). Each pass is one
	// probe interval later, which is what makes a target due again — waiting
	// five real seconds for that would be the same assertion, slowly.
	onePass := func() {
		checker.Step(context.Background(), clock)
		clock = clock.Add(domain.HealthProbeInterval)
	}

	// A plugin reporting itself, at the clock the spec is holding. This is
	// the only way frontend health enters the service now.
	report := func(state, cause string) {
		Expect(checker.AcceptFrontendReport("fleet-ops", mferegistry.HealthReport{
			PluginID: "fleet-ops", State: state, Cause: cause, At: clock.UnixMilli(),
		}, clock)).To(BeTrue())
	}

	It("pushes a complete snapshot when a plugin's health first becomes known", func() {
		report(mferegistry.HealthReportHealthy, "")
		onePass()

		updates := publisher.updates()
		Expect(updates).To(HaveLen(1))
		Expect(updates[0].OK).To(BeTrue())
		Expect(updates[0].AsOf).To(Equal(time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC).UnixMilli()))
		Expect(updates[0].Plugins["fleet-ops"].Frontend.State).To(Equal(domain.HealthHealthy))
	})

	It("pushes a newer observation when the state stays healthy", func() {
		report(mferegistry.HealthReportHealthy, "")
		onePass()
		report(mferegistry.HealthReportHealthy, "")
		onePass()

		updates := publisher.updates()
		Expect(updates).To(HaveLen(2))
		Expect(updates[1].AsOf).To(BeNumerically(">", updates[0].AsOf))
		Expect(updates[1].Plugins["fleet-ops"].Frontend.LastCheckAt).
			To(BeTemporally(">", updates[0].Plugins["fleet-ops"].Frontend.LastCheckAt))
	})

	// The failure threshold itself is no longer the registry's to apply for a
	// frontend: the plugin folds its own repeated failures and reports one
	// verdict (BR-AS63, decision 14). What is specced here is that the
	// verdict reaches a browser unchanged.
	It("pushes a plugin's own unavailable verdict, with the cause it gave", func() {
		report(mferegistry.HealthReportHealthy, "")
		onePass()

		report(mferegistry.HealthReportUnhealthy, mferegistry.HealthCauseUnreachable)
		onePass()

		updates := publisher.updates()
		Expect(updates).To(HaveLen(2))
		Expect(updates[1].Plugins["fleet-ops"].Frontend.State).To(Equal(domain.HealthUnavailable))
		Expect(updates[1].Plugins["fleet-ops"].Frontend.Cause).To(Equal(mferegistry.HealthCauseUnreachable))
	})

	Context("BR-AS54 — health silence is an observation, never a withdrawal", func() {
		It("keeps a plugin never heard from unknown, with its catalogue entry intact", func() {
			// These are deliberately separate silences. Treating never heard
			// from as absent invents a heartbeat the registry never received;
			// treating it as a withdrawal would let a temporary NATS partition
			// remove a running plugin from every shell.
			before := catalog.doc

			onePass()
			signal := publisher.updates()[0].Plugins["fleet-ops"].Frontend
			Expect(signal.State).To(Equal(domain.HealthUnknown))
			Expect(signal.LastCheckAt).To(BeZero())
			Expect(backend.count()).To(Equal(0))
			Expect(catalog.doc).To(Equal(before))
		})

		It("ages a previously heard plugin to stale and absent through repeated passes without changing the catalogue", func() {
			before := catalog.doc
			report(mferegistry.HealthReportHealthy, "")

			// A naive "N strikes and it is out" implementation passes a single
			// stale-read test. Keep stepping beyond expiry: absence may change
			// health forever, but it must never gain the authority to edit the
			// catalogue.
			for i := 0; i <= int(mferegistry.HealthFrontendFreshness/domain.HealthProbeInterval)+3; i++ {
				onePass()
			}

			updates := publisher.updates()
			last := updates[len(updates)-1].Plugins["fleet-ops"].Frontend
			Expect(last.State).To(Equal(domain.HealthStale))
			Expect(last.Cause).To(Equal(mferegistry.HealthCauseAbsent))
			Expect(backend.count()).To(Equal(0))
			Expect(catalog.doc).To(Equal(before))
		})

		It("holds only a catalogue read capability, so a health pass cannot be given a withdrawal method", func() {
			// This is a structural companion to the snapshots above. A future
			// health implementation that needs Apply has crossed the boundary
			// before any state transition can be hidden behind a helper.
			catalog := reflect.TypeOf((*application.HealthCatalog)(nil)).Elem()
			Expect(catalog.NumMethod()).To(Equal(1))
			Expect(catalog.Method(0).Name).To(Equal("Curated"))
		})
	})

	It("names a subject a shell may only subscribe to", func() {
		subject := notify.HealthChanged()

		Expect(subject.Name).To(Equal("notify._platform.mfe-registry.frontend-plugins.health"))
		Expect(subject.Tokens.Context).To(Equal("_platform"))
		Expect(subject.Tokens.Action).To(Equal("health"))
	})
})
