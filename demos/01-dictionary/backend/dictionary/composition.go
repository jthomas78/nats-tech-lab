// Package dictionary is the composition root of the shipping module: it
// wires the domain, application, and adapter layers into the monolith.
package dictionary

import (
	"context"
	"os"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/dictionary/internal/application/commands"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/dictionary/internal/application/queries"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/dictionary/internal/domain"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/dictionary/internal/eventhandler"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/dictionary/internal/postgres"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/dictionary/internal/rest"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/internal/jstream"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/internal/kvstore"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/internal/monolith"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/internal/refdataconsumer"
)

const (
	// KV bucket prefixes, all context-scoped ({prefix}-{context}):
	//   dict-a    — Shape A ship read model
	//   dict-b    — Shape B ship cache
	//   container — container projection (terminal queries read model)
	//   meta      — cross-cutting lookup sets (known-containers)
	//   refdata   — refdata-service's Q5 versioned-read cache (Phase 11.3
	//               consumer demo; owned/written by refdata-service, this
	//               module only reads it)
	shapeABucketPrefix    = "dict-a"
	shapeBBucketPrefix    = "dict-b"
	containerBucketPrefix = "container"
	metaBucketPrefix      = "meta"
	refdataBucketPrefix   = "refdata"
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
	kvContainers := kvstore.New(js, containerBucketPrefix)
	kvMeta := kvstore.New(js, metaBucketPrefix)
	kvRefdata := kvstore.New(js, refdataBucketPrefix)
	refdata := refdataconsumer.New(kvRefdata, refdataServiceURL())
	shipRepo := postgres.NewRepository(mono.DB())
	containerRepo := postgres.NewContainerRepository(mono.DB())
	portRepo := postgres.NewPortRepository(mono.DB())

	if _, err := eventhandler.RegisterShapeA(ctx, js, kvA, log); err != nil {
		return err
	}
	if _, err := eventhandler.RegisterShapeB(ctx, js, kvB, shipRepo, log); err != nil {
		return err
	}
	if _, err := eventhandler.RegisterContainers(ctx, js, kvContainers, containerRepo, log); err != nil {
		return err
	}
	if _, err := eventhandler.RegisterMeta(ctx, js, kvMeta, log); err != nil {
		return err
	}

	pub := jstream.NewPublisher(js)
	handlers := rest.NewHandlers(rest.Deps{
		Ships:      commands.NewShipHandler(pub, js, portRepo),
		Containers: commands.NewContainerHandler(pub, js, portRepo),
		Ports:      commands.NewPortHandler(portRepo),
		ShapeB:     queries.NewShapeB(kvB, shipRepo),
		ShapeC:     queries.NewShapeC(js),
		Terminal:   queries.NewTerminal(kvContainers),
		Meta:       queries.NewMeta(kvMeta),
		KVA:        kvA,
		KVB:        kvB,
		KVCont:     kvContainers,
		KVMeta:     kvMeta,
		KVRefdata:  kvRefdata,
		Refdata:    refdata,
		JS:         js,
		Log:        log,
	})
	handlers.Mount(mono.Mux())
	return nil
}

func refdataServiceURL() string {
	if v := os.Getenv("REFDATA_SERVICE_URL"); v != "" {
		return v
	}
	return "http://localhost:18081"
}
