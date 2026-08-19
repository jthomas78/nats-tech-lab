// Command main bootstraps accounts-service (Phase 14b): the dynamic
// tenant-provisioning service that mints and revokes NATS accounts at
// runtime via decentralized JWTs, replacing nats/bootstrap-operator.sh's
// one-shot nsc invocation (Phase 14a) with a live API.
//
// @title           Accounts Service API
// @version         1.0
// @description     Dynamic NATS tenant/account provisioning (create/suspend/reactivate, JetStream limits, business units) plus browser NATS credential minting, for the NATS Tech Lab POC.
// @host            localhost:7202
// @BasePath        /
package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/nats-io/jwt/v2"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nkeys"
	httpSwagger "github.com/swaggo/http-swagger"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/accounts-service/accounts"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/accounts-service/auth"
	_ "github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/accounts-service/docs"
	"github.com/jthomas78/nats-tech-lab/shared/natstrace"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	if err := run(log); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	databaseURL := envOr("DATABASE_URL", "postgres://accounts:accounts@localhost:5434/accounts?sslmode=disable")
	natsURL := envOr("NATS_URL", nats.DefaultURL)
	natsCredsPath := envOr("NATS_CREDS_PATH", "")                  // sys.creds — $SYS.REQ.CLAIMS.* is only reachable authenticated as SYS
	natsPlatformCredsPath := envOr("NATS_PLATFORM_CREDS_PATH", "") // platform.creds — Phase 16h: publishes notify.accounts.account.created for shipping-service's PLATFORM-account subscriber (BR-030); optional, publish is skipped if unset
	operatorSigningKeyFile := envOr("OPERATOR_SIGNING_KEY_FILE", "")
	credsDir := envOr("NATS_CREDS_DIR", "") // shared volume shipping-service also mounts
	resolverSeedDir := envOr("RESOLVER_SEED_DIR", "")
	// BR-AC19 — bootstrap-operator.sh's per-account signing key seeds, so the
	// seeded accounts keep a stable identity across a `docker compose down -v`
	// instead of being handed a fresh random one on each wiped boot.
	accountKeysDir := envOr("ACCOUNT_SIGNING_KEYS_DIR", "")
	authSecret := envOr("ACCOUNTS_AUTH_SECRET", "")
	natsMonitorURL := envOr("NATS_MONITOR_URL", "")
	// Phase 19 — folded in from auth-service: the address the browser
	// itself should dial for its NATS WebSocket connection, returned
	// verbatim in connectInfo. Not the in-cluster `nats:9222` hostname,
	// since the browser resolves DNS from the host, not the backend
	// network.
	natsWSUrl := envOr("NATS_WS_URL", "ws://localhost:9222")
	httpAddr := envOr("HTTP_ADDR", ":8080")
	// Phase 22: base URL of refdata-service for BU context registration.
	// Optional: if unset, BU endpoints still persist locally but skip refdata sync.
	refdataURL := envOr("REFDATA_URL", "")

	if operatorSigningKeyFile == "" {
		return errors.New("OPERATOR_SIGNING_KEY_FILE is required")
	}
	if authSecret == "" {
		return errors.New("ACCOUNTS_AUTH_SECRET is required")
	}

	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return err
	}
	defer db.Close()

	startupCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	if err := waitForPostgres(startupCtx, db); err != nil {
		return err
	}
	if err := accounts.Migrate(ctx, db); err != nil {
		return err
	}

	var sysNC *nats.Conn
	sysOpts := []nats.Option{nats.Name("accounts-service")}
	if natsCredsPath != "" {
		sysOpts = append(sysOpts, nats.UserCredentials(natsCredsPath))
	}
	if err := waitForNATS(startupCtx, natsURL, sysOpts, func(conn *nats.Conn) error {
		sysNC = conn
		return nil
	}); err != nil {
		return err
	}
	defer sysNC.Drain() //nolint:errcheck

	// Phase 16h — a second connection, on the PLATFORM account, used only to
	// publish notify.accounts.account.created (accounts.Handlers.NotifyNC) —
	// deliberately not sysNC, since shipping-service's subscriber
	// (composition.go) listens on its own PLATFORM-account connection and
	// core NATS pub/sub never crosses an account boundary. Optional: if
	// unset, accounts-service still runs, it just can't notify shipping-service
	// reactively (EnsureAllTenants at startup / an Admin UI SwitchTenant
	// remain the fallback paths — see EnsureTenantByName's doc comment).
	var platformNC *nats.Conn
	if natsPlatformCredsPath != "" {
		platformOpts := []nats.Option{nats.Name("accounts-service-platform")}
		platformOpts = append(platformOpts, nats.UserCredentials(natsPlatformCredsPath))
		if err := waitForNATS(startupCtx, natsURL, platformOpts, func(conn *nats.Conn) error {
			platformNC = conn
			return nil
		}); err != nil {
			return err
		}
		defer platformNC.Drain() //nolint:errcheck
	} else {
		log.Warn("NATS_PLATFORM_CREDS_PATH not set — accounts-service cannot notify shipping-service when a tenant is created; EnsureAllTenants/SwitchTenant remain the only ways a new tenant's resources get provisioned")
	}

	// Phase 17c — see accounts.RegisterMicroService's doc comment for why
	// this is a call into the accounts package rather than an inline
	// micro.AddService here: that's what makes the registration testable.
	accountsSvc, err := accounts.RegisterMicroService(sysNC)
	if err != nil {
		return err
	}
	defer accountsSvc.Stop() //nolint:errcheck

	operatorSigningKeySeed, err := os.ReadFile(operatorSigningKeyFile)
	if err != nil {
		return err
	}
	provisioner, err := accounts.NewProvisioner(operatorSigningKeySeed, sysNC)
	if err != nil {
		return err
	}

	store := accounts.NewStore(db)
	if resolverSeedDir != "" {
		if err := seedPreexistingAccounts(ctx, store, provisioner, resolverSeedDir, accountKeysDir, refdataURL, log); err != nil {
			return err
		}
	}
	// Phase 22: seed demo BUs for the pre-existing acme account idempotently.
	if err := seedDemoBusinessUnits(ctx, store, refdataURL, log); err != nil {
		log.Warn("seed demo business units", "err", err)
	}

	auditLog := accounts.NewAuditLog(db)
	// Phase 28e: the same PLATFORM connection notify.accounts.* already
	// publishes on (BR-036 requires obs.trace.* publish to the PLATFORM
	// account only) — natstrace.New(nil) is safe if platformNC is unset
	// (NATS_PLATFORM_CREDS_PATH not configured): Span.finish() is
	// panic-recover-wrapped, so a nil connection just means spans silently
	// don't publish, the same "optional, degrades gracefully" contract
	// platformNC already has for notify.accounts.*.
	tracer := natstrace.New(platformNC)
	handlers := accounts.NewHandlers(store, provisioner, credsDir, log, platformNC, auditLog)
	if natsMonitorURL != "" {
		handlers.UsageFetcher = accounts.NewUsageFetcher(natsMonitorURL, store)
	}
	handlers.RefdataURL = refdataURL
	mux := http.NewServeMux()
	handlers.Mount(mux, authSecret)

	// Phase 19 — auth-service folded into this binary: same Store, same
	// Postgres pool, no more cross-service read. Routes are ungated (see
	// auth.Handlers.connectInfo's doc comment for why) and registered on
	// the same mux the accounts routes above already gate with BasicAuth.
	authHandlers := auth.NewHandlers(store, natsWSUrl, log)
	authHandlers.Mount(mux)

	// Swagger UI/spec for both handler packages' routes above — mounted here
	// rather than inside either Handlers.Mount, since neither accounts.Handlers
	// nor auth.Handlers owns the whole API surface on its own.
	mux.Handle("/swagger/", httpSwagger.WrapHandler)

	// Phase 28e — one http.Handler decorator wrapping the whole mux covers
	// every accounts/auth REST endpoint, symmetric to the other four
	// services' micro.Handler Middleware wrapping every svc.AddEndpoint call.
	server := &http.Server{Addr: httpAddr, Handler: tracer.HTTPMiddleware("_platform", "accounts", mux)}
	errCh := make(chan error, 1)
	go func() {
		log.Info("accounts-service: http server listening", "addr", httpAddr)
		errCh <- server.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		log.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil && !errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		return nil
	}
}

