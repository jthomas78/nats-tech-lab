package accounts

// Phase 43a (BUSINESS_RULES-ACCOUNTS.md's BR-AC34): a second Stream export,
// obs.pubsub.>, imported into PLATFORM under a per-tenant LocalSubject remap.
// The remap is ADR-047 amendment A1 — the rule as originally drafted asserted
// no remap was needed, which would have left the Messages panel unable to tell
// which tenant a message came from. The PLATFORM-side counterpart
// (addPlatformPubsubImport) is exercised from provisioner_test.go, alongside
// the $SRV.> import spec it mirrors.

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/nats-io/jwt/v2"
	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
)

func TestTenantExportsIncludesObsPubsubStreamExport(t *testing.T) {
	const tenant = "ATENANTPUBLICKEY"
	const platform = "APLATFORMPUBLICKEY"

	fresh := newAccountClaims(tenant, "", JSLimits{}, nil, CrossAccountOpts{PlatformPublicKey: platform, TenantName: "acme"})
	var pubsubExport *jwt.Export
	for _, exp := range fresh.Exports {
		if string(exp.Subject) == pubsubExportSubject {
			pubsubExport = exp
		}
	}
	if pubsubExport == nil {
		t.Fatalf("expected an obs.pubsub.> export on a freshly-minted tenant's own claims, got %#v", fresh.Exports)
	}
	if pubsubExport.Type != jwt.Stream {
		t.Fatalf("obs.pubsub.> export must be a Stream export, same shape as obs.trace.>, got type %v", pubsubExport.Type)
	}
	if pubsubExport.AllowTrace {
		t.Fatal("AllowTrace must not be set on this Stream export — jwt.Export.Validate rejects it on anything but a Service export")
	}
	if pubsubExport.ResponseType != "" {
		t.Fatalf("ResponseType is meaningless on a Stream export, got %q", pubsubExport.ResponseType)
	}

	// Must survive a plain re-sign exactly like the trace export does — it
	// must not require CrossAccountOpts to be supplied again.
	preserved := newAccountClaims(tenant, "", JSLimits{}, fresh, CrossAccountOpts{})
	found := false
	for _, exp := range preserved.Exports {
		if string(exp.Subject) == pubsubExportSubject {
			found = true
		}
	}
	if !found {
		t.Fatalf("obs.pubsub.> export was dropped on re-sign: %#v", preserved.Exports)
	}
}

// BR-AC34's channel move: the four notify.accounts.account.* publishes are
// fire-and-forget notifications, so they belong on obs.pubsub.* with the rest
// of the notify.* family — carrying them on obs.trace.*, the request/reply
// channel, was the anomaly. One envelope per publish, on one channel, is also
// what makes BR-047's Nats-Msg-Id dedup meaningful: it does not span channels.
func TestAccountLifecycleNotifiesAreInstrumented(t *testing.T) {
	srv, err := server.NewServer(&server.Options{Port: -1})
	if err != nil {
		t.Fatal(err)
	}
	srv.Start()
	defer srv.Shutdown()
	if !srv.ReadyForConnections(10 * time.Second) {
		t.Fatal("nats server not ready")
	}
	nc, err := nats.Connect(srv.ClientURL(), nats.Name("accounts-pubsub-test"))
	if err != nil {
		t.Fatal(err)
	}
	defer nc.Close()

	obs := make(chan *nats.Msg, 8)
	obsSub, err := nc.Subscribe("obs.>", func(m *nats.Msg) { obs <- m })
	if err != nil {
		t.Fatal(err)
	}
	defer obsSub.Unsubscribe() //nolint:errcheck

	h := &Handlers{NotifyNC: nc, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}

	for _, tc := range []struct{ subject, label, action string }{
		{"notify.accounts.account.created", "account-created", "created"},
		{"notify.accounts.account.suspended", "account-suspended", "suspended"},
		{"notify.accounts.account.reactivated", "account-reactivated", "reactivated"},
		{"notify.accounts.account.jslimits_updated", "account-jslimits-updated", "jslimits_updated"},
	} {
		t.Run(tc.action, func(t *testing.T) {
			for len(obs) > 0 {
				<-obs
			}
			h.publishAccountEvent(context.Background(), tc.subject, tc.label, "acme")

			select {
			case m := <-obs:
				want := "obs.pubsub._platform.accounts.account." + tc.action
				if m.Subject != want {
					t.Fatalf("observation subject = %q, want %q (obs.trace.* is the request/reply channel — these must not land there)", m.Subject, want)
				}
				var env struct {
					Subject   string `json:"subject"`
					Direction string `json:"direction"`
				}
				if err := json.Unmarshal(m.Data, &env); err != nil {
					t.Fatal(err)
				}
				if env.Subject != tc.subject {
					t.Fatalf("envelope subject = %q, want %q", env.Subject, tc.subject)
				}
				if env.Direction != "publish" {
					t.Fatalf("direction = %q, want publish", env.Direction)
				}
			case <-time.After(2 * time.Second):
				t.Fatalf("%s emitted no observation at all", tc.subject)
			}

			// Exactly one envelope, on one channel.
			select {
			case extra := <-obs:
				t.Fatalf("a second observation was emitted for one publish: %s", extra.Subject)
			case <-time.After(300 * time.Millisecond):
			}
		})
	}
}
