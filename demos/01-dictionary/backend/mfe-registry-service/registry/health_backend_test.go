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
	"fmt"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

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

	Context("what a plugin may be probed against", func() {
		// BR-AS62, rewritten. The map that used to live in the deployment is
		// gone; a plugin DECLARES its dependencies in the signed manifest and
		// an operator APPROVES them on the catalogue entry. The property the
		// old map bought is the one asserted here: a declaration on its own
		// dials nothing.
		entry := func(declared, approved []string) domain.Entry {
			return domain.Entry{
				ID:                      "fleet-ops",
				BackendServices:         declared,
				ApprovedBackendServices: approved,
			}
		}

		It("probes nothing on a declaration alone", func() {
			// The whole of the threat model in one spec. A publisher may ask
			// for any service it likes; until an operator says yes, the
			// registry dials none of them and the decoration says so.
			e := entry([]string{"shipping-service", "pricing-service"}, nil)

			Expect(e.EffectiveBackendServices()).To(BeNil())
			Expect(domain.SummarizeBackend(signalsFor(e)).State).
				To(Equal(domain.HealthNotConfigured))
		})

		It("probes exactly what the operator approved", func() {
			e := entry([]string{"shipping-service", "pricing-service"}, []string{"pricing-service"})

			Expect(e.EffectiveBackendServices()).To(Equal([]string{"pricing-service"}))
		})

		It("tells never-declared from declared-as-none, because they are different answers", func() {
			Expect(entry(nil, nil).EffectiveBackendServices()).To(BeNil(),
				"a plugin that never said is not configured")
			Expect(entry([]string{}, nil).EffectiveBackendServices()).To(Equal([]string{}),
				"a plugin saying it is frontend-only has been answered")
		})

		It("summarizes an unanswered plugin as not configured and a frontend-only one as not applicable", func() {
			Expect(domain.SummarizeBackend(signalsFor(entry(nil, nil))).State).
				To(Equal(domain.HealthNotConfigured))
			Expect(domain.SummarizeBackend(signalsFor(entry([]string{}, nil))).State).
				To(Equal(domain.HealthNotApplicable))
		})

		It("narrows an approval to what is still declared", func() {
			// A publisher that quietly stops declaring a service must not keep
			// the probe it was granted for it.
			e := entry([]string{"pricing-service"}, []string{"pricing-service", "shipping-service"})

			Expect(e.EffectiveBackendServices()).To(Equal([]string{"pricing-service"}))
		})

		It("reads an operator approving nothing as an answer, not as silence", func() {
			e := entry([]string{"shipping-service"}, []string{})

			Expect(e.EffectiveBackendServices()).To(Equal([]string{}))
			Expect(domain.SummarizeBackend(signalsFor(e)).State).
				To(Equal(domain.HealthNotApplicable))
		})
	})

	Context("what a declaration may say", func() {
		admissible := func(declared, approved []string) error {
			return domain.Entry{
				ID:                      "fleet-ops",
				Name:                    "Fleet Ops",
				Contributions:           []domain.Contribution{{Kind: "route", ID: "fleet-ops-home", Path: "/fleet-ops", Component: "Home", Title: "Home"}},
				BackendServices:         declared,
				ApprovedBackendServices: approved,
			}.Admissible()
		}

		It("accepts a plain declaration and its approval", func() {
			Expect(admissible([]string{"pricing-service"}, []string{"pricing-service"})).To(Succeed())
		})

		It("refuses a service id that is not subject-safe", func() {
			// A dot would split the token and change which subject is dialled;
			// a wildcard would widen it past the one service the grant is for.
			Expect(admissible([]string{"a.b"}, nil)).To(MatchError(domain.ErrEntryNotAdmissible))
			Expect(admissible([]string{"x>"}, nil)).To(MatchError(domain.ErrEntryNotAdmissible))
			Expect(admissible([]string{"*"}, nil)).To(MatchError(domain.ErrEntryNotAdmissible))
			Expect(admissible([]string{""}, nil)).To(MatchError(domain.ErrEntryNotAdmissible))
		})

		It("refuses the same service twice", func() {
			Expect(admissible([]string{"pricing-service", "pricing-service"}, nil)).
				To(MatchError(domain.ErrEntryNotAdmissible))
		})

		It("refuses an approval of a service the plugin never declared", func() {
			// The subset rule is what lets a stored approval be read back as
			// "an operator saw this plugin ask for this".
			Expect(admissible([]string{"pricing-service"}, []string{"shipping-service"})).
				To(MatchError(domain.ErrEntryNotAdmissible))
		})

		It("caps how many services one plugin may ask about", func() {
			many := make([]string, domain.MaxBackendServices+1)
			for i := range many {
				many[i] = fmt.Sprintf("service-%d", i)
			}
			Expect(admissible(many, nil)).To(MatchError(domain.ErrEntryNotAdmissible))
		})

		It("refuses a manifest that asserts its own approval", func() {
			// The declaration is the plugin's to make and the approval is not.
			_, err := domain.ParseManifest([]byte(`{"id":"fleet-ops","name":"Fleet Ops","approvedBackendServices":["pricing-service"]}`))

			Expect(err).To(MatchError(domain.ErrSelfAssertedField))
		})

		It("lets a manifest declare, because that is the plugin describing itself", func() {
			e, err := domain.ParseManifest([]byte(`{"id":"fleet-ops","name":"Fleet Ops","backendServices":["pricing-service"]}`))

			Expect(err).ToNot(HaveOccurred())
			Expect(e.BackendServices).To(Equal([]string{"pricing-service"}))
		})
	})

	Context("an approval across a re-announcement", func() {
		// A manifest cannot carry an approval, so every re-announce arrives
		// without one. Without carry-over each heartbeat would silently revoke
		// what an operator granted.
		enabled := func(declared, approved []string) domain.Entry {
			return domain.Entry{
				ID:                      "fleet-ops",
				Name:                    "Fleet Ops",
				Enabled:                 true,
				Lifecycle:               domain.LifecycleDynamic,
				Remote:                  domain.Remote{URL: "http://localhost:7111/remoteEntry.js"},
				Contributions:           []domain.Contribution{{Kind: "route", ID: "fleet-ops-home", Path: "/fleet-ops", Component: "Home", Title: "Home"}},
				BackendServices:         declared,
				ApprovedBackendServices: approved,
			}
		}

		It("survives an ordinary re-announce", func() {
			existing := enabled([]string{"pricing-service"}, []string{"pricing-service"})
			incoming := enabled([]string{"pricing-service"}, nil)

			outcome, stored := domain.DecideAnnounce(&existing, incoming)

			Expect(outcome).To(Equal(domain.AnnounceConverged))
			Expect(stored.ApprovedBackendServices).To(Equal([]string{"pricing-service"}))
		})

		It("is narrowed when the publisher stops declaring the service", func() {
			existing := enabled([]string{"pricing-service"}, []string{"pricing-service"})
			incoming := enabled([]string{"shipping-service"}, nil)

			_, stored := domain.DecideAnnounce(&existing, incoming)

			Expect(stored.ApprovedBackendServices).To(BeEmpty())
			Expect(stored.EffectiveBackendServices()).To(BeEmpty())
		})

		It("gives a newly declared service nothing until an operator answers", func() {
			existing := enabled([]string{"pricing-service"}, []string{"pricing-service"})
			incoming := enabled([]string{"pricing-service", "shipping-service"}, nil)

			_, stored := domain.DecideAnnounce(&existing, incoming)

			Expect(stored.EffectiveBackendServices()).To(Equal([]string{"pricing-service"}))
		})

		It("gives an unknown plugin no approval at all", func() {
			outcome, stored := domain.DecideAnnounce(nil, enabled([]string{"pricing-service"}, nil))

			Expect(outcome).To(Equal(domain.AnnounceInserted))
			Expect(stored.ApprovedBackendServices).To(BeNil())
			Expect(stored.EffectiveBackendServices()).To(BeNil())
		})
	})
})

// signalsFor is what the worker hands SummarizeBackend: one signal per service
// the entry is effectively probed against, and a nil slice when nobody has
// answered for the plugin at all. The nil is the point — it is how "not
// configured" stays distinguishable from "nothing to ask".
func signalsFor(e domain.Entry) []domain.HealthSignal {
	ids := e.EffectiveBackendServices()
	if ids == nil {
		return nil
	}
	out := make([]domain.HealthSignal, 0, len(ids))
	for range ids {
		out = append(out, domain.HealthSignal{State: domain.HealthHealthy})
	}
	return out
}
