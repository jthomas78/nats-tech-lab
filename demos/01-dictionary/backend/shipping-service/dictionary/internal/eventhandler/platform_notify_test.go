// Tests for Phase 23's PLATFORM-account background bridges — the
// notify.* republishers replacing dictionary/internal/rest/sse.go's
// per-SSE-connection watchRefdata/watchRPCObs OrderedConsumers. Both bridges
// run in their own goroutine, so specs poll with Eventually rather than
// asserting synchronously after a single publish.
package eventhandler_test

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/eventhandler"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/internal/jstream"
)

func newPlatformNotifyTestNATS() (*nats.Conn, jetstream.JetStream) {
	GinkgoHelper()
	opts := &server.Options{JetStream: true, StoreDir: GinkgoT().TempDir(), Port: -1}
	srv, err := server.NewServer(opts)
	Expect(err).NotTo(HaveOccurred())
	srv.Start()
	DeferCleanup(srv.Shutdown)
	Expect(srv.ReadyForConnections(10 * time.Second)).To(BeTrue())

	nc, err := nats.Connect(srv.ClientURL(), nats.Name("platform-notify-test"))
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(nc.Close)

	js, err := jetstream.New(nc)
	Expect(err).NotTo(HaveOccurred())
	return nc, js
}

func subscribeSync(nc *nats.Conn, subject string) chan []byte {
	GinkgoHelper()
	out := make(chan []byte, 8)
	sub, err := nc.Subscribe(subject, func(m *nats.Msg) { out <- m.Data })
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(func() { Expect(sub.Unsubscribe()).To(Succeed()) })
	return out
}

// publishUntilReceived repeatedly republishes on sourceSubject (harmless —
// both bridges under test are pure republish, order/count-independent) until
// notifyCh receives something, since the bridge's OrderedConsumer is set up
// asynchronously in its own goroutine (DeliverNewPolicy: a publish that lands
// before that setup completes is legitimately missed, same race
// dictionary/internal/rest/sse.go's own watchRPCObs doc comment calls out).
func publishUntilReceived(nc *nats.Conn, sourceSubject string, payload []byte, notifyCh chan []byte) []byte {
	GinkgoHelper()
	var payloadOut []byte
	Eventually(func() []byte {
		Expect(nc.Publish(sourceSubject, payload)).To(Succeed())
		select {
		case payloadOut = <-notifyCh:
		case <-time.After(200 * time.Millisecond):
		}
		return payloadOut
	}, 5*time.Second, 250*time.Millisecond).ShouldNot(BeEmpty())
	return payloadOut
}

var _ = Describe("RegisterRefdataNotify (Phase 23)", func() {
	It("republishes evt.{context}.refdata.{typeKey}.changed onto notify._platform.refdata.{context}.{typeKey}.changed", func() {
		nc, js := newPlatformNotifyTestNATS()
		ctx, cancel := context.WithCancel(context.Background())
		DeferCleanup(cancel)

		_, err := jstream.CreateStream(ctx, js, "REFDATA", []string{"evt.*.refdata.>"})
		Expect(err).NotTo(HaveOccurred())

		eventhandler.RegisterRefdataNotify(ctx, js, nc, discardLogger())

		notifyCh := subscribeSync(nc, "notify._platform.refdata.>")

		payload := publishUntilReceived(nc, "evt.acme.refdata.hazard-class.changed", []byte(`{"typeKey":"hazard-class"}`), notifyCh)
		Expect(string(payload)).To(ContainSubstring("hazard-class"))
	})

	// Phase 43a (BR-045): this bridge is one of the five notify.* call sites
	// in this service. Its observation is filed under the refdata change's
	// own {context}, not the "_platform" token its republish subject carries.
	It("observes its republish on obs.pubsub.{context}.refdata.{typeKey}.changed", func() {
		nc, js := newPlatformNotifyTestNATS()
		ctx, cancel := context.WithCancel(context.Background())
		DeferCleanup(cancel)

		_, err := jstream.CreateStream(ctx, js, "REFDATA", []string{"evt.*.refdata.>"})
		Expect(err).NotTo(HaveOccurred())

		eventhandler.RegisterRefdataNotify(ctx, js, nc, discardLogger())

		notifyCh := subscribeSync(nc, "notify._platform.refdata.>")
		obsCh := subscribeSync(nc, "obs.pubsub.acme.refdata.hazard-class.changed")

		publishUntilReceived(nc, "evt.acme.refdata.hazard-class.changed", []byte(`{"typeKey":"hazard-class"}`), notifyCh)

		var envelope []byte
		Eventually(obsCh, 2*time.Second).Should(Receive(&envelope))
		Expect(string(envelope)).To(ContainSubstring(`"subject":"notify._platform.refdata.acme.hazard-class.changed"`))
		Expect(string(envelope)).To(ContainSubstring(`"direction":"publish"`))
	})

	It("does not publish or panic when platformJS or platformNC is nil", func() {
		nc, js := newPlatformNotifyTestNATS()
		ctx, cancel := context.WithCancel(context.Background())
		DeferCleanup(cancel)

		Expect(func() { eventhandler.RegisterRefdataNotify(ctx, nil, nc, discardLogger()) }).NotTo(Panic())
		Expect(func() { eventhandler.RegisterRefdataNotify(ctx, js, nil, discardLogger()) }).NotTo(Panic())
	})
})

// RegisterRPCTraceNotify (Phase 23) had its dedicated Describe block here,
// covering the RPCTRACE-stream-to-notify bridge. Retired in Phase 28g along
// with the function itself and RPCTRACE's provisioning in refdata-service's
// composition.go — see platform_notify.go's retirement note. Removed rather
// than left red.
