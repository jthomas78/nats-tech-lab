// Command main bootstraps accounts-service (Phase 14b): the dynamic
// tenant-provisioning service that mints and revokes NATS accounts at
// runtime via decentralized JWTs, replacing nats/bootstrap-operator.sh's
// one-shot nsc invocation (Phase 14a) with a live API.
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

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/accounts-service/accounts"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/accounts-service/auth"
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
	natsCredsPath := envOr("NATS_CREDS_PATH", "")                // sys.creds — $SYS.REQ.CLAIMS.* is only reachable authenticated as SYS
	natsPlatformCredsPath := envOr("NATS_PLATFORM_CREDS_PATH", "") // platform.creds — Phase 16h: publishes notify.accounts.account.created for shipping-service's PLATFORM-account subscriber (BR-030); optional, publish is skipped if unset
	operatorSigningKeyFile := envOr("OPERATOR_SIGNING_KEY_FILE", "")
	credsDir := envOr("NATS_CREDS_DIR", "") // shared volume shipping-service also mounts
	resolverSeedDir := envOr("RESOLVER_SEED_DIR", "")
	authSecret := envOr("ACCOUNTS_AUTH_SECRET", "")
	natsMonitorURL := envOr("NATS_MONITOR_URL", "")
	// Phase 19 — folded in from auth-service: the address the browser
	// itself should dial for its NATS WebSocket connection, returned
	// verbatim in connectInfo. Not the in-cluster `nats:9222` hostname,
	// since the browser resolves DNS from the host, not the backend
	// network.
	natsWSUrl := envOr("NATS_WS_URL", "ws://localhost:9222")
	httpAddr := envOr("HTTP_ADDR", ":8080")

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
		if err := seedPreexistingAccounts(ctx, store, provisioner, resolverSeedDir, log); err != nil {
			return err
		}
	}

	auditLog := accounts.NewAuditLog(db)
	handlers := accounts.NewHandlers(store, provisioner, credsDir, log, platformNC, auditLog)
	if natsMonitorURL != "" {
		handlers.UsageFetcher = accounts.NewUsageFetcher(natsMonitorURL, store)
	}
	mux := http.NewServeMux()
	handlers.Mount(mux, authSecret)

	// Phase 19 — auth-service folded into this binary: same Store, same
	// Postgres pool, no more cross-service read. Routes are ungated (see
	// auth.Handlers.connectInfo's doc comment for why) and registered on
	// the same mux the accounts routes above already gate with BasicAuth.
	authHandlers := auth.NewHandlers(store, natsWSUrl, log)
	authHandlers.Mount(mux)

	server := &http.Server{Addr: httpAddr, Handler: mux}
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
func seedPreexistingAccounts(ctx context.Context, store *accounts.Store, provisioner *accounts.Provisioner, resolverSeedDir string, log *slog.Logger) error {
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
		if err := store.RenameIfExists(ctx, s.LegacyName, s.Name); err != nil {
			return err
		}
		if err := store.SeedIfMissing(ctx, accounts.Account{
			Name:           s.Name,
			PublicKey:      claims.Subject,
			SigningKeySeed: "", // not this service's to mint users for — see doc comment
			Status:         accounts.StatusActive,
			JSMaxMem:       s.Limits.MaxMem,
			JSMaxFile:      s.Limits.MaxFile,
			JSMaxStreams:   s.Limits.MaxStreams,
			JSMaxConsumers: s.Limits.MaxConsumers,
		}); err != nil {
			return err
		}
		if err := ensureSigningKey(ctx, store, provisioner, s.Name, s.Limits, log); err != nil {
			return err
		}
	}
	return nil
}

// ensureSigningKey establishes a signing key for a seeded pre-existing
// account (Phase 15c) if it doesn't already have one on record — needed
// because GET /api/auth/connectInfo (auth/handler.go, folded into this
// service in Phase 19) mints browser user JWTs by loading an account's
// SigningKeySeed from this same service's own Store (see
// accounts/store.go's Account.SigningKeySeed doc comment), and
// bootstrap-operator.sh's nsc-generated signing key for these accounts was
// never exported anywhere this service can read (nsc's local keystore is
// deleted at the end of that script — see its own header comment). Reuses
// exactly the key-establishment logic accounts/handler.go's
// reactivateAccount already uses for a suspended account with no signing
// key on record, just triggered at startup instead of gated behind
// suspension — Provisioner.ReactivateAccount itself doesn't check status,
// only the REST handler does, so calling it here on an active account is
// safe: it just re-pushes the account's claims with a signing key added,
// which shipping-service's own already-working .creds files (signed
// directly by the account's identity key, not this signing key) are
// unaffected by.
//
// Runs at most once per account: a signing key, once established and
// persisted, is never rotated on a later restart (the Postgres row already
// has one, so this is a no-op) — rotating it would invalidate any browser
// JWT minted against the previous key that's still within its TTL.
func ensureSigningKey(ctx context.Context, store *accounts.Store, provisioner *accounts.Provisioner, name string, limits accounts.JSLimits, log *slog.Logger) error {
	acc, err := store.Get(ctx, name)
	if err != nil {
		return fmt.Errorf("reload seeded account %q: %w", name, err)
	}
	if acc.SigningKeySeed != "" {
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

	if err := provisioner.ReactivateAccount(ctx, acc.PublicKey, string(seed), limits); err != nil {
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
