package orchestration_test

import (
	"context"
	"errors"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	organizationdomain "github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/internal/domain"
	profiledomain "github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/transporterprofile/domain"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/transporterprofile/orchestration"
	"github.com/jthomas78/nats-tech-lab/shared/otelnats"
)

// flakyProjection fails the first upsertFailures Upserts and the first
// putFailures Puts, then succeeds — one failed processing attempt followed
// by a redelivery that works.
type flakyProjection struct {
	mu             sync.Mutex
	upsertFailures int
	putFailures    int
	writes         int
}

func (f *flakyProjection) Upsert(context.Context, profiledomain.State) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.upsertFailures > 0 {
		f.upsertFailures--
		return errors.New("postgres: connection refused")
	}
	f.writes++
	return nil
}

func (f *flakyProjection) Put(context.Context, profiledomain.State) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.putFailures > 0 {
		f.putFailures--
		return errors.New("kv: put timed out")
	}
	return nil
}

// flakyCertificates fails the first failures certificate writes, then
// records each write.
type flakyCertificates struct {
	mu       sync.Mutex
	failures int
	writes   int
}

func (f *flakyCertificates) UpsertCertificate(context.Context, string, organizationdomain.ProjectedCertificate) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failures > 0 {
		f.failures--
		return errors.New("postgres: compliance_documents write failed")
	}
	f.writes++
	return nil
}

func (f *flakyCertificates) written() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.writes
}

func (f *flakyProjection) written() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.writes
}

