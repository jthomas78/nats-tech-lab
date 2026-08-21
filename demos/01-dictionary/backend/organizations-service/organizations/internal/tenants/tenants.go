// Package tenants wires organizations-service's per-tenant resource — a
// refdataclient.Client for BR-TP14 validation, plus a browserrpc.Adapter
// mounted in a second pass — onto shared/natstenants' connection-lifecycle
// machinery (Phase 35). The two-pass wiring exists because
// composition.Startup needs this package's Manager to satisfy
// domain.VehicleTypeValidator before the command handlers it needs for the
// adapter exist yet: connections come up first (MountTenants, so refdata
// validation works), then MountAPI backfills adapters onto them, using
// shared/natstenants.Manager's Update method for tenants already connected
// and a captured apiDeps pointer (read by the provision callback) for any
// tenant that connects afterward.
package tenants

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"sync"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/internal/application/commands"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/internal/browserrpc"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/internal/domain"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/internal/objectstore"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/internal/refdataclient"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/internal/secrets"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/transporterprofile"
	profileactivities "github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/transporterprofile/activities"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/transporterprofile/orchestration"
	"github.com/jthomas78/nats-tech-lab/shared/natstenants"
)

// resource is one tenant's connection-scoped state. adapter is nil until
// MountAPI has run for that tenant — see Manager's doc comment.
type resource struct {
	client   *refdataclient.Client
	adapter  *browserrpc.Adapter
	profiles *transporterprofile.Runtime
	// docs is this tenant's compliance-document Object Store bucket
	// (Phase 38c-ii). Per-tenant like everything else here: the bucket is a
	// JetStream stream inside the tenant's own account, so the account
	// boundary is the isolation — there is no cross-tenant bucket to guard.
	docs *objectstore.Store
	// creds is this tenant's sealed tracking-credential bucket (BR-TP52).
	// nil when no encryption key is configured: the store fails closed
	// rather than storing secrets in the clear, so the feature is simply
	// unavailable instead of quietly insecure.
	creds *secrets.Store
}

// Manager implements domain.VehicleTypeValidator directly (BR-TP14) — it's
// the natural owner of "which tenant's connection to use," so there's no
// separate wrapper type.
var _ domain.VehicleTypeValidator = (*Manager)(nil)
var _ domain.OperatingAreaResolver = (*Manager)(nil)

// Manager wraps shared/natstenants.Manager with this service's resource
// shape and two-pass adapter wiring.
type Manager struct {
	mgr *natstenants.Manager[*resource]

	// secretKey seals tracking-credential payloads (BR-TP52). Empty disables
	// the feature; it is never a reason to store a secret unsealed.
	secretKey []byte

	mu      sync.RWMutex
	apiDeps *browserrpc.Deps // nil until MountAPI has run
}

func NewManager(natsURL, credsDir string, db *sql.DB, log *slog.Logger, secretKey []byte) *Manager {
	m := &Manager{secretKey: secretKey}
	m.mgr = natstenants.NewManager(natsURL, credsDir, "organizations-service", log,
		func(ctx context.Context, nc *nats.Conn, tenant string) (*resource, error) {
			profiles, err := transporterprofile.Start(ctx, nc, db)
			if err != nil {
				return nil, err
			}
			js, err := jetstream.New(nc)
			if err != nil {
				profiles.Close()
				return nil, err
			}
			docs, err := objectstore.New(ctx, js)
			if err != nil {
				profiles.Close()
				return nil, err
			}
			// BR-TP52: only opened when a key is configured. An absent key
			// disables the feature; it never downgrades to plaintext.
			var credStore *secrets.Store
			if len(m.secretKey) > 0 {
				credStore, err = secrets.New(ctx, js, m.secretKey)
				if err != nil {
					profiles.Close()
					return nil, err
				}
			}
			res := &resource{client: refdataclient.New(nc), profiles: profiles, docs: docs, creds: credStore}
			m.mu.RLock()
			deps := m.apiDeps
			m.mu.RUnlock()
			// Only if MountAPI has already run — a tenant connecting before
			// that carries no api.* adapter until MountAPI's Update pass
			// backfills it below.
			if deps != nil {
				scoped := *deps
				scoped.Tenant = tenant
				scoped.ProfileProjection = profiles.Projection
				scoped.ProfileCommands = profiles.Commands
				if scoped.VettingFactory != nil {
					scoped.Vetting = scoped.VettingFactory(tenant, profiles.Commands)
				}
				adapter, err := browserrpc.New(nc, scoped)
				if err != nil {
					profiles.Close()
					return nil, err
				}
				res.adapter = adapter
			}
			return res, nil
		},
		func(_ string, res *resource) error {
			defer res.profiles.Close()
			if res.adapter != nil {
				return res.adapter.Stop()
			}
			return nil
		},
	)
	return m
}

// EnsureAll connects to every tenant currently discoverable in credsDir —
// see shared/natstenants.Manager.EnsureAll.
func (m *Manager) EnsureAll(ctx context.Context) error {
	return m.mgr.EnsureAll(ctx)
}

// ErrTenantNotConnected is returned when Exists is called for a tenant this
// Manager has no live connection for (an unknown or not-yet-discovered
// .creds file).
var ErrTenantNotConnected = fmt.Errorf("tenant is not connected")

