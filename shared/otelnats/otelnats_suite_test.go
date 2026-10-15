package otelnats_test

import (
	"context"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/jthomas78/nats-tech-lab/shared/otelnats"
)

func TestOtelnats(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "otelnats Suite (Phase 54 — shared/otelnats)")
}

// startNATS runs an embedded NATS server for one spec and returns a named
// connection to it. shared/natstest takes a *testing.T, which a Ginkgo spec
// does not have, so this is the same shape written for Ginkgo.
func startNATS(name string) *nats.Conn {
	srv, err := server.NewServer(&server.Options{Port: -1})
	Expect(err).NotTo(HaveOccurred())
	srv.Start()
	Expect(srv.ReadyForConnections(10 * time.Second)).To(BeTrue())
	nc, err := nats.Connect(srv.ClientURL(), nats.Name(name))
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(func() { nc.Close(); srv.Shutdown() })
	return nc
}

// newProvider builds a provider for one service that exports into exp.
func newProvider(serviceName string, exp sdktrace.SpanExporter) *otelnats.Provider {
	p, err := otelnats.Init(context.Background(), otelnats.Config{
		ServiceName: serviceName,
		Exporter:    exp,
	})
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(func() { _ = p.Shutdown(context.Background()) })
	return p
}

// flushed flushes every provider, then returns what exp holds.
func flushed(exp *tracetest.InMemoryExporter, ps ...*otelnats.Provider) tracetest.SpanStubs {
	for _, p := range ps {
		Expect(p.ForceFlush(context.Background())).To(Succeed())
	}
	return exp.GetSpans()
}

// byKind returns the one span of kind k, failing the spec if there is not
// exactly one.
func byKind(spans tracetest.SpanStubs, k trace.SpanKind) tracetest.SpanStub {
	var found []tracetest.SpanStub
	for _, s := range spans {
		if s.SpanKind == k {
			found = append(found, s)
		}
	}
	ExpectWithOffset(1, found).To(HaveLen(1), "want exactly one %s span", k)
	return found[0]
}

// attr returns the string value of key on s, and whether it is present.
func attr(s tracetest.SpanStub, key string) (string, bool) {
	for _, kv := range s.Attributes {
		if string(kv.Key) == key {
			return kv.Value.Emit(), true
		}
	}
	return "", false
}

// serviceName returns s's resource service.name.
func serviceName(s tracetest.SpanStub) string {
	if s.Resource == nil {
		return ""
	}
	for _, kv := range s.Resource.Attributes() {
		if kv.Key == "service.name" {
			return kv.Value.AsString()
		}
	}
	return ""
}

// Fixed IDs for a traceparent a browser might send.
const (
	sentTraceID = "4bf92f3577b34da6a3ce929d0e0e4736"
	sentSpanID  = "00f067aa0ba902b7"
)

func sentTraceparent(flags string) string {
	return "00-" + sentTraceID + "-" + sentSpanID + "-" + flags
}
