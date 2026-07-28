package rest

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sort"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/application/commands"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/application/queries"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/domain"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/dictionary/internal/eventhandler"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/internal/jstream"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/internal/kvstore"
)

// SwitchTenant connects a fresh, tenant-credentialed NATS connection and
// rebuilds every tenant-scoped resource against it — JetStream context, the
// four KV stores, the four durable projectors, and the ship/container
// command and query handlers — then swaps them all into Deps atomically via
// SetDeps.
//
// This is deliberately the *same* code path used for the initial connect at
// Startup and for every later switch (composition.go calls it once with the
// initial tenant before Mount) — there is no separate bootstrap case to keep
// in sync.
//
// Per Main-POC-Plan.md Phase 13b: the four projector durables are
// server-side state that outlives its client (NATS docs: durables "remain
// even when there are periods of inactivity"), so this only stops the old
// client-side Consume() loops — it never deletes or recreates a durable, and
// no stream position is lost switching back to a previously active tenant.
// The memoized kvstore.Store bucket handles are never reused across a
// switch — kvstore.New always starts a fresh instance — so a cached handle
// from the old account can never leak into the new one.
func (h *Handlers) SwitchTenant(ctx context.Context, tenant string) error {
	prev := h.deps()
	creds, ok := prev.TenantCreds[tenant]
	if !ok {
		return fmt.Errorf("unknown tenant %q", tenant)
	}
	if tenant == prev.Tenant && prev.TenantNC != nil && !prev.TenantNC.IsClosed() {
		return nil // already active; not an error, just a no-op
	}

	// eventhandler.register() closes over this ctx for the *entire lifetime*
	// of each projector — every event it ever processes calls project(ctx, ...).
	// Startup's ctx is already long-lived (canceled only on SIGINT/SIGTERM), but
	// SwitchTenant is also called with an HTTP request's r.Context() from
	// switchTenant below, which is canceled the instant that response is sent —
	// stripping cancellation (keeping any values) is required so a
	// REST-triggered switch doesn't leave every subsequent event failing with
	// "context canceled" and stuck redelivering forever.
	ctx = context.WithoutCancel(ctx)

	nc, err := nats.Connect(prev.NatsURL, nats.Name("shipping-service"), nats.UserInfo(creds.User, creds.Password))
	if err != nil {
		return fmt.Errorf("connect as tenant %q: %w", tenant, err)
	}

	js, err := jetstream.New(nc)
	if err != nil {
		nc.Close()
		return fmt.Errorf("jetstream context for tenant %q: %w", tenant, err)
	}
	if _, err := jstream.CreateStream(ctx, js, domain.StreamName, domain.StreamSubjects()); err != nil {
		nc.Close()
		return fmt.Errorf("create stream for tenant %q: %w", tenant, err)
	}

	kvA := kvstore.New(js, domain.ShapeABucketPrefix)
	kvB := kvstore.New(js, domain.ShapeBBucketPrefix)
	kvContainers := kvstore.New(js, domain.ContainerBucketPrefix)
	kvMeta := kvstore.New(js, domain.MetaBucketPrefix)

	projectors, err := registerProjectors(ctx, js, kvA, kvB, kvContainers, kvMeta, prev.ShipRepo, prev.ContainerRepo, prev.Log)
	if err != nil {
		nc.Close()
		return fmt.Errorf("register projectors for tenant %q: %w", tenant, err)
	}

	pub := jstream.NewPublisher(js)
	next := prev // copy: static fields (Ports, Refdata, DefaultJS, NC, repos, NatsURL, TenantCreds, Log) carry over unchanged
	next.Ships = commands.NewShipHandler(pub, js, prev.PortRepo)
	next.Containers = commands.NewContainerHandler(pub, js, prev.PortRepo)
	next.ShapeB = queries.NewShapeB(kvB, prev.ShipRepo)
	next.ShapeC = queries.NewShapeC(js)
	next.Terminal = queries.NewTerminal(kvContainers)
	next.Meta = queries.NewMeta(kvMeta)
	next.KVA, next.KVB, next.KVCont, next.KVMeta = kvA, kvB, kvContainers, kvMeta
	next.JS = js
	next.Tenant = tenant
	next.TenantNC = nc
	next.Projectors = projectors

	h.SetDeps(next)

	// Stop the OLD client-side subscriptions only — the durables themselves
	// are server-side state and are left exactly as they are.
	for _, cc := range prev.Projectors {
		cc.Stop()
	}
	if prev.TenantNC != nil {
		prev.TenantNC.Drain() //nolint:errcheck
	}
	return nil
}

