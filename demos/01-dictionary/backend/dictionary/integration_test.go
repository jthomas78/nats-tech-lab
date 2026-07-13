package dictionary

// Integration tests running against an embedded in-process NATS server (real
// JetStream, real KV). Covers:
//   - Shape A: ship command → event → projector → KV → query
//   - Shape B: ship command → event → Postgres (fake) + KV → cache hit/miss
//   - Shape C: ships + containers → full JetStream replay → fleet reconstruction
//   - Ship domain rules: BR-001 … BR-003, BR-017 (container rules live in container_test.go)

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/dictionary/internal/application/commands"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/dictionary/internal/application/queries"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/dictionary/internal/domain"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/dictionary/internal/eventhandler"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/internal/jstream"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/internal/kvstore"
)

// ─── Helpers ─────────────────────────────────────────────────────────────────

func newJetStream() jetstream.JetStream {
	GinkgoHelper()
	opts := &server.Options{JetStream: true, StoreDir: GinkgoT().TempDir(), Port: -1}
	srv, err := server.NewServer(opts)
	Expect(err).NotTo(HaveOccurred())
	srv.Start()
	DeferCleanup(srv.Shutdown)
	Expect(srv.ReadyForConnections(10 * time.Second)).To(BeTrue())

	nc, err := nats.Connect(srv.ClientURL())
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(nc.Close)

	js, err := jetstream.New(nc)
	Expect(err).NotTo(HaveOccurred())

	_, err = jstream.CreateStream(context.Background(), js, domain.StreamName, domain.StreamSubjects())
	Expect(err).NotTo(HaveOccurred())
	return js
}

// eventually retries fn until it returns nil or the timeout elapses.
func eventually(fn func() error) {
	GinkgoHelper()
	Eventually(fn, 5*time.Second, 25*time.Millisecond).Should(Succeed())
}

// ─── Shape A ─────────────────────────────────────────────────────────────────

var _ = Describe("Shape A — KV as read model", func() {
	var (
		ctx  context.Context
		ship *commands.ShipHandler
		kvA  *kvstore.Store
	)

	BeforeEach(func() {
		ctx = context.Background()
		js := newJetStream()
		kvA = kvstore.New(js, "dict-a")
		log := slog.New(slog.DiscardHandler)
		consume, err := eventhandler.RegisterShapeA(ctx, js, kvA, log)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(consume.Stop)
		ship = commands.NewShipHandler(jstream.NewPublisher(js), js, newFakePortRepo())
	})

	It("projects ship state into KV after arrive / depart", func() {
		const fleetCtx = "global"

		By("arriving at Hamburg")
		_, err := ship.ArrivePort(ctx, commands.ShipInput{
			Context: fleetCtx, ShipID: "orient-express", ShipName: "Orient Express", Port: "Hamburg",
		})
		Expect(err).NotTo(HaveOccurred())

		By("verifying KV reflects the docked state")
		eventually(func() error {
			q := queries.NewShapeA(kvA)
			ships, err := q.ListShips(ctx, fleetCtx)
			if err != nil {
				return err
			}
			if len(ships) != 1 {
				return errors.New("waiting for KV entry")
			}
			s := ships[0]
			if s.CurrentPort != "Hamburg" || s.Status != domain.StatusDocked {
				return errors.New("docked state not projected yet")
			}
			return nil
		})

		By("departing Hamburg")
		_, err = ship.DepartPort(ctx, commands.ShipInput{
			Context: fleetCtx, ShipID: "orient-express", Port: "Hamburg",
		})
		Expect(err).NotTo(HaveOccurred())

		By("verifying KV reflects the at-sea state")
		eventually(func() error {
			q := queries.NewShapeA(kvA)
			ships, err := q.ListShips(ctx, fleetCtx)
			if err != nil {
				return err
			}
			if len(ships) != 1 {
				return errors.New("no ships in KV")
			}
			s := ships[0]
			if s.CurrentPort != "" || s.Status != domain.StatusInTransit {
				return errors.New("at-sea state not projected yet")
			}
			return nil
		})
	})
})

// ─── Shape B ─────────────────────────────────────────────────────────────────

