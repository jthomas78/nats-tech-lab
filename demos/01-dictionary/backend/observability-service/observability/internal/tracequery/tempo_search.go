package tracequery

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// maxSearchBytes bounds one Tempo search or tag-values reply.
const maxSearchBytes = 4 << 20

// rootNotReceived is Tempo's placeholder for a trace whose root span it
// has not stored yet. It is not a service name.
const rootNotReceived = "<root span not yet received>"

// traceQL builds the search query. Each filter is its own span set, so the
// matching spans may be different spans of one trace. The values were
// checked by checkSearch and hold no quote or backslash.
func traceQL(q SearchQuery) string {
	var sets []string
	if q.Service != "" {
		sets = append(sets, `{ resource.service.name = "`+q.Service+`" }`)
	}
	if q.Name != "" {
		sets = append(sets, `{ name = "`+q.Name+`" }`)
	}
	if q.Tenant != "" {
		sets = append(sets, `{ span.lab.tenant.originating = "`+q.Tenant+`" }`)
	}
	if q.ErrorsOnly {
		sets = append(sets, `{ status = error }`)
	}
	if len(sets) == 0 {
		sets = []string{`{ }`}
	}
	return strings.Join(sets, " && ") + " with (most_recent=true)"
}

// Search runs a TraceQL search on Tempo and maps each trace to the lab's
// summary (BR-074). spss=1: the summary needs no matched spans, and Tempo's
// serviceStats already counts the whole trace.
func (t *Tempo) Search(ctx context.Context, q SearchQuery) ([]TraceSummary, error) {
	params := url.Values{}
	params.Set("q", traceQL(q))
	params.Set("limit", strconv.Itoa(q.Limit))
	params.Set("spss", "1")
	params.Set("start", strconv.FormatInt(q.Start.Unix(), 10))
	params.Set("end", strconv.FormatInt(q.End.Unix()+1, 10))
	var in tempoSearchReply
	if err := t.getJSON(ctx, "/api/search?"+params.Encode(), &in); err != nil {
		return nil, err
	}
	out := make([]TraceSummary, 0, len(in.Traces))
	for _, tr := range in.Traces {
		s, err := mapSummary(tr)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

// Services lists resource.service.name values from Tempo's tag API (BR-075).
func (t *Tempo) Services(ctx context.Context, start, end time.Time) ([]string, error) {
	params := url.Values{}
	params.Set("start", strconv.FormatInt(start.Unix(), 10))
	params.Set("end", strconv.FormatInt(end.Unix()+1, 10))
	var in struct {
		TagValues []struct {
			Value string `json:"value"`
		} `json:"tagValues"`
	}
	if err := t.getJSON(ctx, "/api/v2/search/tag/resource.service.name/values?"+params.Encode(), &in); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(in.TagValues))
	for _, v := range in.TagValues {
		out = append(out, v.Value)
	}
	return out, nil
}

func (t *Tempo) getJSON(ctx context.Context, path string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(t.baseURL, "/")+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := t.client.Do(req)
	if err != nil {
		return fmt.Errorf("tracequery: tempo: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("tracequery: tempo: status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxSearchBytes+1))
	if err != nil {
		return fmt.Errorf("tracequery: tempo: %w", err)
	}
	if len(body) > maxSearchBytes {
		return fmt.Errorf("tracequery: tempo: reply is larger than %d bytes", maxSearchBytes)
	}
	if err := json.Unmarshal(body, v); err != nil {
		return fmt.Errorf("tracequery: tempo: %w", err)
	}
	return nil
}

type tempoSearchReply struct {
	Traces []tempoSearchTrace `json:"traces"`
}

type tempoSearchTrace struct {
	TraceID           string `json:"traceID"`
	RootServiceName   string `json:"rootServiceName"`
	RootTraceName     string `json:"rootTraceName"`
	StartTimeUnixNano string `json:"startTimeUnixNano"`
	DurationMs        int64  `json:"durationMs"`
	ServiceStats      map[string]struct {
		SpanCount  int `json:"spanCount"`
		ErrorCount int `json:"errorCount"`
	} `json:"serviceStats"`
}

func mapSummary(tr tempoSearchTrace) (TraceSummary, error) {
	// Tempo drops leading zeros from a trace ID.
	id := strings.ToLower(tr.TraceID)
	if len(id) > 32 || !isHex(strings.Repeat("0", len(id)%2)+id) {
		return TraceSummary{}, fmt.Errorf("tracequery: tempo: bad trace id %q", tr.TraceID)
	}
	id = strings.Repeat("0", 32-len(id)) + id
	start, err := nanos(tr.StartTimeUnixNano)
	if err != nil {
		return TraceSummary{}, err
	}
	out := TraceSummary{
		TraceID:       id,
		RootService:   tr.RootServiceName,
		RootName:      tr.RootTraceName,
		StartUnixNano: start,
		DurationMs:    tr.DurationMs,
		Services:      []string{},
	}
	if out.RootService == rootNotReceived {
		out.RootService = ""
	}
	for name, st := range tr.ServiceStats {
		out.Services = append(out.Services, name)
		out.SpanCount += st.SpanCount
		out.ErrorCount += st.ErrorCount
	}
	sort.Strings(out.Services)
	return out, nil
}
