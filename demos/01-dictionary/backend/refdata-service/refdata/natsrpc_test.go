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

	Context("BR-D36: every obs.rpc.* event carries its headers, a publisher-side timestamp, and its payload size", func() {
		type obsEnvelope struct {
			Direction     string              `json:"direction"`
			Error         string              `json:"error,omitempty"`
			Headers       map[string][]string `json:"headers,omitempty"`
			Timestamp     time.Time           `json:"timestamp"`
			PayloadBytes  int                 `json:"payloadBytes"`
			CorrelationID string              `json:"correlationId"`
		}

		It("stamps the request-side event with the caller's real headers, a non-zero timestamp, and the true payload size", func() {
			obsMsgs := make(chan *nats.Msg, 4)
			sub, err := nc.Subscribe("obs.rpc.>", func(m *nats.Msg) { obsMsgs <- m })
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { Expect(sub.Unsubscribe()).To(Succeed()) })

			reqBody, err := json.Marshal(natsrpc.ItemGetRequest{TypeKey: "currency", Code: "EUR", Locale: "en"})
			Expect(err).NotTo(HaveOccurred())

			msg := nats.NewMsg(rpcSubject)
			msg.Data = reqBody
			msg.Header = nats.Header{"X-Client": []string{"admin-ui-test"}}
			before := time.Now()
			reply, err := nc.RequestMsg(msg, 2*time.Second)
			Expect(err).NotTo(HaveOccurred())
			Expect(reply).NotTo(BeNil())

			var reqEnv obsEnvelope
			Eventually(func() bool {
				select {
				case m := <-obsMsgs:
					Expect(json.Unmarshal(m.Data, &reqEnv)).To(Succeed())
					return reqEnv.Direction == "request"
				default:
					return false
				}
			}, 2*time.Second).Should(BeTrue(), "expected a request-side obs.rpc.* event")

			Expect(reqEnv.Headers).To(HaveKeyWithValue("X-Client", []string{"admin-ui-test"}))
			Expect(reqEnv.Timestamp).To(BeTemporally(">=", before))
			Expect(reqEnv.PayloadBytes).To(Equal(len(reqBody)))
		})

		It("attaches real Nats-Service-Error/-Code headers to a failed reply, on both the obs event and the actual wire reply", func() {
			obsMsgs := make(chan *nats.Msg, 4)
			sub, err := nc.Subscribe("obs.rpc.>", func(m *nats.Msg) { obsMsgs <- m })
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { Expect(sub.Unsubscribe()).To(Succeed()) })

			reqBody, err := json.Marshal(natsrpc.ItemGetRequest{TypeKey: "currency", Code: "does-not-exist", Locale: "en"})
			Expect(err).NotTo(HaveOccurred())
			reply, err := nc.Request(rpcSubject, reqBody, 2*time.Second)
			Expect(err).NotTo(HaveOccurred())

			Expect(reply.Header.Get("Nats-Service-Error")).NotTo(BeEmpty(), "the real wire reply must carry the error header, not just the obs event")
			Expect(reply.Header.Get("Nats-Service-Error-Code")).To(Equal("404"))

			var sawErrorHeaders bool
			for i := 0; i < 2; i++ {
				select {
				case m := <-obsMsgs:
					var env obsEnvelope
					Expect(json.Unmarshal(m.Data, &env)).To(Succeed())
					if env.Direction == "reply" {
						Expect(env.Headers).To(HaveKeyWithValue("Nats-Service-Error-Code", []string{"404"}))
						sawErrorHeaders = true
					}
				case <-time.After(2 * time.Second):
					Fail("timed out waiting for obs.rpc.* events")
				}
			}
			Expect(sawErrorHeaders).To(BeTrue())
		})

		It("still decodes an old-shape obs event with no headers/timestamp/payloadBytes fields at all", func() {
			var env obsEnvelope
			old := []byte(`{"direction":"reply","correlationId":"legacy-1","subject":"rpc.acme-test.refdata.item.get.v1","payload":{"item":{}}}`)
			Expect(json.Unmarshal(old, &env)).To(Succeed())
			Expect(env.Direction).To(Equal("reply"))
			Expect(env.Headers).To(BeNil())
			Expect(env.Timestamp).To(BeZero())
		})
	})

	Context("BR-D37: every rpc.* request carries a Nats-Requestor header identifying its caller, and every reply a Nats-Responder header identifying the answering service instance", func() {
		type obsEnvelope struct {
			Direction     string              `json:"direction"`
			Error         string              `json:"error,omitempty"`
			Headers       map[string][]string `json:"headers,omitempty"`
			Timestamp     time.Time           `json:"timestamp"`
			PayloadBytes  int                 `json:"payloadBytes"`
			CorrelationID string              `json:"correlationId"`
		}

		It("forwards the caller's Nats-Requestor header into the obs.rpc.* request event, same as any other caller-supplied header", func() {
			obsMsgs := make(chan *nats.Msg, 4)
			sub, err := nc.Subscribe("obs.rpc.>", func(m *nats.Msg) { obsMsgs <- m })
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { Expect(sub.Unsubscribe()).To(Succeed()) })

			reqBody, err := json.Marshal(natsrpc.ItemGetRequest{TypeKey: "currency", Code: "EUR", Locale: "en"})
			Expect(err).NotTo(HaveOccurred())

			msg := nats.NewMsg(rpcSubject)
			msg.Data = reqBody
			// Instance-qualified "<service>/<instance ID>" — the same
			// name/instance split Nats-Responder uses (BR-D37).
			msg.Header = nats.Header{"Nats-Requestor": []string{"shipping-service/test-instance-1"}}
			reply, err := nc.RequestMsg(msg, 2*time.Second)
			Expect(err).NotTo(HaveOccurred())
			Expect(reply).NotTo(BeNil())

			var reqEnv obsEnvelope
			Eventually(func() bool {
				select {
				case m := <-obsMsgs:
					Expect(json.Unmarshal(m.Data, &reqEnv)).To(Succeed())
					return reqEnv.Direction == "request"
				default:
					return false
				}
			}, 2*time.Second).Should(BeTrue(), "expected a request-side obs.rpc.* event")

			Expect(reqEnv.Headers).To(HaveKeyWithValue("Nats-Requestor", []string{"shipping-service/test-instance-1"}))
		})

		It("attaches a Nats-Responder header (service name/instance ID) to a successful reply, on both the real wire reply and the obs.rpc.* event", func() {
			obsMsgs := make(chan *nats.Msg, 4)
			sub, err := nc.Subscribe("obs.rpc.>", func(m *nats.Msg) { obsMsgs <- m })
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { Expect(sub.Unsubscribe()).To(Succeed()) })

			reqBody, err := json.Marshal(natsrpc.ItemGetRequest{TypeKey: "currency", Code: "EUR", Locale: "en"})
			Expect(err).NotTo(HaveOccurred())
			reply, err := nc.Request(rpcSubject, reqBody, 2*time.Second)
			Expect(err).NotTo(HaveOccurred())

			responder := reply.Header.Get("Nats-Responder")
			Expect(responder).To(HavePrefix("refdata-service/"), "the real wire reply must carry the responder header, not just the obs event")

			var replyEnv obsEnvelope
			Eventually(func() bool {
				select {
				case m := <-obsMsgs:
					Expect(json.Unmarshal(m.Data, &replyEnv)).To(Succeed())
					return replyEnv.Direction == "reply"
				default:
					return false
				}
			}, 2*time.Second).Should(BeTrue(), "expected a reply-side obs.rpc.* event")
			Expect(replyEnv.Headers).To(HaveKeyWithValue("Nats-Responder", []string{responder}))
		})

		It("attaches a Nats-Responder header to a failed reply too", func() {
			reqBody, err := json.Marshal(natsrpc.ItemGetRequest{TypeKey: "currency", Code: "does-not-exist", Locale: "en"})
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

			reqBody, err := json.Marshal(natsrpc.ItemGetRequest{TypeKey: "currency", Code: "EUR", Locale: "en"})
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
			reqBody, err := json.Marshal(natsrpc.TypeListRequest{TypeKey: "currency", Locale: "en"})
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

	Context("BR-D29: obs.rpc.* is retained on RPCTRACE so a reconnecting Admin UI can catch up on the last 10 minutes", func() {
		It("persists both the request and reply obs.rpc.* events, replayable after the RPC call completes, while still delivering them live", func() {
			// This adapter needs its own JetStream-backed nc (unlike the
			// plain core-NATS nc used by the rest of this file) — same
			// embedded-server convention as the BR-D27 backfill test above.
			opts := &server.Options{JetStream: true, StoreDir: GinkgoT().TempDir(), Port: -1}
			srv, err := server.NewServer(opts)
			Expect(err).NotTo(HaveOccurred())
			srv.Start()
			DeferCleanup(srv.Shutdown)
			Expect(srv.ReadyForConnections(10 * time.Second)).To(BeTrue())

			jsNC, err := nats.Connect(srv.ClientURL(), nats.Name("refdata-service-natsrpc-rpctrace-test"))
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(jsNC.Close)

			js, err := jetstream.New(jsNC)
			Expect(err).NotTo(HaveOccurred())
			_, err = jstream.CreateChangeStream(ctx, js, "RPCTRACE", []string{natsrpc.ObsSubjectWildcard}, time.Hour)
			Expect(err).NotTo(HaveOccurred())

			// A live core subscriber must still see both events — BR-D26's
			// existing live-tail contract is unaffected by JetStream
			// retention (a JetStream publish is still an ordinary NATS
			// message on the wire).
			liveMsgs := make(chan *nats.Msg, 8)
			liveSub, err := jsNC.Subscribe(natsrpc.ObsSubjectWildcard, func(m *nats.Msg) { liveMsgs <- m })
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { Expect(liveSub.Unsubscribe()).To(Succeed()) })

			rtAdapter, err := natsrpc.New(jsNC, natsrpc.Deps{Localizations: locH, Items: itemH, JS: js})
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { Expect(rtAdapter.Stop()).To(Succeed()) })

			reqBody, err := json.Marshal(natsrpc.ItemGetRequest{TypeKey: "currency", Code: "EUR", Locale: "en"})
			Expect(err).NotTo(HaveOccurred())
			_, err = jsNC.Request(rpcSubject, reqBody, 2*time.Second)
			Expect(err).NotTo(HaveOccurred())

			for i := 0; i < 2; i++ {
				select {
				case <-liveMsgs:
				case <-time.After(2 * time.Second):
					Fail("timed out waiting for live obs.rpc.* events")
				}
			}

			// The stream is what makes catch-up-on-reconnect possible: a
			// fresh ordered consumer with DeliverAllPolicy, created well
			// after the call completed (as a reconnecting tab would),
			// must still see both events. PublishAsync's server ack isn't
			// on the RPC reply's critical path, so this polls briefly
			// rather than asserting synchronously.
			Eventually(func() ([]string, error) {
				consumer, err := js.OrderedConsumer(ctx, "RPCTRACE", jetstream.OrderedConsumerConfig{
					DeliverPolicy: jetstream.DeliverAllPolicy,
				})
				if err != nil {
					return nil, err
				}
				batch, err := consumer.FetchNoWait(10)
				if err != nil {
					return nil, err
				}
				var directions []string
				for msg := range batch.Messages() {
					var env struct {
						Direction string `json:"direction"`
					}
					if err := json.Unmarshal(msg.Data(), &env); err != nil {
						return nil, err
					}
					directions = append(directions, env.Direction)
				}
				return directions, batch.Error()
			}, 2*time.Second, 20*time.Millisecond).Should(ConsistOf("request", "reply"))
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
		versionReader := kvcache.NewVersionReader(kv, newTestNamespaces(domain.DictionaryType{TypeKey: "currency", Category: domain.CategoryStandards}))

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
