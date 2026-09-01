// Package healthnats asks one backend service whether it is ready.
//
// It is a request/reply client and nothing else: no subscription, no
// wildcard, no stream. One probe names one service, so the grant this needs
// is a list of exact subjects rather than `rpc._platform.health.>` — a
// wildcard here would be one capability covering every service on the
// platform, held by the one process that talks to browsers (BR-AS62).
package healthnats

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/domain"
	"github.com/jthomas78/nats-tech-lab/shared/mferegistry"
)

type Client struct{ nc *nats.Conn }

func New(nc *nats.Conn) *Client { return &Client{nc: nc} }

// readyResponse is the whole contract. `ready` is a required boolean and not
// an optional one: a missing field decodes as false, which is the safe way
// round — a service whose reply we cannot read is not a service we can call
// healthy.
type readyResponse struct {
	Ready bool `json:"ready"`
}

// Probe asks once. The connection being up says nothing on its own — a
// service holds its NATS connection open while its database is gone — so
// only the agreed answer counts.
func (c *Client) Probe(ctx context.Context, serviceID string, at time.Time) domain.HealthProbe {
	ctx, cancel := context.WithTimeout(ctx, domain.HealthProbeTimeout)
	defer cancel()

	msg, err := c.nc.RequestWithContext(ctx, mferegistry.ServiceReady(serviceID), nil)
	if err != nil {
		switch {
		case errors.Is(err, nats.ErrNoResponders):
			return domain.HealthProbeFailed("no-responders", at)
		case errors.Is(err, context.DeadlineExceeded), errors.Is(err, nats.ErrTimeout):
			return domain.HealthProbeFailed("timeout", at)
		}
		// Anything else is a broker-level failure whose text names hosts and
		// subjects. Closed vocabulary, like every other cause (BR-AS60).
		return domain.HealthProbeFailed("unreachable", at)
	}

	var parsed readyResponse
	if len(msg.Data) == 0 || json.Unmarshal(msg.Data, &parsed) != nil {
		return domain.HealthProbeFailed("invalid-response", at)
	}
	if !parsed.Ready {
		// The case this package exists for: it answered, and the answer is no.
		return domain.HealthProbeFailed("not-ready", at)
	}
	return domain.HealthProbeOK(at)
}
