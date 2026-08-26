// Package natsconn holds the one connection policy every long-lived service
// in this repo shares.
//
// It exists because nats.go's defaults are wrong for a service: MaxReconnects
// is 60, so roughly two minutes after NATS goes away the client stops retrying
// and CLOSES the connection permanently. Every later JetStream/KV/request call
// then fails with "nats: connection closed" forever and only a process restart
// recovers it. observability-service hit exactly that on a
// `docker compose restart nats` — the Admin UI's browser was back in seconds
// while the service backing its panels stayed dead until restarted by hand.
//
// Every service had open-coded the same three lines (Name, maybe
// UserCredentials, connect) and so every service had the same bug. One package
// so the fix lands once and a new service inherits it.
package natsconn

import (
	"log/slog"
	"time"

	"github.com/nats-io/nats.go"
)

// Options is the standard option set for a connection that must outlive the
// NATS server it talks to. name is required — an anonymous connection is
// indistinguishable in `nats server list connections` / /connz (see CLAUDE.md's
// "Architectural Notes"). credsPath may be empty for a no-auth local run. log
// may be nil, in which case the lifecycle transitions go unlogged.
//
// Callers append their own options after these; nats.Options are applied in
// order, so a caller that genuinely needs different behaviour can still
// override one.
func Options(name, credsPath string, log *slog.Logger) []nats.Option {
	opts := []nats.Option{
		nats.Name(name),
		// Retry forever. A service has nothing useful to do while
		// disconnected and no reason to prefer dying to waiting, and the
		// URL is a hostname — nats.go re-resolves it per attempt, so this
		// also picks up the new container IP a restart hands out.
		nats.MaxReconnects(-1),
		nats.ReconnectWait(2 * time.Second),
		// Without jitter every service in the stack re-dials in lockstep
		// after a shared outage.
		nats.ReconnectJitter(100*time.Millisecond, time.Second),
	}
	if credsPath != "" {
		opts = append(opts, nats.UserCredentials(credsPath))
	}
	if log != nil {
		opts = append(opts,
			nats.DisconnectErrHandler(func(*nats.Conn, error) {
				log.Warn("nats disconnected, will keep retrying", "conn", name)
			}),
			nats.ReconnectHandler(func(nc *nats.Conn) {
				log.Info("nats reconnected", "conn", name, "url", nc.ConnectedUrl())
			}),
			nats.ClosedHandler(func(nc *nats.Conn) {
				// With MaxReconnects(-1) this fires only on a deliberate
				// Drain/Close, so it should never be a surprise.
				log.Warn("nats connection closed", "conn", name, "err", nc.LastError())
			}),
		)
	}
	return opts
}
