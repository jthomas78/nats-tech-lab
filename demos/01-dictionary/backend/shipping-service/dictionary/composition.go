// Package dictionary is the composition root of the shipping module: it
// wires the domain, application, and adapter layers into the monolith.
package dictionary

import (
	"context"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/application/commands"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/postgres"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/rest"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/internal/monolith"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/internal/refdataconsumer"
)

// tenantCredentials are Phase 13a's static server-config account fixtures —
// spike-only, plaintext, must match nats/nats.conf's accounts{} block. This
// is a deliberate spike simplification (Main-POC-Plan.md Phase 13b):
// real tenant onboarding would mint credentials, not hardcode them here.
var tenantCredentials = map[string]rest.TenantCredentials{
	"acme":   {User: "acme", Password: "acme-spike-pass"},
	"globex": {User: "globex", Password: "globex-spike-pass"},
}

// initialTenant is which tenant account shipping-service connects as when
// the process starts, before any operator has used the tenant selector.
const initialTenant = "acme"

type Module struct{}

func (Module) Startup(ctx context.Context, mono monolith.Monolith) error {
	log := mono.Logger()

	if err := postgres.Migrate(ctx, mono.DB()); err != nil {
		return err
	}

	// mono.JS()/mono.NC() are the permanent, unauthenticated DEFAULT-account
	// connection (see monolith.Monolith doc comment) — used only for
	// refdata-service's rpc.* calls, its REFDATA change stream, and the
	// obs.rpc.> observability bridge. The SHIPPING stream is deliberately
	// NOT created here anymore: Phase 13b moves it entirely into whichever
	// tenant account is active, via Handlers.SwitchTenant below.
	refdata := refdataconsumer.New(mono.NC())
	shipRepo := postgres.NewRepository(mono.DB())
	containerRepo := postgres.NewContainerRepository(mono.DB())
	portRepo := postgres.NewPortRepository(mono.DB())

	handlers := rest.NewHandlers(rest.Deps{
		Ports:         commands.NewPortHandler(portRepo),
		Refdata:       refdata,
		DefaultJS:     mono.JS(),
		NC:            mono.NC(),
		Log:           log,
		ShipRepo:      shipRepo,
		ContainerRepo: containerRepo,
		PortRepo:      portRepo,
		NatsURL:       mono.NatsURL(),
		TenantCreds:   tenantCredentials,
	})

	// The initial tenant connect and every later switch are the same code
	// path — see SwitchTenant's doc comment for why that's deliberate.
	if err := handlers.SwitchTenant(ctx, initialTenant); err != nil {
		return err
	}

	handlers.Mount(mono.Mux())
	return nil
}
