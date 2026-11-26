package otelnats_test

import (
	"net/http"
	"net/http/httptest"

	"github.com/nats-io/nats.go/micro"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.opentelemetry.io/otel/trace"

	"github.com/jthomas78/nats-tech-lab/shared/otelnats"
)

// fakeRequest is the part of micro.Request that Middleware reads. Any other
// method call panics on the nil embedded interface.
type fakeRequest struct{ micro.Request }

func (fakeRequest) Subject() string        { return "api.acme.organizations.fleet-asset.add.v1" }
func (fakeRequest) Headers() micro.Headers { return micro.Headers{} }
func (fakeRequest) Data() []byte           { return []byte(`{}`) }

var _ = Describe("BR-079: with a nil provider a SERVER middleware returns the handler unchanged", func() {
	var nilP *otelnats.Provider

	Context("BR-079: micro Middleware", func() {
		It("runs the handler once with the request it got, so ContextFrom gives no span", func() {
			calls := 0
			var got micro.Request
			h := func(req micro.Request) {
				calls++
				got = req
			}
			var wrapped micro.HandlerFunc
			Expect(func() {
				wrapped = nilP.Middleware(otelnats.EdgeAPI, func(micro.Request) otelnats.Trust { return otelnats.Trust{} }, h)
			}).NotTo(Panic())

			req := fakeRequest{}
			Expect(func() { wrapped(req) }).NotTo(Panic())
			Expect(calls).To(Equal(1))
			Expect(got).To(Equal(micro.Request(req)))
			Expect(trace.SpanContextFromContext(otelnats.ContextFrom(got)).IsValid()).To(BeFalse())
		})
	})

	Context("BR-079: HTTP middleware", func() {
		for _, tc := range []struct {
			name string
			wrap func(http.Handler) http.Handler
		}{
			{"HTTPMiddleware", func(next http.Handler) http.Handler { return nilP.HTTPMiddleware("PLATFORM", next) }},
			{"HTTPMiddlewareFor", func(next http.Handler) http.Handler {
				return nilP.HTTPMiddlewareFor(func(*http.Request) string { return "acme" }, next)
			}},
		} {
			It(tc.name+" runs next once with the request it got and adds no span", func() {
				calls := 0
				var gotCtxSpan trace.SpanContext
				next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls++
					gotCtxSpan = trace.SpanContextFromContext(r.Context())
					w.WriteHeader(http.StatusTeapot)
				})
				var h http.Handler
				Expect(func() { h = tc.wrap(next) }).NotTo(Panic())

				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, restRequest("00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01"))
				Expect(calls).To(Equal(1))
				Expect(rec.Code).To(Equal(http.StatusTeapot))
				Expect(gotCtxSpan.IsValid()).To(BeFalse())
			})
		}
	})
})
