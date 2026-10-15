// Package tracequery is the trace query gateway (Phase 54, decision 9).
//
// Clients ask for traces over NATS on three api._platform.* subjects and
// never reach Tempo themselves. The reply is the lab's own versioned schema,
// never Tempo's passed through (BR-070), so a later swap to another trace
// store is a change to the Backend adapter only. The adapter is mounted on
// the PLATFORM connection only, and only MintAdminToken grants the subjects
// (BR-067).
//
// STATUS: trace.get implemented (54a-5). search and services are served,
// so the subject set is fixed, but they answer 501 until their query shape
// is specified in 54c.
package tracequery

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/micro"
)

// The three query subjects (decision 9).
const (
	SearchSubject   = "api._platform.observability.trace.search.v1"
	GetSubject      = "api._platform.observability.trace.get.v1"
	ServicesSubject = "api._platform.observability.trace.services.v1"
)

// SchemaVersion names the reply schema. A breaking change gets a new one.
const SchemaVersion = "lab.trace.v1"

// ErrNotFound means the backend has no trace with that ID.
var ErrNotFound = errors.New("tracequery: trace not found")

// Subjects lists every subject the adapter serves. It reads the same table
// Mount registers, so the two cannot drift.
func Subjects() []string {
	var out []string
	for _, ep := range (&adapter{}).endpoints() {
		out = append(out, ep.subject)
	}
	return out
}

// QueryTimeout bounds one backend call, whatever the caller's timeout.
const QueryTimeout = 5 * time.Second

// Trace is one trace in the lab schema.
type Trace struct {
	Schema  string `json:"schema"`
	TraceID string `json:"traceId"`
	Spans   []Span `json:"spans"`
}

// Span is one span in the lab schema. Resource, attributes, events and links
// are kept (decision 9).
type Span struct {
	TraceID       string         `json:"traceId"`
	SpanID        string         `json:"spanId"`
	ParentSpanID  string         `json:"parentSpanId,omitempty"`
	Name          string         `json:"name"`
	Kind          string         `json:"kind"`
	Service       string         `json:"service"`
	StartUnixNano int64          `json:"startUnixNano"`
	EndUnixNano   int64          `json:"endUnixNano"`
	StatusCode    string         `json:"statusCode"`
	StatusMessage string         `json:"statusMessage,omitempty"`
	Resource      map[string]any `json:"resource"`
	Attributes    map[string]any `json:"attributes"`
	Events        []Event        `json:"events"`
	Links         []Link         `json:"links"`
}

// Event is a span event, for example rpc.request.body (decision 7).
type Event struct {
	Name         string         `json:"name"`
	TimeUnixNano int64          `json:"timeUnixNano"`
	Attributes   map[string]any `json:"attributes"`
}

// Link is a span link, for example a browser's sent traceparent (BR-071).
type Link struct {
	TraceID    string         `json:"traceId"`
	SpanID     string         `json:"spanId"`
	Attributes map[string]any `json:"attributes,omitempty"`
}

// Backend is the trace store behind the gateway.
type Backend interface {
	GetTrace(ctx context.Context, traceID string) (Trace, error)
}

// GetRequest is the trace.get.v1 request body.
type GetRequest struct {
	TraceID string `json:"traceId"`
}

var traceIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

// Mount serves the query subjects on nc. nc must be the PLATFORM
// connection (BR-067). Call stop on shutdown.
func Mount(nc *nats.Conn, backend Backend) (stop func() error, err error) {
	svc, err := micro.AddService(nc, micro.Config{
		Name:        "observability-service",
		Version:     "1.0.0",
		Description: "observability-service trace query gateway (Phase 54, decision 9)",
	})
	if err != nil {
		return nil, err
	}
	a := &adapter{backend: backend}
	for _, ep := range a.endpoints() {
		if err := svc.AddEndpoint(ep.name, ep.handler, micro.WithEndpointSubject(ep.subject)); err != nil {
			_ = svc.Stop()
			return nil, err
		}
	}
	return svc.Stop, nil
}

type adapter struct {
	backend Backend
}

type endpoint struct {
	name    string
	subject string
	handler micro.HandlerFunc
}

func (a *adapter) endpoints() []endpoint {
	return []endpoint{
		{"trace-search", SearchSubject, notImplemented},
		{"trace-get", GetSubject, a.handleGet},
		{"trace-services", ServicesSubject, notImplemented},
	}
}

func (a *adapter) handleGet(req micro.Request) {
	var in GetRequest
	if err := json.Unmarshal(req.Data(), &in); err != nil {
		_ = req.Error("400", "request body is not JSON", nil)
		return
	}
	// The ID goes into a Tempo URL path, so only a real trace ID passes.
	if !traceIDPattern.MatchString(in.TraceID) {
		_ = req.Error("400", "traceId must be 32 lowercase hex characters", nil)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), QueryTimeout)
	defer cancel()
	trace, err := a.backend.GetTrace(ctx, in.TraceID)
	switch {
	case errors.Is(err, ErrNotFound):
		_ = req.Error("404", "trace not found", nil)
	case err != nil:
		// The backend's own error text is not passed on (BR-070).
		_ = req.Error("503", "trace store unavailable", nil)
	default:
		_ = req.RespondJSON(trace)
	}
}

func notImplemented(req micro.Request) {
	_ = req.Error("501", "not implemented until Phase 54c", nil)
}

// Tempo is the Backend for Grafana Tempo's HTTP API.
type Tempo struct {
	baseURL string
	client  *http.Client
}

// NewTempo returns a Tempo backend at baseURL, for example
// "http://tempo:3200".
func NewTempo(baseURL string, client *http.Client) *Tempo {
	return &Tempo{baseURL: baseURL, client: client}
}
