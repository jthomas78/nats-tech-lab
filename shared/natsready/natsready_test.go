package natsready_test

// BR-AS62 — presence is not readiness.
//
// The specs are all one shape: something is wrong with the service and the
// answer has to say so, even though the process is up, the connection is
// open and a reply comes back quickly. That gap is the whole reason this
// package exists.

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/shared/mferegistry"
	"github.com/jthomas78/nats-tech-lab/shared/natsready"
	"github.com/jthomas78/nats-tech-lab/shared/natstest"
)

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestReadyWhenTheCheckPasses(t *testing.T) {
	g := NewWithT(t)
	nc := natstest.Start(t, "natsready-spec")

	r, err := natsready.Mount(nc, "refdata-service", func(context.Context) error { return nil }, quiet())
	g.Expect(err).NotTo(HaveOccurred())
	defer r.Stop()

	msg, err := nc.Request(mferegistry.ServiceReady("refdata-service"), nil, 2*time.Second)
	g.Expect(err).NotTo(HaveOccurred())

	var out map[string]any
	g.Expect(json.Unmarshal(msg.Data, &out)).To(Succeed())
	g.Expect(out["ready"]).To(BeTrue())
}

// The process is up and replies immediately — and is still not ready. A
// registry that treated a reply as readiness would call this healthy.
func TestNotReadyWhenTheCheckFails(t *testing.T) {
	g := NewWithT(t)
	nc := natstest.Start(t, "natsready-spec")

	r, err := natsready.Mount(nc, "refdata-service", func(context.Context) error {
		return errors.New("dial tcp 10.0.0.5:5432: connect: connection refused")
	}, quiet())
	g.Expect(err).NotTo(HaveOccurred())
	defer r.Stop()

	msg, err := nc.Request(mferegistry.ServiceReady("refdata-service"), nil, 2*time.Second)
	g.Expect(err).NotTo(HaveOccurred())

	var out map[string]any
	g.Expect(json.Unmarshal(msg.Data, &out)).To(Succeed())
	g.Expect(out["ready"]).To(BeFalse())

	// The cause is a word, not the error. The real text named a host, a port
	// and a database — deployment topology, which never leaves this process
	// (BR-AS60).
	g.Expect(out["cause"]).To(Equal("not-ready"))
	g.Expect(string(msg.Data)).NotTo(ContainSubstring("10.0.0.5"))
}

// Each ask runs the check again. Caching would turn a readiness answer into a
// memory of one, which is the thing this package exists not to be.
func TestEveryAskRunsTheCheck(t *testing.T) {
	g := NewWithT(t)
	nc := natstest.Start(t, "natsready-spec")

	calls := 0
	r, err := natsready.Mount(nc, "refdata-service", func(context.Context) error {
		calls++
		return nil
	}, quiet())
	g.Expect(err).NotTo(HaveOccurred())
	defer r.Stop()

	for i := 0; i < 3; i++ {
		_, err := nc.Request(mferegistry.ServiceReady("refdata-service"), nil, 2*time.Second)
		g.Expect(err).NotTo(HaveOccurred())
	}
	g.Expect(calls).To(Equal(3))
}

// A check is given a deadline, so a hung dependency fails closed instead of
// holding the reply open until the asker times out with no answer at all.
func TestTheCheckIsBounded(t *testing.T) {
	g := NewWithT(t)

	g.Expect(natsready.CheckTimeout).To(BeNumerically("<=", 2*time.Second))

	nc := natstest.Start(t, "natsready-spec")
	deadline := false
	r, err := natsready.Mount(nc, "refdata-service", func(ctx context.Context) error {
		_, deadline = ctx.Deadline()
		return nil
	}, quiet())
	g.Expect(err).NotTo(HaveOccurred())
	defer r.Stop()

	_, err = nc.Request(mferegistry.ServiceReady("refdata-service"), nil, 2*time.Second)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(deadline).To(BeTrue())
}

// Stopping takes the answer away rather than leaving a stale yes behind: a
// shutting-down service must look absent, not ready.
func TestStopStopsAnswering(t *testing.T) {
	g := NewWithT(t)
	nc := natstest.Start(t, "natsready-spec")

	r, err := natsready.Mount(nc, "refdata-service", func(context.Context) error { return nil }, quiet())
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(r.Stop()).To(Succeed())

	_, err = nc.Request(mferegistry.ServiceReady("refdata-service"), nil, 300*time.Millisecond)
	g.Expect(err).To(HaveOccurred())
}

// The subject is built from the deployment's service ID, and two services
// never share one.
func TestSubjectIsPerService(t *testing.T) {
	g := NewWithT(t)

	g.Expect(mferegistry.ServiceReady("refdata-service")).To(Equal("rpc._platform.health.refdata-service.ready.v1"))
	g.Expect(mferegistry.ServiceReady("refdata-service")).NotTo(Equal(mferegistry.ServiceReady("shipping-service")))

	// Outbound only: it is in neither browser profile, so no shell or
	// operator credential can ask a service whether it is ready.
	g.Expect(mferegistry.Subjects()).NotTo(ContainElement(mferegistry.ServiceReady("refdata-service")))
	g.Expect(mferegistry.Operator()).NotTo(ContainElement(mferegistry.ServiceReady("refdata-service")))
}
