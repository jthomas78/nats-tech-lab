package registry_test

/*
	BR-AS73 — what the registry does with the conclusion "the catalogue was
	lost", and, more importantly, what it does the rest of the time.

	These specs are mostly about SILENCE. The predicate's whole value is that
	it fires on a real loss and on nothing else: a notice sent on every
	restart would turn a rolling restart into a fleet-wide re-announce storm,
	and the storm would look exactly like the thing the notice exists to
	prevent. So most of what follows asserts nothing was published.

	The witness is the read cache, and the reason it can be trusted is that it
	is written through from the same call that commits Postgres — it can lag
	the source of truth but never lead it. A cache holding a revision higher
	than Postgres therefore is not a stale cache; it is a Postgres that went
	backwards.
*/

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/application"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/domain"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/postgres"
	"github.com/jthomas78/nats-tech-lab/shared/mferegistry"
	"github.com/jthomas78/nats-tech-lab/shared/natsnotify"
	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
)

// resettableCache is stubCache plus the one backwards write BR-AS51 allows,
// so a spec can see whether the witness was actually repaired.
type resettableCache struct {
	stubCache
	resets []int64
}

func (c *resettableCache) Reset(_ context.Context, doc domain.Document) error {
	c.resets = append(c.resets, doc.Revision)
	c.held, c.present, c.storedAt = doc, true, time.Now()
	return nil
}

