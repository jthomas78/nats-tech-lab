// Package monolith defines the composition contract between the application
// bootstrap (cmd/main.go) and the feature modules that plug into it.
package monolith

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// Monolith exposes the shared infrastructure a module may depend on.
type Monolith interface {
	DB() *sql.DB
	JS() jetstream.JetStream
	// NC is the raw core-NATS connection — needed by adapters (e.g.
	// refdata-service's natsrpc/, Phase 12.10, or this service's own
	// dictionary/internal/browserrpc/, Phase 15a) that can't work off
	// jetstream.JetStream alone, such as micro.AddService or a plain
	// Request/Reply client.
	//
	// This is the permanent, permission-restricted shipping-admin connection
	// in PLATFORM. Tenant-scoped work uses connections owned by
	// rest.Handlers; this one is limited to admin inspection/replay and
	// $SRV discovery. DB/JS/NC here never change after Startup.
	NC() *nats.Conn
	// PlatformFullJS is a SECOND, unrestricted PLATFORM-account JetStream
	// context — connected with platform.creds (the same broad-access
	// credential refdata-service and accounts-service already use), not
	// shipping-admin.creds like JS()/NC() above. Deliberately kept separate:
	// shipping-admin is intentionally locked out of $JS.API.> ("Do not grant
	// $JS.API.> or access to tenant streams/KV" — nats/bootstrap-operator.sh,
	// enforced by TestShippingAdminCanOnlyUseNarrowOrderedConsumerAccess), so
	// widening JS()/NC() themselves would erode that boundary for every
	// existing caller. This exists solely so the Admin UI's two cross-account
	// panels can enumerate PLATFORM ($JS.API.STREAM.LIST): listKVBuckets for
	// refdata-service's KV_* streams, and listStreams for REFDATA/RPCTRACE.
	// Read-only cross-account introspection, nothing else should use it —
	// note that *replaying* REFDATA/RPCTRACE does not need it, since
	// bootstrap-operator.sh grants shipping-admin the ordered-consumer API for
	// exactly those two streams (see rpcTraceReplayOnce). Nil
	// if NATS_PLATFORM_CREDS_PATH/platform.creds isn't configured (e.g. local
	// dev outside Docker) or the connection failed at Startup — callers must
	// handle nil rather than treating it as always available.
	PlatformFullJS() jetstream.JetStream
	// NatsURL is needed by rest.Handlers.SwitchTenant (Phase 13b) to open a
	// tenant-credentialed connection independent of the admin NC() above.
	NatsURL() string
	// CredsDir is the directory of <tenant>.creds files (Phase 14a —
	// operator mode) that SwitchTenant scans to resolve a tenant name to its
	// NATS credentials. Empty when running locally outside Docker without
	// operator mode configured.
	CredsDir() string
	// NatsMonitorURL is the NATS server's HTTP monitoring endpoint (default
	// port 8222) — used by the admin Connections panel (Phase 17c) to proxy
	// GET /connz. Distinct from NatsURL, which is the client (4222) port.
	NatsMonitorURL() string
	// NatsLogPath is the local filesystem path to NATS's log_file (see
	// nats/nats.conf), mounted read-only into this container from the same
	// volume NATS writes into — used by the admin Log panel to tail it.
	// Empty when running locally outside Docker without a log_file
	// configured; the Log panel's endpoint reports unavailable rather than
	// erroring.
	NatsLogPath() string
	Mux() *http.ServeMux
	Logger() *slog.Logger
}

// Module is a self-contained feature that wires itself into the monolith.
type Module interface {
	Startup(ctx context.Context, mono Monolith) error
}
