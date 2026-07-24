package refdata_test

// Integration tests against an embedded in-process NATS server (core NATS
// only — no JetStream needed) for the rpc.* dual-transport adapter (Phase
// 12.10). Same embedded-server convention as kvcache_test.go.

import (
	"context"
	"encoding/json"
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
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata/internal/natsrpc"
)

func newTestNATSConn() *nats.Conn {
	GinkgoHelper()
	opts := &server.Options{Port: -1}
	srv, err := server.NewServer(opts)
	Expect(err).NotTo(HaveOccurred())
	srv.Start()
	DeferCleanup(srv.Shutdown)
	Expect(srv.ReadyForConnections(10 * time.Second)).To(BeTrue())

	nc, err := nats.Connect(srv.ClientURL(), nats.Name("refdata-service-natsrpc-test"))
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(nc.Close)
	Expect(nc.Opts.Name).NotTo(BeEmpty(), "nats connection must be named")
	return nc
}

var _ = Describe("NATS RPC Adapter (Phase 12.10)", func() {
	const itemCtx = "emea-acme"

	var (
		ctx     context.Context
		itemH   *commands.ItemHandler
		locH    *commands.LocalizationHandler
		nc      *nats.Conn
		adapter *natsrpc.Adapter
	)

	BeforeEach(func() {
		ctx = context.Background()
		items := newFakeItemRepo()
		refs := newFakeReferenceRepo()
		locs := newFakeLocalizationRepo()
		locales := newFakeLocaleRepo()

		itemH = commands.NewItemHandler(items, refs, nil)
		locH = commands.NewLocalizationHandler(items, locs, locales, nil)

		_, err := itemH.RegisterItem(ctx, commands.ItemInput{TypeKey: "currency", Code: "EUR", Context: itemCtx})
		Expect(err).NotTo(HaveOccurred())
		Expect(locH.SetLocalization(ctx, commands.LocalizationInput{
			TypeKey: "currency", Code: "EUR", Context: itemCtx, Locale: "en", Label: "Euro",
		})).To(Succeed())

		// GBP deliberately has no localizations at all (not even "en"), so
		// requesting any locale for it exercises BR-D03's terminal
		// code-echo fallback rather than landing on the default locale.
		_, err = itemH.RegisterItem(ctx, commands.ItemInput{TypeKey: "currency", Code: "GBP", Context: itemCtx})
		Expect(err).NotTo(HaveOccurred())

		nc = newTestNATSConn()
		adapter, err = natsrpc.New(nc, natsrpc.Deps{Localizations: locH, Items: itemH})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { Expect(adapter.Stop()).To(Succeed()) })
	})

	rpcSubject := "rpc." + itemCtx + ".refdata.item.get.v1"

	Context("BR-D25: an rpc.* operation must exist as a commands/queries method already exposed via REST", func() {
		It("resolves the same item via rpc.* that ResolveItem (the REST handler's method) returns directly", func() {
			reqBody, err := json.Marshal(natsrpc.ItemGetRequest{TypeKey: "currency", Code: "EUR", Locale: "en"})
			Expect(err).NotTo(HaveOccurred())

			msg, err := nc.Request(rpcSubject, reqBody, 2*time.Second)
			Expect(err).NotTo(HaveOccurred())

			var resp natsrpc.ItemGetResponse
			Expect(json.Unmarshal(msg.Data, &resp)).To(Succeed())

			direct, err := locH.ResolveItem(ctx, "currency", itemCtx, "EUR", "en")
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Item).To(Equal(direct.Item))
			Expect(resp.Label).To(Equal(direct.Localization.Label))
			Expect(resp.IsFallback).To(Equal(direct.Localization.IsFallback))
			Expect(resp.IsFallback).To(BeFalse(), "EUR/en is a real, exact-locale match")
		})

		It("marks an rpc.* response as a fallback when the locale falls back to the code, same as ResolveItem", func() {
			reqBody, err := json.Marshal(natsrpc.ItemGetRequest{TypeKey: "currency", Code: "GBP", Locale: "ja-jp"})
			Expect(err).NotTo(HaveOccurred())

			msg, err := nc.Request(rpcSubject, reqBody, 2*time.Second)
			Expect(err).NotTo(HaveOccurred())

			var resp natsrpc.ItemGetResponse
			Expect(json.Unmarshal(msg.Data, &resp)).To(Succeed())
			Expect(resp.Label).To(Equal("GBP"))
			Expect(resp.IsFallback).To(BeTrue())
		})

		It("marks an rpc.* response as a fallback (not an exact match) when a nonsense locale falls through to the default locale's real data", func() {
			reqBody, err := json.Marshal(natsrpc.ItemGetRequest{TypeKey: "currency", Code: "EUR", Locale: "e"})
			Expect(err).NotTo(HaveOccurred())

			msg, err := nc.Request(rpcSubject, reqBody, 2*time.Second)
			Expect(err).NotTo(HaveOccurred())

			var resp natsrpc.ItemGetResponse
			Expect(json.Unmarshal(msg.Data, &resp)).To(Succeed())
			Expect(resp.Label).To(Equal("Euro"))
			Expect(resp.Locale).To(Equal("en"))
			Expect(resp.IsFallback).To(BeTrue(), "'e' itself never matched anything")
		})

		It("surfaces the same not-found condition ResolveItem itself returns", func() {
			reqBody, err := json.Marshal(natsrpc.ItemGetRequest{TypeKey: "currency", Code: "does-not-exist", Locale: "en"})
			Expect(err).NotTo(HaveOccurred())

			msg, err := nc.Request(rpcSubject, reqBody, 2*time.Second)
			Expect(err).NotTo(HaveOccurred())

			var errResp struct {
				Error string `json:"error"`
			}
			Expect(json.Unmarshal(msg.Data, &errResp)).To(Succeed())
			Expect(errResp.Error).NotTo(BeEmpty())

			_, directErr := locH.ResolveItem(ctx, "currency", itemCtx, "does-not-exist", "en")
			Expect(directErr).To(HaveOccurred())
			Expect(errResp.Error).To(Equal(directErr.Error()))
		})
	})

	Context("BR-D25/BR-D28: type.list is the rpc.* counterpart of listItems (Phase 12.11)", func() {
		typeListSubject := "rpc." + itemCtx + ".refdata.type.list.v1"

		It("resolves every assignable item of a type via rpc.* identically to ListAssignable + ResolveItem called directly", func() {
			reqBody, err := json.Marshal(natsrpc.TypeListRequest{TypeKey: "currency", Locale: "en"})
			Expect(err).NotTo(HaveOccurred())

			msg, err := nc.Request(typeListSubject, reqBody, 2*time.Second)
			Expect(err).NotTo(HaveOccurred())

			var resp natsrpc.TypeListResponse
			Expect(json.Unmarshal(msg.Data, &resp)).To(Succeed())
			Expect(resp.Items).To(HaveLen(2), "EUR and GBP were both registered in BeforeEach")

			directItems, err := itemH.ListAssignable(ctx, "currency", itemCtx)
			Expect(err).NotTo(HaveOccurred())
			byCode := map[string]natsrpc.ItemGetResponse{}
			for _, item := range resp.Items {
				byCode[item.Item.Code] = item
			}
			for _, item := range directItems {
				direct, err := locH.ResolveItem(ctx, "currency", itemCtx, item.Code, "en")
				Expect(err).NotTo(HaveOccurred())
				Expect(byCode[item.Code].Label).To(Equal(direct.Localization.Label))
				Expect(byCode[item.Code].IsFallback).To(Equal(direct.Localization.IsFallback))
			}
		})
	})

	Context("BR-D25/BR-D28: locales.list is the rpc.* counterpart of listLocales (Phase 12.11)", func() {
		localesListSubject := "rpc." + itemCtx + ".refdata.locales.list.v1"

		It("returns the same locales and default locale as ListLocales/DefaultLocale called directly", func() {
			Expect(locH.AddLocale(ctx, itemCtx, "en", true)).To(Succeed())
			Expect(locH.AddLocale(ctx, itemCtx, "fr", false)).To(Succeed())

			msg, err := nc.Request(localesListSubject, nil, 2*time.Second)
			Expect(err).NotTo(HaveOccurred())

			var resp natsrpc.LocalesListResponse
			Expect(json.Unmarshal(msg.Data, &resp)).To(Succeed())

			directLocales, err := locH.ListLocales(ctx, itemCtx)
			Expect(err).NotTo(HaveOccurred())
			directDefault, err := locH.DefaultLocale(ctx, itemCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Locales).To(ConsistOf(directLocales))
			Expect(resp.DefaultLocale).To(Equal(directDefault))
			Expect(resp.DefaultLocale).To(Equal("en"))
		})
	})

	Context("BR-D26: an obs.rpc.* publish must never block or fail the real RPC reply", func() {
		It("still returns the real reply promptly with no obs.rpc.> subscriber at all", func() {
			reqBody, err := json.Marshal(natsrpc.ItemGetRequest{TypeKey: "currency", Code: "EUR", Locale: "en"})
			Expect(err).NotTo(HaveOccurred())

			start := time.Now()
			msg, err := nc.Request(rpcSubject, reqBody, 2*time.Second)
			Expect(err).NotTo(HaveOccurred())
			Expect(time.Since(start)).To(BeNumerically("<", 500*time.Millisecond))

			var resp natsrpc.ItemGetResponse
			Expect(json.Unmarshal(msg.Data, &resp)).To(Succeed())
			Expect(resp.Label).To(Equal("Euro"))
		})

		It("still returns the real reply promptly even with a slow obs.rpc.> subscriber", func() {
			sub, err := nc.Subscribe("obs.rpc.>", func(*nats.Msg) {
				time.Sleep(2 * time.Second) // deliberately slower than the RPC timeout below
			})
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { Expect(sub.Unsubscribe()).To(Succeed()) })

			reqBody, err := json.Marshal(natsrpc.ItemGetRequest{TypeKey: "currency", Code: "EUR", Locale: "en"})
			Expect(err).NotTo(HaveOccurred())

			start := time.Now()
			msg, err := nc.Request(rpcSubject, reqBody, 2*time.Second)
			Expect(err).NotTo(HaveOccurred())
			Expect(time.Since(start)).To(BeNumerically("<", 500*time.Millisecond))

			var resp natsrpc.ItemGetResponse
			Expect(json.Unmarshal(msg.Data, &resp)).To(Succeed())
			Expect(resp.Label).To(Equal("Euro"))
		})

		It("still publishes the reply-side obs.rpc.* event when the real call errors", func() {
			obsMsgs := make(chan *nats.Msg, 4)
			sub, err := nc.Subscribe("obs.rpc.>", func(m *nats.Msg) { obsMsgs <- m })
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { Expect(sub.Unsubscribe()).To(Succeed()) })

			reqBody, err := json.Marshal(natsrpc.ItemGetRequest{TypeKey: "currency", Code: "does-not-exist", Locale: "en"})
			Expect(err).NotTo(HaveOccurred())
			_, err = nc.Request(rpcSubject, reqBody, 2*time.Second)
			Expect(err).NotTo(HaveOccurred())

			var sawErroredReply bool
			for i := 0; i < 2; i++ {
				select {
				case m := <-obsMsgs:
					var env struct {
						Direction string `json:"direction"`
						Error     string `json:"error"`
					}
					Expect(json.Unmarshal(m.Data, &env)).To(Succeed())
					if env.Direction == "reply" && env.Error != "" {
						sawErroredReply = true
					}
				case <-time.After(2 * time.Second):
					Fail("timed out waiting for obs.rpc.* events")
				}
			}
			Expect(sawErroredReply).To(BeTrue(), "expected the reply-side obs event to carry the error even on failure")
		})
	})

	Context("BR-D27: an rpc.* lookup backfills the KV cache just like REST's getItem handler", func() {
		It("writes a fresh cache entry after a cold rpc.* lookup, using the same Projector.Backfill REST relies on", func() {
			// This adapter needs its own JetStream-backed nc (unlike the
			// plain core-NATS nc used by the rest of this file) since the KV
			// cache it backfills into is JetStream-backed — same embedded-
			// server convention as kvcache_test.go's newRefdataJetStream().
			opts := &server.Options{JetStream: true, StoreDir: GinkgoT().TempDir(), Port: -1}
			srv, err := server.NewServer(opts)
			Expect(err).NotTo(HaveOccurred())
			srv.Start()
			DeferCleanup(srv.Shutdown)
			Expect(srv.ReadyForConnections(10 * time.Second)).To(BeTrue())

			jsNC, err := nats.Connect(srv.ClientURL(), nats.Name("refdata-service-natsrpc-backfill-test"))
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(jsNC.Close)

			js, err := jetstream.New(jsNC)
			Expect(err).NotTo(HaveOccurred())
			_, err = jstream.CreateChangeStream(ctx, js, kvcache.ChangeStreamName, []string{kvcache.ChangeSubjectWildcard}, time.Hour)
			Expect(err).NotTo(HaveOccurred())

			kv := kvstore.New(js, "refdata")
			items := newFakeItemRepo()
			refs := newFakeReferenceRepo()
			locs := newFakeLocalizationRepo()
			locales := newFakeLocaleRepo()
			versions := newFakeVersionRepo()
			projector := kvcache.NewProjector(kv, items, locs, refs, versions, jstream.NewPublisher(js))

			itemH := commands.NewItemHandler(items, refs, nil) // nil notifier: writes don't auto-project, so the cache starts cold
			backfillLocH := commands.NewLocalizationHandler(items, locs, locales, nil)
			_, err = itemH.RegisterItem(ctx, commands.ItemInput{TypeKey: "currency", Code: "EUR", Context: itemCtx})
			Expect(err).NotTo(HaveOccurred())
			Expect(backfillLocH.SetLocalization(ctx, commands.LocalizationInput{
				TypeKey: "currency", Code: "EUR", Context: itemCtx, Locale: "en", Label: "Euro",
			})).To(Succeed())

			_, _, err = kv.Get(ctx, itemCtx, "currency.EUR")
			Expect(err).To(HaveOccurred(), "cache starts cold — nothing has written it yet")

			backfillAdapter, err := natsrpc.New(jsNC, natsrpc.Deps{Localizations: backfillLocH, Projector: projector})
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { Expect(backfillAdapter.Stop()).To(Succeed()) })

			reqBody, err := json.Marshal(natsrpc.ItemGetRequest{TypeKey: "currency", Code: "EUR", Locale: "en"})
			Expect(err).NotTo(HaveOccurred())
			_, err = jsNC.Request(rpcSubject, reqBody, 2*time.Second)
			Expect(err).NotTo(HaveOccurred())

			raw, _, err := kv.Get(ctx, itemCtx, "currency.EUR")
			Expect(err).NotTo(HaveOccurred(), "the rpc.* read should have backfilled the cache, same as REST's getItem")
			var entry kvcache.Entry
			Expect(json.Unmarshal(raw, &entry)).To(Succeed())
			Expect(entry.Item.Code).To(Equal("EUR"))
		})
	})
})

