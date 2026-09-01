package registry_test

// Phase 5d — the health plane's own read (BR-AS65).
//
// The rule these specs are for is SEPARATION. Health is an observation the
// platform made a moment ago; the catalogue is what an operator and a
// publisher agreed. The health read is therefore a different subject, a
// different reply shape and a different code path, and the specs check the
// two do not touch: probing moves no revision, writes no audit row, edits no
// signed byte, and a slow probe cannot make a catalogue read wait.

import (
	"context"
	"reflect"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/application"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/browserrpc"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/domain"
	"github.com/jthomas78/nats-tech-lab/shared/mferegistry"
)

// stubHealth stands in for the checker: the transport's job is to hand a
// snapshot over, not to make one.
type stubHealth struct {
	snapshot map[string]application.PluginHealth
	calls    int
}

func (s *stubHealth) Snapshot(time.Time) map[string]application.PluginHealth {
	s.calls++
	return s.snapshot
}

var _ = Describe("BR-AS65 — health is read on its own subject", func() {
	t0 := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

	var (
		health    *stubHealth
		endpoints *browserrpc.Endpoints
		ctx       context.Context
	)

	BeforeEach(func() {
		ctx = context.Background()
		health = &stubHealth{snapshot: map[string]application.PluginHealth{
			"fleet-ops": {
				Frontend: domain.HealthSignal{State: domain.HealthHealthy, LastCheckAt: t0},
				Backend:  domain.HealthSignal{State: domain.HealthUnavailable, Cause: "not-ready", LastCheckAt: t0},
			},
		}}
		endpoints = browserrpc.NewWithHealth(nil, nil, health)
	})

	It("is a subject of its own, on the shell's list and not the operator's", func() {
		Expect(mferegistry.HealthRead).To(Equal("api._platform.registry.frontend-plugins.health.v1"))
		Expect(mferegistry.Subjects()).To(ContainElement(mferegistry.HealthRead))
		Expect(mferegistry.Operator()).ToNot(ContainElement(mferegistry.HealthRead))
	})

	It("answers with each plugin's two signals, separately", func() {
		out, err := endpoints.Health(ctx)

		Expect(err).NotTo(HaveOccurred())
		Expect(out.OK).To(BeTrue())
		Expect(out.Plugins).To(HaveKey("fleet-ops"))
		Expect(out.Plugins["fleet-ops"].Frontend.State).To(Equal(domain.HealthHealthy))
		Expect(out.Plugins["fleet-ops"].Backend.State).To(Equal(domain.HealthUnavailable))
	})

	It("carries a safe cause and never an address", func() {
		// The reply goes to a browser, into the same JS realm every loaded
		// plugin shares. A cause is a word from a closed list; a host, port or
		// credential is deployment topology and never leaves the process
		// (BR-AS60/AS65).
		out, _ := endpoints.Health(ctx)

		Expect(out.Plugins["fleet-ops"].Backend.Cause).To(Equal("not-ready"))
		Expect(out.Plugins["fleet-ops"].Backend.Cause).ToNot(ContainSubstring("://"))
		Expect(out.Plugins["fleet-ops"].Backend.Cause).ToNot(ContainSubstring(":"))
	})

	It("says when each signal was last checked", func() {
		out, _ := endpoints.Health(ctx)

		Expect(out.Plugins["fleet-ops"].Frontend.LastCheckAt).To(Equal(t0))
	})

	It("reads memory, so it never waits on a probe", func() {
		_, err := endpoints.Health(ctx)

		Expect(err).NotTo(HaveOccurred())
		Expect(health.calls).To(Equal(1), "one snapshot read and no fetch of any kind")
	})

	It("answers with nothing configured rather than failing when no checker is wired", func() {
		// A deployment that mapped nothing still has a shell asking. The
		// answer is an empty snapshot, not an error: an error would be
		// indistinguishable from the health plane being broken.
		bare := browserrpc.New(nil, nil)

		out, err := bare.Health(ctx)

		Expect(err).NotTo(HaveOccurred())
		Expect(out.OK).To(BeTrue())
		Expect(out.Plugins).To(BeEmpty())
	})

	It("carries no catalogue at all — no revision, no entries, no signed bytes", func() {
		// Asserted on the reply's SHAPE rather than its values: a health reply
		// that grew a revision field would be a health observation that could
		// make a shell think the catalogue moved (BR-AS65).
		out, _ := endpoints.Health(ctx)

		Expect(out).To(BeAssignableToTypeOf(browserrpc.HealthResponse{}))
		Expect(fieldNames(out)).To(ConsistOf("OK", "Plugins", "AsOf"))
	})
})

// fieldNames is how the shape assertion above stays honest as the struct
// changes: a new field on the health reply has to be a deliberate edit here.
func fieldNames(v any) []string {
	out := []string{}
	t := reflect.TypeOf(v)
	for i := 0; i < t.NumField(); i++ {
		out = append(out, t.Field(i).Name)
	}
	return out
}