// seedPreexistingAccounts registers nats/bootstrap-operator.sh's
// default/acme/globex accounts (Phase 14a) in Postgres if not already
// present, decoding each account's public key straight from its resolver
// JWT so the list this service serves is complete from first boot — without
// re-minting or touching their JWTs, which this service does not have the
// signing keys for (only the operator's own signing key, not each
// individual account's).
//
// Name is the account's lowercase tenant identity — the same string used by
// its .creds filename and every downstream NATS subject/KV-bucket
// (shipping-service has never known these accounts by any other casing).
// LegacyName is the uppercase nsc account name bootstrap-operator.sh
// actually gave them (also the resolver JWT's filename stem, via File) —
// this service originally seeded Postgres rows under that same uppercase
// name (a 2026-07-28 mistake: nsc/JWT account-naming convention leaking into
// Postgres's tenant-identity column), so RenameIfExists below migrates any
// existing uppercase row to its lowercase identity before SeedIfMissing
// runs. Both steps are safe to run on every startup.
func seedPreexistingAccounts(ctx context.Context, store *accounts.Store, provisioner *accounts.Provisioner, resolverSeedDir, accountKeysDir, refdataURL string, log *slog.Logger) error {
	seeds := []struct {
		Name       string
		LegacyName string
		File       string
		Limits     accounts.JSLimits
	}{
		{"platform", "PLATFORM", "PLATFORM.jwt", accounts.JSLimits{MaxMem: 1 << 30, MaxFile: 5 << 30, MaxStreams: 20, MaxConsumers: 100}},
		{"acme", "ACME", "ACME.jwt", accounts.JSLimits{MaxMem: 256 << 20, MaxFile: 1 << 30, MaxStreams: 10, MaxConsumers: 20}},
		{"globex", "GLOBEX", "GLOBEX.jwt", accounts.JSLimits{MaxMem: 256 << 20, MaxFile: 1 << 30, MaxStreams: 10, MaxConsumers: 20}},
	}
	for _, s := range seeds {
		raw, err := os.ReadFile(resolverSeedDir + "/" + s.File)
		if err != nil {
			log.Warn("seed account: could not read resolver JWT, skipping", "name", s.Name, "err", err)
			continue
		}
		claims, err := jwt.DecodeAccountClaims(string(raw))
		if err != nil {
			return err
		}
		// BR-AC19: prefer the signing key bootstrap-operator.sh exported for
		// this account, verified against the resolver JWT just decoded. Empty
		// when the stack predates that export, in which case ensureSigningKey
		// falls back to minting one.
		bootstrapSeed, err := accounts.ResolveSeededSigningKey(accountKeysDir, s.Name, claims)
		if err != nil {
			return err
		}
		if err := store.RenameIfExists(ctx, s.LegacyName, s.Name); err != nil {
			return err
		}
		if err := store.SeedIfMissing(ctx, accounts.Account{
			Name:           s.Name,
			PublicKey:      claims.Subject,
			SigningKeySeed: bootstrapSeed,
			Status:         accounts.StatusActive,
			JSMaxMem:       s.Limits.MaxMem,
			JSMaxFile:      s.Limits.MaxFile,
			JSMaxStreams:   s.Limits.MaxStreams,
			JSMaxConsumers: s.Limits.MaxConsumers,
		}); err != nil {
			return err
		}
		if err := ensureSigningKey(ctx, store, provisioner, s.Name, s.Limits, claims, bootstrapSeed, log); err != nil {
			return err
		}
		// BR-AC16/BR-AC28: createAccount auto-creates {tenant}-default for every
		// newly registered tenant, but this seeding path inserts rows directly
		// via SeedIfMissing and never goes through that handler — replay the
		// same invariant here so acme/globex end up with the same guarantee a
		// freshly-registered account gets. platform is exempt: it's a reserved
		// account with no business-unit concept (BR-AC06), matching the Admin
		// UI's "Reserved accounts have no business units" treatment.
		if s.Name != "platform" {
			acc, err := store.Get(ctx, s.Name)
			if err != nil {
				return err
			}
			slug := accounts.DefaultContext(s.Name)
			if err := store.InsertBusinessUnitIfMissing(ctx, accounts.NewBusinessUnit{
				AccountID: acc.ID,
				Name:      accounts.DefaultBUName,
				Context:   slug,
				Visible:   true,
				IsDefault: true,
			}); err != nil {
				return fmt.Errorf("seed default BU for %q: %w", s.Name, err)
			}
			// BR-AC29: best-effort, like every other refdata call from this
			// process — a cold refdata-service should delay seeding of its own
			// data, not fail accounts-service's.
			refdata := &accounts.RefdataClient{BaseURL: refdataURL, Log: log}
			if err := refdata.ProvisionDefaultContext(ctx, s.Name, slug); err != nil {
				log.Warn("provision default BU context in refdata", "account", s.Name, "context", slug, "err", err)
			}
		}
	}
	return seedSysAccountForDisplay(ctx, store, resolverSeedDir, log)
}

