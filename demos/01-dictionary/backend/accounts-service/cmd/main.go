// Command main bootstraps accounts-service (Phase 14b): the dynamic
// tenant-provisioning service that mints and revokes NATS accounts at
// runtime via decentralized JWTs, replacing nats/bootstrap-operator.sh's
// one-shot nsc invocation (Phase 14a) with a live API.
package main

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/nats-io/jwt/v2"
	"github.com/nats-io/nats.go"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/accounts-service/accounts"
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
	natsCredsPath := envOr("NATS_CREDS_PATH", "") // sys.creds — $SYS.REQ.CLAIMS.* is only reachable authenticated as SYS
	operatorSigningKeyFile := envOr("OPERATOR_SIGNING_KEY_FILE", "")
	credsDir := envOr("NATS_CREDS_DIR", "") // shared volume shipping-service also mounts
	resolverSeedDir := envOr("RESOLVER_SEED_DIR", "")
	authSecret := envOr("ACCOUNTS_AUTH_SECRET", "")
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
		if err := seedPreexistingAccounts(ctx, store, resolverSeedDir, log); err != nil {
			return err
		}
	}

	handlers := accounts.NewHandlers(store, provisioner, credsDir, log)
	mux := http.NewServeMux()
	handlers.Mount(mux, authSecret)

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
func seedPreexistingAccounts(ctx context.Context, store *accounts.Store, resolverSeedDir string, log *slog.Logger) error {
	seeds := []struct {
		Name       string
		LegacyName string
		File       string
		Limits     accounts.JSLimits
	}{
		{"default", "DEFAULT", "DEFAULT.jwt", accounts.JSLimits{MaxMem: 1 << 30, MaxFile: 5 << 30, MaxStreams: 20, MaxConsumers: 100}},
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
	}
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