// item.get-versioned needs a JetStream-backed KV bucket (VersionReader has
// no Postgres-free/in-memory mode), so it gets its own Describe with its own
// embedded JetStream server rather than sharing the plain core-NATS nc the
// rest of this file uses — same convention as the BR-D27 backfill test
// above. It seeds the versioned bucket directly via VersionMaterializer, so
// (like the rest of this file) it needs no real Postgres.
var _ = Describe("BR-D25/BR-D28: item.get-versioned is the rpc.* counterpart of getVersionedItem (Phase 12.11)", func() {
	const itemCtx = "emea-acme"

	It("resolves a pinned corpus version via rpc.* identically to VersionReader.Get called directly", func() {
		ctx := context.Background()
		opts := &server.Options{JetStream: true, StoreDir: GinkgoT().TempDir(), Port: -1}
		srv, err := server.NewServer(opts)
		Expect(err).NotTo(HaveOccurred())
		srv.Start()
		DeferCleanup(srv.Shutdown)
		Expect(srv.ReadyForConnections(10 * time.Second)).To(BeTrue())

		nc, err := nats.Connect(srv.ClientURL(), nats.Name("refdata-service-natsrpc-versioned-test"))
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(nc.Close)

		js, err := jetstream.New(nc)
		Expect(err).NotTo(HaveOccurred())
		kv := kvstore.New(js, "refdata")

		materializer := kvcache.NewVersionMaterializer(kv)
		Expect(materializer.Materialize(ctx, itemCtx, 1, []domain.CorpusItem{
			{DictionaryItem: domain.DictionaryItem{TypeKey: "currency", Code: "EUR", Context: itemCtx, Status: domain.StatusActive}},
		}, []domain.CorpusLocalization{
			{Localization: domain.Localization{TypeKey: "currency", Code: "EUR", Context: itemCtx, Locale: "en", Label: "Euro"}},
		}, 0)).To(Succeed())

		versionReader := kvcache.NewVersionReader(kv)
		adapter, err := natsrpc.New(nc, natsrpc.Deps{VersionReader: versionReader})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { Expect(adapter.Stop()).To(Succeed()) })

		reqBody, err := json.Marshal(natsrpc.ItemGetVersionedRequest{TypeKey: "currency", Code: "EUR", Version: 1})
		Expect(err).NotTo(HaveOccurred())
		subject := "rpc." + itemCtx + ".refdata.item.get-versioned.v1"
		msg, err := nc.Request(subject, reqBody, 2*time.Second)
		Expect(err).NotTo(HaveOccurred())

		var resp natsrpc.ItemGetVersionedResponse
		Expect(json.Unmarshal(msg.Data, &resp)).To(Succeed())

		direct, err := versionReader.Get(ctx, itemCtx, 1, "currency", "EUR")
		Expect(err).NotTo(HaveOccurred())
		Expect(resp).To(Equal(direct))
		Expect(resp.Localizations["en"].Label).To(Equal("Euro"))
	})

	It("surfaces the same not-found condition VersionReader.Get itself returns for an unknown version", func() {
		ctx := context.Background()
		opts := &server.Options{JetStream: true, StoreDir: GinkgoT().TempDir(), Port: -1}
		srv, err := server.NewServer(opts)
		Expect(err).NotTo(HaveOccurred())
		srv.Start()
		DeferCleanup(srv.Shutdown)
		Expect(srv.ReadyForConnections(10 * time.Second)).To(BeTrue())

		nc, err := nats.Connect(srv.ClientURL(), nats.Name("refdata-service-natsrpc-versioned-notfound-test"))
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(nc.Close)

		js, err := jetstream.New(nc)
		Expect(err).NotTo(HaveOccurred())
		kv := kvstore.New(js, "refdata")
		versionReader := kvcache.NewVersionReader(kv)

		adapter, err := natsrpc.New(nc, natsrpc.Deps{VersionReader: versionReader})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { Expect(adapter.Stop()).To(Succeed()) })

		reqBody, err := json.Marshal(natsrpc.ItemGetVersionedRequest{TypeKey: "currency", Code: "EUR", Version: 999})
		Expect(err).NotTo(HaveOccurred())
		subject := "rpc." + itemCtx + ".refdata.item.get-versioned.v1"
		msg, err := nc.Request(subject, reqBody, 2*time.Second)
		Expect(err).NotTo(HaveOccurred())

		var errResp struct {
			Error string `json:"error"`
		}
		Expect(json.Unmarshal(msg.Data, &errResp)).To(Succeed())
		Expect(errResp.Error).NotTo(BeEmpty())

		_, directErr := versionReader.Get(ctx, itemCtx, 999, "currency", "EUR")
		Expect(directErr).To(HaveOccurred())
		Expect(errResp.Error).To(Equal(directErr.Error()))
	})
})
