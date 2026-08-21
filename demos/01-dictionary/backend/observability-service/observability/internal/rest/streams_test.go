package rest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

func TestListStreamsTagsEachStreamWithItsKind(t *testing.T) {
	nc, cleanup := newTestNATS(t)
	defer cleanup()

	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatalf("jetstream.New: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := js.CreateStream(ctx, jetstream.StreamConfig{Name: "REFDATA", Subjects: []string{"evt.*.refdata.*.changed"}}); err != nil {
		t.Fatalf("create stream: %v", err)
	}
	// A KV bucket is backed by a KV_<bucket> stream and an Object Store by
	// an OBJ_<bucket> stream. Both are reported (38e) rather than filtered
	// out, tagged by Kind — the panel is the only view of how much of the
	// account's MaxStreams budget is spent, and ADR-048 budgets against it.
	if _, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: "ships"}); err != nil {
		t.Fatalf("create kv bucket: %v", err)
	}
	if _, err := js.CreateObjectStore(ctx, jetstream.ObjectStoreConfig{Bucket: "organizations-docs"}); err != nil {
		t.Fatalf("create object store: %v", err)
	}

	h := New(Deps{NC: nc, Log: discardLogger(), Accounts: &AccountsClient{}})
	w := httptest.NewRecorder()
	h.listStreams(w, httptest.NewRequest(http.MethodGet, "/api/jetstream/streams", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var body jsStreamsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	kinds := map[string]string{}
	for _, s := range body.Streams {
		if s.Account != platformAccount {
			t.Fatalf("unexpected account on %+v", s)
		}
		kinds[s.Stream] = s.Kind
	}
	want := map[string]string{
		"REFDATA":                "stream",
		"KV_ships":               "kv",
		"OBJ_organizations-docs": "objstore",
	}
	for name, kind := range want {
		got, ok := kinds[name]
		if !ok {
			t.Fatalf("expected %q in the response, got %+v", name, body.Streams)
		}
		if got != kind {
			t.Fatalf("expected %q to be kind %q, got %q", name, kind, got)
		}
	}
	// Stream keeps the raw backing-stream name — it is the selection key
	// and the name $JS.API is addressed by; only the UI strips the prefix.
	for _, s := range body.Streams {
		if s.Stream == "REFDATA" && s.Subjects != 1 {
			t.Fatalf("expected 1 configured subject on REFDATA, got %d", s.Subjects)
		}
	}
	if len(body.Accounts) != 1 || body.Accounts[0].Name != platformAccount || body.Accounts[0].Status != platformAccountStatus {
		t.Fatalf("expected platform account %q in Accounts, got %+v", platformAccountStatus, body.Accounts)
	}
}

// TestListStreamsSkipsUnreachableAccountRatherThanFailingWholeResponse pins
// the fix for a real bug caught live: a suspended tenant is a legitimate,
// permanent entry in introspectableAccounts (its status is exactly what the
// Streams panel's dot now needs to show), but its cross-account $JS.API
// access always fails "no responders" — the embedded single-account test
// server reproduces the same failure for any tenant name that isn't
// PLATFORM, since nothing listens on a monitor.<tenant>.js-prefixed
// subject here either. Before this fix, that one failing account aborted
// the entire response with 500, discarding PLATFORM's already-successful
// stream too. It must also still appear in Accounts with its real status —
// only its stream rows are missing, not the account group itself.
func TestListStreamsSkipsUnreachableAccountRatherThanFailingWholeResponse(t *testing.T) {
	nc, cleanup := newTestNATS(t)
	defer cleanup()

	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatalf("jetstream.New: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := js.CreateStream(ctx, jetstream.StreamConfig{Name: "REFDATA", Subjects: []string{"evt.*.refdata.*.changed"}}); err != nil {
		t.Fatalf("create stream: %v", err)
	}

	accounts := newAccountsMock(t, []AccountsClientAccount{{Name: "acme", PublicKey: "AAAACME", Status: "suspended"}})
	h := New(Deps{NC: nc, Log: discardLogger(), Accounts: accounts})
	w := httptest.NewRecorder()
	h.listStreams(w, httptest.NewRequest(http.MethodGet, "/api/jetstream/streams", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 despite acme being unreachable, got %d: %s", w.Code, w.Body.String())
	}
	var body jsStreamsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Streams) != 1 || body.Streams[0].Account != platformAccount {
		t.Fatalf("expected platform's REFDATA to survive acme's failure, got %+v", body.Streams)
	}
	got := map[string]string{}
	for _, a := range body.Accounts {
		got[a.Name] = a.Status
	}
	if got[platformAccount] != platformAccountStatus {
		t.Fatalf("expected platform in Accounts as %q, got %+v", platformAccountStatus, body.Accounts)
	}
	if got["acme"] != "suspended" {
		t.Fatalf("expected acme still in Accounts as suspended despite unreachable streams, got %+v", body.Accounts)
	}
}

func TestListStreamsEmptyWhenNothingRegistered(t *testing.T) {
	nc, cleanup := newTestNATS(t)
	defer cleanup()

	h := New(Deps{NC: nc, Log: discardLogger(), Accounts: &AccountsClient{}})
	w := httptest.NewRecorder()
	h.listStreams(w, httptest.NewRequest(http.MethodGet, "/api/jetstream/streams", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var body jsStreamsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Streams == nil || len(body.Streams) != 0 {
		t.Fatalf("expected an empty (non-nil) stream list, got %+v", body.Streams)
	}
}
