package dictionary

// Integration tests running against an embedded in-process NATS server (real
// JetStream, real KV). Covers:
//   - Shape A: ship command → event → projector → KV → query
//   - Shape B: ship command → event → Postgres (fake) + KV → cache hit/miss
//   - Shape C: multiple commands → full JetStream replay → fleet reconstruction
//   - Domain rules: every BR-XXX rule has an isolated spec

import (
	"context"
	"errors"
	"log/slog"
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
		ship = commands.NewShipHandler(jstream.NewPublisher(js), js)
	})

	It("projects ship state into KV after arrive / load / depart / unload", func() {
		const fleetCtx = "global"

		By("arriving at Hamburg")
		_, err := ship.ArrivePort(ctx, commands.ShipInput{
			Context: fleetCtx, ShipID: "orient-express", ShipName: "Orient Express", Port: "Hamburg",
		})
		Expect(err).NotTo(HaveOccurred())

		eventually(func() error {
			keys, err := kvA.Keys(ctx, fleetCtx)
			if err != nil || len(keys) == 0 {
				return errors.New("waiting for KV entry")
			}
			return nil
		})

		By("loading cargo")
		_, err = ship.LoadCargo(ctx, commands.ShipInput{
			Context: fleetCtx, ShipID: "orient-express",
			Cargo: &domain.Cargo{Description: "Electronics", Units: 42},
		})
		Expect(err).NotTo(HaveOccurred())

		By("departing Hamburg")
		_, err = ship.DepartPort(ctx, commands.ShipInput{
			Context: fleetCtx, ShipID: "orient-express", Port: "Hamburg",
		})
		Expect(err).NotTo(HaveOccurred())

		By("verifying KV reflects at-sea state with cargo")
		eventually(func() error {
			q := queries.NewShapeA(kvA)
			ships, err := q.ListShips(ctx, fleetCtx)
			if err != nil {
				return err
			}
			if len(ships) == 0 {
				return errors.New("no ships in KV")
			}
			s := ships[0]
			if s.CurrentPort != "" {
				return errors.New("expected at sea")
			}
			if len(s.Cargo) != 1 || s.Cargo[0].Units != 42 {
				return errors.New("cargo not projected yet")
			}
			return nil
		})

		By("arriving at Rotterdam")
		_, err = ship.ArrivePort(ctx, commands.ShipInput{
			Context: fleetCtx, ShipID: "orient-express", Port: "Rotterdam",
		})
		Expect(err).NotTo(HaveOccurred())

		By("unloading cargo")
		eventually(func() error {
			_, err = ship.UnloadCargo(ctx, commands.ShipInput{
				Context: fleetCtx, ShipID: "orient-express",
				Cargo: &domain.Cargo{Description: "Electronics", Units: 42},
			})
			return err
		})

		By("verifying cargo is empty in KV")
		eventually(func() error {
			q := queries.NewShapeA(kvA)
			ships, err := q.ListShips(ctx, fleetCtx)
			if err != nil {
				return err
			}
			for _, s := range ships {
				if s.ShipID == "orient-express" && len(s.Cargo) != 0 {
					return errors.New("cargo not cleared in KV yet")
				}
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
		ship = commands.NewShipHandler(jstream.NewPublisher(js), js)
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
	It("reconstructs fleet state by replaying JetStream from seq=1", func() {
		ctx := context.Background()
		js := newJetStream()
		ship := commands.NewShipHandler(jstream.NewPublisher(js), js)
		const fleetCtx = "global"

		steps := []func() error{
			func() error {
				_, err := ship.ArrivePort(ctx, commands.ShipInput{
					Context: fleetCtx, ShipID: "orient-express", ShipName: "Orient Express", Port: "Hamburg",
				})
				return err
			},
			func() error {
				_, err := ship.ArrivePort(ctx, commands.ShipInput{
					Context: fleetCtx, ShipID: "pacific-star", ShipName: "Pacific Star", Port: "Rotterdam",
				})
				return err
			},
			func() error {
				_, err := ship.LoadCargo(ctx, commands.ShipInput{
					Context: fleetCtx, ShipID: "orient-express",
					Cargo: &domain.Cargo{Description: "Electronics", Units: 42},
				})
				return err
			},
			func() error {
				_, err := ship.DepartPort(ctx, commands.ShipInput{
					Context: fleetCtx, ShipID: "orient-express", Port: "Hamburg",
				})
				return err
			},
		}
		for _, step := range steps {
			Expect(step()).To(Succeed())
		}

		q := queries.NewShapeC(js)
		fleet, err := q.ReconstructFleet(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(fleet).To(HaveLen(2))

		byID := make(map[string]domain.ShipState)
		for _, s := range fleet {
			byID[s.ShipID] = s
		}

		oe := byID["orient-express"]
		Expect(oe.CurrentPort).To(BeEmpty(), "orient-express should be at sea")
		Expect(oe.Cargo).To(HaveLen(1))
		Expect(oe.Cargo[0].Units).To(Equal(42))

		ps := byID["pacific-star"]
		Expect(ps.CurrentPort).To(Equal("Rotterdam"))
	})
})

// ─── Domain rules ─────────────────────────────────────────────────────────────
//
// Each spec maps directly to a rule in BUSINESS_RULES.md.

var _ = Describe("Domain Rules", func() {
	var (
		ctx  context.Context
		ship *commands.ShipHandler
	)

	BeforeEach(func() {
		ctx = context.Background()
		js := newJetStream()
		ship = commands.NewShipHandler(jstream.NewPublisher(js), js)
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

	Context("BR-004: cannot load cargo unless docked", func() {
		It("returns ErrNotInPort", func() {
			_, err := ship.LoadCargo(ctx, commands.ShipInput{
				Context: fleetCtx, ShipID: "br004-vessel",
				Cargo: &domain.Cargo{Description: "Steel", Units: 10},
			})
			Expect(errors.Is(err, domain.ErrNotInPort)).To(BeTrue())
		})
	})

	Context("BR-005: cannot unload cargo unless docked", func() {
		It("returns ErrNotInPort", func() {
			_, err := ship.UnloadCargo(ctx, commands.ShipInput{
				Context: fleetCtx, ShipID: "br005-vessel",
				Cargo: &domain.Cargo{Description: "Steel", Units: 10},
			})
			Expect(errors.Is(err, domain.ErrNotInPort)).To(BeTrue())
		})
	})

	Context("BR-006: cannot unload cargo not in the manifest", func() {
		It("returns ErrCargoNotFound", func() {
			Expect(ship.ArrivePort(ctx, commands.ShipInput{
				Context: fleetCtx, ShipID: "br006-vessel", ShipName: "BR006", Port: "Hamburg",
			})).Error().NotTo(HaveOccurred())
			Expect(ship.LoadCargo(ctx, commands.ShipInput{
				Context: fleetCtx, ShipID: "br006-vessel",
				Cargo: &domain.Cargo{Description: "Electronics", Units: 10},
			})).Error().NotTo(HaveOccurred())

			_, err := ship.UnloadCargo(ctx, commands.ShipInput{
				Context: fleetCtx, ShipID: "br006-vessel",
				Cargo: &domain.Cargo{Description: "Nonexistent", Units: 1},
			})
			Expect(errors.Is(err, domain.ErrCargoNotFound)).To(BeTrue())
		})
	})

	Context("BR-007: cargo payload is required", func() {
		It("rejects nil cargo on load", func() {
			_, err := ship.LoadCargo(ctx, commands.ShipInput{
				Context: fleetCtx, ShipID: "br007-vessel", Cargo: nil,
			})
			Expect(err).To(MatchError("cargo is required"))
		})

		It("rejects nil cargo on unload", func() {
			_, err := ship.UnloadCargo(ctx, commands.ShipInput{
				Context: fleetCtx, ShipID: "br007-vessel", Cargo: nil,
			})
			Expect(err).To(MatchError("cargo is required"))
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
