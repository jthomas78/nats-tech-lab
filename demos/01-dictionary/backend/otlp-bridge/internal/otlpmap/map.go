// Package otlpmap is otlp-bridge's field-for-field translation from
// natstrace's wire span (obs.trace.*, BR-036/BR-037) to an OTLP
// ExportTraceServiceRequest, JSON-encoded the way Jaeger's OTLP/HTTP
// receiver (OTel collector's pdata codec) actually expects: trace/span ids
// stay hex (not base64 — see toSpanJSON's doc comment, confirmed live
// against a real Jaeger rejection), uint64 timestamps as strings.
// See ARCHITECTURE-COMMUNICATIONS.md § 6 for why this lives in a separate
// container rather than an in-process SDK exporter: a mapping bug here never
// touches a business path, and nothing is lost while Jaeger is unreachable —
// the span stays on TRACES, unacked, until this succeeds.
package otlpmap

import (
	"encoding/json"
	"sort"
	"strconv"
	"time"
)

// WireSpan is the subset of natstrace.go's traceSpan this package needs —
// the same "just enough to do the job, not a copy of the full shape"
// precedent trace_store.go's traceSpanKey already sets in this codebase.
// Payload/Headers/PayloadBytes/Redacted/Truncated are deliberately omitted:
// Jaeger's job is trace shape and timing, not payload replay, and the Admin
// UI's own detail pane already shows the payload — carrying it into OTLP
// attributes would duplicate BR-036's redaction/truncation surface for no
// reader. Unknown JSON fields (this struct's omissions) are silently ignored
// by encoding/json, so a wire span decodes into this type without error.
type WireSpan struct {
	Direction     string    `json:"direction"`
	CorrelationID string    `json:"correlationId"`
	Subject       string    `json:"subject"`
	Error         string    `json:"error,omitempty"`
	Timestamp     time.Time `json:"timestamp"`

	TraceID       string            `json:"traceId,omitempty"`
	SpanID        string            `json:"spanId,omitempty"`
	ParentSpanID  string            `json:"parentSpanId,omitempty"`
	Service       string            `json:"service,omitempty"`
	Entity        string            `json:"entity,omitempty"`
	Action        string            `json:"action,omitempty"`
	StatusCode    string            `json:"statusCode,omitempty"`
	StatusMessage string            `json:"statusMessage,omitempty"`
	Attributes    map[string]string `json:"attributes,omitempty"`
	DurationUs    int64             `json:"durationUs"`
}

// Span is the intermediate shape ToSpan produces from a WireSpan — plain
// Go values (not yet base64/string-encoded) so map_test.go can assert on
// them directly instead of parsing the final JSON back apart.
type Span struct {
	TraceID       string
	SpanID        string
	ParentSpanID  string
	Service       string
	Name          string
	StartUnixNano int64
	EndUnixNano   int64
	StatusCode    int
	StatusMessage string
	Attributes    map[string]string
}

// ToSpan maps one wire span field-for-field. w.Timestamp is the span's
// finish moment (natstrace.go's traceSpan doc comment: "there is no separate
// wire 'start' timestamp"), so the start time is recovered the same way the
// Admin UI would: Timestamp minus DurationUs.
//
// Microseconds, not milliseconds (BR-056). This subtraction is the reason
// the wire field's resolution is a rule at all: Timestamp is nanosecond
// precise, so a millisecond-truncated duration puts the derived start up to
// 0.999ms too late — always late — and the bridge exported that error
// straight into OTLP, inverting nested spans in whatever backend consumes it
// exactly as it inverted the Admin UI's own waterfall.
//
// spanKind is never set (OTLP's SPAN_KIND_UNSPECIFIED, the zero value) —
// natstrace has no real kind data (BR-035's Phase 28g amendment: direction
// is always "reply"), and inventing one would be interpretation, not
// mapping. Attributes carries the span's own rpc.* attributes (e.g.
// rpc.retry_count) plus a handful of fields OTLP's Span message has no
// dedicated slot for (subject, correlationId, entity, action, direction) so
// they aren't lost — everything else on the wire (payload, headers,
// redaction bookkeeping) is out of scope per WireSpan's own doc comment.
func ToSpan(w WireSpan) Span {
	end := w.Timestamp
	start := end.Add(-time.Duration(w.DurationUs) * time.Microsecond)

	attrs := make(map[string]string, len(w.Attributes)+5)
	for k, v := range w.Attributes {
		attrs[k] = v
	}
	attrs["subject"] = w.Subject
	attrs["correlationId"] = w.CorrelationID
	attrs["entity"] = w.Entity
	attrs["action"] = w.Action
	attrs["direction"] = w.Direction
	if w.Error != "" {
		attrs["error"] = w.Error
	}

	return Span{
		TraceID:       w.TraceID,
		SpanID:        w.SpanID,
		ParentSpanID:  w.ParentSpanID,
		Service:       w.Service,
		Name:          w.Subject,
		StartUnixNano: start.UnixNano(),
		EndUnixNano:   end.UnixNano(),
		StatusCode:    otlpStatusCode(w.StatusCode),
		StatusMessage: w.StatusMessage,
		Attributes:    attrs,
	}
}

