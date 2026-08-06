package accounts

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nats-io/jwt/v2"
	"github.com/nats-io/nkeys"
)

// SigningKeyFileName is the name bootstrap-operator.sh exports an account's
// signing key seed under, keyed by the account's lowercase tenant identity
// (the same form used for .creds filenames, NATS subjects, and KV buckets —
// see BR-AC05's naming note), not the uppercase nsc account name.
func SigningKeyFileName(accountName string) string {
	return strings.ToLower(accountName) + "-signing-key.nk"
}

// ResolveSeededSigningKey loads the signing key seed bootstrap-operator.sh
// exported for a seeded pre-existing account (BR-AC19) and verifies it
// against that account's own resolver JWT before it is trusted.
//
// Returning ("", nil) means "no seed on disk" — the caller falls back to
// generating one, which is the pre-BR-AC19 behaviour and keeps a stack
// bootstrapped before this existed working unchanged.
//
// An error means the file is present but unusable, and that is deliberately
// not silent: persisting a seed the resolver does not trust would mint user
// JWTs the server rejects, which is precisely the failure BR-AC19 exists to
// prevent. Failing loudly at startup beats an authorization violation later.
func ResolveSeededSigningKey(dir, accountName string, claims *jwt.AccountClaims) (string, error) {
	if dir == "" {
		return "", nil
	}
	path := filepath.Join(dir, SigningKeyFileName(accountName))
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read account signing key %s: %w", path, err)
	}

	seed := strings.TrimSpace(string(raw))
	kp, err := nkeys.FromSeed([]byte(seed))
	if err != nil {
		return "", fmt.Errorf("parse account signing key %s: %w", path, err)
	}
	pub, err := kp.PublicKey()
	if err != nil {
		return "", fmt.Errorf("account signing key public key %s: %w", path, err)
	}
	if !nkeys.IsValidPublicAccountKey(pub) {
		return "", fmt.Errorf("account signing key %s is not an account key", path)
	}
	if claims == nil {
		return "", fmt.Errorf("account signing key %s: no resolver claims to verify it against", path)
	}
	if !claims.SigningKeys.Contains(pub) {
		return "", fmt.Errorf("account signing key %s (%s) is not listed in %s's resolver JWT — regenerate them together with bootstrap-operator.sh --force", path, pub, accountName)
	}
	return seed, nil
}
