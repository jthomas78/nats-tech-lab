package accounts

// Phase 50a, BR-AC39 — start-up convergence for users this service did not
// mint.
//
// nats/bootstrap-operator.sh creates shipping-admin, observability, sys and
// the three tenant credentials with `nsc add user` before accounts-service
// has ever run. Those users are real and connected, but nothing recorded
// them: the only trace they leave is the .creds file on the shared volume.
// So the creds directory is read on every start and each decodable file is
// recorded if it isn't already — the same idempotent, no-re-sign,
// no-claims-update shape BR-AC37 established for PLATFORM's cross-account
// imports. Nothing here signs, pushes or rewrites anything; a healthy stack
// on its second start writes zero rows.

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nats-io/jwt/v2"
)

// BackfillCredsDirUsers records every user credential found in dir that the
// registry doesn't already know about, and returns how many files it
// decoded. accountNames maps account public key → account name, so a
// credential can be filed under the same lowercase account name the rest of
// the schema uses; an issuer this service has no name for is filed under its
// public key rather than dropped.
//
// A missing directory, an unreadable file, or a file that doesn't decode is
// logged and skipped, never fatal: this runs on the start-up path, and a
// stray file on the creds volume must not be able to stop the service from
// booting.
func BackfillCredsDirUsers(ctx context.Context, sink BootstrapUserSink, dir string, accountNames map[string]string, log *slog.Logger) (int, error) {
	if dir == "" {
		return 0, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}

	recorded := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".creds") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		raw, err := os.ReadFile(path)
		if err != nil {
			log.Warn("backfill user: unreadable creds file, skipping", "path", path, "err", err)
			continue
		}
		token, err := jwt.ParseDecoratedJWT(raw)
		if err != nil {
			log.Warn("backfill user: creds file has no user JWT, skipping", "path", path, "err", err)
			continue
		}
		claims, err := jwt.DecodeUserClaims(token)
		if err != nil {
			log.Warn("backfill user: undecodable user JWT, skipping", "path", path, "err", err)
			continue
		}

		// A user JWT signed by an account signing key carries the account it
		// belongs to in IssuerAccount and the signing key in Issuer; one signed
		// by the account identity key itself carries only Issuer.
		accountKey := claims.IssuerAccount
		if accountKey == "" {
			accountKey = claims.Issuer
		}
		account := normalizeAccountName(accountNames[accountKey])
		if account == "" {
			account = accountKey
		}

		name := claims.Name
		if name == "" {
			name = strings.TrimSuffix(entry.Name(), ".creds")
		}

		var expiresAt *time.Time
		if claims.Expires > 0 {
			exp := time.Unix(claims.Expires, 0).UTC()
			expiresAt = &exp
		}

		// Issuer is literally the key that signed this JWT, which is exactly
		// what BR-AC41's scope resolution needs — and unlike a live mint,
		// here it is read back off the credential rather than known in
		// advance. Permissions come off the same decode: the .creds file is
		// the only record an nsc-minted user leaves, so this is the one
		// chance to capture what it was granted.
		perms := claims.UserPermissionLimits

		u := NewUser{
			PublicKey:   claims.Subject,
			Name:        name,
			Account:     account,
			AccountKey:  accountKey,
			IssuerKey:   claims.Issuer,
			Permissions: &perms,
			// Every bootstrap user is a process-held .creds file, never a
			// browser session — sessions are minted by auth/token.go and never
			// written to disk.
			Kind:      UserKindCredential,
			Bearer:    claims.BearerToken,
			ExpiresAt: expiresAt,
			Source:    UserSourceBootstrap,
			// A credential whose .creds file exists was signed successfully:
			// there is no pending state to reconstruct after the fact.
			Status: UserStatusActive,
		}
		if err := sink.InsertUserIfMissing(ctx, u); err != nil {
			log.Warn("backfill user: could not record, skipping", "path", path, "err", err)
			continue
		}
		recorded++
	}
	return recorded, nil
}
