// Package dictionary is the composition root of the shipping module: it
// wires the domain, application, and adapter layers into the monolith.
package dictionary

import (
	"context"
	"encoding/json"

	"github.com/nats-io/nats.go"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/application/commands"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/postgres"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/rest"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/internal/monolith"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/internal/refdataconsumer"
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

	// mono.JS()/mono.NC() are the permanent DEFAULT-account connection (see
	// monolith.Monolith doc comment) — used only for
	// refdata-service's rpc.* calls, its REFDATA change stream, and the
	// obs.rpc.> observability bridge. The SHIPPING stream is deliberately
	// NOT created here anymore: Phase 13b moves it entirely into whichever
	// tenant account is active, via Handlers.SwitchTenant below.
	refdata := refdataconsumer.New(mono.NC())
	shipRepo := postgres.NewRepository(mono.DB())
	containerRepo := postgres.NewContainerRepository(mono.DB())
	portRepo := postgres.NewPortRepository(mono.DB())

	handlers := rest.NewHandlers(rest.Deps{
		Ports:          commands.NewPortHandler(portRepo),
		Refdata:        refdata,
		DefaultJS:      mono.JS(),
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

	// BR-030: react to a tenant minted by accounts-service *after* this
	// process started, instead of leaving it unprovisioned until an operator
	// happens to switch the Admin UI to it (EnsureAllTenants above only
	// covers tenants that already existed at startup) — see
	// Handlers.EnsureTenantByName's doc comment.
	if _, err := mono.NC().Subscribe("notify.accounts.account.created", func(msg *nats.Msg) {
		var evt struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(msg.Data, &evt); err != nil || evt.Name == "" {
			log.Error("decode notify.accounts.account.created", "err", err)
			return
		}
		if err := handlers.EnsureTenantByName(ctx, evt.Name); err != nil {
			log.Error("ensure tenant resources on provisioning event", "tenant", evt.Name, "err", err)
		}
	}); err != nil {
		return err
	}

	// BR-031: the mirror of BR-030 above — react to a tenant being suspended
	// by tearing its resources down, instead of leaving its per-tenant
	// connection to reconnect forever against a .creds file
	// accounts-service's suspendAccount has already deleted (see
	// ARCHITECTURE-ACCOUNTS.md § 2t-a). Producer is accounts-service's
	// Handlers.publishAccountSuspended (BR-AC09).
	if _, err := mono.NC().Subscribe("notify.accounts.account.suspended", func(msg *nats.Msg) {
		var evt struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(msg.Data, &evt); err != nil || evt.Name == "" {
			log.Error("decode notify.accounts.account.suspended", "err", err)
			return
		}
		if err := handlers.TeardownTenantByName(ctx, evt.Name); err != nil {
			log.Error("tear down tenant resources on suspension event", "tenant", evt.Name, "err", err)
		}
	}); err != nil {
		return err
	}

	// BR-032: completes the lifecycle triple. Without this, BR-031's teardown
	// is a one-way door — a reactivated tenant would stay unusable until this
	// process restarted or an operator switched the Admin UI to it. Reuses
	// EnsureTenantByName unchanged: the teardown removed the tenant from
	// TenantResources, so this rebuilds it from scratch against the fresh
	// .creds file reactivation just wrote (BR-AC04/BR-AC10 — the old creds are
	// deleted on suspend and never reused).
	if _, err := mono.NC().Subscribe("notify.accounts.account.reactivated", func(msg *nats.Msg) {
		var evt struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(msg.Data, &evt); err != nil || evt.Name == "" {
			log.Error("decode notify.accounts.account.reactivated", "err", err)
			return
		}
		if err := handlers.EnsureTenantByName(ctx, evt.Name); err != nil {
			log.Error("ensure tenant resources on reactivation event", "tenant", evt.Name, "err", err)
		}
	}); err != nil {
		return err
	}

	handlers.Mount(mono.Mux())
	return nil
}
