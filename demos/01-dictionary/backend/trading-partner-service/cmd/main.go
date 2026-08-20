// Command main bootstraps trading-partner-service: Postgres connection,
// schema migration, BR-TP14's tenant-scoped NATS connections (one per known
// tenant, discovered from NATS_CREDS_DIR) used to validate vehicleTypeCode
// against refdata-service and to serve TradingPartner/ComplianceDocument/
// FleetAsset over api.* (Phase 33.5 retired the equivalent business REST —
// the HTTP server now answers /healthz only). See
// tradingpartner/composition.go's doc comment.
package main

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/nats-io/nats.go"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/trading-partner-service/tradingpartner"
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

	databaseURL := envOr("DATABASE_URL", "postgres://trading_partner:trading_partner@localhost:5436/trading_partner?sslmode=disable")
	httpAddr := envOr("HTTP_ADDR", ":8080")
	natsURL := envOr("NATS_URL", nats.DefaultURL)
	credsDir := envOr("NATS_CREDS_DIR", "")

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

	// One NATS connection per known tenant, carrying BR-TP14's refdata
	// validation client and (Phase 26g) a micro-service registration for $SRV
	// discovery. The Admin UI still reads/writes over REST below.
	tenantMgr, err := tradingpartner.MountTenants(ctx, natsURL, credsDir, db, log, trackingSecretKey(log))
	if err != nil {
		return err
	}
	defer tenantMgr.Close()

	h, err := tradingpartner.Startup(ctx, db, tenantMgr)
	if err != nil {
		return err
	}

	// Third pass, and it must be third: MountTenants opened the connections
	// Startup needs for BR-TP14, and only now do the handlers the api.* adapter
	// serves exist. A failure here is fatal rather than degraded-to-REST — a
	// half-mounted transport is worse than not starting.
	if err := h.MountAPI(tenantMgr, log); err != nil {
		return err
	}

	// Phase 33.5: this service has no admin/operator REST left to gate — its
	// business routes were deleted outright rather than reclassified as
	// admin. Mount serves /healthz only, unauthenticated like every other
	// service's infra check.
	mux := http.NewServeMux()
	h.Mount(mux, log)

	server := &http.Server{Addr: httpAddr, Handler: mux}
	errCh := make(chan error, 1)
	go func() {
		log.Info("trading-partner-service: http server listening", "addr", httpAddr)
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

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// trackingSecretKey loads BR-TP52's 32-byte sealing key from
// ORGANIZATIONS_SECRETS_KEY, hex-encoded.
//
// An absent key disables tracking credentials rather than storing them
// unsealed — the rule exists precisely to stop a missing config turning into
// a plaintext secret. A present-but-malformed key is louder still: it is a
// deployment that *intended* to seal and would silently not, so the service
// refuses to start rather than run degraded in a way nobody would notice.
func trackingSecretKey(log *slog.Logger) []byte {
	raw := os.Getenv("ORGANIZATIONS_SECRETS_KEY")
	if raw == "" {
		log.Warn("trading-partner-service: ORGANIZATIONS_SECRETS_KEY unset — tracking credentials disabled (BR-TP52)")
		return nil
	}
	key, err := hex.DecodeString(raw)
	if err != nil || len(key) != 32 {
		log.Error("trading-partner-service: ORGANIZATIONS_SECRETS_KEY must be 64 hex characters (32 bytes)")
		os.Exit(1)
	}
	return key
}
