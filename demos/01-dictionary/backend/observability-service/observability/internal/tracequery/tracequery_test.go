package tracequery_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/observability-service/observability/internal/tracequery"
)

const fixtureTraceID = "0af7651916cd43dd8448eb211c80319c"

// fakeTempo serves testdata/tempo-v2-trace.json for fixtureTraceID on
// Tempo's v2 trace-by-ID route, and 404 for anything else.
//
// The fixture is a real reply, captured 2026-10-15 from the pinned Tempo
// (grafana/tempo 3.1.0) in compose.otel.yaml. Re-capture it when the pin
// moves. Note what Tempo really sends: trace and span IDs as base64, not
// hex, and a top-level "metrics" key — the lab schema has hex IDs and
// neither wrapper.
func fakeTempo() *httptest.Server {
	body, err := os.ReadFile("testdata/tempo-v2-trace.json")
	Expect(err).NotTo(HaveOccurred())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v2/traces/"+fixtureTraceID {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(body)
			return
		}
		http.NotFound(w, r)
	}))
	DeferCleanup(srv.Close)
	return srv
}

func startNATS() *nats.Conn {
	srv, err := server.NewServer(&server.Options{Port: -1})
	Expect(err).NotTo(HaveOccurred())
	srv.Start()
	Expect(srv.ReadyForConnections(10 * time.Second)).To(BeTrue())
	nc, err := nats.Connect(srv.ClientURL(), nats.Name("tracequery-spec"))
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(func() { nc.Close(); srv.Shutdown() })
	return nc
}

// keysAnywhere collects every object key in a decoded JSON value.
func keysAnywhere(v any, into map[string]bool) {
	switch t := v.(type) {
	case map[string]any:
		for k, child := range t {
			into[k] = true
			keysAnywhere(child, into)
		}
	case []any:
		for _, child := range t {
			keysAnywhere(child, into)
		}
	}
}

var _ = Describe("BR-070: a trace reply is the lab's own schema", func() {
	Context("BR-070: mapping a Tempo trace", func() {
		var trace tracequery.Trace

		BeforeEach(func() {
			tempo := tracequery.NewTempo(fakeTempo().URL, http.DefaultClient)
			var err error
			trace, err = tempo.GetTrace(context.Background(), fixtureTraceID)
			Expect(err).NotTo(HaveOccurred())
		})

		It("names the lab schema version and the trace", func() {
			Expect(trace.Schema).To(Equal(tracequery.SchemaVersion))
			Expect(trace.TraceID).To(Equal(fixtureTraceID))
			Expect(trace.Spans).To(HaveLen(2))
		})

		It("keeps service, kind, parent, timing and status per span", func() {
			byID := map[string]tracequery.Span{}
			for _, s := range trace.Spans {
				byID[s.SpanID] = s
			}
			server := byID["b7ad6b7169203331"]
			Expect(server.Service).To(Equal("organizations-service"))
			Expect(server.Kind).To(Equal("SERVER"))
			Expect(server.ParentSpanID).To(BeEmpty())
			Expect(server.StartUnixNano).To(Equal(int64(1792064400000000000)))
			Expect(server.EndUnixNano).To(Equal(int64(1792064400012000000)))

			client := byID["c8be7c8270314442"]
			Expect(client.Kind).To(Equal("CLIENT"))
			Expect(client.ParentSpanID).To(Equal("b7ad6b7169203331"))
			Expect(client.StatusCode).To(Equal("ERROR"))
			Expect(client.StatusMessage).To(Equal("NO_SUCH_VEHICLE_TYPE"))
		})

		It("keeps resource, attributes, events and links", func() {
			var server tracequery.Span
			for _, s := range trace.Spans {
				if s.SpanID == "b7ad6b7169203331" {
					server = s
				}
			}
			Expect(server.Resource).To(HaveKeyWithValue("service.name", "organizations-service"))
			Expect(server.Attributes).To(HaveKeyWithValue("lab.tenant.receiving_account", "ACME"))
			Expect(server.Events).To(HaveLen(1))
			Expect(server.Events[0].Name).To(Equal("rpc.request.body"))
			Expect(server.Events[0].Attributes).To(HaveKeyWithValue("redacted", false))
			Expect(server.Links).To(HaveLen(1))
			Expect(server.Links[0].TraceID).To(Equal("4bf92f3577b34da6a3ce929d0e0e4736"))
			Expect(server.Links[0].SpanID).To(Equal("00f067aa0ba902b7"))
		})

		It("carries none of Tempo's own keys", func() {
			raw, err := json.Marshal(trace)
			Expect(err).NotTo(HaveOccurred())
			var decoded any
			Expect(json.Unmarshal(raw, &decoded)).To(Succeed())
			keys := map[string]bool{}
			keysAnywhere(decoded, keys)
			for _, tempoKey := range []string{"trace", "metrics", "batches", "resourceSpans", "scopeSpans", "instrumentationLibrarySpans", "stringValue"} {
				Expect(keys).NotTo(HaveKey(tempoKey))
			}
		})
	})

	It("BR-070: reports an unknown trace as ErrNotFound", func() {
		tempo := tracequery.NewTempo(fakeTempo().URL, http.DefaultClient)
		_, err := tempo.GetTrace(context.Background(), "ffffffffffffffffffffffffffffffff")
		Expect(errors.Is(err, tracequery.ErrNotFound)).To(BeTrue())
	})
})