// seedSysAccountForDisplay stores a Postgres row for the SYS account —
// decoded from the same resolver JWT as the other seeds above — purely so
// the Admin UI's Accounts panel can resolve its public key to the name
// "sys" instead of showing a raw, unlabeled NKey. Deliberately NOT run
// through the seeds loop above / ensureSigningKey: this repo's whole
// design keeps SYS credentials to a single static sys.creds file this
// service holds for itself (see BUSINESS_RULES-ACCOUNTS.md and
// ARCHITECTURE-ACCOUNTS.md's "NATS operator-mode trust chain") — nothing
// else may ever authenticate as SYS. Establishing a signing key here would
// make this service able to mint additional SYS-account users on demand,
// which is a real widening of authority (SYS controls $SYS.REQ.CLAIMS.*
// for every tenant) that nothing in this system needs or should have.
// reservedAccountNames (BR-AC06) already blocks "sys" from creation,
// suspension, or tenant-switching through the REST API regardless of this
// row's presence, so adding it here only affects display, never behavior.
func seedSysAccountForDisplay(ctx context.Context, store *accounts.Store, resolverSeedDir string, log *slog.Logger) error {
	raw, err := os.ReadFile(resolverSeedDir + "/SYS.jwt")
	if err != nil {
		log.Warn("seed sys account: could not read resolver JWT, skipping", "err", err)
		return nil
	}
	claims, err := jwt.DecodeAccountClaims(string(raw))
	if err != nil {
		return err
	}
	return store.SeedIfMissing(ctx, accounts.Account{
		Name:      "sys",
		PublicKey: claims.Subject,
		Status:    accounts.StatusActive,
		// SYS runs no JetStream workload of its own (bootstrap-operator.sh
		// never assigns it JS limits — confirmed zero in the decoded JWT).
	})
}

