package refdata_test

// Integration tests against an embedded in-process NATS server (real
// JetStream, real KV) — same convention as the shipping backend's
// integration_test.go. Covers the Q5 versioned-read protocol (Phase 11.3):
// set-version bump atomicity, cache rebuild on mutation, change-event
// publication, cold start, and miss backfill.

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata/internal/application/commands"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata/internal/domain"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata/internal/jstream"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata/internal/kvcache"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata/internal/kvstore"
)

func newRefdataJetStream() jetstream.JetStream {
	GinkgoHelper()
	opts := &server.Options{JetStream: true, StoreDir: GinkgoT().TempDir(), Port: -1}
	srv, err := server.NewServer(opts)
	Expect(err).NotTo(HaveOccurred())
	srv.Start()
	DeferCleanup(srv.Shutdown)
	Expect(srv.ReadyForConnections(10 * time.Second)).To(BeTrue())

	nc, err := nats.Connect(srv.ClientURL(), nats.Name("refdata-service-test"))
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(nc.Close)
	Expect(nc.Opts.Name).NotTo(BeEmpty(), "nats connection must be named")

	js, err := jetstream.New(nc)
	Expect(err).NotTo(HaveOccurred())

	_, err = jstream.CreateChangeStream(context.Background(), js, kvcache.ChangeStreamName, []string{kvcache.ChangeSubjectWildcard}, time.Hour)
	Expect(err).NotTo(HaveOccurred())
	return js
}

