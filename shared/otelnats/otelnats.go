// Package otelnats is the lab's thin glue between the OpenTelemetry Go SDK
// and NATS (Phase 54, decision 14). It replaces shared/natstrace on the OTel
// path.
//
// It owns four jobs and nothing else:
//
//   - SDK lifecycle: one Init and one Shutdown, with explicit export and
//     shutdown timeouts so a dead Collector never holds a service up (BR-066).
//   - NATS propagation: standard W3C inject/extract through nats.Header
//     (BR-064).
//   - Span policy at the two kinds of edge: a browser api.* call starts a new
//     root and keeps any sent traceparent as a link only (BR-071); a trusted
//     rpc.* call continues its parent.
//   - Tenant attribution: the attribute names, and the rule that they come
//     from the Trust value the receiving adapter supplies, never from a
//     header or the body (BR-069).
//
// It is not a tracing framework. Callers get ordinary OTel spans and a
// plain context.Context; domain code never imports this package.
//
// STATUS: skeleton. The exported shape is fixed so the BR-064..071 specs
// compile; the behaviour is not written yet, and the specs are red.
package otelnats

import (
	"context"
	"time"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

// Config is what a service supplies. Everything else is this package's.
type Config struct {
	// ServiceName becomes the resource's service.name (BR-065).
	ServiceName string
	// Endpoint is the Collector's OTLP/HTTP base URL, for example
	// "http://otel-collector:4318". Ignored when Exporter is set.
	Endpoint string
	// ExportTimeout bounds one export attempt. Zero means the package default.
	ExportTimeout time.Duration
	// ShutdownTimeout bounds Shutdown, whatever context it is given. Zero
	// means the package default.
	ShutdownTimeout time.Duration
	// Exporter replaces the OTLP exporter. Specs pass an in-memory one.
	Exporter sdktrace.SpanExporter
}

// Provider is one service's tracer provider.
type Provider struct {
	cfg Config
}

// Init builds the provider for one service.
func Init(_ context.Context, cfg Config) (*Provider, error) {
	return &Provider{cfg: cfg}, nil
}

// Tracer returns the service's tracer.
func (p *Provider) Tracer() trace.Tracer {
	return noop.NewTracerProvider().Tracer("")
}

// ForceFlush exports every ended span now. Specs call it before reading an
// in-memory exporter.
func (p *Provider) ForceFlush(_ context.Context) error {
	return nil
}

// Shutdown flushes and stops the provider within Config.ShutdownTimeout.
func (p *Provider) Shutdown(_ context.Context) error {
	return nil
}
