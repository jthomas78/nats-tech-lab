package tradingpartner_test

// Integration tests against an embedded in-process NATS server (core NATS
// only) for the micro-service registration that makes trading-partner-service
// discoverable to the Admin UI's Services panel. Same embedded-server
// convention as refdata-service's natsrpc_test.go.
//
// What these guard is a real regression that already happened once: the
// service ran fine, held live NATS connections, and was still absent from the
// Services panel, because an outbound-only rpc.* requester has nothing for
// $SRV discovery to find. A unit test asserting "New() returns no error" would
// not have caught that — so these assert discoverability over the wire, by
// broadcasting $SRV.PING the way nats_ops.go's panel query does.

import (
	"encoding/json"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/trading-partner-service/tradingpartner/internal/browserrpc"
)

// srvDiscoveryWindow mirrors the collection window shipping-service's
// internal/rest/nats_ops.go uses for the real panel query.
const srvDiscoveryWindow = 500 * time.Millisecond

func newTestNATSConn() *nats.Conn {
	GinkgoHelper()
	opts := &server.Options{Port: -1}
	srv, err := server.NewServer(opts)
	Expect(err).NotTo(HaveOccurred())
	srv.Start()
	DeferCleanup(srv.Shutdown)
	Expect(srv.ReadyForConnections(10 * time.Second)).To(BeTrue())

	// CLAUDE.md: every nats.Connect must set nats.Name, and the adapter's
	// ServiceName must match it so Nats-Responder/Nats-Requestor agree.
	nc, err := nats.Connect(srv.ClientURL(), nats.Name(browserrpc.ServiceName))
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(nc.Close)
	Expect(nc.Opts.Name).To(Equal(browserrpc.ServiceName))
	return nc
}

// pingSRV broadcasts $SRV.PING and collects every reply arriving inside the
// discovery window — the same shape as the Services panel's own query.
func pingSRV(nc *nats.Conn) []map[string]any {
	GinkgoHelper()
	inbox := nats.NewInbox()
	replies := make(chan []byte, 16)
	sub, err := nc.Subscribe(inbox, func(msg *nats.Msg) {
		// When a subject has zero responders, the server delivers a "503 No
		// Responders" status message — empty body — to the reply inbox rather
		// than staying silent. That's the absence of a service, not a service
		// reply, so it must not be counted (or JSON-decoded).
		if len(msg.Data) == 0 {
			return
		}
		replies <- msg.Data
	})
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(func() { _ = sub.Unsubscribe() })

	Expect(nc.PublishRequest("$SRV.PING", inbox, nil)).To(Succeed())
	Expect(nc.Flush()).To(Succeed())

	var out []map[string]any
	deadline := time.After(srvDiscoveryWindow)
	for {
		select {
		case data := <-replies:
			var m map[string]any
			Expect(json.Unmarshal(data, &m)).To(Succeed())
			out = append(out, m)
		case <-deadline:
			return out
		}
	}
}