var _ = Describe("BR-067: the gateway serves exactly the three trace subjects", func() {
	It("BR-067: lists exactly search, get and services", func() {
		Expect(tracequery.Subjects()).To(ConsistOf(
			"api._platform.observability.trace.search.v1",
			"api._platform.observability.trace.get.v1",
			"api._platform.observability.trace.services.v1",
		))
	})

	It("BR-067: answers trace.get on the connection it is mounted on, in the lab schema", func() {
		nc := startNATS()
		stop, err := tracequery.Mount(nc, tracequery.NewTempo(fakeTempo().URL, http.DefaultClient))
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(stop)

		reply, err := nc.Request(tracequery.GetSubject, []byte(`{"traceId":"`+fixtureTraceID+`"}`), 5*time.Second)
		Expect(err).NotTo(HaveOccurred())
		Expect(reply.Header.Get("Nats-Service-Error")).To(BeEmpty())
		var got tracequery.Trace
		Expect(json.Unmarshal(reply.Data, &got)).To(Succeed())
		Expect(got.Schema).To(Equal(tracequery.SchemaVersion))
		Expect(got.Spans).To(HaveLen(2))
	})

	DescribeTable("BR-067: replies with a micro service error, never a Tempo body, when trace.get cannot answer",
		func(body, code string) {
			nc := startNATS()
			stop, err := tracequery.Mount(nc, tracequery.NewTempo(fakeTempo().URL, http.DefaultClient))
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(stop)

			reply, err := nc.Request(tracequery.GetSubject, []byte(body), 5*time.Second)
			Expect(err).NotTo(HaveOccurred())
			Expect(reply.Header.Get("Nats-Service-Error-Code")).To(Equal(code))
			Expect(reply.Header.Get("Nats-Service-Error")).NotTo(BeEmpty())
		},
		Entry("an unknown trace ID", `{"traceId":"ffffffffffffffffffffffffffffffff"}`, "404"),
		Entry("a trace ID that is not 32 hex characters", `{"traceId":"../../api/search"}`, "400"),
		Entry("a body that is not JSON", `not json`, "400"),
	)

	It("BR-067: does not answer a fourth subject under the same prefix", func() {
		nc := startNATS()
		stop, err := tracequery.Mount(nc, tracequery.NewTempo(fakeTempo().URL, http.DefaultClient))
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(stop)

		_, err = nc.Request("api._platform.observability.trace.delete.v1", []byte(`{}`), 500*time.Millisecond)
		Expect(err).To(HaveOccurred())
		Expect(strings.Contains(err.Error(), "no responders")).To(BeTrue(), "got %v", err)
	})
})
