// Package dictionary is the composition root of the dictionary module: it
// wires the domain, application, and adapter layers into the monolith.
package dictionary

import (
	"context"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/dictionary/internal/application/commands"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/dictionary/internal/application/queries"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/dictionary/internal/domain"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/dictionary/internal/eventhandler"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/dictionary/internal/postgres"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/dictionary/internal/rest"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/internal/jstream"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/internal/kvstore"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/internal/monolith"
)

const (
	// KV bucket prefixes: dict-a-{context} is the Shape A read model,
	// dict-b-{context} is the Shape B cache. Separate families keep the
	// two shapes independent for the side-by-side comparison.
	shapeABucketPrefix = "dict-a"
	shapeBBucketPrefix = "dict-b"
)

type Module struct{}

func (Module) Startup(ctx context.Context, mono monolith.Monolith) error {
	js := mono.JS()
	log := mono.Logger()

	if _, err := jstream.CreateStream(ctx, js, domain.StreamName, domain.StreamSubjects()); err != nil {
		return err
	}
	if err := postgres.Migrate(ctx, mono.DB()); err != nil {
		return err
	}

	kvA := kvstore.New(js, shapeABucketPrefix)
	kvB := kvstore.New(js, shapeBBucketPrefix)
	repo := postgres.NewRepository(mono.DB())

	if _, err := eventhandler.RegisterShapeA(ctx, js, kvA, log); err != nil {
		return err
	}
	if _, err := eventhandler.RegisterShapeB(ctx, js, kvB, repo, log); err != nil {
		return err
	}

	shipHandler := commands.NewShipHandler(jstream.NewPublisher(js), js)
	shapeB := queries.NewShapeB(kvB, repo)
	shapeC := queries.NewShapeC(js)

	handlers := rest.NewHandlers(shipHandler, shapeB, shapeC, kvA, kvB, js, log)
	handlers.Mount(mono.Mux())
	return nil
}
