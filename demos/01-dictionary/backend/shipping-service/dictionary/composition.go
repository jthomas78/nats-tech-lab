// Package dictionary is the composition root of the shipping module: it
// wires the domain, application, and adapter layers into the monolith.
package dictionary

import (
	"context"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/application/commands"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/eventhandler"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/postgres"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/rest"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/internal/monolith"
)

// initialTenant is which tenant account shipping-service connects as when
// the process starts, before any operator has used the tenant selector.
const initialTenant = "acme"

type Module struct{}

func (Module) Startup(ctx context.Context, mono monolith.Monolith) error {
	log := mono.Logger()

	if err := postgres.Migrate(ctx, mono.DB()); err != nil {
		return err
	}

	// mono.JS()/mono.NC() are the narrowed shipping-admin PLATFORM connection:
	// inspection/replay only. Refdata RPC is deliberately created from each
	// tenant connection in ensureTenantResources, so NATS account imports can
	// stamp the tenant identity server-side.
	shipRepo := postgres.NewRepository(mono.DB())
	containerRepo := postgres.NewContainerRepository(mono.DB())
	portRepo := postgres.NewPortRepository(mono.DB())

	handlers := rest.NewHandlers(rest.Deps{
		Ports:          commands.NewPortHandler(portRepo),
		PlatformJS:     mono.JS(),
		NC:             mono.NC(),
		Log:            log,
		ShipRepo:       shipRepo,
		ContainerRepo:  containerRepo,
		PortRepo:       portRepo,
		NatsURL:        mono.NatsURL(),
		CredsDir:       mono.CredsDir(),
		NatsMonitorURL: mono.NatsMonitorURL(),
	})

	// The initial tenant connect and every later switch are the same code
	// path — see SwitchTenant's doc comment for why that's deliberate.
	if err := handlers.SwitchTenant(ctx, initialTenant); err != nil {
		return err
	}

	// Phase 15a: bring up every OTHER known tenant's persistent resources
	// (rpc.* adapter, projectors) too, not just initialTenant's — a browser
	// connecting straight to GLOBEX's account must work from the moment
	// this process starts, without needing an operator to have switched
	// REST to GLOBEX first. See EnsureAllTenants's doc comment.
	if err := handlers.EnsureAllTenants(ctx); err != nil {
		return err
	}

	// Phase 23: permanent PLATFORM-account background bridges replacing
	// dictionary/internal/rest/sse.go's watchRefdata/watchRPCObs per-request
	// OrderedConsumers — see eventhandler.RegisterRefdataNotify's doc comment
	// for why these run unconditionally for the process lifetime rather than
	// per tenant. Both are nil-safe on mono.JS()/mono.NC(), the same
	// convention PlatformJS/NC already follow elsewhere in this Deps.
	eventhandler.RegisterRefdataNotify(ctx, mono.JS(), mono.NC(), log)
	eventhandler.RegisterRPCTraceNotify(ctx, mono.JS(), mono.NC(), log)

	handlers.Mount(mono.Mux())
	return nil
}
