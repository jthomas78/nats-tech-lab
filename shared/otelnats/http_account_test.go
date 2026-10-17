package otelnats_test

import (
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/jthomas78/nats-tech-lab/shared/otelnats"
)

// Phase 54d: shipping-service's REST routes move onto OTel. Its active
// tenant can change between requests (SwitchTenant), so the receiving
// account is read per request. A REST caller's Nats-Requestor is kept as
// lab.requester, display only (BR-041).
var _ = Describe("BR-073: a REST SERVER span whose account is read per request", func() {
	Context("BR-073: HTTPMiddlewareFor", func() {
		It("records the account the function returns for this request", func() {
			exp := tracetest.NewInMemoryExporter()
			p := newProvider("shipping-service", exp)

			account := "acme"
			h := p.HTTPMiddlewareFor(func(*http.Request) string { return account }, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))
			h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/tenant", nil))

			server := byKind(flushed(exp, p), trace.SpanKindServer)
			got, ok := attr(server, string(otelnats.AttrReceivingAccount))
			Expect(ok).To(BeTrue())
			Expect(got).To(Equal("acme"))
		})
	})

	Context("BR-041: a REST caller's requester", func() {
		It("copies the Nats-Requestor header onto the span as lab.requester", func() {
			exp := tracetest.NewInMemoryExporter()
			p := newProvider("shipping-service", exp)

			h := p.HTTPMiddleware("acme", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))
			r := httptest.NewRequest(http.MethodGet, "/api/tenant", nil)
			r.Header.Set(otelnats.RequestorHeader, "admin-app/abc123")
			h.ServeHTTP(httptest.NewRecorder(), r)

			server := byKind(flushed(exp, p), trace.SpanKindServer)
			got, ok := attr(server, string(otelnats.AttrRequester))
			Expect(ok).To(BeTrue())
			Expect(got).To(Equal("admin-app/abc123"))
			_, ok = attr(server, string(otelnats.AttrOriginatingTenant))
			Expect(ok).To(BeFalse())
		})
	})
})