// Exists implements domain.VehicleTypeValidator (BR-TP14) by resolving
// tenant's own connection and delegating to its refdataclient.Client.
func (m *Manager) Exists(ctx context.Context, tenant, contextKey, code string) (bool, error) {
	res, ok := m.mgr.Resource(tenant)
	if !ok {
		return false, ErrTenantNotConnected
	}
	return res.client.Exists(ctx, contextKey, code)
}

// ResolveArea implements domain.OperatingAreaResolver (BR-TP47) — the same
// "this Manager owns which tenant's connection to use" role it plays for
// BR-TP14's Exists. No contextKey parameter: region/country corpora are
// platform-scoped standards data (see refdataclient's platformContext).
func (m *Manager) ResolveArea(ctx context.Context, tenant string, level domain.AreaLevel, code string) (string, bool, error) {
	res, ok := m.mgr.Resource(tenant)
	if !ok {
		return "", false, ErrTenantNotConnected
	}
	return res.client.ResolveArea(ctx, level, code)
}

// DocumentStore implements commands.ObjectStoreResolver (Phase 38c-ii) — the
// same "this Manager owns which tenant's connection to use" role it already
// plays for BR-TP14's Exists.
//
// It returns ErrTenantNotConnected rather than lazily connecting: a tenant
// whose credential this service has never seen must not get a bucket created
// on the strength of a redeemed ticket naming it.
func (m *Manager) DocumentStore(tenant string) (commands.DocumentObjectStore, error) {
	res, ok := m.mgr.Resource(tenant)
	if !ok {
		return nil, ErrTenantNotConnected
	}
	return res.docs, nil
}

// SecretStore implements commands.SecretStoreResolver (BR-TP52).
func (m *Manager) SecretStore(tenant string) (commands.SecretStore, error) {
	res, ok := m.mgr.Resource(tenant)
	if !ok {
		return nil, ErrTenantNotConnected
	}
	if res.creds == nil {
		return nil, secrets.ErrNoEncryptionKey
	}
	return res.creds, nil
}

// ProfileCommands implements commands.ProfileEventAppenderResolver
// (BR-TP55) — the TRANSPORTER stream is inside the tenant's own account, so
// the appender is per-tenant like every other resource here.
func (m *Manager) ProfileCommands(tenant string) (commands.ProfileEventAppender, error) {
	res, ok := m.mgr.Resource(tenant)
	if !ok {
		return nil, ErrTenantNotConnected
	}
	return res.profiles.Commands, nil
}

// ProfileEvents implements activities.PublisherResolver (BR-TP58) — the
// Temporal worker is a single process polling one constant task queue, so
// its activities must pick the connection per invocation from the Tenant on
// their own input. Same shape and same fail-closed behaviour as the
// DocumentStore/SecretStore/ProfileCommands resolvers above: a tenant this
// service holds no credential for is an error, never a fallback to whichever
// connection happens to be at hand.
func (m *Manager) ProfileEvents(tenant string) (profileactivities.WorkflowEventPublisher, error) {
	res, ok := m.mgr.Resource(tenant)
	if !ok {
		return nil, ErrTenantNotConnected
	}
	return res.profiles.Events, nil
}

// ProfileStore exposes one tenant's event store for BR-TP28's drop handler,
// which needs Hydrate/Append rather than ProfileEvents' append-only publisher
// view. Same fail-closed contract as every other resolver here.
func (m *Manager) ProfileStore(tenant string) (orchestration.EventStore, error) {
	res, ok := m.mgr.Resource(tenant)
	if !ok {
		return nil, ErrTenantNotConnected
	}
	return res.profiles.Events, nil
}

// MountAPI registers the api.* adapter on every currently-connected tenant,
// and arms every tenant that connects afterward to get one too (the
// provision closure above reads apiDeps). Called once, after
// composition.Startup has built the command handlers deps carries.
// deps.Tenant is ignored — it's filled in per connection.
func (m *Manager) MountAPI(deps browserrpc.Deps) error {
	m.mu.Lock()
	m.apiDeps = &deps
	m.mu.Unlock()

	var firstErr error
	m.mgr.Range(func(tenant string, _ *nats.Conn, res *resource) {
		if res.adapter != nil {
			return
		}
		err := m.mgr.Update(tenant, func(nc *nats.Conn, cur *resource) (*resource, error) {
			if cur.adapter != nil {
				return cur, nil
			}
			scoped := deps
			scoped.Tenant = tenant
			scoped.ProfileProjection = cur.profiles.Projection
			scoped.ProfileCommands = cur.profiles.Commands
			if scoped.VettingFactory != nil {
				scoped.Vetting = scoped.VettingFactory(tenant, cur.profiles.Commands)
			}
			adapter, err := browserrpc.New(nc, scoped)
			if err != nil {
				return cur, err
			}
			cur.adapter = adapter
			return cur, nil
		})
		if err != nil && firstErr == nil {
			firstErr = fmt.Errorf("mount api adapter for tenant %q: %w", tenant, err)
		}
	})
	return firstErr
}

// Close closes every tenant's connection — called on process shutdown.
func (m *Manager) Close() {
	m.mgr.Close()
}
