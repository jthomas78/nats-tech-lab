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
func (s *stubStore) Sources(context.Context) (map[string]string, error) {
	return map[string]string{}, nil
}
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

/*
BR-AS49 / decision 100 — revocation reaches the running browser.

Withholding an entry governs the next admission. It does nothing at all to
code already running: a plugin's timers, subscriptions and handlers keep
going in every open tab. Phase 5 settled that a `static` plugin is not
unloaded under the user, and that stays true of every ordinary catalogue
change — but revocation is a security event and outranks it.

For the shell to act on that it has to be able to SEE it, and an entry that
simply vanished from the readable document is indistinguishable from one an
operator disabled. So a withheld entry is served as a tombstone: its id,
marked withheld, and nothing else. Not the remote, not the manifest, not the
contribution list — nothing a shell could load and nothing the revoked key
signed.

Carried in the document rather than in the change notification on purpose. A
notify message is fire-and-forget and is recovered by the shell's next
unconditional read; a revocation that only travelled that way would be lost
by a shell that reconnected at the wrong moment.
*/
var _ = Describe("a withheld entry on the wire", func() {
	allowlist := domain.NewAllowlist([]string{"http://localhost:7110"})
	live := func(id string) domain.Entry {
		return domain.Entry{
			ID: id, Name: id, Enabled: true, Lifecycle: domain.LifecycleDynamic,
			Remote: domain.Remote{Kind: "federated", URL: "http://localhost:7110/" + id + ".js", Module: id},
		}
	}
	withheld := func(id string) domain.Entry {
		e := live(id)
		e.Enabled, e.Withheld = false, true
		e.Manifest = &domain.Manifest{Bytes: []byte(`{"id":"` + id + `"}`), Signature: "sig", SigningKey: "key"}
		return e
	}

	It("is served, where a merely disabled entry is not", func() {
		doc := domain.Document{Revision: 4, Entries: []domain.Entry{withheld("alpha")}}
		out := doc.Readable(allowlist)

		Expect(out.Entries).To(HaveLen(1))
		Expect(out.Entries[0].ID).To(Equal("alpha"))
		Expect(out.Entries[0].Withheld).To(BeTrue())
	})

	It("carries nothing loadable, and nothing the revoked key signed", func() {
		doc := domain.Document{Revision: 4, Entries: []domain.Entry{withheld("alpha")}}
		out := doc.Readable(allowlist)

		Expect(out.Entries[0].Remote).To(Equal(domain.Remote{}))
		Expect(out.Entries[0].Manifest).To(BeNil())
		Expect(out.Entries[0].Contributions).To(BeEmpty())
		Expect(out.Entries[0].ExtensionPoints).To(BeEmpty())
		Expect(out.Entries[0].Enabled).To(BeFalse())
	})

	It("is served even though its origin is no longer allowed, having no origin left to check", func() {
		// The tombstone is built before the allowlist runs. It has to be: a
		// tombstone has no remote, so an allowlist check would drop the very
		// news the shell is waiting for.
		narrowed := domain.NewAllowlist([]string{"http://localhost:7999"})
		doc := domain.Document{Revision: 4, Entries: []domain.Entry{withheld("alpha")}}

		Expect(doc.Readable(narrowed).Entries).To(HaveLen(1))
	})

	It("leaves a merely disabled entry withheld from the document entirely", func() {
		disabled := live("beta")
		disabled.Enabled = false
		doc := domain.Document{Revision: 4, Entries: []domain.Entry{disabled}}

		Expect(doc.Readable(allowlist).Entries).To(BeEmpty())
	})

	It("sits in id order beside the entries that are still running", func() {
		doc := domain.Document{Revision: 4, Entries: []domain.Entry{live("charlie"), withheld("alpha")}}
		out := doc.Readable(allowlist)

		Expect(out.Entries).To(HaveLen(2))
		Expect(out.Entries[0].ID).To(Equal("alpha"))
		Expect(out.Entries[1].ID).To(Equal("charlie"))
	})
})
