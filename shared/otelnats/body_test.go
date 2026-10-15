package otelnats_test

import (
	"context"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/micro"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/jthomas78/nats-tech-lab/shared/otelnats"
)

// captureProvider builds a provider with body capture on.
func captureProvider(serviceName string, exp sdktrace.SpanExporter) *otelnats.Provider {
	p, err := otelnats.Init(context.Background(), otelnats.Config{
		ServiceName:   serviceName,
		Exporter:      exp,
		CaptureBodies: true,
	})
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(func() { _ = p.Shutdown(context.Background()) })
	return p
}

// event returns the one event called name on s.
func event(s tracetest.SpanStub, name string) sdktrace.Event {
	var found []sdktrace.Event
	for _, e := range s.Events {
		if e.Name == name {
			found = append(found, e)
		}
	}
	ExpectWithOffset(1, found).To(HaveLen(1), "want exactly one %q event", name)
	return found[0]
}

// eventAttr returns the value of key on e as a string, and whether it is
// present. A string slice is joined with commas.
func eventAttr(e sdktrace.Event, key string) (string, bool) {
	for _, kv := range e.Attributes {
		if string(kv.Key) == key {
			if s := kv.Value.AsStringSlice(); s != nil {
				return strings.Join(s, ","), true
			}
			return kv.Value.Emit(), true
		}
	}
	return "", false
}

// recordOne records body as a request body on one client span and returns
// that span's request event.
func recordOne(body []byte, h nats.Header) sdktrace.Event {
	exp := tracetest.NewInMemoryExporter()
	p := captureProvider("organizations-service", exp)
	_, span := p.StartClient(context.Background(), "refdata.item.get.v1", "refdata")
	p.RecordRequestBody(span, body, h)
	span.End()
	return event(byKind(flushed(exp, p), trace.SpanKindClient), otelnats.EventRequestBody)
}

