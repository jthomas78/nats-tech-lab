// Package tracequery is the trace query gateway (Phase 54, decision 9).
//
// Clients ask for traces over NATS on three api._platform.* subjects and
// never reach Tempo themselves. The reply is the lab's own versioned schema,
// never Tempo's passed through (BR-070), so a later swap to another trace
// store is a change to the Backend adapter only. The adapter is mounted on
// the PLATFORM connection only, and only MintAdminToken grants the subjects
// (BR-067).
//
// STATUS: skeleton. The exported shape is fixed so the BR-067/BR-070 specs
// compile; the behaviour is not written yet, and the specs are red.
package tracequery

import (
	"context"
	"errors"
	"net/http"

	"github.com/nats-io/nats.go"
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

// Subjects lists every subject the adapter serves.
func Subjects() []string { return nil }

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

// GetTrace fetches one trace and maps it to the lab schema.
func (t *Tempo) GetTrace(_ context.Context, _ string) (Trace, error) {
	return Trace{}, nil
}

// Mount serves the query subjects on nc. nc must be the PLATFORM
// connection (BR-067).
func Mount(_ *nats.Conn, _ Backend) (stop func() error, err error) {
	return func() error { return nil }, nil
}