var _ = Describe("Shape B — KV cache in front of Postgres", func() {
	var (
		ctx  context.Context
		ship *commands.ShipHandler
		q    *queries.ShapeB
	)

	BeforeEach(func() {
		ctx = context.Background()
		js := newJetStream()
		kvB := kvstore.New(js, "dict-b")
		repo := newFakeRepo()
		log := slog.New(slog.DiscardHandler)
		consume, err := eventhandler.RegisterShapeB(ctx, js, kvB, repo, log)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(consume.Stop)
		ship = commands.NewShipHandler(jstream.NewPublisher(js), js, newFakePortRepo())
		q = queries.NewShapeB(kvB, repo)
	})

	It("warms the cache on arrive and falls through to Postgres on eviction", func() {
		const fleetCtx = "global"

		_, err := ship.ArrivePort(ctx, commands.ShipInput{
			Context: fleetCtx, ShipID: "pacific-star", ShipName: "Pacific Star", Port: "Singapore",
		})
		Expect(err).NotTo(HaveOccurred())

		By("expecting a cache hit after projection")
		eventually(func() error {
			s, cacheHit, err := q.GetShip(ctx, fleetCtx, "pacific-star")
			if err != nil {
				return err
			}
			if !cacheHit {
				return errors.New("expected cache hit after projection")
			}
			if s.ShipName != "Pacific Star" {
				return errors.New("unexpected ship name")
			}
			return nil
		})

		By("evicting the cache entry")
		Expect(q.EvictCacheShip(ctx, fleetCtx, "pacific-star")).To(Succeed())

		By("expecting a cache miss with Postgres fallthrough")
		s, cacheHit, err := q.GetShip(ctx, fleetCtx, "pacific-star")
		Expect(err).NotTo(HaveOccurred())
		Expect(cacheHit).To(BeFalse(), "expected cache miss after eviction")
		Expect(s.ShipName).To(Equal("Pacific Star"))

		By("expecting the cache to be backfilled on next read")
		eventually(func() error {
			_, cacheHit, err := q.GetShip(ctx, fleetCtx, "pacific-star")
			if err != nil {
				return err
			}
			if !cacheHit {
				return errors.New("expected cache hit after backfill")
			}
			return nil
		})

		By("expecting ErrNotFound for an unknown ship")
		_, _, err = q.GetShip(ctx, fleetCtx, "unknown-vessel")
		Expect(errors.Is(err, domain.ErrNotFound)).To(BeTrue())
	})
})

// ─── Shape C ─────────────────────────────────────────────────────────────────

var _ = Describe("Shape C — pure event sourcing reconstruction", func() {
	It("reconstructs ships, containers, and manifests by replaying JetStream from seq=1", func() {
		ctx := context.Background()
		js := newJetStream()
		pub := jstream.NewPublisher(js)
		portRepo := newFakePortRepo()
		ships := commands.NewShipHandler(pub, js, portRepo)
		containers := commands.NewContainerHandler(pub, js, portRepo)
		const fleetCtx = "global"

		steps := []func() error{
			func() error {
				_, err := ships.ArrivePort(ctx, commands.ShipInput{
					Context: fleetCtx, ShipID: "orient-express", ShipName: "Orient Express", Port: "Hamburg",
				})
				return err
			},
			func() error {
				_, err := ships.ArrivePort(ctx, commands.ShipInput{
					Context: fleetCtx, ShipID: "pacific-star", ShipName: "Pacific Star", Port: "Rotterdam",
				})
				return err
			},
			func() error {
				_, err := containers.RegisterContainer(ctx, commands.ContainerInput{
					Context: fleetCtx, ContainerID: "TCKU1234567",
					Cargo: "Electronics", OriginPort: "Hamburg", DestPort: "Singapore",
				})
				return err
			},
			func() error {
				_, err := containers.LoadContainer(ctx, commands.ContainerInput{
					Context: fleetCtx, ContainerID: "TCKU1234567", ShipID: "orient-express",
				})
				return err
			},
			func() error {
				_, err := ships.DepartPort(ctx, commands.ShipInput{
					Context: fleetCtx, ShipID: "orient-express", Port: "Hamburg",
				})
				return err
			},
		}
		for _, step := range steps {
			Expect(step()).To(Succeed())
		}

		q := queries.NewShapeC(js)
		result, err := q.ReconstructFleet(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Fleet).To(HaveLen(2))
		Expect(result.Containers).To(HaveLen(1))

		byID := make(map[string]queries.ShipWithManifest)
		for _, s := range result.Fleet {
			byID[s.ShipID] = s
		}

		By("orient-express is at sea, still carrying the loaded container")
		oe := byID["orient-express"]
		Expect(oe.CurrentPort).To(BeEmpty())
		Expect(oe.Manifest).To(HaveLen(1))
		Expect(oe.Manifest[0].ContainerID).To(Equal("TCKU1234567"))

		By("pacific-star is docked at Rotterdam with an empty manifest")
		ps := byID["pacific-star"]
		Expect(ps.CurrentPort).To(Equal("Rotterdam"))
		Expect(ps.Manifest).To(BeEmpty())

		By("the container is on-ship, reconstructed from events alone")
		c := result.Containers[0]
		Expect(c.Status).To(Equal(domain.ContainerOnShip))
		Expect(c.OnShipID).To(HaveValue(Equal("orient-express")))
		Expect(c.TerminalPort).To(BeNil())
	})
})

// ─── Ship domain rules ────────────────────────────────────────────────────────
//
// Each spec maps directly to a rule in BUSINESS_RULES.md. BR-004 … BR-007 were
// retired in Phase 8 (cargo moved to the container aggregate); the container
// rules BR-008 … BR-015 live in container_test.go.

