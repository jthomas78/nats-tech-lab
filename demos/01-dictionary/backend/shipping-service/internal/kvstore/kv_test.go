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
	"github.com/jthomas78/nats-tech-lab/shared/natstrace"
)

func newJSForKV() (jetstream.JetStream, *nats.Conn) {
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
	return js, nc
}

var _ = Describe("KVStore — per-tenant bucket with context-prefixed keys", func() {
	var (
		ctx   context.Context
		js    jetstream.JetStream
		nc    *nats.Conn
		store *kvstore.Store
	)

	BeforeEach(func() {
		ctx = context.Background()
		js, nc = newJSForKV()
		store = kvstore.New(js, "ships")
	})

	Describe("Put / Get", func() {
		It("stores and retrieves a value by context and key", func() {
			_, err := store.Put(ctx, "acme-pacific-fleet", "ship.SHIP1", []byte(`{"id":"SHIP1"}`))
			Expect(err).NotTo(HaveOccurred())

			val, _, err := store.Get(ctx, "acme-pacific-fleet", "ship.SHIP1")
			Expect(err).NotTo(HaveOccurred())
			Expect(val).To(Equal([]byte(`{"id":"SHIP1"}`)))
		})

		It("returns ErrKeyNotFound for a missing key", func() {
			_, _, err := store.Get(ctx, "acme-pacific-fleet", "ship.MISSING")
			Expect(err).To(MatchError(jetstream.ErrKeyNotFound))
		})

		It("does not leak across contexts — getting a key from a different context returns not found", func() {
			_, err := store.Put(ctx, "acme-pacific-fleet", "ship.SHIP1", []byte("v"))
			Expect(err).NotTo(HaveOccurred())

			_, _, err = store.Get(ctx, "acme-atlantic-fleet", "ship.SHIP1")
			Expect(err).To(MatchError(jetstream.ErrKeyNotFound),
				"acme-atlantic-fleet must not see acme-pacific-fleet's key even in the same bucket")
		})
	})

	Describe("Delete", func() {
		It("removes a key so subsequent Get returns ErrKeyNotFound", func() {
			_, err := store.Put(ctx, "acme-pacific-fleet", "ship.SHIP1", []byte("v"))
			Expect(err).NotTo(HaveOccurred())

			Expect(store.Delete(ctx, "acme-pacific-fleet", "ship.SHIP1")).To(Succeed())

			_, _, err = store.Get(ctx, "acme-pacific-fleet", "ship.SHIP1")
			Expect(err).To(MatchError(jetstream.ErrKeyNotFound))
		})
	})

	Describe("Keys() — context isolation", func() {
		It("uses one shared bucket while returning non-overlapping keys for each context", func() {
			_, err := store.Put(ctx, "acme-pacific-fleet", "ship.SHIP1", []byte("a"))
			Expect(err).NotTo(HaveOccurred())
			_, err = store.Put(ctx, "acme-atlantic-fleet", "ship.SHIP2", []byte("b"))
			Expect(err).NotTo(HaveOccurred())

			buckets := js.KeyValueStores(ctx)
			var names []string
			for status := range buckets.Status() {
				names = append(names, status.Bucket())
			}
			Expect(buckets.Error()).NotTo(HaveOccurred())
			Expect(names).To(ConsistOf("ships"),
				"two contexts must share the tenant-scoped ships bucket")

			pacificKeys, err := store.Keys(ctx, "acme-pacific-fleet")
			Expect(err).NotTo(HaveOccurred())
			Expect(pacificKeys).To(ConsistOf("ship.SHIP1"))
			atlanticKeys, err := store.Keys(ctx, "acme-atlantic-fleet")
			Expect(err).NotTo(HaveOccurred())
			Expect(atlanticKeys).To(ConsistOf("ship.SHIP2"))
		})

		It("returns only keys for the requested context, without the {context}. prefix", func() {
			_, err := store.Put(ctx, "acme-pacific-fleet", "ship.SHIP1", []byte("a"))
			Expect(err).NotTo(HaveOccurred())
			_, err = store.Put(ctx, "acme-atlantic-fleet", "ship.SHIP2", []byte("b"))
			Expect(err).NotTo(HaveOccurred())
			_, err = store.Put(ctx, "acme-pacific-fleet", "ship.SHIP3", []byte("c"))
			Expect(err).NotTo(HaveOccurred())

			pacificKeys, err := store.Keys(ctx, "acme-pacific-fleet")
			Expect(err).NotTo(HaveOccurred())
			Expect(pacificKeys).To(ConsistOf("ship.SHIP1", "ship.SHIP3"),
				"acme-pacific-fleet Keys() must return bare keys and must not include acme-atlantic-fleet entries")

			atlanticKeys, err := store.Keys(ctx, "acme-atlantic-fleet")
			Expect(err).NotTo(HaveOccurred())
			Expect(atlanticKeys).To(ConsistOf("ship.SHIP2"),
				"acme-atlantic-fleet Keys() must return bare keys and must not include acme-pacific-fleet entries")
		})
	})

	Describe("Watch() — context isolation and prefix stripping", func() {
		It("initial replay contains only entries for the requested context, with bare keys", func() {
			_, err := store.Put(ctx, "acme-pacific-fleet", "ship.SHIP1", []byte("apf-val"))
			Expect(err).NotTo(HaveOccurred())
			_, err = store.Put(ctx, "acme-atlantic-fleet", "ship.SHIP2", []byte("alf-val"))
			Expect(err).NotTo(HaveOccurred())

			watcher, err := store.Watch(ctx, "acme-pacific-fleet")
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

			Expect(entries).To(HaveLen(1), "only acme-pacific-fleet's entry should appear in a pacific-fleet watcher")
			Expect(entries[0].Key()).To(Equal("ship.SHIP1"), "key must be the bare key without the context prefix")
			Expect(entries[0].Value()).To(Equal([]byte("apf-val")))
		})

		It("live updates are scoped to the requested context only", func() {
			watcher, err := store.Watch(ctx, "acme-pacific-fleet")
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(watcher.Stop)

			// Consume the INIT_DONE nil marker (empty bucket)
			Eventually(watcher.Updates(), 5*time.Second).Should(Receive(BeNil()))

			_, err = store.Put(ctx, "acme-pacific-fleet", "ship.SHIP1", []byte("v1"))
			Expect(err).NotTo(HaveOccurred())
			_, err = store.Put(ctx, "acme-atlantic-fleet", "ship.SHIP2", []byte("should-not-appear"))
			Expect(err).NotTo(HaveOccurred())

			var liveEntry jetstream.KeyValueEntry
			Eventually(watcher.Updates(), 5*time.Second).Should(Receive(&liveEntry),
				"acme-pacific-fleet watcher must receive the pacific-fleet write")
			Expect(liveEntry.Key()).To(Equal("ship.SHIP1"))
			Expect(liveEntry.Value()).To(Equal([]byte("v1")))

			Consistently(watcher.Updates(), 300*time.Millisecond).ShouldNot(Receive(),
				"acme-pacific-fleet watcher must not receive the acme-atlantic-fleet write")
		})
	})

	Describe("EnableNotify", func() {
		It("does not publish before EnableNotify is called", func() {
			sub, err := nc.SubscribeSync("notify.>")
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(sub.Unsubscribe)

			_, err = store.Put(ctx, "acme-pacific-fleet", "ship.SHIP1", []byte("v1"))
			Expect(err).NotTo(HaveOccurred())

			_, err = sub.NextMsg(200 * time.Millisecond)
			Expect(err).To(MatchError(nats.ErrTimeout))
		})

		It("publishes notify.{context}.kv.{bucket}.{key}.changed with the new value after Put", func() {
			store.EnableNotify(nc, nil)

			sub, err := nc.SubscribeSync("notify.>")
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(sub.Unsubscribe)

			_, err = store.Put(ctx, "acme-pacific-fleet", "ship.SHIP1", []byte("v1"))
			Expect(err).NotTo(HaveOccurred())

			msg, err := sub.NextMsg(2 * time.Second)
			Expect(err).NotTo(HaveOccurred())
			Expect(msg.Subject).To(Equal("notify.acme-pacific-fleet.kv.ships.ship.SHIP1.changed"))
			Expect(msg.Data).To(Equal([]byte("v1")))
		})

		It("publishes an empty-payload notify on Delete, distinguishing DEL from PUT", func() {
			store.EnableNotify(nc, nil)
			_, err := store.Put(ctx, "acme-pacific-fleet", "ship.SHIP1", []byte("v1"))
			Expect(err).NotTo(HaveOccurred())

			sub, err := nc.SubscribeSync("notify.>")
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(sub.Unsubscribe)

			Expect(store.Delete(ctx, "acme-pacific-fleet", "ship.SHIP1")).To(Succeed())

			msg, err := sub.NextMsg(2 * time.Second)
			Expect(err).NotTo(HaveOccurred())
			Expect(msg.Subject).To(Equal("notify.acme-pacific-fleet.kv.ships.ship.SHIP1.changed"))
			Expect(msg.Data).To(BeEmpty())
		})

		// Phase 28d (BR-037): a NATS KV entry itself can never carry a
		// traceparent (jetstream.KeyValue.Put takes no headers), so the
		// derived notify is what lets a trace waterfall show the write's
		// async tail. ctx carries the span the same way commands.go's
		// publish and eventhandler's projectors do, via
		// natstrace.ContextWithSpan.
		It("attaches a Traceparent header to the notify publish when ctx carries a span", func() {
			store.EnableNotify(nc, nil)
			tracer := natstrace.New(nc)
			sp := tracer.StartFromHeaders(nil, "evt.acme-pacific-fleet.shipping.ship.SHIP1.arrived", nil, "acme-pacific-fleet", "shipping", "ship", "arrived")
			spanCtx := natstrace.ContextWithSpan(ctx, sp)

			sub, err := nc.SubscribeSync("notify.>")
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(sub.Unsubscribe)

			_, err = store.Put(spanCtx, "acme-pacific-fleet", "ship.SHIP1", []byte("v1"))
			Expect(err).NotTo(HaveOccurred())

			msg, err := sub.NextMsg(2 * time.Second)
			Expect(err).NotTo(HaveOccurred())
			Expect(msg.Header.Get(natstrace.TraceparentHeader)).To(Equal(sp.Traceparent()))
		})

		// Phase 43a (BR-045): the KV-change notify is one of the five
		// instrumented notify.* call sites. Its observation names its tokens
		// explicitly — the key is itself dotted, so nothing positional would
		// be reliable here.
		It("observes the notify publish on obs.pubsub.{context}.kv.{bucket}.changed", func() {
			store.EnableNotify(nc, nil)

			sub, err := nc.SubscribeSync("obs.pubsub.>")
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(sub.Unsubscribe)

			_, err = store.Put(ctx, "acme-pacific-fleet", "ship.SHIP1", []byte(`{"id":"SHIP1"}`))
			Expect(err).NotTo(HaveOccurred())

			msg, err := sub.NextMsg(2 * time.Second)
			Expect(err).NotTo(HaveOccurred())
			Expect(msg.Subject).To(Equal("obs.pubsub.acme-pacific-fleet.kv.ships.changed"))
			Expect(string(msg.Data)).To(ContainSubstring(`"subject":"notify.acme-pacific-fleet.kv.ships.ship.SHIP1.changed"`))
		})

		It("omits the Traceparent header cleanly when ctx carries no span", func() {
			store.EnableNotify(nc, nil)

			sub, err := nc.SubscribeSync("notify.>")
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(sub.Unsubscribe)

			_, err = store.Put(ctx, "acme-pacific-fleet", "ship.SHIP1", []byte("v1"))
			Expect(err).NotTo(HaveOccurred())

			msg, err := sub.NextMsg(2 * time.Second)
			Expect(err).NotTo(HaveOccurred())
			Expect(msg.Header.Get(natstrace.TraceparentHeader)).To(BeEmpty())
		})
	})
})
