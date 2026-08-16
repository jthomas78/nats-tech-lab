package otlpmap_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/otlp-bridge/internal/otlpmap"
)

const (
	traceIDHex = "0123456789abcdef0123456789abcdef" // 32 hex chars = 16 bytes
	spanIDHex  = "0123456789abcdef"                 // 16 hex chars = 8 bytes
)

func TestToSpanRecoversStartTimeFromDurationMs(t *testing.T) {
	end := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	w := otlpmap.WireSpan{
		Subject:    "rpc.acme.refdata.item.get.v1",
		Service:    "refdata",
		StatusCode: "OK",
		Timestamp:  end,
		DurationMs: 250,
		TraceID:    traceIDHex,
		SpanID:     spanIDHex,
	}

	sp := otlpmap.ToSpan(w)

	wantStart := end.Add(-250 * time.Millisecond).UnixNano()
	if sp.StartUnixNano != wantStart {
		t.Fatalf("StartUnixNano = %d, want %d", sp.StartUnixNano, wantStart)
	}
	if sp.EndUnixNano != end.UnixNano() {
		t.Fatalf("EndUnixNano = %d, want %d", sp.EndUnixNano, end.UnixNano())
	}
}

func TestToSpanMapsStatusCode(t *testing.T) {
	cases := map[string]int{"OK": 1, "ERROR": 2, "": 0, "WEIRD": 0}
	for in, want := range cases {
		got := otlpmap.ToSpan(otlpmap.WireSpan{StatusCode: in}).StatusCode
		if got != want {
			t.Errorf("statusCode(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestToSpanNeverFabricatesKind(t *testing.T) {
	// otlpmap.Span has no Kind field at all — BR-035's Phase 28g amendment
	// documents spanKind as omitted, never guessed, and this test pins that
	// by asserting the marshaled JSON has no "kind" key.
	sp := otlpmap.ToSpan(otlpmap.WireSpan{TraceID: traceIDHex, SpanID: spanIDHex, Service: "shipping"})
	data, err := otlpmap.MarshalExportRequest([]otlpmap.Span{sp})
	if err != nil {
		t.Fatalf("MarshalExportRequest: %v", err)
	}
	if bytesContain(data, `"kind"`) {
		t.Fatalf("export request unexpectedly contains a kind field: %s", data)
	}
}

func TestToSpanCarriesRetryCountAndCoreFieldsAsAttributes(t *testing.T) {
	w := otlpmap.WireSpan{
		Subject:       "rpc.acme.refdata.item.get.v1",
		CorrelationID: "corr-1",
		Entity:        "item",
		Action:        "get",
		Direction:     "reply",
		Error:         "not found",
		Attributes:    map[string]string{"rpc.retry_count": "2"},
		TraceID:       traceIDHex,
		SpanID:        spanIDHex,
	}

	attrs := otlpmap.ToSpan(w).Attributes
	want := map[string]string{
		"rpc.retry_count": "2",
		"subject":         "rpc.acme.refdata.item.get.v1",
		"correlationId":   "corr-1",
		"entity":          "item",
		"action":          "get",
		"direction":       "reply",
		"error":           "not found",
	}
	for k, v := range want {
		if attrs[k] != v {
			t.Errorf("attrs[%q] = %q, want %q", k, attrs[k], v)
		}
	}
}

func TestMarshalExportRequestGroupsByService(t *testing.T) {
	spans := []otlpmap.Span{
		{Service: "shipping", TraceID: traceIDHex, SpanID: spanIDHex},
		{Service: "refdata", TraceID: traceIDHex, SpanID: spanIDHex},
		{Service: "shipping", TraceID: traceIDHex, SpanID: spanIDHex},
	}

	data, err := otlpmap.MarshalExportRequest(spans)
	if err != nil {
		t.Fatalf("MarshalExportRequest: %v", err)
	}

	var decoded struct {
		ResourceSpans []struct {
			Resource struct {
				Attributes []struct {
					Key   string `json:"key"`
					Value struct {
						StringValue string `json:"stringValue"`
					} `json:"value"`
				} `json:"attributes"`
			} `json:"resource"`
			ScopeSpans []struct {
				Spans []json.RawMessage `json:"spans"`
			} `json:"scopeSpans"`
		} `json:"resourceSpans"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal export request: %v", err)
	}

	if len(decoded.ResourceSpans) != 2 {
		t.Fatalf("got %d resourceSpans, want 2 (one per distinct service)", len(decoded.ResourceSpans))
	}
	if decoded.ResourceSpans[0].Resource.Attributes[0].Value.StringValue != "shipping" {
		t.Fatalf("first resourceSpans should be shipping (first seen), got %+v", decoded.ResourceSpans[0].Resource)
	}
	if len(decoded.ResourceSpans[0].ScopeSpans[0].Spans) != 2 {
		t.Fatalf("shipping resourceSpans should hold 2 spans, got %d", len(decoded.ResourceSpans[0].ScopeSpans[0].Spans))
	}
}

func TestMarshalExportRequestPassesIdsThroughAsHex(t *testing.T) {
	// Jaeger's OTLP/HTTP receiver decodes trace/span ids as hex (OTel
	// collector's pdata codec), not the base64 generic protobuf JSON would
	// use for a `bytes` field — confirmed live against a real Jaeger 400
	// ("invalid length for ID") before this test was written. natstrace
	// already emits hex, so the fix is "don't touch it," which this test
	// pins against a future re-introduction of an encoding conversion.
	spans := []otlpmap.Span{{Service: "shipping", TraceID: traceIDHex, SpanID: spanIDHex, ParentSpanID: spanIDHex}}

	data, err := otlpmap.MarshalExportRequest(spans)
	if err != nil {
		t.Fatalf("MarshalExportRequest: %v", err)
	}

	var decoded struct {
		ResourceSpans []struct {
			ScopeSpans []struct {
				Spans []struct {
					TraceID      string `json:"traceId"`
					SpanID       string `json:"spanId"`
					ParentSpanID string `json:"parentSpanId"`
				} `json:"spans"`
			} `json:"scopeSpans"`
		} `json:"resourceSpans"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal export request: %v", err)
	}

	gotSpan := decoded.ResourceSpans[0].ScopeSpans[0].Spans[0]
	if gotSpan.TraceID != traceIDHex {
		t.Errorf("traceId = %q, want %q (hex passthrough, not base64)", gotSpan.TraceID, traceIDHex)
	}
	if gotSpan.ParentSpanID != gotSpan.SpanID {
		t.Errorf("parentSpanId should encode the same way spanId does for the same hex input")
	}
}

func TestMarshalExportRequestOmitsParentSpanIdForRootSpans(t *testing.T) {
	spans := []otlpmap.Span{{Service: "shipping", TraceID: traceIDHex, SpanID: spanIDHex}}

	data, err := otlpmap.MarshalExportRequest(spans)
	if err != nil {
		t.Fatalf("MarshalExportRequest: %v", err)
	}
	if bytesContain(data, `"parentSpanId"`) {
		t.Fatalf("a root span (no ParentSpanID) must omit parentSpanId entirely, got: %s", data)
	}
}

func bytesContain(data []byte, substr string) bool {
	return len(data) >= len(substr) && indexOf(string(data), substr) >= 0
}

func indexOf(s, substr string) int {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
