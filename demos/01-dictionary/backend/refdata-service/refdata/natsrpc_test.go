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
	const itemCtx = "acme-test"

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
			reqBody, err := json.Marshal(natsrpc.ItemGetRequest{Context: itemCtx, TypeKey: "currency", Code: "EUR", Locale: "en"})
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
			reqBody, err := json.Marshal(natsrpc.ItemGetRequest{Context: itemCtx, TypeKey: "currency", Code: "GBP", Locale: "ja-jp"})
			Expect(err).NotTo(HaveOccurred())

			msg, err := nc.Request(rpcSubject, reqBody, 2*time.Second)
			Expect(err).NotTo(HaveOccurred())

			var resp natsrpc.ItemGetResponse
			Expect(json.Unmarshal(msg.Data, &resp)).To(Succeed())
			Expect(resp.Label).To(Equal("GBP"))
			Expect(resp.IsFallback).To(BeTrue())
		})

		It("marks an rpc.* response as a fallback (not an exact match) when a nonsense locale falls through to the default locale's real data", func() {
			reqBody, err := json.Marshal(natsrpc.ItemGetRequest{Context: itemCtx, TypeKey: "currency", Code: "EUR", Locale: "e"})
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
			reqBody, err := json.Marshal(natsrpc.ItemGetRequest{Context: itemCtx, TypeKey: "currency", Code: "does-not-exist", Locale: "en"})
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
			reqBody, err := json.Marshal(natsrpc.TypeListRequest{Context: itemCtx, TypeKey: "currency", Locale: "en"})
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

			reqBody, err := json.Marshal(natsrpc.LocalesListRequest{Context: itemCtx})
			Expect(err).NotTo(HaveOccurred())
			msg, err := nc.Request(localesListSubject, reqBody, 2*time.Second)
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

	Context("obs.trace.* side-channel (BR-036/BR-D39)", func() {
		// traceSpan is a strict superset of the pre-Phase-28 obsEnvelope:
		// this decodes it into the old shape (ignoring every new field) to
		// assert backward compatibility, then again into the full shape for
		// the new tracing fields. Mirrors organizations-service's
		// browserrpc_roundtrip_test.go, which this test was cloned from —
		// Phase 28b replaced this adapter's publishObs side-channel
		// (obs.rpc.*, BR-D26/BR-D36/BR-D37) with natstrace's obs.trace.*
		// spans the same way Phase 28a did for browserrpc's obs.api.*.
		type legacyEnvelope struct {
			Direction     string              `json:"direction"`
			CorrelationID string              `json:"correlationId"`
			Subject       string              `json:"subject"`
			Payload       json.RawMessage     `json:"payload,omitempty"`
			Error         string              `json:"error,omitempty"`
			Headers       map[string][]string `json:"headers,omitempty"`
			Timestamp     time.Time           `json:"timestamp"`
			PayloadBytes  int                 `json:"payloadBytes"`
		}
		type traceSpan struct {
			legacyEnvelope
			TraceID       string            `json:"traceId,omitempty"`
			SpanID        string            `json:"spanId,omitempty"`
			ParentSpanID  string            `json:"parentSpanId,omitempty"`
			Service       string            `json:"service,omitempty"`
			Entity        string            `json:"entity,omitempty"`
			Action        string            `json:"action,omitempty"`
			StatusCode    string            `json:"statusCode,omitempty"`
			StatusMessage string            `json:"statusMessage,omitempty"`
			Attributes    map[string]string `json:"attributes,omitempty"`
			Redacted      []string          `json:"redacted,omitempty"`
			Truncated     bool              `json:"truncated,omitempty"`
		}

		It("publishes one span per call to obs.trace.{context}.refdata.{entity}.{action}, decodable both as the old envelope shape and the new one", func() {
			spans := make(chan *nats.Msg, 8)
			sub, err := nc.Subscribe("obs.trace.>", func(m *nats.Msg) { spans <- m })
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { Expect(sub.Unsubscribe()).To(Succeed()) })
			Expect(nc.Flush()).To(Succeed())

			reqBody, err := json.Marshal(natsrpc.ItemGetRequest{Context: itemCtx, TypeKey: "currency", Code: "EUR", Locale: "en"})
			Expect(err).NotTo(HaveOccurred())
			_, err = nc.Request(rpcSubject, reqBody, 2*time.Second)
			Expect(err).NotTo(HaveOccurred())

			var msg *nats.Msg
			Eventually(spans).Should(Receive(&msg))
			Expect(msg.Subject).To(Equal("obs.trace." + itemCtx + ".refdata.item.get"))

			var legacy legacyEnvelope
			Expect(json.Unmarshal(msg.Data, &legacy)).To(Succeed())
			Expect(legacy.Subject).To(Equal(rpcSubject))
			Expect(legacy.PayloadBytes).To(BeNumerically(">", 0))

			var span traceSpan
			Expect(json.Unmarshal(msg.Data, &span)).To(Succeed())
			Expect(span.TraceID).NotTo(BeEmpty())
			Expect(span.SpanID).NotTo(BeEmpty())
			Expect(span.ParentSpanID).To(BeEmpty(), "a call with no inbound traceparent is a root span")
			Expect(span.Service).To(Equal("refdata"))
			Expect(span.Entity).To(Equal("item"))
			Expect(span.Action).To(Equal("get"))
			Expect(span.StatusCode).To(Equal("OK"))
		})

		It("marks a failed call with statusCode ERROR and the error message", func() {
			spans := make(chan *nats.Msg, 8)
			sub, err := nc.Subscribe("obs.trace.>", func(m *nats.Msg) { spans <- m })
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { Expect(sub.Unsubscribe()).To(Succeed()) })
			Expect(nc.Flush()).To(Succeed())

			reqBody, err := json.Marshal(natsrpc.ItemGetRequest{Context: itemCtx, TypeKey: "currency", Code: "does-not-exist", Locale: "en"})
			Expect(err).NotTo(HaveOccurred())
			_, err = nc.Request(rpcSubject, reqBody, 2*time.Second)
			Expect(err).NotTo(HaveOccurred())

			var msg *nats.Msg
			Eventually(spans).Should(Receive(&msg))

			var span traceSpan
			Expect(json.Unmarshal(msg.Data, &span)).To(Succeed())
			Expect(span.StatusCode).To(Equal("ERROR"))
			Expect(span.StatusMessage).NotTo(BeEmpty())
		})
	})

	Context("BR-D37: every reply carries a Nats-Responder header identifying the answering service instance", func() {
		// The obs.rpc.*-event assertions this Context used to make (forwarding
		// Nats-Requestor into a request-side event) have no analog under
		// natstrace: a span is a single reply-side publish, not a
		// request/reply pair, so there is no separate "request event" left to
		// assert against — only the real wire reply, checked here.
		It("attaches a Nats-Responder header (service name/instance ID) to a successful reply", func() {
			reqBody, err := json.Marshal(natsrpc.ItemGetRequest{Context: itemCtx, TypeKey: "currency", Code: "EUR", Locale: "en"})
			Expect(err).NotTo(HaveOccurred())
			reply, err := nc.Request(rpcSubject, reqBody, 2*time.Second)
			Expect(err).NotTo(HaveOccurred())

			Expect(reply.Header.Get("Nats-Responder")).To(HavePrefix("refdata-service/"))
		})

		It("attaches a Nats-Responder header to a failed reply too", func() {
			reqBody, err := json.Marshal(natsrpc.ItemGetRequest{Context: itemCtx, TypeKey: "currency", Code: "does-not-exist", Locale: "en"})
			Expect(err).NotTo(HaveOccurred())
			reply, err := nc.Request(rpcSubject, reqBody, 2*time.Second)
			Expect(err).NotTo(HaveOccurred())

			Expect(reply.Header.Get("Nats-Responder")).To(HavePrefix("refdata-service/"))
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
			projector := kvcache.NewProjector(kv, items, locs, refs, versions, newTestNamespaces(domain.DictionaryType{TypeKey: "currency", Category: domain.CategoryStandards}), jstream.NewPublisher(js))

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

			reqBody, err := json.Marshal(natsrpc.ItemGetRequest{Context: itemCtx, TypeKey: "currency", Code: "EUR", Locale: "en"})
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

	Context("BR-D08: item.get and type.list serve from a warm KV cache without querying Postgres", func() {
		It("resolves item.get from KV alone after the backing Postgres item has been deleted", func() {
			// Same embedded-server / fake-repo convention as the BR-D27
			// backfill test above — this needs a JetStream-backed KV bucket.
			opts := &server.Options{JetStream: true, StoreDir: GinkgoT().TempDir(), Port: -1}
			srv, err := server.NewServer(opts)
			Expect(err).NotTo(HaveOccurred())
			srv.Start()
			DeferCleanup(srv.Shutdown)
			Expect(srv.ReadyForConnections(10 * time.Second)).To(BeTrue())

			jsNC, err := nats.Connect(srv.ClientURL(), nats.Name("refdata-service-natsrpc-kvfirst-test"))
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
			projector := kvcache.NewProjector(kv, items, locs, refs, versions, newTestNamespaces(domain.DictionaryType{TypeKey: "currency", Category: domain.CategoryStandards}), jstream.NewPublisher(js))

			kvFirstItemH := commands.NewItemHandler(items, refs, nil)
			kvFirstLocH := commands.NewLocalizationHandler(items, locs, locales, nil)
			_, err = kvFirstItemH.RegisterItem(ctx, commands.ItemInput{TypeKey: "currency", Code: "EUR", Context: itemCtx})
			Expect(err).NotTo(HaveOccurred())
			Expect(kvFirstLocH.SetLocalization(ctx, commands.LocalizationInput{
				TypeKey: "currency", Code: "EUR", Context: itemCtx, Locale: "en", Label: "Euro",
			})).To(Succeed())

			// Warm the cache from Postgres, then delete the item from
			// Postgres entirely — if the RPC handler fell through to
			// ResolveItem instead of reading the warm KV entry, this call
			// would fail with ErrItemNotFound.
			Expect(projector.Backfill(ctx, itemCtx, "currency", "EUR")).To(Succeed())
			Expect(items.Delete(ctx, "currency", itemCtx, "EUR")).To(Succeed())
			_, directErr := kvFirstLocH.ResolveItem(ctx, "currency", itemCtx, "EUR", "en")
			Expect(directErr).To(HaveOccurred(), "sanity check: Postgres genuinely no longer has this item")

			kvFirstAdapter, err := natsrpc.New(jsNC, natsrpc.Deps{Localizations: kvFirstLocH, Items: kvFirstItemH, Projector: projector})
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { Expect(kvFirstAdapter.Stop()).To(Succeed()) })

			reqBody, err := json.Marshal(natsrpc.ItemGetRequest{Context: itemCtx, TypeKey: "currency", Code: "EUR", Locale: "en"})
			Expect(err).NotTo(HaveOccurred())
			msg, err := jsNC.Request(rpcSubject, reqBody, 2*time.Second)
			Expect(err).NotTo(HaveOccurred())

			var resp natsrpc.ItemGetResponse
			Expect(json.Unmarshal(msg.Data, &resp)).To(Succeed())
			Expect(resp.Item.Code).To(Equal("EUR"))
			Expect(resp.Label).To(Equal("Euro"), "the label must come from the warm KV entry, not a (now-failing) Postgres read")
		})

		It("resolves type.list from a warm, complete KV cache without querying Postgres", func() {
			opts := &server.Options{JetStream: true, StoreDir: GinkgoT().TempDir(), Port: -1}
			srv, err := server.NewServer(opts)
			Expect(err).NotTo(HaveOccurred())
			srv.Start()
			DeferCleanup(srv.Shutdown)
			Expect(srv.ReadyForConnections(10 * time.Second)).To(BeTrue())

			jsNC, err := nats.Connect(srv.ClientURL(), nats.Name("refdata-service-natsrpc-kvfirst-type-test"))
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
			projector := kvcache.NewProjector(kv, items, locs, refs, versions, newTestNamespaces(domain.DictionaryType{TypeKey: "currency", Category: domain.CategoryStandards}), jstream.NewPublisher(js))

			kvFirstItemH := commands.NewItemHandler(items, refs, nil)
			kvFirstLocH := commands.NewLocalizationHandler(items, locs, locales, nil)
			for code, label := range map[string]string{"EUR": "Euro", "GBP": "Pound Sterling"} {
				_, err = kvFirstItemH.RegisterItem(ctx, commands.ItemInput{TypeKey: "currency", Code: code, Context: itemCtx})
				Expect(err).NotTo(HaveOccurred())
				Expect(kvFirstLocH.SetLocalization(ctx, commands.LocalizationInput{
					TypeKey: "currency", Code: code, Context: itemCtx, Locale: "en", Label: label,
				})).To(Succeed())
				Expect(projector.Backfill(ctx, itemCtx, "currency", code)).To(Succeed())
			}

			// Delete both items from Postgres — a fall-through to
			// ListAssignable would now return an empty list.
			Expect(items.Delete(ctx, "currency", itemCtx, "EUR")).To(Succeed())
			Expect(items.Delete(ctx, "currency", itemCtx, "GBP")).To(Succeed())
			directItems, err := kvFirstItemH.ListAssignable(ctx, "currency", itemCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(directItems).To(BeEmpty(), "sanity check: Postgres genuinely has no items of this type left")

			kvFirstAdapter, err := natsrpc.New(jsNC, natsrpc.Deps{Localizations: kvFirstLocH, Items: kvFirstItemH, Projector: projector})
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { Expect(kvFirstAdapter.Stop()).To(Succeed()) })

			typeListSubject := "rpc." + itemCtx + ".refdata.type.list.v1"
			reqBody, err := json.Marshal(natsrpc.TypeListRequest{Context: itemCtx, TypeKey: "currency", Locale: "en"})
			Expect(err).NotTo(HaveOccurred())
			msg, err := jsNC.Request(typeListSubject, reqBody, 2*time.Second)
			Expect(err).NotTo(HaveOccurred())

			var resp natsrpc.TypeListResponse
			Expect(json.Unmarshal(msg.Data, &resp)).To(Succeed())
			labels := map[string]string{}
			for _, item := range resp.Items {
				labels[item.Item.Code] = item.Label
			}
			Expect(labels).To(Equal(map[string]string{"EUR": "Euro", "GBP": "Pound Sterling"}),
				"both items' labels must come from the warm KV cache, not a (now-empty) Postgres list")
		})
	})

	// BR-D29 ("obs.rpc.* is retained on RPCTRACE so a reconnecting Admin UI
	// can catch up") had its dedicated test here. Phase 28b retired the
	// publishObs side-channel this RPCTRACE retention depended on (see the
	// obs.trace.* Context above) — nothing publishes to obs.rpc.* anymore.
	// Phase 28g removed the RPCTRACE stream/consumer provisioning itself
	// (composition.go's Startup, this adapter's now-removed
	// ObsSubjectWildcard) now that the TRACES stream/KV design supersedes
	// it. Removed rather than left red.
})

// item.get-versioned needs a JetStream-backed KV bucket (VersionReader has
// no Postgres-free/in-memory mode), so it gets its own Describe with its own
// embedded JetStream server rather than sharing the plain core-NATS nc the
// rest of this file uses — same convention as the BR-D27 backfill test
// above. It seeds the versioned bucket directly via VersionMaterializer, so
// (like the rest of this file) it needs no real Postgres.
var _ = Describe("BR-D25/BR-D28: item.get-versioned is the rpc.* counterpart of getVersionedItem (Phase 12.11)", func() {
	const itemCtx = "acme-test"

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

		materializer := kvcache.NewVersionMaterializer(kv, newTestNamespaces(domain.DictionaryType{TypeKey: "currency", Category: domain.CategoryStandards}))
		Expect(materializer.Materialize(ctx, itemCtx, 1, []domain.CorpusItem{
			{DictionaryItem: domain.DictionaryItem{TypeKey: "currency", Code: "EUR", Context: itemCtx, Status: domain.StatusActive}},
		}, []domain.CorpusLocalization{
			{Localization: domain.Localization{TypeKey: "currency", Code: "EUR", Context: itemCtx, Locale: "en", Label: "Euro"}},
		}, 0)).To(Succeed())

		versionReader := kvcache.NewVersionReader(kv, newTestNamespaces(domain.DictionaryType{TypeKey: "currency", Category: domain.CategoryStandards}))
		adapter, err := natsrpc.New(nc, natsrpc.Deps{VersionReader: versionReader})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { Expect(adapter.Stop()).To(Succeed()) })

		reqBody, err := json.Marshal(natsrpc.ItemGetVersionedRequest{Context: itemCtx, TypeKey: "currency", Code: "EUR", Version: 1})
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
		versionReader := kvcache.NewVersionReader(kv, newTestNamespaces(domain.DictionaryType{TypeKey: "currency", Category: domain.CategoryStandards}))

		adapter, err := natsrpc.New(nc, natsrpc.Deps{VersionReader: versionReader})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { Expect(adapter.Stop()).To(Succeed()) })

		reqBody, err := json.Marshal(natsrpc.ItemGetVersionedRequest{Context: itemCtx, TypeKey: "currency", Code: "EUR", Version: 999})
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

var _ = Describe("BR-D25/BR-D28: context.list is the rpc.* counterpart of listContexts (Phase 16f)", func() {
	var (
		ctx       context.Context
		contextsH *commands.ContextHandler
		nc        *nats.Conn
		adapter   *natsrpc.Adapter
		platform  string
		acmeCo    string
		acmeUnit  string
		globexCo  string
	)

	BeforeEach(func() {
		ctx = context.Background()
		repo := newFakeContextRepo()
		contextsH = commands.NewContextHandler(repo)

		platform, acmeCo, acmeUnit, globexCo = "_platform", "acme-pacific-fleet", "acme-atlantic-fleet", "globex"
		Expect(contextsH.RegisterPlatformRoot(ctx, domain.Context{Context: platform, Name: platform})).To(Succeed())
		Expect(contextsH.Register(ctx, domain.Context{Context: acmeCo, Parent: platform, Name: acmeCo, Tenant: "acme"})).To(Succeed())
		Expect(contextsH.Register(ctx, domain.Context{Context: acmeUnit, Parent: platform, Name: acmeUnit, Tenant: "acme"})).To(Succeed())
		Expect(contextsH.Register(ctx, domain.Context{Context: globexCo, Parent: platform, Name: globexCo, Tenant: "globex"})).To(Succeed())

		nc = newTestNATSConn()
		var err error
		adapter, err = natsrpc.New(nc, natsrpc.Deps{Contexts: contextsH})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { Expect(adapter.Stop()).To(Succeed()) })
	})

	names := func(contexts []domain.Context) []string {
		out := make([]string, len(contexts))
		for i, c := range contexts {
			out[i] = c.Context
		}
		return out
	}

	It("returns every context when no tenant is given, identically to ContextHandler.List called directly", func() {
		reqBody, err := json.Marshal(natsrpc.ContextListRequest{})
		Expect(err).NotTo(HaveOccurred())
		msg, err := nc.Request(natsrpc.ContextListSubject, reqBody, 2*time.Second)
		Expect(err).NotTo(HaveOccurred())

		var resp natsrpc.ContextListResponse
		Expect(json.Unmarshal(msg.Data, &resp)).To(Succeed())

		direct, err := contextsH.List(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(names(resp.Contexts)).To(ConsistOf(names(direct)))
	})

	It("scopes the result to one tenant plus the shared platform roots when Tenant is set, identically to ListByTenant called directly", func() {
		reqBody, err := json.Marshal(natsrpc.ContextListRequest{Tenant: "acme"})
		Expect(err).NotTo(HaveOccurred())
		msg, err := nc.Request(natsrpc.ContextListSubject, reqBody, 2*time.Second)
		Expect(err).NotTo(HaveOccurred())

		var resp natsrpc.ContextListResponse
		Expect(json.Unmarshal(msg.Data, &resp)).To(Succeed())

		direct, err := contextsH.ListByTenant(ctx, "acme")
		Expect(err).NotTo(HaveOccurred())
		Expect(names(resp.Contexts)).To(ConsistOf(names(direct)))
		Expect(names(resp.Contexts)).To(ContainElements(platform, acmeCo, acmeUnit))
		Expect(names(resp.Contexts)).NotTo(ContainElement(globexCo))
	})
})
