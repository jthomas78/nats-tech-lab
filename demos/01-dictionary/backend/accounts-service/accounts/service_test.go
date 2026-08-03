package accounts_test

// Regression coverage for the gap flagged during Phase 17c review: startup
// succeeding (main() not returning an error) only proves micro.AddService
// didn't error — it doesn't prove the registration actually responds on the
// wire, which depends on the SYS account's permissions allowing micro's
// internal $SRV.* subscriptions. That was verified manually once (`nats
// micro info accounts-service --creds sys.creds`) but had no automated
// check. Runs against the same operator-mode embedded server
// (newOperatorTestServer, operator_helper_test.go) provisioner_test.go
// uses, connected as the real SYS account — not a plain unauthenticated
// embedded server — so a permissions regression on that account would be
// caught here too.

import (
	"encoding/json"
	"time"

	"github.com/nats-io/nats.go/micro"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/accounts-service/accounts"
)

var _ = Describe("RegisterMicroService (Phase 17c)", func() {
	var ots *operatorTestServer

	BeforeEach(func() {
		ots = newOperatorTestServer(GinkgoT())
		DeferCleanup(ots.Shutdown)
	})

	It("registers on the SYS connection and responds to a $SRV.PING broadcast with its name and version", func() {
		nc := ots.ConnectSys(GinkgoT())
		DeferCleanup(nc.Close)

		svc, err := accounts.RegisterMicroService(nc)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { Expect(svc.Stop()).To(Succeed()) })

		subject, err := micro.ControlSubject(micro.PingVerb, "", "")
		Expect(err).NotTo(HaveOccurred())
		reply, err := nc.Request(subject, nil, 2*time.Second)
		Expect(err).NotTo(HaveOccurred())

		var ping micro.Ping
		Expect(json.Unmarshal(reply.Data, &ping)).To(Succeed())
		Expect(ping.Name).To(Equal("accounts-service"))
		Expect(ping.Version).To(Equal("1.0.0"))
	})

	It("registers no endpoints — it's discoverable, but has nothing to route requests to (provisioning stays REST-only)", func() {
		nc := ots.ConnectSys(GinkgoT())
		DeferCleanup(nc.Close)

		svc, err := accounts.RegisterMicroService(nc)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { Expect(svc.Stop()).To(Succeed()) })

		Expect(svc.Info().Endpoints).To(BeEmpty())
	})
})