// registerProjectors starts the four projector durables against js and
// returns their ConsumeContexts so a future switch can stop them cleanly.
func registerProjectors(
	ctx context.Context,
	js jetstream.JetStream,
	kvA, kvB, kvContainers, kvMeta *kvstore.Store,
	shipRepo domain.ShipRepository,
	containerRepo domain.ContainerRepository,
	log *slog.Logger,
) ([]jetstream.ConsumeContext, error) {
	var out []jetstream.ConsumeContext

	ccA, err := eventhandler.RegisterShapeA(ctx, js, kvA, log)
	if err != nil {
		return nil, err
	}
	out = append(out, ccA)

	ccB, err := eventhandler.RegisterShapeB(ctx, js, kvB, shipRepo, log)
	if err != nil {
		stopAll(out)
		return nil, err
	}
	out = append(out, ccB)

	ccCont, err := eventhandler.RegisterContainers(ctx, js, kvContainers, containerRepo, log)
	if err != nil {
		stopAll(out)
		return nil, err
	}
	out = append(out, ccCont)

	ccMeta, err := eventhandler.RegisterMeta(ctx, js, kvMeta, log)
	if err != nil {
		stopAll(out)
		return nil, err
	}
	out = append(out, ccMeta)

	return out, nil
}

func stopAll(ccs []jetstream.ConsumeContext) {
	for _, cc := range ccs {
		cc.Stop()
	}
}

// tenantResponse is what GET /api/tenant returns: the active tenant plus the
// known, switchable tenant list (sorted for a stable dropdown order).
type tenantResponse struct {
	Tenant    string   `json:"tenant"`
	Available []string `json:"available"`
}

// @Summary      Active tenant + switchable tenant list
// @Tags         tenant
// @Produce      json
// @Success      200  {object}  tenantResponse
// @Router       /api/tenant [get]
func (h *Handlers) getTenant(w http.ResponseWriter, r *http.Request) {
	deps := h.deps()
	available := make([]string, 0, len(deps.TenantCreds))
	for t := range deps.TenantCreds {
		available = append(available, t)
	}
	sort.Strings(available)
	writeJSON(w, http.StatusOK, tenantResponse{Tenant: deps.Tenant, Available: available})
}

type switchTenantRequest struct {
	Tenant string `json:"tenant"`
}

// @Summary      Switch the active tenant (Phase 13b)
// @Description  Reconnects shipping-service's tenant-scoped NATS connection under a different account and rebinds every ship/container resource to it. The previous tenant's data becomes unreachable because the server enforces the account boundary, not because of an application-level filter.
// @Tags         tenant
// @Accept       json
// @Produce      json
// @Param        request  body      switchTenantRequest  true  "target tenant"
// @Success      200      {object}  tenantResponse
// @Failure      400      {object}  errorResponse
// @Failure      500      {object}  errorResponse
// @Router       /api/tenant/switch [post]
func (h *Handlers) switchTenant(w http.ResponseWriter, r *http.Request) {
	var in switchTenantRequest
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Tenant == "" {
		writeError(w, http.StatusBadRequest, "tenant is required")
		return
	}
	if err := h.SwitchTenant(r.Context(), in.Tenant); err != nil {
		h.deps().Log.Error("switch tenant", "tenant", in.Tenant, "err", err)
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.getTenant(w, r)
}