var _ = Describe("BR-AS73 — stating a catalogue reset", func() {
	var (
		ctx      context.Context
		nc       *nats.Conn
		notices  chan *nats.Msg
		allowed  domain.Allowlist
		now      time.Time
		notifier *natsnotify.Notifier
	)

	doc := func(revision int64) domain.Document {
		return domain.Document{SchemaVersion: domain.SchemaVersion, Revision: revision}
	}

	BeforeEach(func() {
		ctx = context.Background()
		allowed = domain.NewAllowlist([]string{"http://localhost:7110"})
		now = time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)

		srv, err := server.NewServer(&server.Options{Host: "127.0.0.1", Port: -1})
		Expect(err).NotTo(HaveOccurred())
		srv.Start()
		DeferCleanup(srv.Shutdown)
		Expect(srv.ReadyForConnections(5 * time.Second)).To(BeTrue())
		nc, err = nats.Connect(srv.ClientURL(), nats.Name("registry-reset-test"))
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(nc.Close)

		notices = make(chan *nats.Msg, 8)
		_, err = nc.ChanSubscribe(mferegistry.EntriesReset, notices)
		Expect(err).NotTo(HaveOccurred())
		Expect(nc.Flush()).To(Succeed())

		notifier = natsnotify.New(nc, slog.Default())
	})

	build := func(store application.Store, cache application.Cache) *application.Service {
		return application.New(store, cache, allowed, notifier, slog.Default())
	}

	// Publishing is fire-and-forget on core NATS, so "nothing was published"
	// needs a flush and a beat rather than an immediate empty check.
	silent := func() {
		Expect(nc.Flush()).To(Succeed())
		Consistently(notices, 150*time.Millisecond).ShouldNot(Receive())
	}

	Context("the scenarios the review's table named", func() {
		It("says nothing when the registry restarts with its catalogue intact", func() {
			cache := &resettableCache{stubCache: stubCache{held: doc(7), present: true}}
			fired, err := build(&stubStore{doc: doc(7)}, cache).AnnounceCatalogueReset(ctx, "startup", now)
			Expect(err).NotTo(HaveOccurred())
			Expect(fired).To(BeFalse())
			Expect(cache.resets).To(BeEmpty())
			silent()
		})

		It("says nothing on a first boot, where nothing has ever been witnessed", func() {
			cache := &resettableCache{}
			fired, err := build(&stubStore{doc: doc(0)}, cache).AnnounceCatalogueReset(ctx, "startup", now)
			Expect(err).NotTo(HaveOccurred())
			Expect(fired).To(BeFalse())
			silent()
		})

		It("says nothing when the catalogue moved forward while this process was down", func() {
			cache := &resettableCache{stubCache: stubCache{held: doc(3), present: true}}
			fired, err := build(&stubStore{doc: doc(9)}, cache).AnnounceCatalogueReset(ctx, "startup", now)
			Expect(err).NotTo(HaveOccurred())
			Expect(fired).To(BeFalse())
			silent()
		})

		It("states a reset when the catalogue was truncated under live plugins", func() {
			cache := &resettableCache{stubCache: stubCache{held: doc(12), present: true}}
			fired, err := build(&stubStore{doc: doc(0)}, cache).AnnounceCatalogueReset(ctx, "startup", now)
			Expect(err).NotTo(HaveOccurred())
			Expect(fired).To(BeTrue())

			var msg *nats.Msg
			Eventually(notices).Should(Receive(&msg))
			Expect(msg.Subject).To(Equal(mferegistry.EntriesReset))
		})

		/* The discriminating case. A predicate written as "is the catalogue
		   empty" passes every spec above and still misses this one, because a
		   stale backup is OLDER, not absent — and it would go on looking
		   correct, because the hole it reopens is silent. */
		It("states a reset when the catalogue was restored from a stale backup", func() {
			cache := &resettableCache{stubCache: stubCache{held: doc(12), present: true}}
			fired, err := build(&stubStore{doc: doc(5)}, cache).AnnounceCatalogueReset(ctx, "restore", now)
			Expect(err).NotTo(HaveOccurred())
			Expect(fired).To(BeTrue())
			Eventually(notices).Should(Receive())
		})
	})

	Context("what the notice carries", func() {
		It("carries a jitter window the plugins can honour, and nothing installable", func() {
			cache := &resettableCache{stubCache: stubCache{held: doc(12), present: true}}
			_, err := build(&stubStore{doc: doc(0)}, cache).AnnounceCatalogueReset(ctx, "startup", now)
			Expect(err).NotTo(HaveOccurred())

			var msg *nats.Msg
			Eventually(notices).Should(Receive(&msg))

			var notice mferegistry.ResetNotice
			Expect(jsonUnmarshalReset(msg.Data, &notice)).To(Succeed())
			Expect(notice.JitterWindow()).To(Equal(mferegistry.ResetJitterDefault))
			Expect(notice.At).To(Equal(now.UnixMilli()))
			// No entry, no remote, no revision: the notice must never become
			// a second, unsigned way into the catalogue.
			Expect(string(msg.Data)).NotTo(ContainSubstring("remote"))
			Expect(string(msg.Data)).NotTo(ContainSubstring("entries"))
		})
	})

	Context("repairing the witness, so the notice fires once and not every restart", func() {
		It("resets the cache to the recovered revision, leaving a second startup silent", func() {
			cache := &resettableCache{stubCache: stubCache{held: doc(12), present: true}}
			service := build(&stubStore{doc: doc(0)}, cache)

			fired, err := service.AnnounceCatalogueReset(ctx, "startup", now)
			Expect(err).NotTo(HaveOccurred())
			Expect(fired).To(BeTrue())
			Expect(cache.resets).To(Equal([]int64{0}))
			Eventually(notices).Should(Receive())

			/* Without Reset this second call fires again, forever: the
			   ordinary cached write refuses to go backwards (BR-AS51), so the
			   witness would stay at 12 and every restart would restate a loss
			   that was already recovered. */
			fired, err = service.AnnounceCatalogueReset(ctx, "startup", now)
			Expect(err).NotTo(HaveOccurred())
			Expect(fired).To(BeFalse())
			silent()
		})
	})

	Context("what an outage must not be mistaken for", func() {
		It("says nothing when the source of truth cannot be read at all", func() {
			cache := &resettableCache{stubCache: stubCache{held: doc(12), present: true}}
			fired, err := build(&stubStore{doc: doc(0), err: errors.New("connection refused")}, cache).
				AnnounceCatalogueReset(ctx, "startup", now)
			Expect(err).NotTo(HaveOccurred())
			Expect(fired).To(BeFalse())
			Expect(cache.resets).To(BeEmpty())
			silent()
		})

		It("says nothing when the witness itself is unreadable", func() {
			cache := &resettableCache{stubCache: stubCache{getErr: errors.New("kv down")}}
			fired, err := build(&stubStore{doc: doc(0)}, cache).AnnounceCatalogueReset(ctx, "startup", now)
			Expect(err).NotTo(HaveOccurred())
			Expect(fired).To(BeFalse())
			silent()
		})

		It("says nothing in a deployment that runs without a cache, rather than guessing", func() {
			fired, err := build(&stubStore{doc: doc(0)}, nil).AnnounceCatalogueReset(ctx, "startup", now)
			Expect(err).NotTo(HaveOccurred())
			Expect(fired).To(BeFalse())
			silent()
		})
	})

	Context("BR-AS54 — a reset notice never edits or withdraws a quiet publisher", func() {
		It("leaves the real catalogue revision, entry and audit history unchanged until a publisher chooses to re-announce", func() {
			if pgUnavailable != "" {
				Skip("Postgres integration fixture unavailable: " + pgUnavailable)
			}
			Expect(pgDB.PingContext(ctx)).To(Succeed())
			_, err := pgDB.ExecContext(ctx, `TRUNCATE registry.entries, registry.audit; UPDATE registry.revision SET revision = 0`)
			Expect(err).NotTo(HaveOccurred())

			store := postgres.NewStore(pgDB, allowed)
			entry := federated("quiet-plugin", "http://localhost:7110/remoteEntry.js")
			entry.Lifecycle = domain.LifecycleDynamic
			entry.Enabled = true
			before, err := store.Apply(ctx, domain.Write{Op: domain.OpUpsert, EntryID: entry.ID, Entry: &entry, Actor: domain.SharedAdminActor, IfRevision: domain.NoRevision})
			Expect(err).NotTo(HaveOccurred())
			auditBefore, err := store.Audit(ctx, 100)
			Expect(err).NotTo(HaveOccurred())

			// The cache proves a loss while the real catalogue still retains the
			// quiet plugin. The reset says that fact aloud; it does not turn the
			// publisher's lack of a reply into an authority to mutate its row.
			cache := &resettableCache{stubCache: stubCache{held: domain.Document{
				SchemaVersion: domain.SchemaVersion, Revision: before.Revision + 1, Entries: before.Entries,
			}, present: true}}
			fired, err := build(store, cache).AnnounceCatalogueReset(ctx, "restore", now)
			Expect(err).NotTo(HaveOccurred())
			Expect(fired).To(BeTrue())
			Eventually(notices).Should(Receive())

			after, err := store.Current(ctx)
			Expect(err).NotTo(HaveOccurred())
			auditAfter, err := store.Audit(ctx, 100)
			Expect(err).NotTo(HaveOccurred())
			Expect(after).To(Equal(before))
			Expect(after.Entries).To(HaveLen(1))
			Expect(after.Entries[0].Enabled).To(BeTrue())
			Expect(after.Entries[0].Withdrawn).To(BeFalse())
			Expect(auditAfter).To(Equal(auditBefore))
		})
	})
})

func jsonUnmarshalReset(data []byte, into *mferegistry.ResetNotice) error {
	return json.Unmarshal(data, into)
}
