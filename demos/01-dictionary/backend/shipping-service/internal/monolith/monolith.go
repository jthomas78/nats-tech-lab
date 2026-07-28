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
	// natsrpc/, Phase 12.10) that can't work off jetstream.JetStream alone,
	// such as micro.AddService or a plain Request/Reply client.
	//
	// This is the permanent, unauthenticated (DEFAULT-account) connection —
	// Phase 18b's tenant-scoped connection is a second, separate connection
	// owned by rest.Handlers, reconnected on tenant switch. DB/JS/NC here
	// never change after Startup.
	NC() *nats.Conn
	// NatsURL is needed by rest.Handlers.SwitchTenant (Phase 18b) to open a
	// second, tenant-credentialed connection independent of NC() above.
	NatsURL() string
	Mux() *http.ServeMux
	Logger() *slog.Logger
}

// Module is a self-contained feature that wires itself into the monolith.
type Module interface {
	Startup(ctx context.Context, mono Monolith) error
}
