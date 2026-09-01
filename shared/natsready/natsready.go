// Package natsready answers one question about the process it runs in: is
// this service ready to do its work right now.
//
// It exists because presence is not readiness (BR-AS62). A service holds its
// NATS connection open while its database is gone, so "the connection is
// there" and even "something replied" are both weaker than the question being
// asked. The only answer that counts is {"ready":true}, and it is produced by
// running a check the service supplies — a database ping, a dependency probe
// — at the moment of asking.
//
// Deliberately tiny and outside every service's hexagon: the responder holds
// no state, caches nothing, and can say nothing a check did not just prove.
package natsready

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/jthomas78/nats-tech-lab/shared/mferegistry"
)

// CheckTimeout bounds one check. A readiness answer that arrives after the
// asker gave up is not an answer, and a check that hangs must fail closed
// rather than hold the reply open.
const CheckTimeout = 2 * time.Second

// Check is the service's own readiness test. Returning an error means not
// ready; the error's text stays in this process (BR-AS60 — a cause that
// travels is a word from a closed list, never a message that could name a
// host, a query or a credential).
type Check func(ctx context.Context) error

type reply struct {
	Ready bool   `json:"ready"`
	Cause string `json:"cause,omitempty"`
}

// Responder is a mounted subscription. Stop it with Stop.
type Responder struct {
	sub *nats.Subscription
}

// Mount answers on the service's readiness subject. The subject is built from
// the service ID the deployment gave this process, the same value the
// registry is configured with — neither side reads it from a manifest.
func Mount(nc *nats.Conn, serviceID string, check Check, log *slog.Logger) (*Responder, error) {
	sub, err := nc.Subscribe(mferegistry.ServiceReady(serviceID), func(msg *nats.Msg) {
		ctx, cancel := context.WithTimeout(context.Background(), CheckTimeout)
		defer cancel()

		out := reply{Ready: true}
		if err := check(ctx); err != nil {
			// Logged here in full, answered in one word. This process may
			// name its own database; the answer travels to a service that
			// decorates a browser reply with it.
			log.Warn("readiness check failed", "service", serviceID, "error", err)
			out = reply{Ready: false, Cause: "not-ready"}
		}
		body, _ := json.Marshal(out)
		_ = msg.Respond(body)
	})
	if err != nil {
		return nil, err
	}
	return &Responder{sub: sub}, nil
}

func (r *Responder) Stop() error {
	if r == nil || r.sub == nil {
		return nil
	}
	return r.sub.Unsubscribe()
}
