package dictionary

// API integration tests: spins up an httptest.Server wrapping the real
// http.ServeMux + rest.Handlers against an embedded in-process NATS server.
//
// Scope: REST's infra/admin HTTP surface only — health, the admin ports
// table, the admin read-path (Shape B) diagnostics panel, and tenant
// discovery/switch. Phase 33 (BR-039) deleted every REST business route
// (ships/containers/terminal/manifest/ports/meta); those operations are now
// reachable only over api.*/rpc.*, whose HTTP-shaped parity tests live in
// browserrpc_test.go. Business-rule correctness itself is covered in depth
// by integration_test.go and container_test.go, independent of transport.
//
// Setup for the tests below (getting a ship into Postgres/KV so the admin
// read-path panel has something to read) goes straight through the
// application layer (commands.ShipHandler), not REST — REST no longer
// exposes any way to create one.

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/application/commands"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/application/queries"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/eventhandler"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/rest"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/internal/jstream"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/internal/kvstore"
)

// ─── test server ─────────────────────────────────────────────────────────────

type apiServer struct {
	base   string
	client *http.Client
	ctx    context.Context
	ships  *commands.ShipHandler
}

func newAPIServer() *apiServer {
	GinkgoHelper()
	ctx := context.Background()
	js := newJetStream()
	log := slog.New(slog.DiscardHandler)

	kvB := kvstore.New(js, "ships")
	kvContainers := kvstore.New(js, "container")
	kvMeta := kvstore.New(js, "meta")
	repo := newFakeRepo()
	portRepo := newFakePortRepo()

	consumeB, err := eventhandler.RegisterShips(ctx, js, kvB, nil, repo, log)
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(consumeB.Stop)

	consumeC, err := eventhandler.RegisterContainers(ctx, js, kvContainers, nil, newFakeContainerRepo(), log)
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(consumeC.Stop)

	consumeM, err := eventhandler.RegisterMeta(ctx, js, kvMeta, nil, log)
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(consumeM.Stop)

	pub := jstream.NewPublisher(js)
	ships := commands.NewShipHandler(pub, js, portRepo)
	handlers := rest.NewHandlers(rest.Deps{
		Ships:      ships,
		Containers: commands.NewContainerHandler(pub, js, portRepo),
		Ports:      commands.NewPortHandler(portRepo),
		ShipReads:  queries.NewShips(kvB, repo),
		Terminal:   queries.NewTerminal(kvContainers),
		Meta:       queries.NewMeta(kvMeta),
		KVCont:     kvContainers,
		KVMeta:     kvMeta,
		JS:         js,
		Log:        log,
	})

	mux := http.NewServeMux()
	handlers.Mount(mux)
	srv := httptest.NewServer(mux)
	DeferCleanup(srv.Close)

	return &apiServer{base: srv.URL, client: srv.Client(), ctx: ctx, ships: ships}
}

// get sends a GET and returns the response.
func (a *apiServer) get(path string) *http.Response {
	GinkgoHelper()
	resp, err := a.client.Get(a.base + path)
	Expect(err).NotTo(HaveOccurred())
	return resp
}

// del sends a DELETE and returns the response.
func (a *apiServer) del(path string) *http.Response {
	GinkgoHelper()
	req, err := http.NewRequestWithContext(a.ctx, http.MethodDelete, a.base+path, nil)
	Expect(err).NotTo(HaveOccurred())
	resp, err := a.client.Do(req)
	Expect(err).NotTo(HaveOccurred())
	return resp
}

// readBody decodes the response body into a generic map and closes it.
func readBody(resp *http.Response) map[string]any {
	GinkgoHelper()
	defer resp.Body.Close()
	var out map[string]any
	Expect(json.NewDecoder(resp.Body).Decode(&out)).To(Succeed())
	return out
}

