package otelnats_test

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/jthomas78/nats-tech-lab/shared/otelnats"
)

// pubsubEnvelope is the part of the obs.pubsub.* envelope these specs read.
// The field names are the legacy tracer's, unchanged (BR-045, BR-047).
type pubsubEnvelope struct {
	Direction    string          `json:"direction"`
	Subject      string          `json:"subject"`
	Payload      json.RawMessage `json:"payload"`
	PayloadBytes int             `json:"payloadBytes"`
	TraceID      string          `json:"traceId"`
	SpanID       string          `json:"spanId"`
	ParentSpanID string          `json:"parentSpanId"`
	Service      string          `json:"service"`
	Entity       string          `json:"entity"`
	Action       string          `json:"action"`
	Redacted     []string        `json:"redacted"`
	Truncated    bool            `json:"truncated"`
}

// observations subscribes to obs.pubsub.> on nc.
func observations(nc *nats.Conn) *nats.Subscription {
	sub, err := nc.SubscribeSync("obs.pubsub.>")
	Expect(err).NotTo(HaveOccurred())
	Expect(nc.Flush()).To(Succeed())
	return sub
}

func nextObservation(sub *nats.Subscription) (*nats.Msg, pubsubEnvelope) {
	msg, err := sub.NextMsg(2 * time.Second)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "want an obs.pubsub.* observation")
	var env pubsubEnvelope
	ExpectWithOffset(1, json.Unmarshal(msg.Data, &env)).To(Succeed())
	return msg, env
}

func noObservation(sub *nats.Subscription) {
	_, err := sub.NextMsg(300 * time.Millisecond)
	ExpectWithOffset(1, err).To(MatchError(nats.ErrTimeout), "want no obs.pubsub.* observation")
}

var _ = Describe("BR-045: the obs.pubsub.* publisher takes its IDs from the OTel span (decision 13)", func() {
	const subject = "evt.acme.shipping.ship.s1.arrived"

	It("BR-045: observes under the derived tokens, with the real subject in the envelope", func() {
		nc := startNATS("otelnats-observe-spec")
		sub := observations(nc)

		otelnats.NewObserver(nc).ObservePublish(context.Background(), subject, []byte(`{"port":"ZADUR"}`))

		msg, env := nextObservation(sub)
		Expect(msg.Subject).To(Equal("obs.pubsub.acme.shipping.ship.arrived"))
		Expect(env.Direction).To(Equal("publish"))
		Expect(env.Subject).To(Equal(subject))
		Expect(env.Service).To(Equal("shipping"))
		Expect(env.Entity).To(Equal("ship"))
		Expect(env.Action).To(Equal("arrived"))
		Expect(string(env.Payload)).To(Equal(`{"port":"ZADUR"}`))
	})

	It("BR-045: takes traceId and spanId from the PRODUCER span in ctx, and parentSpanId from its parent", func() {
		nc := startNATS("otelnats-observe-spec")
		sub := observations(nc)
		exp := tracetest.NewInMemoryExporter()
		p := newProvider("shipping-service", exp)

		ctx, parent := p.Tracer().Start(context.Background(), "api.acme.shipping.ship.arrive.v1")
		pctx, producer := p.StartProducer(ctx, subject)
		otelnats.NewObserver(nc).ObservePublish(pctx, subject, []byte(`{}`))
		producer.End()
		parent.End()

		_, env := nextObservation(sub)
		Expect(env.TraceID).To(Equal(producer.SpanContext().TraceID().String()))
		Expect(env.SpanID).To(Equal(producer.SpanContext().SpanID().String()))
		Expect(env.ParentSpanID).To(Equal(parent.SpanContext().SpanID().String()))
	})

	It("BR-045: starts a new root trace ID when ctx holds no span", func() {
		nc := startNATS("otelnats-observe-spec")
		sub := observations(nc)

		otelnats.NewObserver(nc).ObservePublish(context.Background(), subject, []byte(`{}`))

		_, env := nextObservation(sub)
		Expect(env.TraceID).To(MatchRegexp(`^[0-9a-f]{32}$`))
		Expect(env.SpanID).To(MatchRegexp(`^[0-9a-f]{16}$`))
		Expect(env.ParentSpanID).To(BeEmpty())
	})

	It("BR-047: sets Nats-Msg-Id to the envelope's spanId", func() {
		nc := startNATS("otelnats-observe-spec")
		sub := observations(nc)
		exp := tracetest.NewInMemoryExporter()
		p := newProvider("shipping-service", exp)

		pctx, producer := p.StartProducer(context.Background(), subject)
		otelnats.NewObserver(nc).ObservePublish(pctx, subject, []byte(`{}`))
		producer.End()

		msg, env := nextObservation(sub)
		Expect(env.SpanID).NotTo(BeEmpty())
		Expect(msg.Header.Get(nats.MsgIdHdr)).To(Equal(env.SpanID))
	})

	It("BR-045: ObservePublishAs uses the tokens it was given, not the ones in the subject", func() {
		nc := startNATS("otelnats-observe-spec")
		sub := observations(nc)

		otelnats.NewObserver(nc).ObservePublishAs(context.Background(),
			"notify._platform.refdata.acme.ports.changed", []byte(`{}`),
			"acme", "refdata", "ports", "changed")

		msg, env := nextObservation(sub)
		Expect(msg.Subject).To(Equal("obs.pubsub.acme.refdata.ports.changed"))
		Expect(env.Subject).To(Equal("notify._platform.refdata.acme.ports.changed"))
	})

	It("BR-046: redacts the payload before it truncates it", func() {
		nc := startNATS("otelnats-observe-spec")
		sub := observations(nc)
		big := `{"pad":"` + strings.Repeat("x", 4070) + `","actorName":"Jo Bloggs"}`

		otelnats.NewObserver(nc).ObservePublish(context.Background(), subject, []byte(big))

		_, env := nextObservation(sub)
		// Truncated first, the cut at 4 KiB would keep the key and part of
		// the name. Redacted first, the payload fits under the cap.
		Expect(string(env.Payload)).NotTo(ContainSubstring("actorName"))
		Expect(string(env.Payload)).NotTo(ContainSubstring("Jo"))
		Expect(env.Redacted).To(ConsistOf("actorName"))
		Expect(env.Truncated).To(BeFalse())
		// payloadBytes is the size after redaction, before truncation, as
		// the retired legacy tracer sent it (decision 13: behaviour unchanged).
		Expect(env.PayloadBytes).To(Equal(len(`{"pad":"` + strings.Repeat("x", 4070) + `"}`)))
	})

	It("BR-045: never observes an obs.* subject (no self-observation)", func() {
		nc := startNATS("otelnats-observe-spec")
		sub := observations(nc)

		otelnats.NewObserver(nc).ObservePublish(context.Background(), "obs.pubsub.acme.shipping.ship.arrived", []byte(`{}`))

		noObservation(sub)
	})

	It("BR-045: skips a subject too short to name its tokens", func() {
		nc := startNATS("otelnats-observe-spec")
		sub := observations(nc)

		otelnats.NewObserver(nc).ObservePublish(context.Background(), "evt.acme.shipping", []byte(`{}`))

		noObservation(sub)
	})

	It("BR-045: a nil Observer is silent and does not panic", func() {
		var o *otelnats.Observer
		Expect(func() { o.ObservePublish(context.Background(), subject, []byte(`{}`)) }).NotTo(Panic())
		Expect(func() {
			o.ObservePublishAs(context.Background(), subject, []byte(`{}`), "acme", "shipping", "ship", "arrived")
		}).NotTo(Panic())
	})
})