var _ = Describe("Domain Rules", func() {
	var (
		ctx  context.Context
		ship *commands.ShipHandler
	)

	BeforeEach(func() {
		ctx = context.Background()
		js := newJetStream()
		ship = commands.NewShipHandler(jstream.NewPublisher(js), js, newFakePortRepo())
	})

	const fleetCtx = "global"

	Context("BR-001: cannot arrive at port already docked at", func() {
		It("returns ErrAlreadyDocked", func() {
			Expect(ship.ArrivePort(ctx, commands.ShipInput{
				Context: fleetCtx, ShipID: "br001-vessel", ShipName: "BR001", Port: "Hamburg",
			})).Error().NotTo(HaveOccurred())

			_, err := ship.ArrivePort(ctx, commands.ShipInput{
				Context: fleetCtx, ShipID: "br001-vessel", Port: "Hamburg",
			})
			Expect(errors.Is(err, domain.ErrAlreadyDocked)).To(BeTrue())
		})
	})

	Context("BR-002: must depart before arriving at a new port", func() {
		It("returns ErrMustDepart", func() {
			Expect(ship.ArrivePort(ctx, commands.ShipInput{
				Context: fleetCtx, ShipID: "br002-vessel", ShipName: "BR002", Port: "Hamburg",
			})).Error().NotTo(HaveOccurred())

			_, err := ship.ArrivePort(ctx, commands.ShipInput{
				Context: fleetCtx, ShipID: "br002-vessel", Port: "Rotterdam",
			})
			Expect(errors.Is(err, domain.ErrMustDepart)).To(BeTrue())
		})
	})

	Context("BR-003: cannot depart a port the ship is not at", func() {
		It("returns ErrNotDocked", func() {
			_, err := ship.DepartPort(ctx, commands.ShipInput{
				Context: fleetCtx, ShipID: "br003-vessel", Port: "Hamburg",
			})
			Expect(errors.Is(err, domain.ErrNotDocked)).To(BeTrue())
		})
	})

	Context("BR-017: cannot arrive at a port that is not registered", func() {
		It("returns ErrUnknownPort", func() {
			_, err := ship.ArrivePort(ctx, commands.ShipInput{
				Context: fleetCtx, ShipID: "br017-vessel", ShipName: "BR017", Port: "Atlantis",
			})
			Expect(errors.Is(err, domain.ErrUnknownPort)).To(BeTrue())
		})
	})
})

// ─── fakeRepo ────────────────────────────────────────────────────────────────

type fakeRepo struct {
	mu    sync.Mutex
	ships map[string]domain.ShipState
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{ships: make(map[string]domain.ShipState)}
}

func (r *fakeRepo) key(kvContext, shipID string) string { return kvContext + "/" + shipID }

func (r *fakeRepo) Upsert(_ context.Context, state domain.ShipState) (domain.ShipState, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ships[r.key(state.Context, state.ShipID)] = state
	return state, nil
}

func (r *fakeRepo) Find(_ context.Context, kvContext, shipID string) (domain.ShipState, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.ships[r.key(kvContext, shipID)]
	if !ok {
		return domain.ShipState{}, domain.ErrNotFound
	}
	return s, nil
}

func (r *fakeRepo) List(_ context.Context, kvContext string) ([]domain.ShipState, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []domain.ShipState
	for _, s := range r.ships {
		if s.Context == kvContext {
			out = append(out, s)
		}
	}
	return out, nil
}

// ─── fakePortRepo ────────────────────────────────────────────────────────────

// defaultTestPorts mirrors the production seedDefaultPorts list (postgres
// package) so existing scenarios keep working under BR-017/BR-018 without
// every test explicitly registering a port first.
var defaultTestPorts = []string{"Hamburg", "Rotterdam", "Singapore", "New York", "Shanghai", "Sydney"}
var defaultTestContexts = []string{"global", "atlantic-fleet", "pacific-fleet"}

type fakePortRepo struct {
	mu    sync.Mutex
	known map[string]bool
}

func newFakePortRepo() *fakePortRepo {
	r := &fakePortRepo{known: make(map[string]bool)}
	for _, kvContext := range defaultTestContexts {
		for _, name := range defaultTestPorts {
			r.known[r.key(kvContext, name)] = true
		}
	}
	return r
}

func (r *fakePortRepo) key(kvContext, name string) string { return kvContext + "|" + name }

func (r *fakePortRepo) Exists(_ context.Context, kvContext, name string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.known[r.key(kvContext, name)], nil
}

func (r *fakePortRepo) Register(_ context.Context, kvContext, name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.known[r.key(kvContext, name)] = true
	return nil
}

func (r *fakePortRepo) List(_ context.Context, kvContext string) ([]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	prefix := kvContext + "|"
	for k := range r.known {
		if strings.HasPrefix(k, prefix) {
			out = append(out, strings.TrimPrefix(k, prefix))
		}
	}
	sort.Strings(out)
	return out, nil
}
