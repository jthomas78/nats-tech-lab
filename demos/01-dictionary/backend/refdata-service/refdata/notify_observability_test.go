package refdata

// Phase 43a (BR-D45, BR-045, ADR-047 A2/A3): this service's pub/sub
// observability, tested at the two seams the placement rule distinguishes.
//
//   - evt.* is instrumented *inside* the shared jstream.Publisher seam, so a
//     future evt.* publisher in this service is covered by construction —
//     asserted behaviourally in internal/jstream/stream_test.go, and as a
//     checked convention here (no call site does its own observing).
//   - notify.* has no seam, so it is wired per call site — for refdata, the
//     one call site is the per-tenant fan-out below.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
)

func runEmbeddedNATS(t *testing.T) *server.Server {
	t.Helper()
	srv, err := server.NewServer(&server.Options{JetStream: true, StoreDir: t.TempDir(), Port: -1})
	if err != nil {
		t.Fatalf("start embedded NATS: %v", err)
	}
	go srv.Start()
	if !srv.ReadyForConnections(5 * time.Second) {
		t.Fatal("embedded NATS not ready")
	}
	t.Cleanup(srv.Shutdown)
	return srv
}

func TestNotifyFanOutIsObservedPerTenantConnection(t *testing.T) {
	srv := runEmbeddedNATS(t)

	watcher, err := nats.Connect(srv.ClientURL(), nats.Name("watcher"))
	if err != nil {
		t.Fatalf("connect watcher: %v", err)
	}
	defer watcher.Close()

	// Subscribing to obs.> rather than obs.pubsub.> so an envelope landing on
	// the wrong family (obs.trace.*) fails loudly instead of silently.
	obs := make(chan *nats.Msg, 8)
	if _, err := watcher.ChanSubscribe("obs.>", obs); err != nil {
		t.Fatalf("subscribe obs.>: %v", err)
	}
	notified := make(chan *nats.Msg, 8)
	if _, err := watcher.ChanSubscribe("notify.>", notified); err != nil {
		t.Fatalf("subscribe notify.>: %v", err)
	}
	if err := watcher.Flush(); err != nil {
		t.Fatalf("flush watcher: %v", err)
	}

	tenant, err := nats.Connect(srv.ClientURL(), nats.Name("acme"))
	if err != nil {
		t.Fatalf("connect tenant: %v", err)
	}
	defer tenant.Close()

	p := tenantPublisher{}
	p.publishTo("acme", tenant, "notify.acme.refdata.port.changed", []byte(`{"typeKey":"port"}`))
	if err := tenant.Flush(); err != nil {
		t.Fatalf("flush tenant: %v", err)
	}

	select {
	case msg := <-notified:
		if msg.Subject != "notify.acme.refdata.port.changed" {
			t.Fatalf("unexpected notify subject %q", msg.Subject)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the notify.* publish never arrived")
	}

	select {
	case msg := <-obs:
		want := "obs.pubsub.acme.refdata.port.changed"
		if msg.Subject != want {
			t.Fatalf("observation subject = %q, want %q", msg.Subject, want)
		}
		if !strings.Contains(string(msg.Data), "notify.acme.refdata.port.changed") {
			t.Fatalf("envelope does not carry the observed subject: %s", msg.Data)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the notify.* fan-out published no obs.pubsub.* envelope")
	}

	select {
	case msg := <-obs:
		t.Fatalf("a second envelope was emitted for one publish: %s", msg.Subject)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestNotifyObservationIsSkippedWhenTheTenantPublishFails(t *testing.T) {
	srv := runEmbeddedNATS(t)

	watcher, err := nats.Connect(srv.ClientURL(), nats.Name("watcher"))
	if err != nil {
		t.Fatalf("connect watcher: %v", err)
	}
	defer watcher.Close()
	obs := make(chan *nats.Msg, 4)
	if _, err := watcher.ChanSubscribe("obs.>", obs); err != nil {
		t.Fatalf("subscribe obs.>: %v", err)
	}
	if err := watcher.Flush(); err != nil {
		t.Fatalf("flush watcher: %v", err)
	}

	tenant, err := nats.Connect(srv.ClientURL(), nats.Name("acme"))
	if err != nil {
		t.Fatalf("connect tenant: %v", err)
	}
	tenant.Close()

	p := tenantPublisher{}
	p.publishTo("acme", tenant, "notify.acme.refdata.port.changed", []byte(`{"typeKey":"port"}`))

	select {
	case msg := <-obs:
		t.Fatalf("a failed publish was still observed: %s", msg.Subject)
	case <-time.After(300 * time.Millisecond):
	}
}

// TestEvtPublishIsObservedViaTheSeamNotTheCallSite is a checked convention in
// the same spirit as BR-049's notify coverage test: kvcache is where this
// service's evt.* publish is *called* (kvcache.go's NotifyItemChanged), and it
// must stay free of natstrace wiring — the observation belongs to the
// jstream.Publisher seam it delegates to, so the next evt.* caller inherits it.
func TestEvtPublishIsObservedViaTheSeamNotTheCallSite(t *testing.T) {
	dir := filepath.Join("internal", "kvcache")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}

	fset := token.NewFileSet()
	sawEvtCallSite := false
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		path := filepath.Join(dir, name)
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if strings.Contains(string(src), `"evt.`) || strings.Contains(string(src), "evt.%s") {
			sawEvtCallSite = true
		}
		file, err := parser.ParseFile(fset, path, src, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		// natstrace itself is expected here — kvcache carries the caller's
		// span through to the publisher. What must not appear is an
		// Observe* call: that is the seam's job.
		ast.Inspect(file, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			id, ok := sel.X.(*ast.Ident)
			if !ok || id.Name != "natstrace" {
				return true
			}
			if strings.HasPrefix(sel.Sel.Name, "Observe") {
				t.Errorf("%s calls natstrace.%s at the call site: evt.* observation "+
					"belongs to the jstream.Publisher seam (ADR-047 A3), so the next "+
					"evt.* caller inherits it", path, sel.Sel.Name)
			}
			return true
		})
	}
	if !sawEvtCallSite {
		t.Fatal("no evt.* publish found in internal/kvcache — this convention test " +
			"is guarding code that has moved; re-point it at the new call site")
	}
}
