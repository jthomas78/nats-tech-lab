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
// STATUS: implemented (54a-3). The BR-064, 065, 066, 069 and 071 unit specs
// are green. The stack checks in 54a-6 are still to run.
package otelnats

import (
	"context"
	"errors"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// Package defaults for the two timeouts (BR-066).
const (
	DefaultExportTimeout   = 2 * time.Second
	DefaultShutdownTimeout = 5 * time.Second
)

// instrumentationName is the tracer's scope name.
const instrumentationName = "github.com/jthomas78/nats-tech-lab/shared/otelnats"

// tracesPath is the OTLP/HTTP route for traces, added to Config.Endpoint.
const tracesPath = "/v1/traces"

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
	cfg    Config
	tp     *sdktrace.TracerProvider
	tracer trace.Tracer
}

// Init builds the provider for one service. It never sets the OTel globals:
// each service holds its own Provider, so two in one process (the specs, the
// monolith) do not share a service.name.
func Init(ctx context.Context, cfg Config) (*Provider, error) {
	if strings.TrimSpace(cfg.ServiceName) == "" {
		return nil, errors.New("otelnats: ServiceName is required")
	}
	if cfg.Exporter == nil && strings.TrimSpace(cfg.Endpoint) == "" {
		return nil, errors.New("otelnats: an Endpoint or an Exporter is required")
	}
	if cfg.ExportTimeout <= 0 {
		cfg.ExportTimeout = DefaultExportTimeout
	}
	if cfg.ShutdownTimeout <= 0 {
		cfg.ShutdownTimeout = DefaultShutdownTimeout
	}

	exp := cfg.Exporter
	if exp == nil {
		var err error
		exp, err = otlptracehttp.New(ctx,
			otlptracehttp.WithEndpointURL(tracesURL(cfg.Endpoint)),
			otlptracehttp.WithTimeout(cfg.ExportTimeout),
			// No retry in the service. A failed export drops its batch; the
			// Collector's persistent queue is where durability lives.
			otlptracehttp.WithRetry(otlptracehttp.RetryConfig{Enabled: false}),
		)
		if err != nil {
			return nil, err
		}
	}

	tp := sdktrace.NewTracerProvider(
		// The batcher never blocks span.End(): a full queue drops spans.
		sdktrace.WithBatcher(exp, sdktrace.WithExportTimeout(cfg.ExportTimeout)),
		sdktrace.WithResource(resource.NewSchemaless(attribute.String("service.name", cfg.ServiceName))),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.AlwaysSample())),
	)
	return &Provider{cfg: cfg, tp: tp, tracer: tp.Tracer(instrumentationName)}, nil
}

// tracesURL adds the OTLP traces route to a base URL, unless it is there.
func tracesURL(endpoint string) string {
	base := strings.TrimRight(strings.TrimSpace(endpoint), "/")
	if strings.HasSuffix(base, tracesPath) {
		return base
	}
	return base + tracesPath
}

// Tracer returns the service's tracer.
func (p *Provider) Tracer() trace.Tracer {
	return p.tracer
}

// ForceFlush exports every ended span now. Specs call it before reading an
// in-memory exporter.
func (p *Provider) ForceFlush(ctx context.Context) error {
	return p.tp.ForceFlush(ctx)
}

// Shutdown flushes and stops the provider within Config.ShutdownTimeout,
// even when ctx has no deadline (BR-066).
func (p *Provider) Shutdown(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, p.cfg.ShutdownTimeout)
	defer cancel()
	return p.tp.Shutdown(ctx)
}