var _ = Describe("NATS micro registration", func() {
	var nc *nats.Conn

	BeforeEach(func() {
		nc = newTestNATSConn()
	})

	Context("before the adapter is registered", func() {
		It("is invisible to $SRV discovery", func() {
			// The pre-change state: a live, named connection that answers
			// nothing on $SRV. This is why the service was missing from the
			// Services panel.
			Expect(pingSRV(nc)).To(BeEmpty())
		})
	})

	Context("once the adapter is registered", func() {
		It("answers $SRV.PING with its service name and version", func() {
			adapter, err := browserrpc.New(nc, browserrpc.Deps{Tenant: "acme"})
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { _ = adapter.Stop() })

			pongs := pingSRV(nc)
			Expect(pongs).To(HaveLen(1))
			Expect(pongs[0]).To(HaveKeyWithValue("name", browserrpc.ServiceName))
			Expect(pongs[0]).To(HaveKeyWithValue("version", browserrpc.ServiceVersion))
		})

		It("labels the registration with its tenant, so per-tenant rows are distinguishable", func() {
			// One connection per tenant means several registrations share the
			// service name; without this the panel can't tell them apart.
			adapter, err := browserrpc.New(nc, browserrpc.Deps{Tenant: "globex"})
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { _ = adapter.Stop() })

			pongs := pingSRV(nc)
			Expect(pongs).To(HaveLen(1))
			Expect(pongs[0]["metadata"]).To(HaveKeyWithValue("tenant", "globex"))
		})

		// Replaces 26g's "registers no endpoints yet" spec, which existed to pin
		// that increment's deliberate scope. Phase 26h is the phase that spec
		// said should replace it.
		It("advertises all 23 api.* endpoints on $SRV.INFO", func() {
			adapter, err := browserrpc.New(nc, browserrpc.Deps{Tenant: "acme"})
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { _ = adapter.Stop() })

			reply, err := nc.Request("$SRV.INFO", nil, srvDiscoveryWindow)
			Expect(err).NotTo(HaveOccurred())
			var info struct {
				Endpoints []struct {
					Name    string `json:"name"`
					Subject string `json:"subject"`
				} `json:"endpoints"`
			}
			Expect(json.Unmarshal(reply.Data, &info)).To(Succeed())

			subjects := make([]string, 0, len(info.Endpoints))
			for _, ep := range info.Endpoints {
				subjects = append(subjects, ep.Subject)
			}
			Expect(subjects).To(ConsistOf(
				"api.*.trading-partner.partner.register.v1",
				"api.*.trading-partner.partner.list.v1",
				"api.*.trading-partner.partner.get.v1",
				"api.*.trading-partner.partner.update.v1",
				"api.*.trading-partner.partner.activate.v1",
				"api.*.trading-partner.partner.suspend.v1",
				"api.*.trading-partner.partner.reactivate.v1",
				"api.*.trading-partner.partner.audit.v1",
				"api.*.trading-partner.partner.profile.v1",
				"api.*.trading-partner.document.add.v1",
				"api.*.trading-partner.document.list.v1",
				"api.*.trading-partner.document.approve.v1",
				"api.*.trading-partner.document.reject.v1",
				"api.*.trading-partner.document.resubmit.v1",
				// Phase 38c-ii (BR-TP41). Endpoints 17 and 18 mint capability
				// tickets; the document bytes they authorize never travel over
				// api.* at all — see internal/rest/document_files.go.
				"api.*.trading-partner.document.upload-ticket.v1",
				"api.*.trading-partner.document.download-ticket.v1",
				"api.*.trading-partner.fleet-asset.add.v1",
				"api.*.trading-partner.fleet-asset.list.v1",
				// Phase 38d-ii (BR-TP46-BR-TP50). add carries no tenant and no
				// countryCode: the tenant is this adapter's connection, and the
				// parent country is resolved from refdata's own `country`
				// relation (BR-D47), never taken from the caller.
				"api.*.trading-partner.operating-area.add.v1",
				"api.*.trading-partner.operating-area.list.v1",
				"api.*.trading-partner.operating-area.remove.v1",
				// Phase 38d-ii (BR-TP51-BR-TP55). Two endpoints, not three:
				// there is deliberately no read-payload subject, because
				// BR-TP52 means no api.* call may return credential
				// material. The absence is the enforcement — if a third
				// tracking-credential subject ever appears here, that rule
				// has been broken.
				"api.*.trading-partner.tracking-credential.configure.v1",
				"api.*.trading-partner.tracking-credential.list.v1",
			))
		})

		It("uses api.*, never rpc.*, for its browser-facing endpoints", func() {
			// CLAUDE.md / ARCHITECTURE-COMMUNICATIONS.md § 2: "a browser
			// credential is never granted rpc.>". Registering a browser-facing
			// operation under rpc.* would make it unreachable by the only caller
			// that has permission to use it, so this is a correctness rule, not
			// a style preference.
			adapter, err := browserrpc.New(nc, browserrpc.Deps{Tenant: "acme"})
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { _ = adapter.Stop() })

			reply, err := nc.Request("$SRV.INFO", nil, srvDiscoveryWindow)
			Expect(err).NotTo(HaveOccurred())
			var info struct {
				Endpoints []struct {
					Subject string `json:"subject"`
				} `json:"endpoints"`
			}
			Expect(json.Unmarshal(reply.Data, &info)).To(Succeed())
			Expect(info.Endpoints).NotTo(BeEmpty())
			for _, ep := range info.Endpoints {
				Expect(ep.Subject).To(HavePrefix("api."))
				Expect(ep.Subject).NotTo(HavePrefix("rpc."))
			}
		})

		It("keeps every subject at 6 tokens so {context} is readable by position", func() {
			// contextFromSubject reads parts[1]. A subject with different arity
			// would silently resolve the wrong context rather than fail loudly.
			adapter, err := browserrpc.New(nc, browserrpc.Deps{Tenant: "acme"})
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { _ = adapter.Stop() })

			reply, err := nc.Request("$SRV.INFO", nil, srvDiscoveryWindow)
			Expect(err).NotTo(HaveOccurred())
			var info struct {
				Endpoints []struct {
					Subject string `json:"subject"`
				} `json:"endpoints"`
			}
			Expect(json.Unmarshal(reply.Data, &info)).To(Succeed())
			for _, ep := range info.Endpoints {
				parts := strings.Split(ep.Subject, ".")
				Expect(parts).To(HaveLen(6), "subject %q", ep.Subject)
				Expect(parts[1]).To(Equal("*"), "token 1 must be the {context} wildcard")
				Expect(parts[2]).To(Equal("trading-partner"))
				Expect(parts[5]).To(Equal("v1"))
			}
		})
	})

	Context("after Stop", func() {
		It("stops answering discovery, leaving no phantom row in the panel", func() {
			// Guards the teardown path: TeardownByName stops the adapter before
			// closing the connection precisely so a suspended tenant stops
			// being discoverable (BR-031).
			adapter, err := browserrpc.New(nc, browserrpc.Deps{Tenant: "acme"})
			Expect(err).NotTo(HaveOccurred())
			Expect(pingSRV(nc)).To(HaveLen(1))

			Expect(adapter.Stop()).To(Succeed())

			Expect(pingSRV(nc)).To(BeEmpty())
		})

		It("is safe to call twice", func() {
			adapter, err := browserrpc.New(nc, browserrpc.Deps{Tenant: "acme"})
			Expect(err).NotTo(HaveOccurred())

			Expect(adapter.Stop()).To(Succeed())
			Expect(adapter.Stop()).To(Succeed())
		})
	})
})
