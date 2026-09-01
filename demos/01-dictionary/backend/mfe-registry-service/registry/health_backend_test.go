package registry_test

// Phase 5d — the backend readiness probe, over a real broker (BR-AS62).
//
// The rule this file is mostly about is that PRESENCE IS NOT READINESS. A
// service can hold a NATS connection open while its database is gone, so
// "something answered" is not the question — "did it say it is ready" is. A
// probe that accepted a connection, an empty reply or a malformed body as
// health would report the most common kind of outage as green.
//
// The targets come from deployment configuration and never from a manifest.
// A publisher that could name its own probe target could point the registry
// at a service it does not own and read the answer back out of the health
// decoration.

import (
	"context"
	"encoding/json"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/domain"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/healthnats"
	"github.com/jthomas78/nats-tech-lab/shared/mferegistry"
)

var _ = Describe("BR-AS62 — backend readiness is asked for, never assumed", func() {
	var (
		nc     *nats.Conn
		client *healthnats.Client
		ctx    context.Context
	)

	BeforeEach(func() {
		srv, err := server.NewServer(&server.Options{Host: "127.0.0.1", Port: -1})
		Expect(err).NotTo(HaveOccurred())
		go srv.Start()
		DeferCleanup(srv.Shutdown)
		Expect(srv.ReadyForConnections(5 * time.Second)).To(BeTrue())

		nc, err = nats.Connect(srv.ClientURL(), nats.Name("registry-health-test"))
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(nc.Close)

		client = healthnats.New(nc)
		ctx = context.Background()
	})

	// answering stands a service up on its readiness subject.
	answering := func(serviceID string, reply func() []byte) {
		sub, err := nc.Subscribe(mferegistry.ServiceReady(serviceID), func(m *nats.Msg) {
			_ = m.Respond(reply())
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(nc.Flush()).To(Succeed())
		DeferCleanup(func() { _ = sub.Unsubscribe() })
	}

	ready := func(v bool) func() []byte {
		return func() []byte {
			body, _ := json.Marshal(map[string]bool{"ready": v})
			return body
		}
	}

	Context("the probe", func() {
		It("accepts a service that says it is ready", func() {
			answering("shipping-service", ready(true))

			Expect(client.Probe(ctx, "shipping-service", time.Now()).OK).To(BeTrue())
		})

		It("refuses a service that answers but says it is NOT ready", func() {
			// The case the whole file exists for: the connection is fine and
			// the service is not.
			answering("shipping-service", ready(false))

			probe := client.Probe(ctx, "shipping-service", time.Now())

			Expect(probe.OK).To(BeFalse())
			Expect(probe.Cause).To(Equal("not-ready"))
		})

		It("refuses an answer that is not the agreed shape", func() {
			answering("shipping-service", func() []byte { return []byte("OK") })

			Expect(client.Probe(ctx, "shipping-service", time.Now()).Cause).To(Equal("invalid-response"))
		})

		It("refuses an empty answer, because silence in a reply is not a yes", func() {
			answering("shipping-service", func() []byte { return nil })

			Expect(client.Probe(ctx, "shipping-service", time.Now()).Cause).To(Equal("invalid-response"))
		})

		It("reports no responders when nothing is listening", func() {
			probe := client.Probe(ctx, "absent-service", time.Now())

			Expect(probe.OK).To(BeFalse())
			Expect(probe.Cause).To(Equal("no-responders"))
		})

		It("gives up at the timeout", func() {
			answering("slow-service", func() []byte {
				time.Sleep(domain.HealthProbeTimeout + 500*time.Millisecond)
				return ready(true)()
			})

			started := time.Now()
			probe := client.Probe(ctx, "slow-service", started)

			Expect(probe.Cause).To(Equal("timeout"))
			Expect(time.Since(started)).To(BeNumerically("<", 2*domain.HealthProbeTimeout))
		})

		It("asks one service and not a wildcard", func() {
			// A probe that subscribed or requested across `rpc._platform.health.>`
			// would be one grant covering every service in the platform. Each
			// probe names exactly one.
			Expect(mferegistry.ServiceReady("shipping-service")).
				To(Equal("rpc._platform.health.shipping-service.ready.v1"))
			Expect(mferegistry.ServiceReady("shipping-service")).ToNot(ContainSubstring(">"))
			Expect(mferegistry.ServiceReady("shipping-service")).ToNot(ContainSubstring("*"))
		})
	})

	Context("the deployment mapping", func() {
		parsed := func(raw string) domain.HealthTargets {
			targets, warnings := registry.ParseHealthTargets(raw)
			Expect(warnings).To(BeEmpty())
			return targets
		}

		It("resolves the service IDs the deployment configured", func() {
			targets := parsed(`{"fleet-ops":["shipping-service","pricing-service"]}`)

			Expect(targets.Dependencies("fleet-ops")).To(Equal([]string{"pricing-service", "shipping-service"}))
		})

		It("tells absent from empty, because they are different answers", func() {
			targets := parsed(`{"fleet-ops":[]}`)

			Expect(targets.Dependencies("fleet-ops")).To(Equal([]string{}), "an empty list is frontend-only")
			Expect(targets.Dependencies("pricing")).To(BeNil(), "an unmapped plugin was never judged")
		})

		It("summarizes an unmapped plugin as not configured and an empty one as not applicable", func() {
			targets := parsed(`{"fleet-ops":[]}`)

			Expect(domain.SummarizeBackend(nil).State).To(Equal(domain.HealthNotConfigured))
			Expect(domain.SummarizeBackend(signalsFor(targets, "fleet-ops")).State).
				To(Equal(domain.HealthNotApplicable))
		})

		It("ignores a malformed configuration rather than guessing at it", func() {
			targets, warnings := registry.ParseHealthTargets(`not json`)

			Expect(warnings).ToNot(BeEmpty())
			Expect(targets.Dependencies("fleet-ops")).To(BeNil())
		})

		It("drops a service ID that is not subject-safe", func() {
			// A dot would split the token and change which subject is dialled;
			// a wildcard would widen it. Either would let configuration reach
			// somewhere it did not mean to (CLAUDE.md, subject-safe IDs).
			targets, warnings := registry.ParseHealthTargets(`{"fleet-ops":["a.b","x>","ok-service"]}`)

			Expect(warnings).To(HaveLen(2))
			Expect(targets.Dependencies("fleet-ops")).To(Equal([]string{"ok-service"}))
		})
	})
})

// signalsFor is what the worker will hand SummarizeBackend: one signal per
// configured dependency, and a nil slice when nothing was configured.
func signalsFor(targets domain.HealthTargets, pluginID string) []domain.HealthSignal {
	ids := targets.Dependencies(pluginID)
	if ids == nil {
		return nil
	}
	out := make([]domain.HealthSignal, 0, len(ids))
	for range ids {
		out = append(out, domain.HealthSignal{State: domain.HealthHealthy})
	}
	return out
}
