package dictionary

// Smoke tests running against an embedded in-process NATS server (real
// JetStream, real KV). Shape A is tested end to end: command → event →
// projector → KV → query. Shape B is tested with a fake repository standing
// in for Postgres, exercising the cache hit / miss / backfill paths.

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
	opts := &server.Options{
		JetStream: true,
		StoreDir:  t.TempDir(),
		Port:      -1,
	}
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

// eventually polls fn until it returns nil or the timeout expires.
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

func TestShapeA_CreateUpdateReadFromKV(t *testing.T) {
	ctx := context.Background()
	js := startJetStream(t)
	log := slog.New(slog.DiscardHandler)

	kvA := kvstore.New(js, "dict-a")
	consume, err := eventhandler.RegisterShapeA(ctx, js, kvA, log)
	if err != nil {
		t.Fatalf("register shape A: %v", err)
	}
	defer consume.Stop()

	cmd := commands.NewHandler(jstream.NewPublisher(js))
	query := queries.NewShapeA(kvA)

	// Create: command → event → projector → KV → query.
	_, err = cmd.CreateEntry(ctx, commands.EntryInput{
		Context: "en-GB", EntityType: "currency", ID: "GBP", Label: "Pound Sterling",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	var createdAt time.Time
	eventually(t, 5*time.Second, func() error {
		entry, revision, err := query.GetEntry(ctx, "en-GB", "currency", "GBP")
		if err != nil {
			return err
		}
		if entry.Label != "Pound Sterling" {
			return fmt.Errorf("label = %q", entry.Label)
		}
		if revision == 0 {
			return errors.New("revision = 0")
		}
		createdAt = entry.CreatedAt
		return nil
	})

	// Update: overwrites the KV value but preserves createdAt.
	_, err = cmd.UpdateEntry(ctx, commands.EntryInput{
		Context: "en-GB", EntityType: "currency", ID: "GBP", Label: "Pound Sterling (GBP)",
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	eventually(t, 5*time.Second, func() error {
		entry, _, err := query.GetEntry(ctx, "en-GB", "currency", "GBP")
		if err != nil {
			return err
		}
		if entry.Label != "Pound Sterling (GBP)" {
			return fmt.Errorf("label = %q", entry.Label)
		}
		if !entry.CreatedAt.Equal(createdAt) {
			return fmt.Errorf("createdAt changed: %s != %s", entry.CreatedAt, createdAt)
		}
		return nil
	})

	// Context isolation: the same key does not exist in another context.
	if _, _, err := query.GetEntry(ctx, "en-US", "currency", "GBP"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound in en-US context, got %v", err)
	}
}

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

	cmd := commands.NewHandler(jstream.NewPublisher(js))
	query := queries.NewShapeB(kvB, repo)

	_, err = cmd.CreateEntry(ctx, commands.EntryInput{
		Context: "en-GB", EntityType: "currency", ID: "EUR", Label: "Euro",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Projector writes Postgres (fake) then warms the cache → hit.
	eventually(t, 5*time.Second, func() error {
		entry, cacheHit, err := query.GetEntry(ctx, "en-GB", "currency", "EUR")
		if err != nil {
			return err
		}
		if !cacheHit {
			return errors.New("expected cache hit after projection")
		}
		if entry.Version != 1 {
			return fmt.Errorf("version = %d", entry.Version)
		}
		return nil
	})

	// Evict → miss → falls through to the repo → backfills the cache.
	if err := query.EvictCacheEntry(ctx, "en-GB", "currency", "EUR"); err != nil {
		t.Fatalf("evict: %v", err)
	}
	entry, cacheHit, err := query.GetEntry(ctx, "en-GB", "currency", "EUR")
	if err != nil {
		t.Fatalf("get after evict: %v", err)
	}
	if cacheHit {
		t.Fatal("expected cache miss after eviction")
	}
	if entry.Label != "Euro" {
		t.Fatalf("label = %q", entry.Label)
	}

	// Backfilled: next read is a hit again.
	eventually(t, 5*time.Second, func() error {
		_, cacheHit, err := query.GetEntry(ctx, "en-GB", "currency", "EUR")
		if err != nil {
			return err
		}
		if !cacheHit {
			return errors.New("expected cache hit after backfill")
		}
		return nil
	})

	// Unknown entries miss the cache AND the projection.
	if _, _, err := query.GetEntry(ctx, "en-GB", "currency", "XXX"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// fakeRepo is an in-memory stand-in for the Postgres projection.
type fakeRepo struct {
	mu      sync.Mutex
	entries map[string]domain.DictionaryEntry
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{entries: make(map[string]domain.DictionaryEntry)}
}

func (r *fakeRepo) key(kvContext, entityType, id string) string {
	return kvContext + "/" + entityType + "/" + id
}

func (r *fakeRepo) Upsert(_ context.Context, entry domain.DictionaryEntry) (domain.DictionaryEntry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	k := r.key(entry.Context, entry.EntityType, entry.ID)
	if existing, ok := r.entries[k]; ok {
		entry.Version = existing.Version + 1
		entry.CreatedAt = existing.CreatedAt
	} else {
		entry.Version = 1
	}
	r.entries[k] = entry
	return entry, nil
}

func (r *fakeRepo) Find(_ context.Context, kvContext, entityType, id string) (domain.DictionaryEntry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, ok := r.entries[r.key(kvContext, entityType, id)]
	if !ok {
		return domain.DictionaryEntry{}, domain.ErrNotFound
	}
	return entry, nil
}

func (r *fakeRepo) List(_ context.Context, kvContext string) ([]domain.DictionaryEntry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []domain.DictionaryEntry
	for _, e := range r.entries {
		if e.Context == kvContext {
			out = append(out, e)
		}
	}
	return out, nil
}
