package dictionary

// API integration tests: spins up an httptest.Server wrapping the real
// http.ServeMux + rest.Handlers wired with an embedded NATS server.
//
// Scope: HTTP concerns — status codes, response body shape, JSON codec,
// error mapping (400 / 404 / 422).  Business-rule correctness is covered
// in depth by integration_test.go and container_test.go.

import (
	"bytes"
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
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/domain"
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
}

func newAPIServer() *apiServer {
	GinkgoHelper()
	ctx := context.Background()
	js := newJetStream()
	log := slog.New(slog.DiscardHandler)

	kvA := kvstore.New(js, "dict-a")
	kvB := kvstore.New(js, "dict-b")
	kvContainers := kvstore.New(js, "container")
	kvMeta := kvstore.New(js, "meta")
	repo := newFakeRepo()
	portRepo := newFakePortRepo()

	consumeA, err := eventhandler.RegisterShapeA(ctx, js, kvA, nil, log)
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(consumeA.Stop)

	consumeB, err := eventhandler.RegisterShapeB(ctx, js, kvB, repo, log)
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(consumeB.Stop)

	consumeC, err := eventhandler.RegisterContainers(ctx, js, kvContainers, nil, newFakeContainerRepo(), log)
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(consumeC.Stop)

	consumeM, err := eventhandler.RegisterMeta(ctx, js, kvMeta, nil, log)
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(consumeM.Stop)

	pub := jstream.NewPublisher(js)
	handlers := rest.NewHandlers(rest.Deps{
		Ships:      commands.NewShipHandler(pub, js, portRepo),
		Containers: commands.NewContainerHandler(pub, js, portRepo),
		Ports:      commands.NewPortHandler(portRepo),
		ShapeB:     queries.NewShapeB(kvB, repo),
		ShapeC:     queries.NewShapeC(js),
		Terminal:   queries.NewTerminal(kvContainers),
		Meta:       queries.NewMeta(kvMeta),
		KVA:        kvA,
		KVB:        kvB,
		KVCont:     kvContainers,
		KVMeta:     kvMeta,
		JS:         js,
		Log:        log,
	})

	mux := http.NewServeMux()
	handlers.Mount(mux)
	srv := httptest.NewServer(mux)
	DeferCleanup(srv.Close)

	return &apiServer{base: srv.URL, client: srv.Client(), ctx: ctx}
}

// post marshals body to JSON and sends a POST; the caller owns the response.
func (a *apiServer) post(path string, body any) *http.Response {
	GinkgoHelper()
	b, err := json.Marshal(body)
	Expect(err).NotTo(HaveOccurred())
	resp, err := a.client.Post(a.base+path, "application/json", bytes.NewReader(b))
	Expect(err).NotTo(HaveOccurred())
	return resp
}