var _ = Describe("Projector CONSUMER span status", func() {
	// BR-TP76: the CONSUMER span is one processing attempt. A failed attempt
	// marks its span Error even when a redelivery later succeeds; a
	// successful attempt and an intentional skip leave the status Unset.
	Context("BR-TP76 a failed processing attempt marks its CONSUMER span Error", func() {
		var (
			ctx  context.Context
			prov *otelnats.Provider
			exp  *tracetest.InMemoryExporter
		)

		consumerSpans := func() []tracetest.SpanStub {
			GinkgoHelper()
			Expect(prov.ForceFlush(ctx)).To(Succeed())
			var found []tracetest.SpanStub
			for _, s := range exp.GetSpans() {
				if s.SpanKind == trace.SpanKindConsumer {
					found = append(found, s)
				}
			}
			return found
		}
		statuses := func() []codes.Code {
			var got []codes.Code
			for _, s := range consumerSpans() {
				got = append(got, s.Status.Code)
			}
			return got
		}
		recordedAnError := func(s tracetest.SpanStub) bool {
			for _, e := range s.Events {
				if e.Name == "exception" {
					return true
				}
			}
			return false
		}
		firstErrorSpan := func() (tracetest.SpanStub, bool) {
			for _, s := range consumerSpans() {
				if s.Status.Code == codes.Error {
					return s, true
				}
			}
			return tracetest.SpanStub{}, false
		}

		BeforeEach(func() {
			ctx = context.Background()
			exp = tracetest.NewInMemoryExporter()
			var err error
			prov, err = otelnats.Init(ctx, otelnats.Config{ServiceName: "organizations-service", Exporter: exp})
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { _ = prov.Shutdown(context.Background()) })
		})

		It("marks the span Error when the event cannot be decoded and is terminated", func() {
			_, js := testJetStream()
			Expect(orchestration.EnsureStream(ctx, js)).To(Succeed())
			projection := &flakyProjection{}
			projector := orchestration.NewProjector(js, projection, projection).WithOTel(prov, "acme")
			Expect(projector.Start(ctx)).To(Succeed())
			DeferCleanup(projector.Stop)

			const id = "01J0TRANSPORTER0000000201"
			_, err := js.Publish(ctx, profiledomain.Subject("acme", id, profiledomain.CreatedEvent), []byte("not json"))
			Expect(err).NotTo(HaveOccurred())

			Eventually(statuses).Should(Equal([]codes.Code{codes.Error}))
			Expect(recordedAnError(consumerSpans()[0])).To(BeTrue(), "the decode error is recorded on the span")
			Consistently(statuses, 200*time.Millisecond).Should(HaveLen(1), "a terminated event is not redelivered")
			Expect(projection.written()).To(BeZero())
		})

		It("marks the failed attempt Error and the successful redelivery Unset when the projection write fails once", func() {
			_, js := testJetStream()
			Expect(orchestration.EnsureStream(ctx, js)).To(Succeed())
			projection := &flakyProjection{upsertFailures: 1}
			projector := orchestration.NewProjector(js, projection, projection).WithOTel(prov, "acme")
			Expect(projector.Start(ctx)).To(Succeed())
			DeferCleanup(projector.Stop)

			const id = "01J0TRANSPORTER0000000202"
			_, err := orchestration.NewJetStreamEventStore(js).Append(ctx, "acme", id, profiledomain.NewCreatedEvent("acme", id), 0)
			Expect(err).NotTo(HaveOccurred())

			Eventually(projection.written).Should(Equal(1))
			Eventually(statuses).Should(Equal([]codes.Code{codes.Error, codes.Unset}))
			Expect(recordedAnError(consumerSpans()[0])).To(BeTrue(), "the projection error is recorded on the span")
		})

		It("marks the failed attempt Error when the cache write fails", func() {
			_, js := testJetStream()
			Expect(orchestration.EnsureStream(ctx, js)).To(Succeed())
			projection := &flakyProjection{putFailures: 1}
			projector := orchestration.NewProjector(js, projection, projection).WithOTel(prov, "acme")
			Expect(projector.Start(ctx)).To(Succeed())
			DeferCleanup(projector.Stop)

			const id = "01J0TRANSPORTER0000000203"
			_, err := orchestration.NewJetStreamEventStore(js).Append(ctx, "acme", id, profiledomain.NewCreatedEvent("acme", id), 0)
			Expect(err).NotTo(HaveOccurred())

			Eventually(statuses).Should(Equal([]codes.Code{codes.Error, codes.Unset}))
		})

		It("marks the span Error when the aggregate cannot be hydrated", func() {
			_, js := testJetStream()
			Expect(orchestration.EnsureStream(ctx, js)).To(Succeed())
			projection := &flakyProjection{}
			projector := orchestration.NewProjector(js, projection, projection).WithOTel(prov, "acme")
			Expect(projector.Start(ctx)).To(Succeed())
			DeferCleanup(projector.Stop)

			// The event decodes, but its empty organizationID makes the
			// hydration filter an invalid subject (an empty token), so the
			// server refuses the ordered consumer — a real hydrate failure,
			// no seam needed.
			const id = "01J0TRANSPORTER0000000205"
			_, err := js.Publish(ctx, profiledomain.Subject("acme", id, profiledomain.CreatedEvent),
				[]byte(`{"context":"acme","organizationID":""}`))
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() bool { _, ok := firstErrorSpan(); return ok }).Should(BeTrue())
			failed, _ := firstErrorSpan()
			Expect(recordedAnError(failed)).To(BeTrue(), "the hydrate error is recorded on the span")
			Expect(failed.Status.Description).To(ContainSubstring("hydrate transporter profile"))
			Expect(projection.written()).To(BeZero())
		})

		It("marks the failed attempt Error when a certificate write fails", func() {
			_, js := testJetStream()
			Expect(orchestration.EnsureStream(ctx, js)).To(Succeed())
			const id = "01J0TRANSPORTER0000000206"
			store := orchestration.NewJetStreamEventStore(js)
			_, err := store.Append(ctx, "acme", id, profiledomain.NewCreatedEvent("acme", id), 0)
			Expect(err).NotTo(HaveOccurred())
			registered := profiledomain.Event{
				Type: profiledomain.DocumentRegisteredEvent, Context: "acme", OrganizationID: id,
				Certificate: &profiledomain.Certificate{ID: "01J0CERTIFICATE00000000206"},
			}
			_, err = store.Append(ctx, "acme", id, registered, 1)
			Expect(err).NotTo(HaveOccurred())

			// Both events are on the stream before the projector starts, so
			// every delivery hydrates a state that carries the certificate;
			// whichever delivery meets the one failing write is the Error span.
			projection := &flakyProjection{}
			certificates := &flakyCertificates{failures: 1}
			projector := orchestration.NewProjector(js, projection, projection).
				WithCertificateWriter(certificates).WithOTel(prov, "acme")
			Expect(projector.Start(ctx)).To(Succeed())
			DeferCleanup(projector.Stop)

			Eventually(certificates.written).Should(Equal(2))
			Eventually(statuses).Should(ConsistOf(codes.Error, codes.Unset, codes.Unset))
			failed, _ := firstErrorSpan()
			Expect(recordedAnError(failed)).To(BeTrue(), "the certificate error is recorded on the span")
			Expect(failed.Status.Description).To(ContainSubstring("upsert certificate"))
		})

		It("leaves the span Unset when an audit-only or unknown event is intentionally skipped", func() {
			_, js := testJetStream()
			Expect(orchestration.EnsureStream(ctx, js)).To(Succeed())
			projection := &flakyProjection{}
			projector := orchestration.NewProjector(js, projection, projection).WithOTel(prov, "acme")
			Expect(projector.Start(ctx)).To(Succeed())
			DeferCleanup(projector.Stop)

			const id = "01J0TRANSPORTER0000000204"
			_, err := js.Publish(ctx, profiledomain.Subject("acme", id, "details-updated"), []byte("not json either"))
			Expect(err).NotTo(HaveOccurred())

			Eventually(statuses).Should(Equal([]codes.Code{codes.Unset}))
			Consistently(statuses, 200*time.Millisecond).Should(HaveLen(1), "a skipped event is acknowledged, not redelivered")
			Expect(projection.written()).To(BeZero())
		})
	})
})
