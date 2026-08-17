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
	// NatsURL is needed by rest.Handlers.SwitchTenant (Phase 13b) to open a
	// tenant-credentialed connection independent of the admin NC() above.
	NatsURL() string
	// CredsDir is the directory of <tenant>.creds files (Phase 14a —
	// operator mode) that SwitchTenant scans to resolve a tenant name to its
	// NATS credentials. Empty when running locally outside Docker without
	// operator mode configured.
	CredsDir() string
	Mux() *http.ServeMux
	Logger() *slog.Logger
}

// Module is a self-contained feature that wires itself into the monolith.
type Module interface {
	Startup(ctx context.Context, mono Monolith) error
}