var _ = Describe("BR-036: request and reply bodies are span events, redacted then truncated (decision 7)", func() {
	Context("BR-036: capture is opt-in per service", func() {
		It("adds no body events when the provider was built without CaptureBodies", func() {
			exp := tracetest.NewInMemoryExporter()
			p := newProvider("organizations-service", exp)
			_, span := p.StartClient(context.Background(), "refdata.item.get.v1", "refdata")
			p.RecordRequestBody(span, []byte(`{"code":"TAUTLINER"}`), nil)
			p.RecordReplyBody(span, []byte(`{}`), nil)
			span.End()

			Expect(byKind(flushed(exp, p), trace.SpanKindClient).Events).To(BeEmpty())
		})

		It("records the request and the reply of a Middleware-wrapped handler when capture is on", func() {
			exp := tracetest.NewInMemoryExporter()
			p := captureProvider("refdata-service", exp)
			nc := mountMicro(p, "rpc.acme.refdata.item.get.v1", otelnats.EdgeRPC,
				otelnats.Trust{ReceivingAccount: "PLATFORM"},
				func(req micro.Request) { _ = req.Respond([]byte(`{"label":"Tautliner"}`)) })

			_, err := nc.Request("rpc.acme.refdata.item.get.v1", []byte(`{"code":"TAUTLINER"}`), 5*time.Second)
			Expect(err).NotTo(HaveOccurred())

			server := byKind(flushed(exp, p), trace.SpanKindServer)
			req, _ := eventAttr(event(server, otelnats.EventRequestBody), otelnats.AttrBody)
			Expect(req).To(Equal(`{"code":"TAUTLINER"}`))
			rep, _ := eventAttr(event(server, otelnats.EventReplyBody), otelnats.AttrBody)
			Expect(rep).To(Equal(`{"label":"Tautliner"}`))
		})

		It("records the reply body of a micro error reply too", func() {
			exp := tracetest.NewInMemoryExporter()
			p := captureProvider("refdata-service", exp)
			nc := mountMicro(p, "rpc.acme.refdata.item.get.v1", otelnats.EdgeRPC,
				otelnats.Trust{ReceivingAccount: "PLATFORM"},
				func(req micro.Request) { _ = req.Error("404", "dictionary item not found", []byte(`{"error":"not found"}`)) })

			_, err := nc.Request("rpc.acme.refdata.item.get.v1", []byte(`{}`), 5*time.Second)
			Expect(err).NotTo(HaveOccurred())

			server := byKind(flushed(exp, p), trace.SpanKindServer)
			rep, _ := eventAttr(event(server, otelnats.EventReplyBody), otelnats.AttrBody)
			Expect(rep).To(Equal(`{"error":"not found"}`))
		})
	})

	Context("BR-036: redaction, then truncation, before export", func() {
		It("removes a denylisted field at any depth and names its path", func() {
			e := recordOne([]byte(`{"user":{"password":"s3cr3t"},"name":"n"}`), nil)
			body, _ := eventAttr(e, otelnats.AttrBody)
			Expect(body).NotTo(ContainSubstring("s3cr3t"))
			Expect(body).To(ContainSubstring(`"name":"n"`))
			red, ok := eventAttr(e, otelnats.AttrBodyRedacted)
			Expect(ok).To(BeTrue())
			Expect(red).To(Equal("user.password"))
		})

		DescribeTable("uses the shared denylist, matched without regard to case (BR-046)",
			func(key string) {
				e := recordOne([]byte(`{"`+key+`":"hidden-value"}`), nil)
				body, _ := eventAttr(e, otelnats.AttrBody)
				Expect(body).NotTo(ContainSubstring("hidden-value"))
			},
			Entry(nil, "password"), Entry(nil, "Secret"), Entry(nil, "token"),
			Entry(nil, "apiKey"), Entry(nil, "api_key"), Entry(nil, "ssn"),
			Entry(nil, "creditCard"), Entry(nil, "credit_card"), Entry(nil, "Authorization"),
			Entry(nil, "privateKey"), Entry(nil, "private_key"), Entry(nil, "actorName"),
			Entry(nil, "actor_name"), Entry(nil, "actorSourceIP"), Entry(nil, "actor_source_ip"),
		)

		It("truncates a body over 4 KiB, marks it truncated and keeps the true size", func() {
			big := `{"pad":"` + strings.Repeat("x", 5000) + `"}`
			e := recordOne([]byte(big), nil)
			body, _ := eventAttr(e, otelnats.AttrBody)
			Expect(len(body)).To(BeNumerically("<=", otelnats.MaxBodyBytes))
			tr, _ := eventAttr(e, otelnats.AttrBodyTruncated)
			Expect(tr).To(Equal("true"))
			n, _ := eventAttr(e, otelnats.AttrBodyBytes)
			Expect(n).To(Equal("5011"))
		})

		It("redacts before it truncates: a secret that straddles the cap is still removed", func() {
			big := `{"pad":"` + strings.Repeat("x", 4070) + `","password":"s3cr3t-tail"}`
			e := recordOne([]byte(big), nil)
			body, _ := eventAttr(e, otelnats.AttrBody)
			Expect(body).NotTo(ContainSubstring("s3cr3t"))
			red, _ := eventAttr(e, otelnats.AttrBodyRedacted)
			Expect(red).To(Equal("password"))
		})

		It("passes a body that is not JSON through unchanged", func() {
			e := recordOne([]byte("plain text"), nil)
			body, _ := eventAttr(e, otelnats.AttrBody)
			Expect(body).To(Equal("plain text"))
		})

		It("records the headers alongside, with a denylisted header removed and named", func() {
			h := nats.Header{}
			h.Set("Authorization", "Bearer abc")
			h.Set("Nats-Requestor", "acme")
			e := recordOne([]byte(`{}`), h)
			hdrs, ok := eventAttr(e, otelnats.AttrBodyHeaders)
			Expect(ok).To(BeTrue())
			Expect(hdrs).To(ContainSubstring("Nats-Requestor"))
			Expect(hdrs).NotTo(ContainSubstring("Bearer abc"))
			red, _ := eventAttr(e, otelnats.AttrBodyRedacted)
			Expect(red).To(ContainSubstring("headers.Authorization"))
		})
	})
})
