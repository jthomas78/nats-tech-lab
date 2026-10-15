package tracequery

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// maxTraceBytes bounds one Tempo reply. A larger trace is refused, not cut.
const maxTraceBytes = 16 << 20

// GetTrace fetches one trace from Tempo's v2 trace-by-ID route and maps it
// to the lab schema (BR-070).
func (t *Tempo) GetTrace(ctx context.Context, traceID string) (Trace, error) {
	url := strings.TrimRight(t.baseURL, "/") + "/api/v2/traces/" + traceID
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Trace{}, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := t.client.Do(req)
	if err != nil {
		return Trace{}, fmt.Errorf("tracequery: tempo: %w", err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusNotFound:
		return Trace{}, ErrNotFound
	case resp.StatusCode != http.StatusOK:
		return Trace{}, fmt.Errorf("tracequery: tempo: status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxTraceBytes+1))
	if err != nil {
		return Trace{}, fmt.Errorf("tracequery: tempo: %w", err)
	}
	if len(body) > maxTraceBytes {
		return Trace{}, fmt.Errorf("tracequery: tempo: trace is larger than %d bytes", maxTraceBytes)
	}

	var in tempoReply
	if err := json.Unmarshal(body, &in); err != nil {
		return Trace{}, fmt.Errorf("tracequery: tempo: %w", err)
	}
	return mapTrace(traceID, in)
}

// The Tempo v2 reply: OTLP JSON inside a "trace" wrapper. Only what the lab
// schema keeps is decoded.
type tempoReply struct {
	Trace struct {
		ResourceSpans []struct {
			Resource struct {
				Attributes []otlpKeyValue `json:"attributes"`
			} `json:"resource"`
			ScopeSpans []struct {
				Spans []otlpSpan `json:"spans"`
			} `json:"scopeSpans"`
		} `json:"resourceSpans"`
	} `json:"trace"`
}

type otlpSpan struct {
	TraceID           string         `json:"traceId"`
	SpanID            string         `json:"spanId"`
	ParentSpanID      string         `json:"parentSpanId"`
	Name              string         `json:"name"`
	Kind              string         `json:"kind"`
	StartTimeUnixNano string         `json:"startTimeUnixNano"`
	EndTimeUnixNano   string         `json:"endTimeUnixNano"`
	Attributes        []otlpKeyValue `json:"attributes"`
	Events            []struct {
		TimeUnixNano string         `json:"timeUnixNano"`
		Name         string         `json:"name"`
		Attributes   []otlpKeyValue `json:"attributes"`
	} `json:"events"`
	Links []struct {
		TraceID    string         `json:"traceId"`
		SpanID     string         `json:"spanId"`
		Attributes []otlpKeyValue `json:"attributes"`
	} `json:"links"`
	Status struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"status"`
}

type otlpKeyValue struct {
	Key   string    `json:"key"`
	Value otlpValue `json:"value"`
}

type otlpValue struct {
	StringValue *string `json:"stringValue"`
	BoolValue   *bool   `json:"boolValue"`
	// OTLP JSON writes a 64-bit int as a string.
	IntValue    *json.Number `json:"intValue"`
	DoubleValue *float64     `json:"doubleValue"`
	ArrayValue  *struct {
		Values []otlpValue `json:"values"`
	} `json:"arrayValue"`
	KvlistValue *struct {
		Values []otlpKeyValue `json:"values"`
	} `json:"kvlistValue"`
	BytesValue *string `json:"bytesValue"`
}

func mapTrace(traceID string, in tempoReply) (Trace, error) {
	out := Trace{Schema: SchemaVersion, TraceID: traceID, Spans: []Span{}}
	for _, rs := range in.Trace.ResourceSpans {
		resource := attrs(rs.Resource.Attributes)
		service, _ := resource["service.name"].(string)
		for _, ss := range rs.ScopeSpans {
			for _, s := range ss.Spans {
				span, err := mapSpan(s, resource, service)
				if err != nil {
					return Trace{}, err
				}
				out.Spans = append(out.Spans, span)
			}
		}
	}
	if len(out.Spans) == 0 {
		return Trace{}, ErrNotFound
	}
	return out, nil
}

func mapSpan(s otlpSpan, resource map[string]any, service string) (Span, error) {
	var err error
	out := Span{
		Name:          s.Name,
		Kind:          strings.TrimPrefix(s.Kind, "SPAN_KIND_"),
		Service:       service,
		StatusCode:    statusCode(s.Status.Code),
		StatusMessage: s.Status.Message,
		Resource:      resource,
		Attributes:    attrs(s.Attributes),
		Events:        []Event{},
		Links:         []Link{},
	}
	if out.TraceID, err = hexID(s.TraceID); err != nil {
		return Span{}, err
	}
	if out.SpanID, err = hexID(s.SpanID); err != nil {
		return Span{}, err
	}
	if out.ParentSpanID, err = hexID(s.ParentSpanID); err != nil {
		return Span{}, err
	}
	if out.StartUnixNano, err = nanos(s.StartTimeUnixNano); err != nil {
		return Span{}, err
	}
	if out.EndUnixNano, err = nanos(s.EndTimeUnixNano); err != nil {
		return Span{}, err
	}
	for _, e := range s.Events {
		t, err := nanos(e.TimeUnixNano)
		if err != nil {
			return Span{}, err
		}
		out.Events = append(out.Events, Event{Name: e.Name, TimeUnixNano: t, Attributes: attrs(e.Attributes)})
	}
	for _, l := range s.Links {
		link := Link{Attributes: attrs(l.Attributes)}
		if link.TraceID, err = hexID(l.TraceID); err != nil {
			return Span{}, err
		}
		if link.SpanID, err = hexID(l.SpanID); err != nil {
			return Span{}, err
		}
		if len(link.Attributes) == 0 {
			link.Attributes = nil
		}
		out.Links = append(out.Links, link)
	}
	return out, nil
}

// hexID turns Tempo's base64 ID into the lab's hex. An ID that is already
// hex (another backend, or a later Tempo) passes through.
func hexID(id string) (string, error) {
	if id == "" {
		return "", nil
	}
	if (len(id) == 32 || len(id) == 16) && isHex(id) {
		return strings.ToLower(id), nil
	}
	raw, err := base64.StdEncoding.DecodeString(id)
	if err != nil {
		return "", fmt.Errorf("tracequery: tempo: bad id %q: %w", id, err)
	}
	return hex.EncodeToString(raw), nil
}

func isHex(s string) bool {
	_, err := hex.DecodeString(s)
	return err == nil
}

func nanos(s string) (int64, error) {
	if s == "" {
		return 0, nil
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("tracequery: tempo: bad time %q: %w", s, err)
	}
	return n, nil
}

// statusCode maps OTLP's STATUS_CODE_* to the lab's OK, ERROR or UNSET.
func statusCode(code string) string {
	switch code {
	case "STATUS_CODE_OK":
		return "OK"
	case "STATUS_CODE_ERROR":
		return "ERROR"
	default:
		return "UNSET"
	}
}

func attrs(kvs []otlpKeyValue) map[string]any {
	out := make(map[string]any, len(kvs))
	for _, kv := range kvs {
		out[kv.Key] = value(kv.Value)
	}
	return out
}

func value(v otlpValue) any {
	switch {
	case v.StringValue != nil:
		return *v.StringValue
	case v.BoolValue != nil:
		return *v.BoolValue
	case v.IntValue != nil:
		if n, err := v.IntValue.Int64(); err == nil {
			return n
		}
		return v.IntValue.String()
	case v.DoubleValue != nil:
		return *v.DoubleValue
	case v.ArrayValue != nil:
		out := make([]any, 0, len(v.ArrayValue.Values))
		for _, item := range v.ArrayValue.Values {
			out = append(out, value(item))
		}
		return out
	case v.KvlistValue != nil:
		return attrs(v.KvlistValue.Values)
	case v.BytesValue != nil:
		return *v.BytesValue
	default:
		return nil
	}
}
