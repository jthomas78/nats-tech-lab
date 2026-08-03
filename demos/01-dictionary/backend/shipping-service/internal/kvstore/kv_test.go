// Tests for the per-tenant bucket + context-prefixed key design (Phase 3 /
// Gap #5 fix). The central property: multiple business-unit contexts share one
// KV bucket; Keys() and Watch() are scoped so each context sees only its own
// entries, and entry keys are returned without the {context}. prefix — callers
// see the same bare keys (e.g. "ship.SHIP1") they passed in.
package kvstore_test

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/internal/kvstore"
)

func newJSForKV() jetstream.JetStream {
	GinkgoHelper()
	opts := &server.Options{JetStream: true, StoreDir: GinkgoT().TempDir(), Port: -1}
	srv, err := server.NewServer(opts)
	Expect(err).NotTo(HaveOccurred())
	srv.Start()
	DeferCleanup(srv.Shutdown)
	Expect(srv.ReadyForConnections(10 * time.Second)).To(BeTrue())

	nc, err := nats.Connect(srv.ClientURL(), nats.Name("kvstore-test"))
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(nc.Close)

	js, err := jetstream.New(nc)
	Expect(err).NotTo(HaveOccurred())
	return js
}

var _ = Describe("KVStore — per-tenant bucket with context-prefixed keys", func() {
	var (
		ctx   context.Context
		store *kvstore.Store
	)

	BeforeEach(func() {
		ctx = context.Background()
		store = kvstore.New(newJSForKV(), "dict-a")
	})

	Describe("Put / Get", func() {
		It("stores and retrieves a value by context and key", func() {
			_, err := store.Put(ctx, "acme", "ship.SHIP1", []byte(`{"id":"SHIP1"}`))
			Expect(err).NotTo(HaveOccurred())

			val, _, err := store.Get(ctx, "acme", "ship.SHIP1")
			Expect(err).NotTo(HaveOccurred())
			Expect(val).To(Equal([]byte(`{"id":"SHIP1"}`)))
		})

		It("returns ErrKeyNotFound for a missing key", func() {
			_, _, err := store.Get(ctx, "acme", "ship.MISSING")
			Expect(err).To(MatchError(jetstream.ErrKeyNotFound))
		})

		It("does not leak across contexts — getting a key from a different context returns not found", func() {
			_, err := store.Put(ctx, "acme", "ship.SHIP1", []byte("v"))
			Expect(err).NotTo(HaveOccurred())

			_, _, err = store.Get(ctx, "acme-pacific-fleet", "ship.SHIP1")
			Expect(err).To(MatchError(jetstream.ErrKeyNotFound),
				"acme-pacific-fleet must not see acme's key even in the same bucket")
		})
	})

	Describe("Delete", func() {
		It("removes a key so subsequent Get returns ErrKeyNotFound", func() {
			_, err := store.Put(ctx, "acme", "ship.SHIP1", []byte("v"))
			Expect(err).NotTo(HaveOccurred())

			Expect(store.Delete(ctx, "acme", "ship.SHIP1")).To(Succeed())

			_, _, err = store.Get(ctx, "acme", "ship.SHIP1")
			Expect(err).To(MatchError(jetstream.ErrKeyNotFound))
		})
	})

	Describe("Keys() — context isolation", func() {
		It("returns only keys for the requested context, without the {context}. prefix", func() {
			_, err := store.Put(ctx, "acme", "ship.SHIP1", []byte("a"))
			Expect(err).NotTo(HaveOccurred())
			_, err = store.Put(ctx, "acme-pacific-fleet", "ship.SHIP2", []byte("b"))
			Expect(err).NotTo(HaveOccurred())
			_, err = store.Put(ctx, "acme", "ship.SHIP3", []byte("c"))
			Expect(err).NotTo(HaveOccurred())

			acmeKeys, err := store.Keys(ctx, "acme")
			Expect(err).NotTo(HaveOccurred())
			Expect(acmeKeys).To(ConsistOf("ship.SHIP1", "ship.SHIP3"),
				"acme Keys() must return bare keys and must not include acme-pacific-fleet entries")

			apfKeys, err := store.Keys(ctx, "acme-pacific-fleet")
			Expect(err).NotTo(HaveOccurred())
			Expect(apfKeys).To(ConsistOf("ship.SHIP2"),
				"acme-pacific-fleet Keys() must return bare keys and must not include acme entries")
		})
	})

	Describe("Watch() — context isolation and prefix stripping", func() {
		It("initial replay contains only entries for the requested context, with bare keys", func() {
			_, err := store.Put(ctx, "acme", "ship.SHIP1", []byte("acme-val"))
			Expect(err).NotTo(HaveOccurred())
			_, err = store.Put(ctx, "acme-pacific-fleet", "ship.SHIP2", []byte("apf-val"))
			Expect(err).NotTo(HaveOccurred())

			watcher, err := store.Watch(ctx, "acme")
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(watcher.Stop)

			var entries []jetstream.KeyValueEntry
			for {
				entry := <-watcher.Updates()
				if entry == nil {
					break // INIT_DONE
				}
				entries = append(entries, entry)
			}

			Expect(entries).To(HaveLen(1), "only acme's entry should appear in an acme watcher")
			Expect(entries[0].Key()).To(Equal("ship.SHIP1"), "key must be the bare key without the context prefix")
			Expect(entries[0].Value()).To(Equal([]byte("acme-val")))
		})

		It("live updates are scoped to the requested context only", func() {
			watcher, err := store.Watch(ctx, "acme")
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(watcher.Stop)

			// Consume the INIT_DONE nil marker (empty bucket)
			Eventually(watcher.Updates(), 5*time.Second).Should(Receive(BeNil()))

			_, err = store.Put(ctx, "acme", "ship.SHIP1", []byte("v1"))
			Expect(err).NotTo(HaveOccurred())
			_, err = store.Put(ctx, "acme-pacific-fleet", "ship.SHIP2", []byte("should-not-appear"))
			Expect(err).NotTo(HaveOccurred())

			var liveEntry jetstream.KeyValueEntry
			Eventually(watcher.Updates(), 5*time.Second).Should(Receive(&liveEntry),
				"acme watcher must receive the acme write")
			Expect(liveEntry.Key()).To(Equal("ship.SHIP1"))
			Expect(liveEntry.Value()).To(Equal([]byte("v1")))

			Consistently(watcher.Updates(), 300*time.Millisecond).ShouldNot(Receive(),
				"acme watcher must not receive the acme-pacific-fleet write")
		})
	})
})