// ensureSigningKey establishes a signing key for a seeded pre-existing
// account (Phase 15c) if it doesn't already have one on record — needed
// because GET /api/auth/connectInfo (auth/handler.go, folded into this
// service in Phase 19) mints browser user JWTs by loading an account's
// SigningKeySeed from this same service's own Store (see
// accounts/store.go's Account.SigningKeySeed doc comment).
//
// bootstrapSeed (BR-AC19) is the key bootstrap-operator.sh exported for this
// account, already verified against its resolver JWT. When present it is
// simply adopted: the resolver trusts it by construction, so there is
// nothing to re-sign or push, and the account's identity stays byte-stable
// across a `docker compose down -v`.
//
// The generate-and-push path below is the fallback for a stack bootstrapped
// before that export existed. It re-signs the account's claims — safe on an
// active account (Provisioner.ReactivateAccount doesn't check status, only
// the REST handler does), and since BR-AC19 the re-sign preserves any
// signing keys already on the claim rather than replacing them.
//
// **This is the 2026-08-06 incident (BR-AC19).** The generated-key path used
// to replace the account's whole signing key list, on the assumption — stated
// in this comment until now — that every shipped .creds file was signed by
// the account's identity key and therefore unaffected. That held until
// BR-AC04's reactivation rewrote globex.creds as a *signing-key*-signed
// credential, after which each wiped-Postgres boot minted a fresh random key,
// dropped the one globex.creds was signed by, and left the tenant unable to
// connect at all ("authorization violation").
//
// Runs at most once per account: a signing key, once established and
// persisted, is never rotated on a later restart (the Postgres row already
// has one, so this is a no-op) — rotating it would invalidate any browser
// JWT minted against the previous key that's still within its TTL.
func ensureSigningKey(ctx context.Context, store *accounts.Store, provisioner *accounts.Provisioner, name string, limits accounts.JSLimits, prior *jwt.AccountClaims, bootstrapSeed string, log *slog.Logger) error {
	acc, err := store.Get(ctx, name)
	if err != nil {
		return fmt.Errorf("reload seeded account %q: %w", name, err)
	}
	if acc.SigningKeySeed != "" {
		return nil
	}
	if bootstrapSeed != "" {
		if err := store.SetSigningKeySeed(ctx, name, bootstrapSeed); err != nil {
			return fmt.Errorf("persist bootstrap signing key for %q: %w", name, err)
		}
		log.Info("adopted bootstrap signing key for seeded account", "name", name)
		return nil
	}

	signingKP, err := nkeys.CreateAccount()
	if err != nil {
		return fmt.Errorf("generate signing key for %q: %w", name, err)
	}
	seed, err := signingKP.Seed()
	if err != nil {
		return fmt.Errorf("read generated signing key seed for %q: %w", name, err)
	}

	// The resolver JWT was decoded by seedPreexistingAccounts above. Pass its
	// import/export declarations through the re-sign so establishing a browser
	// signing key cannot erase Phase 21 account-import wiring.
	if err := provisioner.ReactivateAccount(ctx, acc.PublicKey, string(seed), limits, accounts.CrossAccountOpts{}, prior); err != nil {
		return fmt.Errorf("establish signing key for %q: %w", name, err)
	}
	if err := store.SetSigningKeySeed(ctx, name, string(seed)); err != nil {
		return fmt.Errorf("persist signing key for %q: %w", name, err)
	}
	log.Info("established signing key for seeded account", "name", name)
	return nil
}