// postRaw sends a POST with an arbitrary (potentially malformed) body.
func (a *apiServer) postRaw(path, body string) *http.Response {
	GinkgoHelper()
	req, err := http.NewRequestWithContext(a.ctx, http.MethodPost, a.base+path, bytes.NewBufferString(body))
	Expect(err).NotTo(HaveOccurred())
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.client.Do(req)
	Expect(err).NotTo(HaveOccurred())
	return resp
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

// fire sends a command whose response is not inspected (setup only).
func (a *apiServer) fire(path string, body any) {
	GinkgoHelper()
	resp := a.post(path, body)
	resp.Body.Close()
	Expect(resp.StatusCode).To(Equal(http.StatusAccepted))
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

	// ── ship commands ─────────────────────────────────────────────────────────

	Describe("POST /api/ships/arrive", func() {
		It("returns 202 with the docked ship state", func() {
			resp := api.post("/api/ships/arrive", map[string]any{
				"context": ctx, "shipID": "api-ship", "shipName": "API Ship", "port": "Hamburg",
			})
			Expect(resp.StatusCode).To(Equal(http.StatusAccepted))
			body := readBody(resp)
			ship := body["ship"].(map[string]any)
			Expect(ship["shipID"]).To(Equal("api-ship"))
			Expect(ship["currentPort"]).To(Equal("Hamburg"))
			Expect(ship["status"]).To(Equal(string(domain.StatusDocked)))
		})

		It("returns 400 for malformed JSON", func() {
			resp := api.postRaw("/api/ships/arrive", `{bad`)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
			body := readBody(resp)
			Expect(body["error"]).NotTo(BeEmpty())
		})

		It("returns 422 when BR-001 is violated (arrive at already-docked port)", func() {
			api.fire("/api/ships/arrive", map[string]any{
				"context": ctx, "shipID": "dup-ship", "shipName": "Dup", "port": "Hamburg",
			})
			resp := api.post("/api/ships/arrive", map[string]any{
				"context": ctx, "shipID": "dup-ship", "port": "Hamburg",
			})
			Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
			body := readBody(resp)
			Expect(body["error"]).To(ContainSubstring(domain.ErrAlreadyDocked.Error()))
		})

		It("returns 422 when BR-002 is violated (arrive without departing first)", func() {
			api.fire("/api/ships/arrive", map[string]any{
				"context": ctx, "shipID": "nodep-ship", "shipName": "NoDep", "port": "Hamburg",
			})
			resp := api.post("/api/ships/arrive", map[string]any{
				"context": ctx, "shipID": "nodep-ship", "port": "Rotterdam",
			})
			Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
			body := readBody(resp)
			Expect(body["error"]).To(ContainSubstring(domain.ErrMustDepart.Error()))
		})
	})

	Describe("POST /api/ships/depart", func() {
		It("returns 202 with the in-transit ship state", func() {
			api.fire("/api/ships/arrive", map[string]any{
				"context": ctx, "shipID": "dep-ship", "shipName": "Dep", "port": "Hamburg",
			})
			resp := api.post("/api/ships/depart", map[string]any{
				"context": ctx, "shipID": "dep-ship", "port": "Hamburg",
			})
			Expect(resp.StatusCode).To(Equal(http.StatusAccepted))
			body := readBody(resp)
			ship := body["ship"].(map[string]any)
			Expect(ship["status"]).To(Equal(string(domain.StatusInTransit)))
			Expect(ship["currentPort"]).To(Equal(""))
		})

		It("returns 422 when BR-003 is violated (depart a port the ship is not at)", func() {
			resp := api.post("/api/ships/depart", map[string]any{
				"context": ctx, "shipID": "ghost-ship", "port": "Hamburg",
			})
			Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
			body := readBody(resp)
			Expect(body["error"]).To(ContainSubstring(domain.ErrNotDocked.Error()))
		})
	})

	Describe("POST /api/ships/register", func() {
		It("returns 202 with the registered ship state", func() {
			resp := api.post("/api/ships/register", map[string]any{
				"context": ctx, "shipID": "api-register-ship", "shipName": "API Register",
			})
			Expect(resp.StatusCode).To(Equal(http.StatusAccepted))
			body := readBody(resp)
			ship := body["ship"].(map[string]any)
			Expect(ship["shipID"]).To(Equal("api-register-ship"))
			Expect(ship["id"]).NotTo(BeEmpty())
		})

		It("returns 400 for malformed JSON", func() {
			resp := api.postRaw("/api/ships/register", `{bad`)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
		})

		It("returns 422 when BR-021 is violated (duplicate registration)", func() {
			api.fire("/api/ships/register", map[string]any{
				"context": ctx, "shipID": "api-dup-register-ship", "shipName": "Dup",
			})
			resp := api.post("/api/ships/register", map[string]any{
				"context": ctx, "shipID": "api-dup-register-ship", "shipName": "Dup Again",
			})
			Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
			body := readBody(resp)
			Expect(body["error"]).To(ContainSubstring(domain.ErrShipExists.Error()))
		})
	})

	Describe("POST /api/ships/correct-id", func() {
		It("returns 202 with the ship's identity renamed, surrogate id preserved", func() {
			registerResp := api.post("/api/ships/register", map[string]any{
				"context": ctx, "shipID": "api-correct-ship", "shipName": "API Correct",
			})
			registered := readBody(registerResp)["ship"].(map[string]any)

			resp := api.post("/api/ships/correct-id", map[string]any{
				"context": ctx, "shipID": "api-correct-ship", "newShipID": "api-correct-ship-renamed",
			})
			Expect(resp.StatusCode).To(Equal(http.StatusAccepted))
			body := readBody(resp)
			ship := body["ship"].(map[string]any)
			Expect(ship["shipID"]).To(Equal("api-correct-ship-renamed"))
			Expect(ship["id"]).To(Equal(registered["id"]))
		})

		It("returns 422 when BR-022 is violated (target shipID already in use)", func() {
			api.fire("/api/ships/register", map[string]any{
				"context": ctx, "shipID": "api-correct-taken", "shipName": "Taken",
			})
			api.fire("/api/ships/register", map[string]any{
				"context": ctx, "shipID": "api-correct-source", "shipName": "Source",
			})
			resp := api.post("/api/ships/correct-id", map[string]any{
				"context": ctx, "shipID": "api-correct-source", "newShipID": "api-correct-taken",
			})
			Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
			body := readBody(resp)
			Expect(body["error"]).To(ContainSubstring(domain.ErrShipIDInUse.Error()))
		})
	})

	// ── container commands ────────────────────────────────────────────────────

	Describe("POST /api/containers/register", func() {
		It("returns 202 with the in-terminal container state", func() {
			resp := api.post("/api/containers/register", map[string]any{
				"context": ctx, "containerID": "TCKU1000001",
				"cargo": "Electronics", "originPort": "Hamburg", "destPort": "Singapore",
			})
			Expect(resp.StatusCode).To(Equal(http.StatusAccepted))
			body := readBody(resp)
			c := body["container"].(map[string]any)
			Expect(c["containerID"]).To(Equal("TCKU1000001"))
			Expect(c["status"]).To(Equal(string(domain.ContainerInTerminal)))
			Expect(c["terminalPort"]).To(Equal("Hamburg"))
			Expect(c["onShipID"]).To(BeNil())
		})

		It("returns 400 for malformed JSON", func() {
			resp := api.postRaw("/api/containers/register", `{bad`)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
		})

		It("returns 422 when BR-015 is violated (duplicate container ID)", func() {
			api.fire("/api/containers/register", map[string]any{
				"context": ctx, "containerID": "TCKU9990015",
				"cargo": "Textiles", "originPort": "Hamburg", "destPort": "Singapore",
			})
			resp := api.post("/api/containers/register", map[string]any{
				"context": ctx, "containerID": "TCKU9990015",
				"cargo": "Textiles", "originPort": "Hamburg", "destPort": "Singapore",
			})
			Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
			body := readBody(resp)
			Expect(body["error"]).To(ContainSubstring(domain.ErrContainerExists.Error()))
		})

		It("returns 422 when BR-016 is violated (invalid container ID format)", func() {
			resp := api.post("/api/containers/register", map[string]any{
				"context": ctx, "containerID": "BADFORMAT",
				"cargo": "Electronics", "originPort": "Hamburg", "destPort": "Singapore",
			})
			Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
			body := readBody(resp)
			Expect(body["error"]).To(ContainSubstring(domain.ErrInvalidContainerID.Error()))
		})
	})

	Describe("POST /api/containers/load", func() {
		It("returns 202 with the on-ship container state", func() {
			api.fire("/api/containers/register", map[string]any{
				"context": ctx, "containerID": "TCKU1000002",
				"cargo": "Electronics", "originPort": "Hamburg", "destPort": "Singapore",
			})
			api.fire("/api/ships/arrive", map[string]any{
				"context": ctx, "shipID": "loader-ship", "shipName": "Loader", "port": "Hamburg",
			})

			resp := api.post("/api/containers/load", map[string]any{
				"context": ctx, "containerID": "TCKU1000002", "shipID": "loader-ship",
			})
			Expect(resp.StatusCode).To(Equal(http.StatusAccepted))
			body := readBody(resp)
			c := body["container"].(map[string]any)
			Expect(c["status"]).To(Equal(string(domain.ContainerOnShip)))
			Expect(c["onShipID"]).To(Equal("loader-ship"))
			Expect(c["terminalPort"]).To(BeNil())
		})

		It("returns 404 for an unregistered container", func() {
			api.fire("/api/ships/arrive", map[string]any{
				"context": ctx, "shipID": "orphan-ship", "shipName": "Orphan", "port": "Hamburg",
			})
			resp := api.post("/api/containers/load", map[string]any{
				"context": ctx, "containerID": "TCKU9999999", "shipID": "orphan-ship",
			})
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
			body := readBody(resp)
			Expect(body["error"]).NotTo(BeEmpty())
		})
	})

	Describe("POST /api/containers/unload", func() {
		It("returns 202 with the in-terminal container state at the destination", func() {
			api.fire("/api/containers/register", map[string]any{
				"context": ctx, "containerID": "TCKU1000003",
				"cargo": "Electronics", "originPort": "Hamburg", "destPort": "Singapore",
			})
			api.fire("/api/ships/arrive", map[string]any{
				"context": ctx, "shipID": "voyage-ship", "shipName": "Voyage", "port": "Hamburg",
			})
			api.fire("/api/containers/load", map[string]any{
				"context": ctx, "containerID": "TCKU1000003", "shipID": "voyage-ship",
			})
			api.fire("/api/ships/depart", map[string]any{
				"context": ctx, "shipID": "voyage-ship", "port": "Hamburg",
			})
			api.fire("/api/ships/arrive", map[string]any{
				"context": ctx, "shipID": "voyage-ship", "port": "Singapore",
			})

			resp := api.post("/api/containers/unload", map[string]any{
				"context": ctx, "containerID": "TCKU1000003", "shipID": "voyage-ship",
			})
			Expect(resp.StatusCode).To(Equal(http.StatusAccepted))
			body := readBody(resp)
			c := body["container"].(map[string]any)
			Expect(c["status"]).To(Equal(string(domain.ContainerInTerminal)))
			Expect(c["terminalPort"]).To(Equal("Singapore"))
			Expect(c["onShipID"]).To(BeNil())
		})
	})

	// ── terminal queries ──────────────────────────────────────────────────────
	//
	// KV projections are async (event handler must process the event before
	// the query sees the update), so each assertion uses eventually().

	Describe("terminal queries", func() {
		BeforeEach(func() {
			api.fire("/api/containers/register", map[string]any{
				"context": ctx, "containerID": "TCKU2000001",
				"cargo": "Electronics", "originPort": "Hamburg", "destPort": "Singapore",
			})
			api.fire("/api/containers/register", map[string]any{
				"context": ctx, "containerID": "TCKU2000002",
				"cargo": "Textiles", "originPort": "Hamburg", "destPort": "Rotterdam",
			})
			api.fire("/api/ships/arrive", map[string]any{
				"context": ctx, "shipID": "query-ship", "shipName": "Query", "port": "Hamburg",
			})
			api.fire("/api/containers/load", map[string]any{
				"context": ctx, "containerID": "TCKU2000001", "shipID": "query-ship",
			})
		})

		It("GET /api/containers/{context} returns all containers in the projection", func() {
			eventually(func() error {
				resp := api.get("/api/containers/" + ctx)
				defer resp.Body.Close()
				if resp.StatusCode != http.StatusOK {
					return errors.New("unexpected status")
				}
				var body struct {
					Containers []map[string]any `json:"containers"`
				}
				if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
					return err
				}
				if len(body.Containers) != 2 {
					return errors.New("waiting for 2 containers in KV projection")
				}
				return nil
			})
		})

		It("GET /api/terminal/{context}/{port} returns containers currently in the yard", func() {
			// After loading TCKU2000001, only TCKU2000002 remains in Hamburg.
			eventually(func() error {
				resp := api.get("/api/terminal/" + ctx + "/Hamburg")
				defer resp.Body.Close()
				if resp.StatusCode != http.StatusOK {
					return errors.New("unexpected status")
				}
				var body struct {
					Containers []map[string]any `json:"containers"`
				}
				if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
					return err
				}
				if len(body.Containers) != 1 {
					return errors.New("waiting for Hamburg yard to show 1 container")
				}
				if body.Containers[0]["containerID"] != "TCKU2000002" {
					return errors.New("wrong container in yard")
				}
				return nil
			})
		})

		It("GET /api/manifest/{context}/{shipID} returns containers on the ship", func() {
			eventually(func() error {
				resp := api.get("/api/manifest/" + ctx + "/query-ship")
				defer resp.Body.Close()
				if resp.StatusCode != http.StatusOK {
					return errors.New("unexpected status")
				}
				var body struct {
					Containers []map[string]any `json:"containers"`
				}
				if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
					return err
				}
				if len(body.Containers) != 1 {
					return errors.New("waiting for 1 container on manifest")
				}
				if body.Containers[0]["containerID"] != "TCKU2000001" {
					return errors.New("wrong container on manifest")
				}
				return nil
			})
		})
	})

	// ── ports ─────────────────────────────────────────────────────────────────

	Describe("ports", func() {
		It("GET /api/ports/{context} returns the seeded default ports", func() {
			resp := api.get("/api/ports/" + ctx)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			var body struct {
				Values []string `json:"values"`
			}
			Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
			Expect(body.Values).To(ContainElement("Hamburg"))
		})

		It("POST /api/ports registers a new port, then it is usable and listed", func() {
			resp := api.post("/api/ports", map[string]any{"context": ctx, "name": "Atlantis"})
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusCreated))

			list := api.get("/api/ports/" + ctx)
			defer list.Body.Close()
			var body struct {
				Values []string `json:"values"`
			}
			Expect(json.NewDecoder(list.Body).Decode(&body)).To(Succeed())
			Expect(body.Values).To(ContainElement("Atlantis"))

			arrive := api.post("/api/ships/arrive", map[string]any{
				"context": ctx, "shipID": "atlantis-ship", "shipName": "Atlantis Ship", "port": "Atlantis",
			})
			defer arrive.Body.Close()
			Expect(arrive.StatusCode).To(Equal(http.StatusAccepted))
		})

		It("returns 422 when arriving at an unregistered port (BR-017)", func() {
			resp := api.post("/api/ships/arrive", map[string]any{
				"context": ctx, "shipID": "no-port-ship", "shipName": "No Port", "port": "Nowhere",
			})
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
			body := readBody(resp)
			Expect(body["error"]).To(ContainSubstring(domain.ErrUnknownPort.Error()))
		})

		It("returns 422 when registering a container with an unregistered destination port (BR-018)", func() {
			resp := api.post("/api/containers/register", map[string]any{
				"context": ctx, "containerID": "TCKU5000001",
				"cargo": "Electronics", "originPort": "Hamburg", "destPort": "Nowhere",
			})
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
			body := readBody(resp)
			Expect(body["error"]).To(ContainSubstring(domain.ErrUnknownPort.Error()))
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

	// ── meta queries ──────────────────────────────────────────────────────────

	Describe("meta queries", func() {
		BeforeEach(func() {
			api.fire("/api/ships/arrive", map[string]any{
				"context": ctx, "shipID": "meta-ship", "shipName": "Meta", "port": "Hamburg",
			})
			api.fire("/api/containers/register", map[string]any{
				"context": ctx, "containerID": "TCKU3000001",
				"cargo": "Electronics", "originPort": "Rotterdam", "destPort": "Singapore",
			})
		})

		It("GET /api/meta/{context}/known-containers lists every registered container ID", func() {
			eventually(func() error {
				resp := api.get("/api/meta/" + ctx + "/known-containers")
				defer resp.Body.Close()
				if resp.StatusCode != http.StatusOK {
					return errors.New("unexpected status")
				}
				var body struct {
					Values []string `json:"values"`
				}
				if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
					return err
				}
				if len(body.Values) != 1 || body.Values[0] != "TCKU3000001" {
					return errors.New("known-containers not populated yet")
				}
				return nil
			})
		})
	})

	// ── Shape B ───────────────────────────────────────────────────────────────

	Describe("Shape B — KV cache → Postgres", func() {
		It("returns 200 with cacheHit true after the KV projection warms", func() {
			api.fire("/api/ships/arrive", map[string]any{
				"context": ctx, "shipID": "shapeb-ship", "shipName": "Shape B", "port": "Hamburg",
			})

			eventually(func() error {
				resp := api.get("/api/shape-b/ships/" + ctx + "/shapeb-ship")
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
			resp := api.get("/api/shape-b/ships/" + ctx + "/no-such-ship")
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
		})

		It("DELETE /api/shape-b/cache evicts the entry; next GET shows a cache miss then backfill", func() {
			api.fire("/api/ships/arrive", map[string]any{
				"context": ctx, "shipID": "evict-ship", "shipName": "Evict", "port": "Rotterdam",
			})

			By("wait for cache to warm")
			eventually(func() error {
				resp := api.get("/api/shape-b/ships/" + ctx + "/evict-ship")
				defer resp.Body.Close()
				var body map[string]any
				json.NewDecoder(resp.Body).Decode(&body)
				if body["cacheHit"] != true {
					return errors.New("cache not warm yet")
				}
				return nil
			})

			By("evict")
			del := api.del("/api/shape-b/cache/" + ctx + "/evict-ship")
			del.Body.Close()
			Expect(del.StatusCode).To(Equal(http.StatusNoContent))

			By("immediate read hits Postgres (cache miss)")
			resp := api.get("/api/shape-b/ships/" + ctx + "/evict-ship")
			body := readBody(resp)
			Expect(body["cacheHit"]).To(BeFalse())
			Expect(body["source"]).To(Equal("postgres"))

			By("subsequent read is a cache hit again after backfill")
			eventually(func() error {
				resp := api.get("/api/shape-b/ships/" + ctx + "/evict-ship")
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

	// ── Shape C ───────────────────────────────────────────────────────────────

	Describe("Shape C — fleet reconstruction from event replay", func() {
		It("GET /api/shape-c/fleet returns fleet and containers rebuilt from JetStream", func() {
			api.fire("/api/ships/arrive", map[string]any{
				"context": ctx, "shipID": "shapec-ship", "shipName": "Shape C", "port": "Hamburg",
			})
			api.fire("/api/containers/register", map[string]any{
				"context": ctx, "containerID": "TCKU4000001",
				"cargo": "Electronics", "originPort": "Hamburg", "destPort": "Singapore",
			})
			api.fire("/api/containers/load", map[string]any{
				"context": ctx, "containerID": "TCKU4000001", "shipID": "shapec-ship",
			})

			// Shape C replays JetStream directly — no async projection involved.
			resp := api.get("/api/shape-c/fleet")
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			body := readBody(resp)
			fleet, ok := body["fleet"].([]any)
			Expect(ok).To(BeTrue(), "fleet field must be an array")
			Expect(fleet).To(HaveLen(1))
			containers, ok := body["containers"].([]any)
			Expect(ok).To(BeTrue(), "containers field must be an array")
			Expect(containers).To(HaveLen(1))

			ship := fleet[0].(map[string]any)
			Expect(ship["shipID"]).To(Equal("shapec-ship"))
			manifest := ship["manifest"].([]any)
			Expect(manifest).To(HaveLen(1))
			Expect(manifest[0].(map[string]any)["containerID"]).To(Equal("TCKU4000001"))
		})
	})
})
