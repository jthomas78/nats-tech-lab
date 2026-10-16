package tracequery

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"sort"
	"time"

	"github.com/nats-io/nats.go/micro"
)

// Schema versions of the search and services replies. A breaking change
// gets a new one.
const (
	SearchSchemaVersion   = "lab.tracesearch.v1"
	ServicesSchemaVersion = "lab.traceservices.v1"
)

// Search bounds (BR-074). A request outside them is refused, not clamped,
// so a caller never mistakes a smaller answer for the one it asked for.
const (
	DefaultSearchLimit  = 50
	MaxSearchLimit      = 200
	DefaultSearchWindow = 15 * time.Minute
	MaxSearchWindow     = 24 * time.Hour
	// ServicesWindow is how far back trace.services looks for names.
	ServicesWindow = time.Hour
	// MaxFilterLength bounds one filter value.
	MaxFilterLength = 256
)

// SearchRequest is the trace.search.v1 request body. Every field is
// optional; an empty body is the same as {}.
type SearchRequest struct {
	// Service matches a span's resource service.name.
	Service string `json:"service,omitempty"`
	// Name matches a span name, which is the subject for a NATS span.
	Name string `json:"name,omitempty"`
	// Tenant matches lab.tenant.originating (BR-069).
	Tenant     string `json:"tenant,omitempty"`
	ErrorsOnly bool   `json:"errorsOnly,omitempty"`
	SinceMs    int64  `json:"sinceMs,omitempty"`
	Limit      int    `json:"limit,omitempty"`
}

// SearchQuery is a checked search, as the Backend receives it. Each filter
// is matched on its own span: a trace matches when some span matches each.
type SearchQuery struct {
	Service, Name, Tenant string
	ErrorsOnly            bool
	Start, End            time.Time
	Limit                 int
}

// TraceSummary is one trace in a search reply.
type TraceSummary struct {
	TraceID string `json:"traceId"`
	// RootService and RootName are empty while the root span has not
	// reached the store.
	RootService   string   `json:"rootService"`
	RootName      string   `json:"rootName"`
	StartUnixNano int64    `json:"startUnixNano"`
	DurationMs    int64    `json:"durationMs"`
	SpanCount     int      `json:"spanCount"`
	ErrorCount    int      `json:"errorCount"`
	Services      []string `json:"services"`
}

// SearchResult is the trace.search.v1 reply. It has no total: Limited
// says only that more traces may exist (decision 15).
type SearchResult struct {
	Schema              string         `json:"schema"`
	Limit               int            `json:"limit"`
	Limited             bool           `json:"limited"`
	WindowStartUnixNano int64          `json:"windowStartUnixNano"`
	WindowEndUnixNano   int64          `json:"windowEndUnixNano"`
	Traces              []TraceSummary `json:"traces"`
}

// ServicesResult is the trace.services.v1 reply.
type ServicesResult struct {
	Schema   string   `json:"schema"`
	Services []string `json:"services"`
}

// errBadRequest carries a fixed reason for a 400.
type errBadRequest string

func (e errBadRequest) Error() string { return string(e) }

// filterValue is what a filter may hold. It has no quote and no backslash,
// so a value cannot end its TraceQL string and add a query of its own.
var filterValue = regexp.MustCompile(`^[-A-Za-z0-9._:/ ]*$`)

// checkSearch turns a request body into a SearchQuery, or refuses it.
func checkSearch(body []byte, now time.Time) (SearchQuery, error) {
	var in SearchRequest
	if len(bytes.TrimSpace(body)) > 0 {
		dec := json.NewDecoder(bytes.NewReader(body))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&in); err != nil {
			return SearchQuery{}, errBadRequest("request body is not a search request")
		}
	}
	for _, v := range []string{in.Service, in.Name, in.Tenant} {
		if len(v) > MaxFilterLength {
			return SearchQuery{}, errBadRequest("filter value is too long")
		}
		if !filterValue.MatchString(v) {
			return SearchQuery{}, errBadRequest("filter value has a character that is not allowed")
		}
	}
	if in.Limit < 0 || in.Limit > MaxSearchLimit {
		return SearchQuery{}, errBadRequest("limit is out of range")
	}
	if in.SinceMs < 0 || time.Duration(in.SinceMs)*time.Millisecond > MaxSearchWindow {
		return SearchQuery{}, errBadRequest("sinceMs is out of range")
	}
	q := SearchQuery{
		Service: in.Service, Name: in.Name, Tenant: in.Tenant,
		ErrorsOnly: in.ErrorsOnly,
		End:        now,
		Limit:      in.Limit,
	}
	if q.Limit == 0 {
		q.Limit = DefaultSearchLimit
	}
	window := DefaultSearchWindow
	if in.SinceMs > 0 {
		window = time.Duration(in.SinceMs) * time.Millisecond
	}
	q.Start = now.Add(-window)
	return q, nil
}

// shapeSearch orders the backend's traces newest first and keeps the limit.
func shapeSearch(q SearchQuery, traces []TraceSummary) SearchResult {
	sort.SliceStable(traces, func(i, j int) bool {
		return traces[i].StartUnixNano > traces[j].StartUnixNano
	})
	limited := len(traces) >= q.Limit
	if len(traces) > q.Limit {
		traces = traces[:q.Limit]
	}
	if traces == nil {
		traces = []TraceSummary{}
	}
	return SearchResult{
		Schema:              SearchSchemaVersion,
		Limit:               q.Limit,
		Limited:             limited,
		WindowStartUnixNano: q.Start.UnixNano(),
		WindowEndUnixNano:   q.End.UnixNano(),
		Traces:              traces,
	}
}

func (a *adapter) handleSearch(req micro.Request) {
	q, err := checkSearch(req.Data(), time.Now())
	var bad errBadRequest
	if errors.As(err, &bad) {
		_ = req.Error("400", string(bad), nil)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), QueryTimeout)
	defer cancel()
	traces, err := a.backend.Search(ctx, q)
	if err != nil {
		_ = req.Error("503", "trace store unavailable", nil)
		return
	}
	_ = req.RespondJSON(shapeSearch(q, traces))
}

func (a *adapter) handleServices(req micro.Request) {
	if body := bytes.TrimSpace(req.Data()); len(body) > 0 && !bytes.Equal(body, []byte("{}")) {
		_ = req.Error("400", "request body must be empty or {}", nil)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), QueryTimeout)
	defer cancel()
	now := time.Now()
	names, err := a.backend.Services(ctx, now.Add(-ServicesWindow), now)
	if err != nil {
		_ = req.Error("503", "trace store unavailable", nil)
		return
	}
	sort.Strings(names)
	if names == nil {
		names = []string{}
	}
	_ = req.RespondJSON(ServicesResult{Schema: ServicesSchemaVersion, Services: names})
}