func waitForPostgres(ctx context.Context, db *sql.DB) error {
	for {
		if err := db.PingContext(ctx); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

func waitForNATS(ctx context.Context, url string, opts []nats.Option, connect func(*nats.Conn) error) error {
	for {
		conn, err := nats.Connect(url, opts...)
		if err == nil {
			return connect(conn)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// demoBusinessUnits are acme's two seeded real business units — a display
// name plus the exact slug Phase 22 hand-seeded, reproduced via DeriveContext
// rather than a literal so the derivation function is exercised by the one
// piece of demo data that predates it.
var demoBusinessUnits = []struct{ Name string }{
	{"Pacific Fleet"},
	{"Atlantic Fleet"},
}

// seedDemoBusinessUnits idempotently registers acme's two demo business units
// (BR-AC26: a display name plus its own immutable context slug) and calls
// refdata-service to register the matching contexts under _platform. Called at
// every startup so a freshly-migrated DB always ends up with the demo data.
func seedDemoBusinessUnits(ctx context.Context, store *accounts.Store, refdataURL string, log *slog.Logger) error {
	acme, err := store.Get(ctx, "acme")
	if err != nil {
		// acme may not exist yet on the first boot of a brand-new deployment.
		log.Warn("seed demo BUs: acme account not found, skipping", "err", err)
		return nil
	}

	refdata := &accounts.RefdataClient{BaseURL: refdataURL, Log: log}
	// Same cold-start race ProvisionDefaultContext guards against: refdata-service
	// is a separate container with no ordering guarantee relative to this one, so
	// the very first call here can hit "connection refused" as easily as
	// "_platform not found yet". Best-effort — logged, not fatal, like every
	// other refdata call this process makes.
	if err := refdata.WaitForPublishedAncestor(ctx, accounts.PlatformContext); err != nil {
		log.Warn("seed demo BUs: refdata-service not ready", "err", err)
	}
	for _, bu := range demoBusinessUnits {
		slug := accounts.DeriveContext("acme", bu.Name)
		if err := store.InsertBusinessUnitIfMissing(ctx, accounts.NewBusinessUnit{
			AccountID: acme.ID,
			Name:      bu.Name,
			Context:   slug,
			Visible:   true,
		}); err != nil {
			return fmt.Errorf("seed BU %q: %w", bu.Name, err)
		}
		if err := refdata.RegisterContext(ctx, accounts.ContextRegistration{
			Context: slug,
			Parent:  accounts.PlatformContext,
			Name:    bu.Name,
			Tenant:  "acme",
		}); err != nil {
			log.Warn("seed BU: register context in refdata", "name", bu.Name, "context", slug, "err", err)
		}
	}

	// BR-AC17: acme now has real BUs, so replay the same hide step the Admin
	// UI prompts an operator to take once their first real BU exists —
	// otherwise acme would show its default as a permanently-visible row
	// that a normal registration-then-add-BU flow would have hidden.
	defaultSlug := accounts.DefaultContext("acme")
	if err := store.SetBusinessUnitVisible(ctx, "acme", defaultSlug, false); err != nil && !errors.Is(err, accounts.ErrBUNotFound) {
		return fmt.Errorf("hide default BU for acme: %w", err)
	}
	return nil
}
