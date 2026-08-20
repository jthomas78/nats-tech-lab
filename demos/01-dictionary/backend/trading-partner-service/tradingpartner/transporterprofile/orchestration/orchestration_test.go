package orchestration_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	server "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	tradingcommands "github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/trading-partner-service/tradingpartner/internal/application/commands"
	tradingdomain "github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/trading-partner-service/tradingpartner/internal/domain"
	profiledomain "github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/trading-partner-service/tradingpartner/transporterprofile/domain"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/trading-partner-service/tradingpartner/transporterprofile/orchestration"
)

type memoryEventStore struct {
	mu          sync.Mutex
	events      map[string][]profiledomain.Event
	hydrated    atomic.Int32
	hydrateGate chan struct{}
}

func newMemoryEventStore() *memoryEventStore {
	return &memoryEventStore{events: make(map[string][]profiledomain.Event)}
}

func (s *memoryEventStore) Hydrate(_ context.Context, contextKey, id string) (*profiledomain.TransporterProfile, uint64, error) {
	if s.hydrateGate != nil && s.hydrated.Add(1) <= 2 {
		<-s.hydrateGate
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	agg := &profiledomain.TransporterProfile{}
	for _, event := range s.events[contextKey+"/"+id] {
		agg.Apply(event)
	}
	return agg, uint64(len(s.events[contextKey+"/"+id])), nil
}

func (s *memoryEventStore) Append(_ context.Context, contextKey, id string, event profiledomain.Event, expected uint64) (uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := contextKey + "/" + id
	if uint64(len(s.events[key])) != expected {
		return 0, orchestration.ErrSequenceConflict
	}
	s.events[key] = append(s.events[key], event)
	return uint64(len(s.events[key])), nil
}

func (s *memoryEventStore) eventCount(contextKey, id string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.events[contextKey+"/"+id])
}

type fakePartnerGateway struct {
	partner       tradingdomain.TradingPartner
	activateCalls int
	suspendCalls  int
}

func (g *fakePartnerGateway) Get(context.Context, string) (tradingdomain.TradingPartner, error) {
	return g.partner, nil
}

func (g *fakePartnerGateway) Activate(_ context.Context, _ tradingcommands.Actor, _ string) (tradingdomain.TradingPartner, error) {
	g.activateCalls++
	g.partner.Status = tradingdomain.StatusActive
	return g.partner, nil
}

func (g *fakePartnerGateway) Suspend(_ context.Context, _ tradingcommands.Actor, _ string, reason string) (tradingdomain.TradingPartner, error) {
	g.suspendCalls++
	g.partner.Status = tradingdomain.StatusSuspended
	_ = reason
	return g.partner, nil
}

type fakeCanonicalProjection struct {
	state profiledomain.State
	err   error
	reads int
}

func (r *fakeCanonicalProjection) Get(context.Context, string) (profiledomain.State, error) {
	r.reads++
	return r.state, r.err
}

type recordingProjection struct {
	mu     sync.Mutex
	writes int
	last   profiledomain.State
	cache  int
}

func (p *recordingProjection) Upsert(_ context.Context, state profiledomain.State) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.writes++
	p.last = state
	return nil
}

func (p *recordingProjection) Put(_ context.Context, state profiledomain.State) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cache++
	p.last = state
	return nil
}

func (p *recordingProjection) counts() (int, int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.writes, p.cache
}

func testJetStream() (*nats.Conn, jetstream.JetStream) {
	GinkgoHelper()
	srv, err := server.NewServer(&server.Options{JetStream: true, StoreDir: GinkgoT().TempDir(), Port: -1})
	Expect(err).NotTo(HaveOccurred())
	srv.Start()
	Expect(srv.ReadyForConnections(5 * time.Second)).To(BeTrue())
	DeferCleanup(srv.Shutdown)

	nc, err := nats.Connect(srv.ClientURL(), nats.Name("transporterprofile-test"))
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(nc.Close)
	js, err := jetstream.New(nc)
	Expect(err).NotTo(HaveOccurred())
	return nc, js
}