// otlpStatusCode maps natstrace's StatusCode ("OK"/"ERROR") to OTLP's
// Status.code enum (STATUS_CODE_UNSET=0, STATUS_CODE_OK=1,
// STATUS_CODE_ERROR=2) — natstrace never emits anything else, but an
// unrecognized value degrades to UNSET rather than a fabricated guess.
func otlpStatusCode(s string) int {
	switch s {
	case "OK":
		return 1
	case "ERROR":
		return 2
	default:
		return 0
	}
}

// exportRequest and friends mirror opentelemetry-proto's
// ExportTraceServiceRequest JSON shape exactly (protobuf JSON mapping:
// bytes fields are base64, uint64 fields are strings) — this is the only
// place in the bridge that needs to know OTLP's wire shape.
type exportRequest struct {
	ResourceSpans []resourceSpansJSON `json:"resourceSpans"`
}

type resourceSpansJSON struct {
	Resource   resourceJSON     `json:"resource"`
	ScopeSpans []scopeSpansJSON `json:"scopeSpans"`
}

type resourceJSON struct {
	Attributes []kvJSON `json:"attributes"`
}

type scopeSpansJSON struct {
	Scope scopeJSON  `json:"scope"`
	Spans []spanJSON `json:"spans"`
}

type scopeJSON struct {
	Name string `json:"name"`
}

type kvJSON struct {
	Key   string       `json:"key"`
	Value anyValueJSON `json:"value"`
}

type anyValueJSON struct {
	StringValue string `json:"stringValue"`
}

type spanJSON struct {
	TraceID           string     `json:"traceId"`
	SpanID            string     `json:"spanId"`
	ParentSpanID      string     `json:"parentSpanId,omitempty"`
	Name              string     `json:"name"`
	StartTimeUnixNano string     `json:"startTimeUnixNano"`
	EndTimeUnixNano   string     `json:"endTimeUnixNano"`
	Attributes        []kvJSON   `json:"attributes,omitempty"`
	Status            statusJSON `json:"status"`
}

type statusJSON struct {
	Code    int    `json:"code"`
	Message string `json:"message,omitempty"`
}

// MarshalExportRequest groups spans into one resourceSpans entry per
// distinct Service (OTLP's "resource" is the process that produced the
// span — natstrace's Service field is exactly that), preserving the order
// services were first seen in spans for deterministic test output.
func MarshalExportRequest(spans []Span) ([]byte, error) {
	var order []string
	bySvc := make(map[string][]Span)
	for _, s := range spans {
		if _, seen := bySvc[s.Service]; !seen {
			order = append(order, s.Service)
		}
		bySvc[s.Service] = append(bySvc[s.Service], s)
	}

	req := exportRequest{ResourceSpans: make([]resourceSpansJSON, 0, len(order))}
	for _, svc := range order {
		req.ResourceSpans = append(req.ResourceSpans, resourceSpansJSON{
			Resource: resourceJSON{Attributes: []kvJSON{{Key: "service.name", Value: anyValueJSON{StringValue: svc}}}},
			ScopeSpans: []scopeSpansJSON{{
				Scope: scopeJSON{Name: "natstrace"},
				Spans: toSpanJSON(bySvc[svc]),
			}},
		})
	}
	return json.Marshal(req)
}

func toSpanJSON(spans []Span) []spanJSON {
	out := make([]spanJSON, len(spans))
	for i, s := range spans {
		out[i] = spanJSON{
			// TraceID/SpanID/ParentSpanID pass through as the same hex
			// natstrace already emits, unconverted. Standard protobuf JSON
			// mapping would base64-encode a `bytes` field, and trace_id/
			// span_id are declared `bytes` in opentelemetry-proto — but
			// Jaeger's OTLP/HTTP receiver decodes JSON through OTel
			// collector's pdata codec, whose TraceID/SpanID types
			// marshal/unmarshal as hex specifically (confirmed live: a
			// base64-encoded id was rejected with "invalid length for ID").
			// natstrace's ids are already the exact byte lengths OTLP
			// requires (32 hex chars/16 bytes, 16 hex chars/8 bytes — see
			// natstrace.go's newTraceID/newSpanID), so no re-encoding step
			// exists to introduce a mismatch.
			TraceID:           s.TraceID,
			SpanID:            s.SpanID,
			Name:              s.Name,
			StartTimeUnixNano: strconv.FormatInt(s.StartUnixNano, 10),
			EndTimeUnixNano:   strconv.FormatInt(s.EndUnixNano, 10),
			Attributes:        toKVJSON(s.Attributes),
			Status:            statusJSON{Code: s.StatusCode, Message: s.StatusMessage},
		}
		if s.ParentSpanID != "" {
			out[i].ParentSpanID = s.ParentSpanID
		}
	}
	return out
}

// toKVJSON sorts by key so MarshalExportRequest's output is deterministic —
// Go's map iteration order is randomized, and a flaky test asserting on raw
// JSON bytes would be worse than the small sort cost here.
func toKVJSON(attrs map[string]string) []kvJSON {
	if len(attrs) == 0 {
		return nil
	}
	keys := make([]string, 0, len(attrs))
	for k := range attrs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]kvJSON, len(keys))
	for i, k := range keys {
		out[i] = kvJSON{Key: k, Value: anyValueJSON{StringValue: attrs[k]}}
	}
	return out
}
