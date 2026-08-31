package registry_test

import (
	"context"
	"errors"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/application"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/domain"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/kvcache"
	"github.com/jthomas78/nats-tech-lab/shared/natsnotify"
	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

/*
	BR-AS51 / decision 105 — a degraded read is stale, never regressive, and
	always says so.

	Two halves that were judged insufficient apart. Monotonic cache writes bound
	how stale trust can get: a lower revision can never overwrite a higher one,
	so a late writer cannot resurrect a document from before a revocation. But
	bounded staleness served silently is still stale trust presented as current,
	so the served copy also carries its revision and the time it was stored, and
	the read that produced it says it was degraded.

	The empty outage document keeps revision 0 and no timestamp: nothing was
	served, so there is nothing to be as-of.
*/

// stubStore answers whatever the spec puts in it, including a failure.
type stubStore struct {
	doc domain.Document
	err error
}

func (s *stubStore) Current(context.Context) (domain.Document, error) { return s.doc, s.err }
func (s *stubStore) Apply(context.Context, domain.Write) (domain.Document, error) {
	return domain.Document{}, errors.New("not used")
}
func (s *stubStore) Publishers(context.Context) (domain.PublisherDocument, error) {
	return domain.PublisherDocument{}, errors.New("not used")
}
func (s *stubStore) ApplyPublisher(context.Context, domain.PublisherWrite) (domain.PublisherDocument, error) {
	return domain.PublisherDocument{}, errors.New("not used")
}

// stubCache is the read cache reduced to the one property under test: it
// remembers what it was last given, and it refuses to go backwards.
type stubCache struct {
	held     domain.Document
	storedAt time.Time
	present  bool
	getErr   error
}

func (c *stubCache) Get(context.Context) (domain.Cached, bool, error) {
	if c.getErr != nil {
		return domain.Cached{}, false, c.getErr
	}
	if !c.present {
		return domain.Cached{}, false, nil
	}
	return domain.Cached{Document: c.held, StoredAt: c.storedAt}, true, nil
}

func (c *stubCache) Put(_ context.Context, doc domain.Document) error {
	if c.present && doc.Revision < c.held.Revision {
		return nil
	}
	c.held, c.present, c.storedAt = doc, true, time.Now()
	return nil
}

var _ = Describe("the degraded read", func() {
	ctx := context.Background()
	allowed := domain.NewAllowlist([]string{"http://localhost:7110"})
	entry := func(id string) domain.Entry {
		return domain.Entry{
			ID: id, Name: id, Enabled: true, Lifecycle: domain.LifecycleDynamic,
			Remote: domain.Remote{Kind: "federated", URL: "http://localhost:7110/" + id + ".js", Module: id},
		}
	}
	build := func(store *stubStore, cache *stubCache) *application.Service {
		return application.New(store, cache, allowed, natsnotify.New(nil, nil), nil)
	}

	Context("a read the source of truth answers", func() {
		It("is not degraded and carries no as-of stamp", func() {
			store := &stubStore{doc: domain.Document{SchemaVersion: domain.SchemaVersion, Revision: 4, Entries: []domain.Entry{entry("alpha")}}}
			cache := &stubCache{}
			doc := build(store, cache).Read(ctx)

			Expect(doc.Degraded).To(BeFalse())
			Expect(doc.Revision).To(Equal(int64(4)))
			Expect(doc.AsOf).To(BeZero())
			Expect(doc.Entries).To(HaveLen(1))
		})
	})

	Context("a read only the cache answers", func() {
		It("still serves the catalogue, so one outage does not empty every shell", func() {
			stored := time.Now().Add(-90 * time.Second)
			cache := &stubCache{
				held:     domain.Document{SchemaVersion: domain.SchemaVersion, Revision: 7, Entries: []domain.Entry{entry("alpha")}},
				storedAt: stored, present: true,
			}
			doc := build(&stubStore{err: errors.New("postgres is unreachable")}, cache).Read(ctx)

			Expect(doc.Entries).To(HaveLen(1))
			Expect(doc.Revision).To(Equal(int64(7)))
		})

		It("says it is degraded, rather than presenting stale trust as current", func() {
			stored := time.Now().Add(-90 * time.Second)
			cache := &stubCache{
				held:     domain.Document{SchemaVersion: domain.SchemaVersion, Revision: 7, Entries: []domain.Entry{entry("alpha")}},
				storedAt: stored, present: true,
			}
			doc := build(&stubStore{err: errors.New("postgres is unreachable")}, cache).Read(ctx)

			Expect(doc.Degraded).To(BeTrue())
			Expect(doc.AsOf.Unix()).To(Equal(stored.Unix()))
		})
	})

	Context("a read neither answers", func() {
		It("is the empty outage document, with no revision to be as-of", func() {
			doc := build(&stubStore{err: errors.New("postgres is unreachable")}, &stubCache{}).Read(ctx)

			Expect(doc.Degraded).To(BeTrue())
			Expect(doc.Entries).To(BeEmpty())
			Expect(doc.Revision).To(Equal(domain.DegradedRevision))
			Expect(doc.AsOf).To(BeZero())
		})
	})

	Context("cache writes are monotonic", func() {
		It("refreshes the cache from a read that reached the source of truth", func() {
			cache := &stubCache{}
			build(&stubStore{doc: domain.Document{SchemaVersion: domain.SchemaVersion, Revision: 4}}, cache).Read(ctx)
			Expect(cache.present).To(BeTrue())
			Expect(cache.held.Revision).To(Equal(int64(4)))
		})

		// The rule itself, stated once where it can be read: the cache asks
		// the domain rather than deciding for itself, so "never regressive"
		// is one sentence of code and not a property of a storage adapter.
		It("accepts a higher revision and refuses a lower one", func() {
			Expect(domain.SupersedesCached(4, 9)).To(BeTrue())
			Expect(domain.SupersedesCached(9, 3)).To(BeFalse())
		})

		It("accepts an equal revision, so a re-put after a restart is not a conflict", func() {
			Expect(domain.SupersedesCached(9, 9)).To(BeTrue())
		})

		It("never writes the outage document over a real one", func() {
			Expect(domain.SupersedesCached(9, domain.DegradedRevision)).To(BeFalse())
		})

		It("accepts anything into an empty cache, including the first real revision", func() {
			Expect(domain.SupersedesCached(domain.NoRevision, 1)).To(BeTrue())
		})
	})

	Context("the real cache enforces it", func() {
		It("keeps the higher revision when a lower one is put after it", func() {
			cache := freshCache()
			higher := domain.Document{SchemaVersion: domain.SchemaVersion, Revision: 9, Entries: []domain.Entry{entry("alpha")}}
			Expect(cache.Put(ctx, higher)).To(Succeed())
			Expect(cache.Put(ctx, domain.Document{SchemaVersion: domain.SchemaVersion, Revision: 3})).To(Succeed())

			back, ok, err := cache.Get(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(ok).To(BeTrue())
			Expect(back.Document.Revision).To(Equal(int64(9)))
			Expect(back.Document.Entries).To(HaveLen(1))
		})

		It("stamps how old the copy it hands back is", func() {
			cache := freshCache()
			Expect(cache.Put(ctx, domain.Document{SchemaVersion: domain.SchemaVersion, Revision: 2})).To(Succeed())
			back, ok, err := cache.Get(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(ok).To(BeTrue())
			Expect(back.StoredAt).NotTo(BeZero())
			Expect(back.StoredAt).To(BeTemporally("~", time.Now(), 30*time.Second))
		})
	})
})

// freshCache is a real kvcache on its own embedded NATS server, so the
// monotonic rule is proved where it is enforced rather than in a double.
func freshCache() *kvcache.Cache {
	srv, err := server.NewServer(&server.Options{Host: "127.0.0.1", Port: -1, JetStream: true, StoreDir: GinkgoT().TempDir()})
	Expect(err).NotTo(HaveOccurred())
	srv.Start()
	DeferCleanup(srv.Shutdown)
	Expect(srv.ReadyForConnections(5 * time.Second)).To(BeTrue())
	nc, err := nats.Connect(srv.ClientURL(), nats.Name("registry-degraded-test"))
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(nc.Close)
	js, err := jetstream.New(nc)
	Expect(err).NotTo(HaveOccurred())
	return kvcache.New(js)
}
