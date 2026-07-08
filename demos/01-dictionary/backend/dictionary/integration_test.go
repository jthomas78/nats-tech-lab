package dictionary

// Integration tests running against an embedded in-process NATS server (real
// JetStream, real KV). Tests cover:
//   - Shape A: ship command → event → projector → KV → query
//   - Shape B: ship command → event → Postgres (fake) + KV → cache hit/miss
//   - Shape C: multiple commands → full JetStream replay → fleet reconstruction

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"time"

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

func startJetStream(t *testing.T) jetstream.JetStream {
	t.Helper()
	opts := &server.Options{JetStream: true, StoreDir: t.TempDir(), Port: -1}
	srv, err := server.NewServer(opts)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	srv.Start()
	t.Cleanup(srv.Shutdown)
	if !srv.ReadyForConnections(10 * time.Second) {
		t.Fatal("nats server not ready")
	}
	nc, err := nats.Connect(srv.ClientURL())
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(nc.Close)
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatalf("jetstream: %v", err)
	}
	if _, err := jstream.CreateStream(context.Background(), js, domain.StreamName, domain.StreamSubjects()); err != nil {
		t.Fatalf("create stream: %v", err)
	}
	return js
}

func eventually(t *testing.T, timeout time.Duration, fn func() error) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var err error
	for time.Now().Before(deadline) {
		if err = fn(); err == nil {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("condition not met within %s: %v", timeout, err)
}

// ─── Shape A ─────────────────────────────────────────────────────────────────

func TestShapeA_ShipProjection(t *testing.T) {
	ctx := context.Background()
	js := startJetStream(t)
	log := slog.New(slog.DiscardHandler)

	kvA := kvstore.New(js, "dict-a")
	consume, err := eventhandler.RegisterShapeA(ctx, js, kvA, log)
	if err != nil {
		t.Fatalf("register shape A: %v", err)
	}
	defer consume.Stop()

	ship := commands.NewShipHandler(jstream.NewPublisher(js), js)
	const ctx2 = "global"

	// Arrive Hamburg
	_, err = ship.ArrivePort(ctx, commands.ShipInput{
		Context: ctx2, ShipID: "orient-express", ShipName: "Orient Express", Port: "Hamburg",
	})
	if err != nil {
		t.Fatalf("arrive: %v", err)
	}

	eventually(t, 5*time.Second, func() error {
		keys, err := kvA.Keys(ctx, ctx2)
		if err != nil || len(keys) == 0 {
			return fmt.Errorf("waiting for KV entry: %v", err)
		}
		return nil
	})

	// Load cargo
	_, err = ship.LoadCargo(ctx, commands.ShipInput{
		Context: ctx2, ShipID: "orient-express",
		Cargo: &domain.Cargo{Description: "Electronics", Units: 42},
	})
	if err != nil {
		t.Fatalf("load cargo: %v", err)
	}

	// Depart
	_, err = ship.DepartPort(ctx, commands.ShipInput{
		Context: ctx2, ShipID: "orient-express", Port: "Hamburg",
	})
	if err != nil {
		t.Fatalf("depart: %v", err)
	}

	// Shape A KV should reflect departed state (no port) with cargo
	eventually(t, 5*time.Second, func() error {
		q := queries.NewShapeA(kvA)
		ships, err := q.ListShips(ctx, ctx2)
		if err != nil {
			return err
		}
		if len(ships) == 0 {
			return errors.New("no ships in KV")
		}
		s := ships[0]
		if s.CurrentPort != "" {
			return fmt.Errorf("expected at sea, got port=%q", s.CurrentPort)
		}
		if len(s.Cargo) != 1 || s.Cargo[0].Units != 42 {
			return fmt.Errorf("unexpected cargo: %+v", s.Cargo)
		}
		return nil
	})

	// Domain rule: cannot depart a port the ship is not at
	_, err = ship.DepartPort(ctx, commands.ShipInput{
		Context: ctx2, ShipID: "orient-express", Port: "Hamburg",
	})
	if !errors.Is(err, domain.ErrNotDocked) {
		t.Fatalf("expected ErrNotDocked, got %v", err)
	}

	// Domain rule: cannot arrive at a new port while at sea (OK) but cannot
	// arrive while already docked.
	_, err = ship.ArrivePort(ctx, commands.ShipInput{
		Context: ctx2, ShipID: "orient-express", Port: "Rotterdam",
	})
	if err != nil {
		t.Fatalf("arrive Rotterdam: %v", err)
	}

	// Domain rule: cannot arrive again without departing first.
	eventually(t, 5*time.Second, func() error {
		_, err = ship.ArrivePort(ctx, commands.ShipInput{
			Context: ctx2, ShipID: "orient-express", Port: "Singapore",
		})
		if !errors.Is(err, domain.ErrMustDepart) {
			return fmt.Errorf("expected ErrMustDepart, got %v", err)
		}
		return nil
	})
}

// ─── Shape B ─────────────────────────────────────────────────────────────────

func TestShapeB_CacheHitMissBackfill(t *testing.T) {
	ctx := context.Background()
	js := startJetStream(t)
	log := slog.New(slog.DiscardHandler)

	kvB := kvstore.New(js, "dict-b")
	repo := newFakeRepo()
	consume, err := eventhandler.RegisterShapeB(ctx, js, kvB, repo, log)
	if err != nil {
		t.Fatalf("register shape B: %v", err)
	}
	defer consume.Stop()

	ship := commands.NewShipHandler(jstream.NewPublisher(js), js)
	q := queries.NewShapeB(kvB, repo)
	const ctx2 = "global"

	_, err = ship.ArrivePort(ctx, commands.ShipInput{
		Context: ctx2, ShipID: "pacific-star", ShipName: "Pacific Star", Port: "Singapore",
	})
	if err != nil {
		t.Fatalf("arrive: %v", err)
	}

	// Projector writes Postgres then warms the cache → hit
	eventually(t, 5*time.Second, func() error {
		s, cacheHit, err := q.GetShip(ctx, ctx2, "pacific-star")
		if err != nil {
			return err
		}
		if !cacheHit {
			return errors.New("expected cache hit after projection")
		}
		if s.ShipName != "Pacific Star" {
			return fmt.Errorf("shipName = %q", s.ShipName)
		}
		return nil
	})

	// Evict → miss → Postgres fallthrough → backfill
	if err := q.EvictCacheShip(ctx, ctx2, "pacific-star"); err != nil {
		t.Fatalf("evict: %v", err)
	}
	s, cacheHit, err := q.GetShip(ctx, ctx2, "pacific-star")
	if err != nil {
		t.Fatalf("get after evict: %v", err)
	}
	if cacheHit {
		t.Fatal("expected cache miss after eviction")
	}
	if s.ShipName != "Pacific Star" {
		t.Fatalf("shipName = %q after miss", s.ShipName)
	}

	// Backfilled: next read is a hit
	eventually(t, 5*time.Second, func() error {
		_, cacheHit, err := q.GetShip(ctx, ctx2, "pacific-star")
		if err != nil {
			return err
		}
		if !cacheHit {
			return errors.New("expected cache hit after backfill")
		}
		return nil
	})

	// Unknown ship misses both KV and Postgres
	if _, _, err := q.GetShip(ctx, ctx2, "unknown-vessel"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// ─── Shape C ─────────────────────────────────────────────────────────────────

func TestShapeC_ReconstructFleet(t *testing.T) {
	ctx := context.Background()
	js := startJetStream(t)

	ship := commands.NewShipHandler(jstream.NewPublisher(js), js)
	const ctx2 = "global"

	// Two ships perform a sequence of operations.
	steps := []func() error{
		func() error {
			_, err := ship.ArrivePort(ctx, commands.ShipInput{
				Context: ctx2, ShipID: "orient-express", ShipName: "Orient Express", Port: "Hamburg",
			})
			return err
		},
		func() error {
			_, err := ship.ArrivePort(ctx, commands.ShipInput{
				Context: ctx2, ShipID: "pacific-star", ShipName: "Pacific Star", Port: "Rotterdam",
			})
			return err
		},
		func() error {
			_, err := ship.LoadCargo(ctx, commands.ShipInput{
				Context: ctx2, ShipID: "orient-express",
				Cargo: &domain.Cargo{Description: "Electronics", Units: 42},
			})
			return err
		},
		func() error {
			_, err := ship.DepartPort(ctx, commands.ShipInput{
				Context: ctx2, ShipID: "orient-express", Port: "Hamburg",
			})
			return err
		},
	}
	for i, step := range steps {
		if err := step(); err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
	}

	// Shape C: reconstruct without KV or Postgres
	q := queries.NewShapeC(js)
	fleet, err := q.ReconstructFleet(ctx)
	if err != nil {
		t.Fatalf("reconstruct fleet: %v", err)
	}
	if len(fleet) != 2 {
		t.Fatalf("expected 2 ships, got %d", len(fleet))
	}

	byID := make(map[string]domain.ShipState)
	for _, s := range fleet {
		byID[s.ShipID] = s
	}

	oe := byID["orient-express"]
	if oe.CurrentPort != "" {
		t.Errorf("orient-express: expected at sea, got %q", oe.CurrentPort)
	}
	if len(oe.Cargo) != 1 || oe.Cargo[0].Units != 42 {
		t.Errorf("orient-express: unexpected cargo %+v", oe.Cargo)
	}

	ps := byID["pacific-star"]
	if ps.CurrentPort != "Rotterdam" {
		t.Errorf("pacific-star: expected Rotterdam, got %q", ps.CurrentPort)
	}
}

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