// arrive is setup-only: puts a ship into the domain directly via the
// application layer (commands.ShipHandler), since REST no longer exposes any
// way to create one (BR-039).
func (a *apiServer) arrive(ctx, shipID, shipName, port string) {
	GinkgoHelper()
	_, err := a.ships.ArrivePort(a.ctx, commands.ShipInput{Context: ctx, ShipID: shipID, ShipName: shipName, Port: port})
	Expect(err).NotTo(HaveOccurred())
}

// ─── specs ───────────────────────────────────────────────────────────────────

var _ = Describe("HTTP API", func() {
	var api *apiServer
	const ctx = "acme-pacific-fleet"

	BeforeEach(func() {
		api = newAPIServer()
	})

	// ── health ────────────────────────────────────────────────────────────────

	Describe("GET /healthz", func() {
		It("returns 200", func() {
			resp := api.get("/healthz")
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
		})
	})

	// ── admin — postgres tables ───────────────────────────────────────────────

	Describe("admin — postgres tables", func() {
		It("GET /api/admin/ports/{context} returns raw rows with name and createdAt", func() {
			resp := api.get("/api/admin/ports/" + ctx)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			var body struct {
				Rows []struct {
					Name      string    `json:"name"`
					CreatedAt time.Time `json:"createdAt"`
				} `json:"rows"`
			}
			Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())

			var hamburg *time.Time
			for _, row := range body.Rows {
				if row.Name == "Hamburg" {
					hamburg = &row.CreatedAt
				}
			}
			Expect(hamburg).NotTo(BeNil(), "expected Hamburg in the raw rows")
			Expect(*hamburg).NotTo(BeZero())
		})
	})

	// ── admin — read path (Shape B, renamed from /api/shape-b/*) ─────────────

	Describe("admin — read path (Shape B — KV cache → Postgres)", func() {
		It("returns 200 with cacheHit true after the KV projection warms", func() {
			api.arrive(ctx, "shapeb-ship", "Shape B", "Hamburg")

			eventually(func() error {
				resp := api.get("/api/admin/read-path/ships/" + ctx + "/shapeb-ship")
				defer resp.Body.Close()
				if resp.StatusCode != http.StatusOK {
					return errors.New("unexpected status")
				}
				var body map[string]any
				if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
					return err
				}
				if body["cacheHit"] != true {
					return errors.New("waiting for KV cache to warm")
				}
				if body["source"] != "kv-cache" {
					return errors.New("expected source kv-cache")
				}
				return nil
			})
		})

		It("returns 404 for an unknown ship", func() {
			resp := api.get("/api/admin/read-path/ships/" + ctx + "/no-such-ship")
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
		})

		It("DELETE /api/admin/read-path/cache evicts the entry; next GET shows a cache miss then backfill", func() {
			api.arrive(ctx, "evict-ship", "Evict", "Rotterdam")

			By("wait for cache to warm")
			eventually(func() error {
				resp := api.get("/api/admin/read-path/ships/" + ctx + "/evict-ship")
				defer resp.Body.Close()
				var body map[string]any
				json.NewDecoder(resp.Body).Decode(&body)
				if body["cacheHit"] != true {
					return errors.New("cache not warm yet")
				}
				return nil
			})

			By("evict")
			del := api.del("/api/admin/read-path/cache/" + ctx + "/evict-ship")
			del.Body.Close()
			Expect(del.StatusCode).To(Equal(http.StatusNoContent))

			By("immediate read hits Postgres (cache miss)")
			resp := api.get("/api/admin/read-path/ships/" + ctx + "/evict-ship")
			body := readBody(resp)
			Expect(body["cacheHit"]).To(BeFalse())
			Expect(body["source"]).To(Equal("postgres"))

			By("subsequent read is a cache hit again after backfill")
			eventually(func() error {
				resp := api.get("/api/admin/read-path/ships/" + ctx + "/evict-ship")
				defer resp.Body.Close()
				var body map[string]any
				json.NewDecoder(resp.Body).Decode(&body)
				if body["cacheHit"] != true {
					return errors.New("waiting for cache backfill")
				}
				return nil
			})
		})
	})

})
