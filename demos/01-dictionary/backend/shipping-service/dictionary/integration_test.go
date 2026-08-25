package dictionary

// Integration tests running against an embedded in-process NATS server (real
// JetStream, real KV). Covers:
//   - Shape B: ship command → event → Postgres (fake) + KV → cache hit/miss
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

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/application/commands"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/application/queries"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/domain"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/eventhandler"
	"github.com/jthomas78/nats-tech-lab/shared/jstream"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/internal/kvstore"
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

	nc, err := nats.Connect(srv.ClientURL(), nats.Name("shipping-service-test"))
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(nc.Close)
	Expect(nc.Opts.Name).NotTo(BeEmpty(), "nats connection must be named")

	js, err := jetstream.New(nc)
	Expect(err).NotTo(HaveOccurred())

	_, err = jstream.CreateStream(context.Background(), js, domain.StreamName, domain.StreamSubjects())
	Expect(err).NotTo(HaveOccurred())
	return js
}

// streamMsgs reports how many messages the SHIPPING stream currently holds.
func streamMsgs(ctx context.Context, js jetstream.JetStream) uint64 {
	GinkgoHelper()
	stream, err := js.Stream(ctx, domain.StreamName)
	Expect(err).NotTo(HaveOccurred())
	info, err := stream.Info(ctx)
	Expect(err).NotTo(HaveOccurred())
	return info.State.Msgs
}

// eventually retries fn until it returns nil or the timeout elapses.
func eventually(fn func() error) {
	GinkgoHelper()
	Eventually(fn, 5*time.Second, 25*time.Millisecond).Should(Succeed())
}

// ─── Shape B ─────────────────────────────────────────────────────────────────