var _ = Describe("TransporterProfile orchestration", func() {
	Context("BR-TP18 CreateTransporterProfile / EnsureTransporterProfile", func() {
		It("uses the partner ID, starts AwaitingDocumentation, stays idempotent, and converges concurrent creates on one event", func() {
			ctx := context.Background()
			store := newMemoryEventStore()
			handler := orchestration.NewProfileHandler(store)

			created, err := handler.CreateTransporterProfile(ctx, "acme", "partner-1")
			Expect(err).NotTo(HaveOccurred())
			Expect(created.ID).To(Equal("partner-1"))
			Expect(created.Status).To(Equal(profiledomain.StatusAwaitingDocumentation))

			again, err := handler.EnsureTransporterProfile(ctx, "acme", "partner-1")
			Expect(err).NotTo(HaveOccurred())
			Expect(again).To(Equal(created))
			Expect(store.eventCount("acme", "partner-1")).To(Equal(1))

			concurrent := newMemoryEventStore()
			concurrent.hydrateGate = make(chan struct{})
			concurrentHandler := orchestration.NewProfileHandler(concurrent)
			results := make(chan profiledomain.State, 2)
			errs := make(chan error, 2)
			for range 2 {
				go func() {
					state, createErr := concurrentHandler.CreateTransporterProfile(ctx, "acme", "partner-2")
					results <- state
					errs <- createErr
				}()
			}
			Eventually(concurrent.hydrated.Load).Should(BeNumerically(">=", 2))
			close(concurrent.hydrateGate)
			for range 2 {
				Expect(<-errs).NotTo(HaveOccurred())
				Expect((<-results).ID).To(Equal("partner-2"))
			}
			Expect(concurrent.eventCount("acme", "partner-2")).To(Equal(1))
		})
	})

	Context("BR-TP19 Transporter activation gate", func() {
		It("rejects missing/non-Vetted profiles via the canonical reader, permits Vetted transporters, and leaves SHIPPER behavior unchanged", func() {
			ctx := context.Background()
			actor := tradingcommands.Actor{Name: "operator"}
			partner := tradingdomain.TradingPartner{ID: "partner-1", Type: tradingdomain.PartnerTypeTransporter, Status: tradingdomain.StatusRegistered}

			missingGateway := &fakePartnerGateway{partner: partner}
			missingProjection := &fakeCanonicalProjection{err: profiledomain.ErrNotFound}
			missing := orchestration.NewActivationHandler(missingGateway, missingProjection)
			_, err := missing.Activate(ctx, actor, partner.ID)
			Expect(err).To(MatchError(orchestration.ErrTransporterProfileNotVetted))
			Expect(missingGateway.activateCalls).To(BeZero())
			Expect(missingGateway.partner.Status).To(Equal(tradingdomain.StatusRegistered))

			pendingGateway := &fakePartnerGateway{partner: partner}
			pendingProjection := &fakeCanonicalProjection{state: profiledomain.State{ID: partner.ID, Status: profiledomain.StatusAwaitingDocumentation}}
			pending := orchestration.NewActivationHandler(pendingGateway, pendingProjection)
			_, err = pending.Activate(ctx, actor, partner.ID)
			Expect(err).To(MatchError(orchestration.ErrTransporterProfileNotVetted))
			Expect(pendingGateway.activateCalls).To(BeZero())

			vettedGateway := &fakePartnerGateway{partner: partner}
			vettedProjection := &fakeCanonicalProjection{state: profiledomain.State{ID: partner.ID, Status: profiledomain.StatusVetted}}
			vetted := orchestration.NewActivationHandler(vettedGateway, vettedProjection)
			activated, err := vetted.Activate(ctx, actor, partner.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(activated.Status).To(Equal(tradingdomain.StatusActive))
			Expect(vettedProjection.reads).To(Equal(1), "the canonical Postgres projection is read directly")

			shipperGateway := &fakePartnerGateway{partner: tradingdomain.TradingPartner{ID: "shipper-1", Type: tradingdomain.PartnerTypeShipper, Status: tradingdomain.StatusRegistered}}
			unusedProjection := &fakeCanonicalProjection{err: errors.New("must not be called")}
			shipper := orchestration.NewActivationHandler(shipperGateway, unusedProjection)
			activated, err = shipper.Activate(ctx, actor, "shipper-1")
			Expect(err).NotTo(HaveOccurred())
			Expect(activated.Status).To(Equal(tradingdomain.StatusActive))
			Expect(unusedProjection.reads).To(BeZero())
		})
	})

	Context("BR-TP20 aggregate-wide optimistic sequence guard", func() {
		It("guards the aggregate wildcard with the hydrated sequence and rejects a stale append after another event leaf advances", func() {
			ctx := context.Background()
			_, js := testJetStream()
			Expect(orchestration.EnsureStream(ctx, js)).To(Succeed())
			store := orchestration.NewJetStreamEventStore(js)
			projection := &recordingProjection{}
			projector := orchestration.NewProjector(js, projection, projection)
			Expect(projector.Start(ctx)).To(Succeed())
			DeferCleanup(projector.Stop)

			id := "partner-guarded"
			event := profiledomain.NewCreatedEvent("acme", id)
			_, err := store.Append(ctx, "acme", id, event, 0)
			Expect(err).NotTo(HaveOccurred())
			Eventually(func() []int {
				writes, cache := projection.counts()
				return []int{writes, cache}
			}).Should(Equal([]int{1, 1}))

			stream, err := js.Stream(ctx, profiledomain.StreamName)
			Expect(err).NotTo(HaveOccurred())
			stored, err := stream.GetMsg(ctx, 1)
			Expect(err).NotTo(HaveOccurred())
			Expect(stored.Header.Get(jetstream.ExpectedLastSubjSeqHeader)).To(Equal("0"))
			Expect(stored.Header.Get(jetstream.ExpectedLastSubjSeqSubjHeader)).To(Equal(profiledomain.InstanceSubject("acme", id)))

			_, observed, err := store.Hydrate(ctx, "acme", id)
			Expect(err).NotTo(HaveOccurred())
			Expect(observed).To(Equal(uint64(1)))

			_, err = js.Publish(ctx, profiledomain.Subject("acme", id, "details-updated"), []byte(`{"context":"acme","tradingPartnerID":"partner-guarded"}`))
			Expect(err).NotTo(HaveOccurred())
			_, err = store.Append(ctx, "acme", id, event, observed)
			Expect(err).To(MatchError(orchestration.ErrSequenceConflict))

			Consistently(func() []int {
				writes, cache := projection.counts()
				return []int{writes, cache}
			}, 200*time.Millisecond).Should(Equal([]int{1, 1}))
			info, err := stream.Info(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(info.State.Msgs).To(Equal(uint64(2)), "the losing append must not create a third event")
		})
	})

	Context("BR-TP28 late GIT invalidation", func() {
		It("appends FleetAvailabilityRevoked before suspending through one idempotent orchestration command", func() {
			ctx := context.Background()
			store := newMemoryEventStore()
			handler := orchestration.NewProfileHandler(store)
			_, err := handler.CreateTransporterProfile(ctx, "acme", "partner-drop")
			Expect(err).NotTo(HaveOccurred())
			Expect(handler.RecordVetted(ctx, "acme", "partner-drop", 1, 10)).To(Succeed())

			partners := &fakePartnerGateway{partner: tradingdomain.TradingPartner{ID: "partner-drop", Type: tradingdomain.PartnerTypeTransporter, Status: tradingdomain.StatusActive}}
			drop := orchestration.NewGitStatusDropHandler("acme", store, partners)
			Expect(drop.HandleGitStatusDrop(ctx, "partner-drop")).To(Succeed())

			aggregate, _, err := store.Hydrate(ctx, "acme", "partner-drop")
			Expect(err).NotTo(HaveOccurred())
			Expect(aggregate.State().FleetAvailabilityGate).To(BeFalse())
			Expect(partners.partner.Status).To(Equal(tradingdomain.StatusSuspended))
			Expect(partners.suspendCalls).To(Equal(1))
			Expect(store.events["acme/partner-drop"][2].Type).To(Equal(profiledomain.FleetAvailabilityRevokedEvent), "revocation must be appended before Suspend")

			Expect(drop.HandleGitStatusDrop(ctx, "partner-drop")).To(Succeed())
			Expect(partners.suspendCalls).To(Equal(1))
			Expect(store.eventCount("acme", "partner-drop")).To(Equal(3))
		})
	})
})