var _ = Describe("KV cache + versioned-read protocol (Phase 11.3)", func() {
	const itemCtx = "emea-acme"

	var (
		ctx       context.Context
		js        jetstream.JetStream
		kv        *kvstore.Store
		items     *fakeItemRepo
		refs      *fakeReferenceRepo
		locs      *fakeLocalizationRepo
		versions  *fakeVersionRepo
		projector *kvcache.Projector
		itemH     *commands.ItemHandler
	)

	BeforeEach(func() {
		ctx = context.Background()
		js = newRefdataJetStream()
		kv = kvstore.New(js, "refdata")
		items = newFakeItemRepo()
		refs = newFakeReferenceRepo()
		locs = newFakeLocalizationRepo()
		versions = newFakeVersionRepo()
		projector = kvcache.NewProjector(kv, items, locs, refs, versions, newTestNamespaces(domain.DictionaryType{TypeKey: "currency", Category: domain.CategoryStandards}), jstream.NewPublisher(js))
		itemH = commands.NewItemHandler(items, refs, projector)
	})

	It("bumps the set version atomically — monotonic under concurrent mutations", func() {
		var wg sync.WaitGroup
		for i := 0; i < 20; i++ {
			wg.Add(1)
			go func(n int) {
				defer wg.Done()
				defer GinkgoRecover()
				_, err := itemH.RegisterItem(ctx, commands.ItemInput{
					TypeKey: "currency", Code: itemCode(n), Context: itemCtx,
				})
				Expect(err).NotTo(HaveOccurred())
			}(i)
		}
		wg.Wait()

		version, err := versions.Current(ctx, itemCtx, "currency")
		Expect(err).NotTo(HaveOccurred())
		Expect(version).To(Equal(20)) // 20 distinct bumps, none lost to a race
	})

	It("rebuilds the item's KV cache entry on registration, stamped with the new version", func() {
		_, err := itemH.RegisterItem(ctx, commands.ItemInput{TypeKey: "currency", Code: "EUR", Context: itemCtx, Attrs: map[string]any{"name": "Euro"}})
		Expect(err).NotTo(HaveOccurred())

		raw, _, err := kv.Get(ctx, itemCtx, "currency.EUR")
		Expect(err).NotTo(HaveOccurred())
		var entry kvcache.Entry
		Expect(json.Unmarshal(raw, &entry)).To(Succeed())
		Expect(entry.Item.Code).To(Equal("EUR"))
		Expect(entry.Version).To(Equal(1))
	})

	It("writes the type's _meta entry with the current version and item count", func() {
		_, err := itemH.RegisterItem(ctx, commands.ItemInput{TypeKey: "currency", Code: "EUR", Context: itemCtx})
		Expect(err).NotTo(HaveOccurred())
		_, err = itemH.RegisterItem(ctx, commands.ItemInput{TypeKey: "currency", Code: "GBP", Context: itemCtx})
		Expect(err).NotTo(HaveOccurred())

		raw, _, err := kv.Get(ctx, itemCtx, "currency._meta")
		Expect(err).NotTo(HaveOccurred())
		var meta kvcache.MetaEntry
		Expect(json.Unmarshal(raw, &meta)).To(Succeed())
		Expect(meta.Version).To(Equal(2))
		Expect(meta.ItemCount).To(Equal(2))
	})

	It("publishes a change-event pointer (never state) on every mutation", func() {
		consumer, err := js.CreateOrUpdateConsumer(ctx, kvcache.ChangeStreamName, jetstream.ConsumerConfig{
			DeliverPolicy: jetstream.DeliverAllPolicy,
		})
		Expect(err).NotTo(HaveOccurred())

		_, err = itemH.RegisterItem(ctx, commands.ItemInput{TypeKey: "currency", Code: "EUR", Context: itemCtx})
		Expect(err).NotTo(HaveOccurred())

		msgs, err := consumer.Fetch(1, jetstream.FetchMaxWait(5*time.Second))
		Expect(err).NotTo(HaveOccurred())
		var msg jetstream.Msg
		for m := range msgs.Messages() {
			msg = m
		}
		Expect(msg).NotTo(BeNil())
		Expect(msg.Subject()).To(Equal(kvcache.ChangeSubject(itemCtx, "currency")))

		var event kvcache.ChangeEvent
		Expect(json.Unmarshal(msg.Data(), &event)).To(Succeed())
		Expect(event.TypeKey).To(Equal("currency"))
		Expect(event.Version).To(Equal(1))
	})

	It("cold start — the KV bucket does not exist yet, and the first mutation creates it", func() {
		_, _, err := kv.Get(ctx, itemCtx, "currency.EUR")
		Expect(errors.Is(err, jetstream.ErrBucketNotFound) || errors.Is(err, jetstream.ErrKeyNotFound)).To(BeTrue())

		_, err = itemH.RegisterItem(ctx, commands.ItemInput{TypeKey: "currency", Code: "EUR", Context: itemCtx})
		Expect(err).NotTo(HaveOccurred())

		_, _, err = kv.Get(ctx, itemCtx, "currency.EUR")
		Expect(err).NotTo(HaveOccurred())
	})

	It("backfills a missing cache entry on a miss, without bumping the version or publishing an event", func() {
		_, err := itemH.RegisterItem(ctx, commands.ItemInput{TypeKey: "currency", Code: "EUR", Context: itemCtx})
		Expect(err).NotTo(HaveOccurred())
		versionBefore, err := versions.Current(ctx, itemCtx, "currency")
		Expect(err).NotTo(HaveOccurred())

		Expect(kv.Delete(ctx, itemCtx, "currency.EUR")).To(Succeed())
		_, _, err = kv.Get(ctx, itemCtx, "currency.EUR")
		Expect(err).To(HaveOccurred())

		Expect(projector.Backfill(ctx, itemCtx, "currency", "EUR")).To(Succeed())

		raw, _, err := kv.Get(ctx, itemCtx, "currency.EUR")
		Expect(err).NotTo(HaveOccurred())
		var entry kvcache.Entry
		Expect(json.Unmarshal(raw, &entry)).To(Succeed())
		Expect(entry.Item.Code).To(Equal("EUR"))
		Expect(entry.Version).To(Equal(versionBefore))

		versionAfter, err := versions.Current(ctx, itemCtx, "currency")
		Expect(err).NotTo(HaveOccurred())
		Expect(versionAfter).To(Equal(versionBefore)) // Backfill never bumps
	})

	It("removes the cache entry when the item is deleted", func() {
		_, err := itemH.RegisterItem(ctx, commands.ItemInput{TypeKey: "currency", Code: "EUR", Context: itemCtx})
		Expect(err).NotTo(HaveOccurred())
		Expect(itemH.DeleteItem(ctx, "currency", itemCtx, "EUR")).To(Succeed())

		_, _, err = kv.Get(ctx, itemCtx, "currency.EUR")
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("BR-D31: a domain-enum type's KV entries are keyed under the enum. namespace", func() {
	const itemCtx = "emea-acme"

	var (
		ctx       context.Context
		kv        *kvstore.Store
		items     *fakeItemRepo
		projector *kvcache.Projector
		itemH     *commands.ItemHandler
	)

	BeforeEach(func() {
		ctx = context.Background()
		js := newRefdataJetStream()
		kv = kvstore.New(js, "refdata")
		items = newFakeItemRepo()
		refs := newFakeReferenceRepo()
		locs := newFakeLocalizationRepo()
		versions := newFakeVersionRepo()
		namespaces := newTestNamespaces(
			domain.DictionaryType{TypeKey: "ship-status", Category: domain.CategoryDomainEnum},
			domain.DictionaryType{TypeKey: "currency", Category: domain.CategoryStandards},
		)
		projector = kvcache.NewProjector(kv, items, locs, refs, versions, namespaces, jstream.NewPublisher(js))
		itemH = commands.NewItemHandler(items, refs, projector)
	})

	It("writes an enum item's entry to enum.{type}.{code}, not {type}.{code}", func() {
		_, err := itemH.RegisterItem(ctx, commands.ItemInput{TypeKey: "ship-status", Code: "in-transit", Context: itemCtx})
		Expect(err).NotTo(HaveOccurred())

		raw, _, err := kv.Get(ctx, itemCtx, "enum.ship-status.in-transit")
		Expect(err).NotTo(HaveOccurred())
		var entry kvcache.Entry
		Expect(json.Unmarshal(raw, &entry)).To(Succeed())
		Expect(entry.Item.Code).To(Equal("in-transit"))
		Expect(entry.Item.TypeKey).To(Equal("ship-status"))

		_, _, err = kv.Get(ctx, itemCtx, "ship-status.in-transit")
		Expect(err).To(HaveOccurred(), "the unnamespaced key must not be written")
	})

	It("keeps an enum type's _meta stamp in the same namespace as its items", func() {
		_, err := itemH.RegisterItem(ctx, commands.ItemInput{TypeKey: "ship-status", Code: "docked", Context: itemCtx})
		Expect(err).NotTo(HaveOccurred())

		raw, _, err := kv.Get(ctx, itemCtx, "enum.ship-status._meta")
		Expect(err).NotTo(HaveOccurred())
		var meta kvcache.MetaEntry
		Expect(json.Unmarshal(raw, &meta)).To(Succeed())
		Expect(meta.ItemCount).To(Equal(1))

		_, _, err = kv.Get(ctx, itemCtx, "ship-status._meta")
		Expect(err).To(HaveOccurred())
	})

	It("puts the whole type under one enum.{type}.> subtree — nothing of it escapes the namespace", func() {
		for _, code := range []string{"docked", "in-transit"} {
			_, err := itemH.RegisterItem(ctx, commands.ItemInput{TypeKey: "ship-status", Code: code, Context: itemCtx})
			Expect(err).NotTo(HaveOccurred())
		}

		keys, err := kv.Keys(ctx, itemCtx)
		Expect(err).NotTo(HaveOccurred())
		Expect(keys).To(ConsistOf(
			"enum.ship-status.docked",
			"enum.ship-status.in-transit",
			"enum.ship-status._meta",
		))
	})

	It("leaves a non-enum category unnamespaced", func() {
		_, err := itemH.RegisterItem(ctx, commands.ItemInput{TypeKey: "currency", Code: "EUR", Context: itemCtx})
		Expect(err).NotTo(HaveOccurred())

		_, _, err = kv.Get(ctx, itemCtx, "currency.EUR")
		Expect(err).NotTo(HaveOccurred())
		_, _, err = kv.Get(ctx, itemCtx, "enum.currency.EUR")
		Expect(err).To(HaveOccurred())
	})

	It("reads an enum type back through the same namespace — ReadEntry and ReadType agree with the write path", func() {
		_, err := itemH.RegisterItem(ctx, commands.ItemInput{TypeKey: "ship-status", Code: "in-transit", Context: itemCtx})
		Expect(err).NotTo(HaveOccurred())

		entry, err := projector.ReadEntry(ctx, itemCtx, "ship-status", "in-transit")
		Expect(err).NotTo(HaveOccurred())
		Expect(entry).NotTo(BeNil(), "a namespaced entry must be found by the KV-first read path")
		Expect(entry.Item.Code).To(Equal("in-transit"))

		entries, ok := projector.ReadType(ctx, itemCtx, "ship-status")
		Expect(ok).To(BeTrue())
		Expect(entries).To(HaveLen(1))
		Expect(entries[0].Item.Code).To(Equal("in-transit"))
	})

	It("removes the namespaced key when an enum item is deleted", func() {
		_, err := itemH.RegisterItem(ctx, commands.ItemInput{TypeKey: "ship-status", Code: "docked", Context: itemCtx})
		Expect(err).NotTo(HaveOccurred())
		Expect(itemH.DeleteItem(ctx, "ship-status", itemCtx, "docked")).To(Succeed())

		_, _, err = kv.Get(ctx, itemCtx, "enum.ship-status.docked")
		Expect(err).To(HaveOccurred())
	})

	It("falls back to the unnamespaced key for a type that is not registered in the type registry", func() {
		_, err := itemH.RegisterItem(ctx, commands.ItemInput{TypeKey: "unregistered", Code: "X", Context: itemCtx})
		Expect(err).NotTo(HaveOccurred())

		_, _, err = kv.Get(ctx, itemCtx, "unregistered.X")
		Expect(err).NotTo(HaveOccurred())
	})
})

func itemCode(n int) string {
	codes := []string{"AAA", "AAB", "AAC", "AAD", "AAE", "AAF", "AAG", "AAH", "AAI", "AAJ",
		"AAK", "AAL", "AAM", "AAN", "AAO", "AAP", "AAQ", "AAR", "AAS", "AAT"}
	return codes[n]
}