var _ = Describe("Shape B — KV cache in front of Postgres", func() {
	var (
		ctx  context.Context
		ship *commands.ShipHandler
		q    *queries.Ships
	)

	BeforeEach(func() {
		ctx = context.Background()
		js := newJetStream()
		kvB := kvstore.New(js, "ships")
		repo := newFakeRepo()
		log := slog.New(slog.DiscardHandler)
		consume, err := eventhandler.RegisterShips(ctx, js, kvB, nil, repo, log)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(consume.Stop)
		ship = commands.NewShipHandler(jstream.NewPublisher(js), js, newFakePortRepo())
		q = queries.NewShips(kvB, repo)
	})

	It("warms the cache on arrive and falls through to Postgres on eviction", func() {
		const fleetCtx = "acme-pacific-fleet"

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

// ─── Ship domain rules ────────────────────────────────────────────────────────
//
// Each spec maps directly to a rule in BUSINESS_RULES.md. BR-004 … BR-007 were
// retired in Phase 8 (cargo moved to the container aggregate); the container
// rules BR-008 … BR-015 live in container_test.go.

var _ = Describe("Domain Rules", func() {
	var (
		ctx  context.Context
		ship *commands.ShipHandler
		js   jetstream.JetStream
	)

	BeforeEach(func() {
		ctx = context.Background()
		js = newJetStream()
		ship = commands.NewShipHandler(jstream.NewPublisher(js), js, newFakePortRepo())
	})

	const fleetCtx = "acme-pacific-fleet"

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

		It("does not register the ship on the way to rejecting it", func() {
			_, err := ship.ArrivePort(ctx, commands.ShipInput{
				Context: fleetCtx, ShipID: "br017-vessel", ShipName: "BR017", Port: "Atlantis",
			})
			Expect(errors.Is(err, domain.ErrUnknownPort)).To(BeTrue())
			Expect(streamMsgs(ctx, js)).To(BeZero())
		})
	})

	// BR-050: a command that returns an error publishes no events. ArrivePort
	// is the only command that emits two (the implicit .registered of BR-021
	// followed by .arrived), so it is the only one that can half-commit.
	Context("BR-050: a rejected command publishes no events", func() {
		It("leaves the stream empty when the arrival is rejected", func() {
			_, err := ship.ArrivePort(ctx, commands.ShipInput{
				Context: fleetCtx, ShipID: "br050-vessel", ShipName: "BR050", Port: "Atlantis",
			})
			Expect(err).To(HaveOccurred())
			Expect(streamMsgs(ctx, js)).To(BeZero())
		})

		It("leaves the shipID free to register afterwards", func() {
			_, err := ship.ArrivePort(ctx, commands.ShipInput{
				Context: fleetCtx, ShipID: "br050-vessel", ShipName: "BR050", Port: "Atlantis",
			})
			Expect(err).To(HaveOccurred())

			// The surrogate id is minted into the log and is immutable, so a
			// half-committed registration would take the natural key for good.
			Expect(ship.RegisterShip(ctx, commands.ShipInput{
				Context: fleetCtx, ShipID: "br050-vessel", ShipName: "BR050",
			})).Error().NotTo(HaveOccurred())
		})
	})

	Context("BR-020: shipID and context must be valid subject/KV-bucket tokens", func() {
		It("accepts a valid lowercase-hyphenated shipID and context", func() {
			_, err := ship.ArrivePort(ctx, commands.ShipInput{
				Context: fleetCtx, ShipID: "br020-vessel", ShipName: "BR020", Port: "Hamburg",
			})
			Expect(err).NotTo(HaveOccurred())
		})

		It("rejects an empty shipID", func() {
			_, err := ship.ArrivePort(ctx, commands.ShipInput{
				Context: fleetCtx, ShipID: "", ShipName: "BR020", Port: "Hamburg",
			})
			Expect(errors.Is(err, domain.ErrInvalidToken)).To(BeTrue())
		})

		It("rejects a shipID containing a dot", func() {
			_, err := ship.ArrivePort(ctx, commands.ShipInput{
				Context: fleetCtx, ShipID: "br020.vessel", ShipName: "BR020", Port: "Hamburg",
			})
			Expect(errors.Is(err, domain.ErrInvalidToken)).To(BeTrue())
		})

		It("rejects a shipID containing '*'", func() {
			_, err := ship.ArrivePort(ctx, commands.ShipInput{
				Context: fleetCtx, ShipID: "br020*vessel", ShipName: "BR020", Port: "Hamburg",
			})
			Expect(errors.Is(err, domain.ErrInvalidToken)).To(BeTrue())
		})

		It("rejects a shipID containing '>'", func() {
			_, err := ship.ArrivePort(ctx, commands.ShipInput{
				Context: fleetCtx, ShipID: "br020>vessel", ShipName: "BR020", Port: "Hamburg",
			})
			Expect(errors.Is(err, domain.ErrInvalidToken)).To(BeTrue())
		})

		It("rejects a shipID containing whitespace", func() {
			_, err := ship.ArrivePort(ctx, commands.ShipInput{
				Context: fleetCtx, ShipID: "br020 vessel", ShipName: "BR020", Port: "Hamburg",
			})
			Expect(errors.Is(err, domain.ErrInvalidToken)).To(BeTrue())
		})

		It("rejects an empty context", func() {
			_, err := ship.ArrivePort(ctx, commands.ShipInput{
				Context: "", ShipID: "br020-ctx-vessel", ShipName: "BR020", Port: "Hamburg",
			})
			Expect(errors.Is(err, domain.ErrInvalidToken)).To(BeTrue())
		})

		It("rejects a context containing a dot", func() {
			_, err := ship.ArrivePort(ctx, commands.ShipInput{
				Context: "atlantic.fleet", ShipID: "br020-ctx-vessel", ShipName: "BR020", Port: "Hamburg",
			})
			Expect(errors.Is(err, domain.ErrInvalidToken)).To(BeTrue())
		})
	})

	Context("BR-021: a shipID can only be registered once", func() {
		It("returns ErrShipExists on a duplicate explicit RegisterShip", func() {
			_, err := ship.RegisterShip(ctx, commands.ShipInput{
				Context: fleetCtx, ShipID: "br021-vessel", ShipName: "BR021",
			})
			Expect(err).NotTo(HaveOccurred())

			_, err = ship.RegisterShip(ctx, commands.ShipInput{
				Context: fleetCtx, ShipID: "br021-vessel", ShipName: "BR021 Again",
			})
			Expect(errors.Is(err, domain.ErrShipExists)).To(BeTrue())
		})

		It("returns ErrShipExists when RegisterShip follows an implicit first-arrival registration", func() {
			_, err := ship.ArrivePort(ctx, commands.ShipInput{
				Context: fleetCtx, ShipID: "br021-implicit-vessel", ShipName: "BR021", Port: "Hamburg",
			})
			Expect(err).NotTo(HaveOccurred())

			_, err = ship.RegisterShip(ctx, commands.ShipInput{
				Context: fleetCtx, ShipID: "br021-implicit-vessel", ShipName: "BR021",
			})
			Expect(errors.Is(err, domain.ErrShipExists)).To(BeTrue())
		})
	})

	Context("BR-022: a shipID can be corrected to another valid, unused shipID", func() {
		It("updates the ship's identity while preserving its surrogate id", func() {
			registered, err := ship.RegisterShip(ctx, commands.ShipInput{
				Context: fleetCtx, ShipID: "br022-vessel", ShipName: "BR022",
			})
			Expect(err).NotTo(HaveOccurred())
			id := registered.ID

			corrected, err := ship.CorrectShipID(ctx, commands.ShipCorrectionInput{
				Context: fleetCtx, ShipID: "br022-vessel", NewShipID: "br022-vessel-renamed",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(corrected.ID).To(Equal(id))
			Expect(corrected.ShipID).To(Equal("br022-vessel-renamed"))
		})

		It("rejects a target shipID already in use by another ship", func() {
			_, err := ship.RegisterShip(ctx, commands.ShipInput{
				Context: fleetCtx, ShipID: "br022-taken", ShipName: "BR022 Taken",
			})
			Expect(err).NotTo(HaveOccurred())
			_, err = ship.RegisterShip(ctx, commands.ShipInput{
				Context: fleetCtx, ShipID: "br022-source", ShipName: "BR022 Source",
			})
			Expect(err).NotTo(HaveOccurred())

			_, err = ship.CorrectShipID(ctx, commands.ShipCorrectionInput{
				Context: fleetCtx, ShipID: "br022-source", NewShipID: "br022-taken",
			})
			Expect(errors.Is(err, domain.ErrShipIDInUse)).To(BeTrue())
		})

		It("rejects correcting an unregistered shipID", func() {
			_, err := ship.CorrectShipID(ctx, commands.ShipCorrectionInput{
				Context: fleetCtx, ShipID: "br022-does-not-exist", NewShipID: "br022-new-name",
			})
			Expect(errors.Is(err, domain.ErrNotFound)).To(BeTrue())
		})

		It("rejects an invalid newShipID (BR-020)", func() {
			_, err := ship.RegisterShip(ctx, commands.ShipInput{
				Context: fleetCtx, ShipID: "br022-invalid-target", ShipName: "BR022",
			})
			Expect(err).NotTo(HaveOccurred())

			_, err = ship.CorrectShipID(ctx, commands.ShipCorrectionInput{
				Context: fleetCtx, ShipID: "br022-invalid-target", NewShipID: "br022.invalid",
			})
			Expect(errors.Is(err, domain.ErrInvalidToken)).To(BeTrue())
		})

		It("rejects newShipID equal to the current shipID", func() {
			_, err := ship.RegisterShip(ctx, commands.ShipInput{
				Context: fleetCtx, ShipID: "br022-noop", ShipName: "BR022",
			})
			Expect(err).NotTo(HaveOccurred())

			_, err = ship.CorrectShipID(ctx, commands.ShipCorrectionInput{
				Context: fleetCtx, ShipID: "br022-noop", NewShipID: "br022-noop",
			})
			Expect(err).To(HaveOccurred())
		})
	})

	Context("surrogate key: the ship's identity is an immutable UUID, not the mutable shipID", func() {
		It("assigns a UUID on first arrival, distinct from the shipID", func() {
			state, err := ship.ArrivePort(ctx, commands.ShipInput{
				Context: fleetCtx, ShipID: "surrogate-vessel", ShipName: "Surrogate Test", Port: "Hamburg",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(state.ID).To(MatchRegexp(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`))
			Expect(state.ID).NotTo(Equal(state.ShipID))
		})

		It("keeps the same id stable across arrive, depart, and a shipID correction", func() {
			arrived, err := ship.ArrivePort(ctx, commands.ShipInput{
				Context: fleetCtx, ShipID: "surrogate-stable-vessel", ShipName: "Surrogate Stable", Port: "Hamburg",
			})
			Expect(err).NotTo(HaveOccurred())
			id := arrived.ID

			departed, err := ship.DepartPort(ctx, commands.ShipInput{
				Context: fleetCtx, ShipID: "surrogate-stable-vessel", Port: "Hamburg",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(departed.ID).To(Equal(id))

			corrected, err := ship.CorrectShipID(ctx, commands.ShipCorrectionInput{
				Context: fleetCtx, ShipID: "surrogate-stable-vessel", NewShipID: "surrogate-stable-vessel-renamed",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(corrected.ID).To(Equal(id))
		})

		It("still rejects a duplicate natural key (BR-021) even though identity is the surrogate key", func() {
			_, err := ship.RegisterShip(ctx, commands.ShipInput{
				Context: fleetCtx, ShipID: "surrogate-dup-vessel", ShipName: "Surrogate Dup",
			})
			Expect(err).NotTo(HaveOccurred())

			_, err = ship.RegisterShip(ctx, commands.ShipInput{
				Context: fleetCtx, ShipID: "surrogate-dup-vessel", ShipName: "Surrogate Dup Again",
			})
			Expect(errors.Is(err, domain.ErrShipExists)).To(BeTrue())
		})
	})

	Context("implicit registration: ArrivePort mints a surrogate without a prior RegisterShip", func() {
		It("registers on first arrival with no explicit RegisterShip call", func() {
			state, err := ship.ArrivePort(ctx, commands.ShipInput{
				Context: fleetCtx, ShipID: "implicit-vessel", ShipName: "Implicit", Port: "Hamburg",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(state.ID).NotTo(BeEmpty())
		})

		It("does not re-mint on a second RegisterShip call after implicit registration", func() {
			_, err := ship.ArrivePort(ctx, commands.ShipInput{
				Context: fleetCtx, ShipID: "implicit-vessel-2", ShipName: "Implicit", Port: "Hamburg",
			})
			Expect(err).NotTo(HaveOccurred())

			_, err = ship.RegisterShip(ctx, commands.ShipInput{
				Context: fleetCtx, ShipID: "implicit-vessel-2", ShipName: "Implicit",
			})
			Expect(errors.Is(err, domain.ErrShipExists)).To(BeTrue())
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

// key is the surrogate-keyed storage key, mirroring the real repository's
// (context, id) conflict target — so a shipID correction (BR-022) updates
// the same entry in place rather than leaving a stale duplicate behind.
func (r *fakeRepo) key(kvContext, id string) string { return kvContext + "/" + id }

func (r *fakeRepo) Upsert(_ context.Context, state domain.ShipState) (domain.ShipState, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ships[r.key(state.Context, state.ID)] = state
	return state, nil
}

// Find queries by the natural key (ship_id) — reads stay natural-key native,
// mirroring the real repository.
func (r *fakeRepo) Find(_ context.Context, kvContext, shipID string) (domain.ShipState, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, s := range r.ships {
		if s.Context == kvContext && s.ShipID == shipID {
			return s, nil
		}
	}
	return domain.ShipState{}, domain.ErrNotFound
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
var defaultTestContexts = []string{"acme-pacific-fleet", "acme-atlantic-fleet"}

type fakePortRepo struct {
	mu      sync.Mutex
	known   map[string]bool
	created map[string]time.Time
}

func newFakePortRepo() *fakePortRepo {
	r := &fakePortRepo{known: make(map[string]bool), created: make(map[string]time.Time)}
	for _, kvContext := range defaultTestContexts {
		for _, name := range defaultTestPorts {
			r.known[r.key(kvContext, name)] = true
			r.created[r.key(kvContext, name)] = time.Now()
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
	k := r.key(kvContext, name)
	if !r.known[k] {
		r.created[k] = time.Now()
	}
	r.known[k] = true
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

func (r *fakePortRepo) ListRecords(_ context.Context, kvContext string) ([]domain.PortRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []domain.PortRecord
	prefix := kvContext + "|"
	for k := range r.known {
		if strings.HasPrefix(k, prefix) {
			out = append(out, domain.PortRecord{
				Name:      strings.TrimPrefix(k, prefix),
				CreatedAt: r.created[k],
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
