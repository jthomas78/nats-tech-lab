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
	NC() *nats.Conn
	Mux() *http.ServeMux
	Logger() *slog.Logger
}

// Module is a self-contained feature that wires itself into the monolith.
type Module interface {
	Startup(ctx context.Context, mono Monolith) error
}
