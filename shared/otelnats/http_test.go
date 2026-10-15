package otelnats_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/jthomas78/nats-tech-lab/shared/otelnats"
)

// serveHTTP sends one request through p's HTTP middleware. The handler
// publishes once with the request context, the way a REST handler that
// calls natsnotify does, then writes status.
func serveHTTP(p *otelnats.Provider, r *http.Request, status int) (gotBody string) {
	h := p.HTTPMiddleware("PLATFORM", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		_, producer := p.StartProducer(r.Context(), "notify.accounts.account.created")
		producer.End()
		w.WriteHeader(status)
	}))
	h.ServeHTTP(httptest.NewRecorder(), r)
	return gotBody
}

func restRequest(traceparent string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/api/accounts", strings.NewReader(`{"name":"acme"}`))
	if traceparent != "" {
		r.Header.Set("Traceparent", traceparent)
	}
	return r
}

var _ = Describe("BR-073: an inbound REST request is one SERVER span", func() {
	Context("BR-073: the SERVER span", func() {
		It("is one SERVER span with the receiving account and no originating tenant", func() {
			exp := tracetest.NewInMemoryExporter()
			p := newProvider("accounts-service", exp)

			serveHTTP(p, restRequest(""), http.StatusCreated)

			server := byKind(flushed(exp, p), trace.SpanKindServer)
			Expect(server.Name).To(Equal("POST /api/accounts"))
			Expect(serviceName(server)).To(Equal("accounts-service"))
			acct, ok := attr(server, string(otelnats.AttrReceivingAccount))
			Expect(ok).To(BeTrue())
			Expect(acct).To(Equal("PLATFORM"))
			_, ok = attr(server, string(otelnats.AttrOriginatingTenant))
			Expect(ok).To(BeFalse(), "a REST caller is never trusted to name a tenant (BR-069)")
		})

		It("makes the handler's publish a PRODUCER child in the same trace", func() {
			exp := tracetest.NewInMemoryExporter()
			p := newProvider("accounts-service", exp)

			serveHTTP(p, restRequest(""), http.StatusCreated)

			spans := flushed(exp, p)
			server := byKind(spans, trace.SpanKindServer)
			producer := byKind(spans, trace.SpanKindProducer)
			Expect(producer.SpanContext.TraceID()).To(Equal(server.SpanContext.TraceID()))
			Expect(producer.Parent.SpanID()).To(Equal(server.SpanContext.SpanID()))
		})

		It("sets Error status on a 5xx reply", func() {
			exp := tracetest.NewInMemoryExporter()
			p := newProvider("accounts-service", exp)

			serveHTTP(p, restRequest(""), http.StatusInternalServerError)

			server := byKind(flushed(exp, p), trace.SpanKindServer)
			Expect(server.Status.Code).To(Equal(codes.Error))
		})
	})

	Context("BR-073: edge policy, as BR-071's EdgeAPI", func() {
		DescribeTable("starts a new, sampled trace with one link to what the caller sent",
			func(flags string) {
				exp := tracetest.NewInMemoryExporter()
				p := newProvider("accounts-service", exp)

				serveHTTP(p, restRequest(sentTraceparent(flags)), http.StatusCreated)

				server := byKind(flushed(exp, p), trace.SpanKindServer)
				Expect(server.SpanContext.IsSampled()).To(BeTrue(), "the caller cannot turn sampling off")
				Expect(server.SpanContext.TraceID().String()).NotTo(Equal(sentTraceID))
				Expect(server.Parent.IsValid()).To(BeFalse(), "a new root has no parent")
				Expect(server.Links).To(HaveLen(1))
				Expect(server.Links[0].SpanContext.TraceID().String()).To(Equal(sentTraceID))
				Expect(server.Links[0].SpanContext.SpanID().String()).To(Equal(sentSpanID))
			},
			Entry("sampled", "01"),
			Entry("not sampled", "00"),
		)

		DescribeTable("starts a sampled trace with no link when there is no valid traceparent",
			func(traceparent string) {
				exp := tracetest.NewInMemoryExporter()
				p := newProvider("accounts-service", exp)

				serveHTTP(p, restRequest(traceparent), http.StatusCreated)

				server := byKind(flushed(exp, p), trace.SpanKindServer)
				Expect(server.SpanContext.IsSampled()).To(BeTrue())
				Expect(server.Parent.IsValid()).To(BeFalse())
				Expect(server.Links).To(BeEmpty())
			},
			Entry("none sent", ""),
			Entry("malformed", "not-a-traceparent"),
		)
	})

	Context("BR-073: body capture keeps BR-036's redaction", func() {
		It("records the request body redacted, drops the Authorization value and leaves the body whole for the handler", func() {
			exp := tracetest.NewInMemoryExporter()
			p := captureProvider("accounts-service", exp)

			r := httptest.NewRequest(http.MethodPost, "/api/accounts",
				strings.NewReader(`{"name":"acme","password":"hunter2"}`))
			r.SetBasicAuth("accounts", "s3cret-value")
			got := serveHTTP(p, r, http.StatusCreated)

			Expect(got).To(Equal(`{"name":"acme","password":"hunter2"}`), "the handler still reads the full body")
			e := event(byKind(flushed(exp, p), trace.SpanKindServer), otelnats.EventRequestBody)
			body, _ := eventAttr(e, otelnats.AttrBody)
			Expect(body).To(ContainSubstring("acme"))
			Expect(body).NotTo(ContainSubstring("hunter2"))
			hdrs, _ := eventAttr(e, otelnats.AttrBodyHeaders)
			Expect(hdrs).NotTo(ContainSubstring("Basic "))
		})

		It("records no body event when capture is off", func() {
			exp := tracetest.NewInMemoryExporter()
			p := newProvider("accounts-service", exp)

			serveHTTP(p, restRequest(""), http.StatusCreated)

			server := byKind(flushed(exp, p), trace.SpanKindServer)
			for _, e := range server.Events {
				Expect(e.Name).NotTo(Equal(otelnats.EventRequestBody))
			}
		})
	})
})
