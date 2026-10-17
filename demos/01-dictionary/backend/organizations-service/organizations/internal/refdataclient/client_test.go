package refdataclient_test

// Wire-level specs for refdataclient.Client. This package had none before
// Phase 28c. Wiring BR-037's traceparent injection here first assumed the
// bare "refdata.item.get.v1" subject was a bug (it looks nothing like
// refdata-service's real registered "rpc.*.refdata.item.get.v1") and
// "fixed" it to construct the full subject directly — which turned out to
// be wrong and worse than the assumed bug: "refdata.item.get.v1" is this
// tenant account's *local* alias, remapped server-side by an account import
// (accounts-service's provisioner.go, jwt.RenamingSubject) to
// "rpc.{tenant}.refdata.item.get.v1" — the account's own identity lands at
// that token, never a value the client supplies. Publishing the full
// subject directly doesn't match the account's LocalSubject (so it doesn't
// route) and would have been a security regression had it routed (a
// business-unit context, not the tenant, ending up in the token the import
// exists specifically to keep caller-uncontrolled). These specs pin the
// correct local-alias subject so it does not regress silently again; the
// trace-propagation specs are in otel_test.go (Phase 54d).

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/internal/refdataclient"
)

func newTestConn() *nats.Conn {
	GinkgoHelper()
	opts := &server.Options{Port: -1}
	srv, err := server.NewServer(opts)
	Expect(err).NotTo(HaveOccurred())
	srv.Start()
	DeferCleanup(srv.Shutdown)
	Expect(srv.ReadyForConnections(10 * time.Second)).To(BeTrue())

	nc, err := nats.Connect(srv.ClientURL(), nats.Name("refdataclient-test"))
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(nc.Close)
	return nc
}

var _ = Describe("refdataclient.Client (BR-TP14/BR-037)", func() {
	var nc *nats.Conn

	BeforeEach(func() {
		nc = newTestConn()
	})

	Context("BR-TP14 — the rpc.* call rides the tenant account's local alias, not a directly-constructed subject", func() {
		It("requests the local alias refdata.item.get.v1 (the real cross-account subject is a NATS-server-side remap this client never constructs)", func() {
			received := make(chan *nats.Msg, 1)
			sub, err := nc.Subscribe("refdata.item.get.v1", func(m *nats.Msg) {
				received <- m
				_ = m.Respond([]byte(`{"item":{"code":"TAUTLINER","status":"active"}}`))
			})
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { _ = sub.Unsubscribe() })
			Expect(nc.Flush()).To(Succeed())

			client := refdataclient.New(nc)
			ok, err := client.Exists(context.Background(), "acme-north", "TAUTLINER")
			Expect(err).NotTo(HaveOccurred())
			Expect(ok).To(BeTrue())

			var msg *nats.Msg
			Eventually(received).Should(Receive(&msg))
			Expect(msg.Subject).To(Equal("refdata.item.get.v1"))
		})

		It("returns false, no error, when refdata-service reports the code not found", func() {
			sub, err := nc.Subscribe("refdata.item.get.v1", func(m *nats.Msg) {
				_ = m.Respond([]byte(`{"error":"item not found","notFound":true}`))
			})
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { _ = sub.Unsubscribe() })
			Expect(nc.Flush()).To(Succeed())

			client := refdataclient.New(nc)
			ok, err := client.Exists(context.Background(), "acme-north", "NOT-A-TYPE")
			Expect(err).NotTo(HaveOccurred())
			Expect(ok).To(BeFalse())
		})
	})

	// Phase 54d retired the legacy tracer. The span rules (BR-037 continuation and
	// one span per call, BR-055's refused reply) are specified on the OTel
	// path in otel_test.go; without a Provider the call is untraced.
	Context("BR-037 — with no Provider the rpc.* call is untraced", func() {
		It("still answers, carries no traceparent, and publishes nothing on obs.trace.>", func() {
			legacy := make(chan *nats.Msg, 4)
			spanSub, err := nc.Subscribe("obs.trace.>", func(m *nats.Msg) { legacy <- m })
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { _ = spanSub.Unsubscribe() })

			received := make(chan *nats.Msg, 1)
			sub, err := nc.Subscribe("refdata.item.get.v1", func(m *nats.Msg) {
				received <- m
				_ = m.Respond([]byte(`{"item":{"code":"TAUTLINER","status":"active"}}`))
			})
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { _ = sub.Unsubscribe() })
			Expect(nc.Flush()).To(Succeed())

			ok, err := refdataclient.New(nc).Exists(context.Background(), "acme-north", "TAUTLINER")
			Expect(err).NotTo(HaveOccurred())
			Expect(ok).To(BeTrue())

			var msg *nats.Msg
			Eventually(received).Should(Receive(&msg))
			Expect(msg.Header.Get("traceparent")).To(BeEmpty())
			Expect(msg.Header.Get("Nats-Requestor")).NotTo(BeEmpty(), "BR-041's requester header does not depend on tracing")
			Expect(nc.Flush()).To(Succeed())
			Consistently(legacy, 200*time.Millisecond).ShouldNot(Receive())
		})
	})
})
