// Package monolith defines the composition contract between the application
// bootstrap (cmd/main.go) and the feature modules that plug into it.
package monolith

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"

	"github.com/nats-io/nats.go/jetstream"
)

// Monolith exposes the shared infrastructure a module may depend on.
type Monolith interface {
	DB() *sql.DB
	JS() jetstream.JetStream
	Mux() *http.ServeMux
	Logger() *slog.Logger
}

// Module is a self-contained feature that wires itself into the monolith.
type Module interface {
	Startup(ctx context.Context, mono Monolith) error
}
