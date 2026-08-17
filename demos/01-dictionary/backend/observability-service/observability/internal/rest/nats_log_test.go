package rest

// Log panel — lifted verbatim from shipping-service's nats_log_test.go. The
// real risk is the tail/filter/cap logic (readLastLines' byte-window seek,
// level+q filtering, the hard natsLogMaxTail ceiling), not the HTTP
// plumbing, so these write a real temp file rather than mocking anything.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func writeTestLog(t *testing.T, lines []string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "nats.log")
	content := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write test log: %v", err)
	}
	return path
}

func TestTailNatsLogReturns503WhenUnconfigured(t *testing.T) {
	h := New(Deps{Log: discardLogger(), NatsLogPath: ""})
	req := httptest.NewRequest(http.MethodGet, "/api/nats/log", nil)
	w := httptest.NewRecorder()

	h.tailNatsLog(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", w.Code, w.Body.String())
	}
}

func TestTailNatsLogDefaultTailReturnsMostRecentLinesInOrder(t *testing.T) {
	lines := []string{
		`[1] 2026/08/05 10:00:00.000000 [INF] Starting nats-server`,
		`[1] 2026/08/05 10:00:01.000000 [INF] Listening for client connections on 0.0.0.0:4222`,
		`[1] 2026/08/05 10:00:02.000000 [WRN] Publish Violation - Subject "$SRV.STATS"`,
	}
	path := writeTestLog(t, lines)
	h := New(Deps{Log: discardLogger(), NatsLogPath: path})
	req := httptest.NewRequest(http.MethodGet, "/api/nats/log", nil)
	w := httptest.NewRecorder()

	h.tailNatsLog(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var body natsLogResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Lines) != 3 {
		t.Fatalf("expected all 3 lines, got %d: %v", len(body.Lines), body.Lines)
	}
	for i, want := range lines {
		if body.Lines[i] != want {
			t.Errorf("line %d = %q, want %q (oldest-first order expected)", i, body.Lines[i], want)
		}
	}
	if body.Truncated {
		t.Error("expected Truncated=false when fewer lines exist than the tail size")
	}
}

func TestTailNatsLogFiltersByLevel(t *testing.T) {
	lines := []string{
		`[1] 10:00:00 [INF] Starting nats-server`,
		`[1] 10:00:01 [WRN] Publish Violation - Subject "$SRV.STATS"`,
		`[1] 10:00:02 [ERR] Authorization Violation`,
		`[1] 10:00:03 [INF] Server is ready`,
	}
	path := writeTestLog(t, lines)
	h := New(Deps{Log: discardLogger(), NatsLogPath: path})
	req := httptest.NewRequest(http.MethodGet, "/api/nats/log?level=error", nil)
	w := httptest.NewRecorder()

	h.tailNatsLog(w, req)

	var body natsLogResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Lines) != 1 || !strings.Contains(body.Lines[0], "Authorization Violation") {
		t.Fatalf("expected exactly the one [ERR] line, got %v", body.Lines)
	}
}

func TestTailNatsLogFiltersByFreeTextSubstringCaseInsensitive(t *testing.T) {
	lines := []string{
		`[1] 10:00:00 [INF] Starting nats-server`,
		`[1] 10:00:01 [WRN] Publish Violation - Subject "$SRV.STATS"`,
		`[1] 10:00:02 [WRN] Publish Violation - Subject "other.subject"`,
	}
	path := writeTestLog(t, lines)
	h := New(Deps{Log: discardLogger(), NatsLogPath: path})
	req := httptest.NewRequest(http.MethodGet, "/api/nats/log?q=SRV.STATS", nil)
	w := httptest.NewRecorder()

	h.tailNatsLog(w, req)

	var body natsLogResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Lines) != 1 || !strings.Contains(body.Lines[0], `"$SRV.STATS"`) {
		t.Fatalf("expected exactly the one matching line, got %v", body.Lines)
	}
}

func TestTailNatsLogLevelAndQCombineWithAnd(t *testing.T) {
	lines := []string{
		`[1] 10:00:00 [ERR] Authorization Violation for account ACME`,
		`[1] 10:00:01 [WRN] Publish Violation - Subject "$SRV.STATS"`,
		`[1] 10:00:02 [ERR] some other error`,
	}
	path := writeTestLog(t, lines)
	h := New(Deps{Log: discardLogger(), NatsLogPath: path})
	req := httptest.NewRequest(http.MethodGet, "/api/nats/log?level=error&q=ACME", nil)
	w := httptest.NewRecorder()

	h.tailNatsLog(w, req)

	var body natsLogResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Lines) != 1 || !strings.Contains(body.Lines[0], "ACME") {
		t.Fatalf("expected only the [ERR] line mentioning ACME, got %v", body.Lines)
	}
}

func TestTailNatsLogCallerTailIsHonoredUnderTheHardCap(t *testing.T) {
	lines := make([]string, 10)
	for i := range lines {
		lines[i] = `[1] 10:00:0` + strconv.Itoa(i) + ` [INF] line ` + strconv.Itoa(i)
	}
	path := writeTestLog(t, lines)
	h := New(Deps{Log: discardLogger(), NatsLogPath: path})
	req := httptest.NewRequest(http.MethodGet, "/api/nats/log?tail=3", nil)
	w := httptest.NewRecorder()

	h.tailNatsLog(w, req)

	var body natsLogResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Lines) != 3 {
		t.Fatalf("expected 3 lines, got %d: %v", len(body.Lines), body.Lines)
	}
	want := []string{"line 7", "line 8", "line 9"}
	for i, w := range want {
		if !strings.Contains(body.Lines[i], w) {
			t.Errorf("line %d = %q, want to contain %q", i, body.Lines[i], w)
		}
	}
	if !body.Truncated {
		t.Error("expected Truncated=true when more matching lines existed than the requested tail")
	}
}

func TestTailNatsLogRequestedTailAboveHardCapIsClamped(t *testing.T) {
	lines := []string{`[1] 10:00:00 [INF] only line`}
	path := writeTestLog(t, lines)
	h := New(Deps{Log: discardLogger(), NatsLogPath: path})
	req := httptest.NewRequest(http.MethodGet, "/api/nats/log?tail=999999", nil)
	w := httptest.NewRecorder()

	h.tailNatsLog(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var body natsLogResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Lines) != 1 {
		t.Fatalf("expected the one available line (not an error from the oversized tail request), got %v", body.Lines)
	}
}
