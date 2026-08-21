// Package transporterprofile composes one tenant account's event store,
// projector, canonical Postgres projection, and KV cache.
package transporterprofile

import (
	"context"
	"database/sql"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/transporterprofile/cache"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/transporterprofile/orchestration"
	profilepostgres "github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/transporterprofile/postgres"
)

type Runtime struct {
	Commands   *orchestration.ProfileHandler
	Projection *profilepostgres.Projection
	// Events is the Activity-facing append boundary (BR-TP24's dedup-keyed
	// AppendWorkflowEvent). Retained rather than kept local to Start because
	// the Temporal worker's activities resolve it per tenant (BR-TP58).
	Events    *orchestration.JetStreamEventStore
	projector *orchestration.Projector
}

func Start(ctx context.Context, nc *nats.Conn, db *sql.DB) (*Runtime, error) {
	js, err := jetstream.New(nc)
	if err != nil {
		return nil, err
	}
	if err := orchestration.EnsureStream(ctx, js); err != nil {
		return nil, err
	}
	projection := profilepostgres.NewProjection(db)
	if err := projection.Migrate(ctx); err != nil {
		return nil, err
	}
	kv, err := cache.New(ctx, js)
	if err != nil {
		return nil, err
	}
	projector := orchestration.NewProjector(js, projection, kv)
	if err := projector.Start(ctx); err != nil {
		return nil, err
	}
	events := orchestration.NewJetStreamEventStore(js)
	return &Runtime{
		Commands:   orchestration.NewProfileHandler(events),
		Projection: projection,
		Events:     events,
		projector:  projector,
	}, nil
}

func (r *Runtime) Close() {
	if r != nil && r.projector != nil {
		r.projector.Stop()
	}
}
