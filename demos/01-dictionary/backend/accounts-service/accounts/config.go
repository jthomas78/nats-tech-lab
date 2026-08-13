package accounts

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Browser/admin NATS user JWT expiry policy (BR-AC20 / BR-AC21).
//
// The TTL stamped on the short-lived JWTs auth.MintBrowserToken and
// auth.MintAdminToken hand to browser WebSocket connections is a durable,
// platform-global system setting — a singleton accounts.system_config row —
// not the compile-time constant it used to be (auth/token.go's old
// `const tokenTTL`). It is read at mint time and edited from the Admin UI's
// System Settings screen.
//
// Two values are configurable: the TTL value actually issued, and the
// operational [min, max] range the value must sit within. BOTH are hard-
// bounded by the BR-UA03 envelope below, which is a code constant precisely
// because it is the business rule itself — "all JWT expiry must be between
// 15 and 30 minutes." Widening that envelope is a change to the rule and must
// go through code + a spec, never a runtime toggle; the configurable range
// only lets an operator *narrow* the window within it.
const (
	// MinTTLMinutes and MaxTTLMinutes are the inclusive BR-UA03 envelope.
	MinTTLMinutes = 15
	MaxTTLMinutes = 30
	// DefaultTokenTTLMinutes is the out-of-box value (BR-AC20): the tight end
	// of the envelope, keeping the POC's security-first default of a short
	// blast-radius window.
	DefaultTokenTTLMinutes = 15
)

// TokenTTLConfig is the platform-global browser/admin JWT expiry policy.
// ValueMinutes is the TTL issued on minted JWTs; [MinMinutes, MaxMinutes] is
// the configurable operational window it must fall within. Validate enforces
// that both the window and the value stay inside the hard BR-UA03 envelope.
type TokenTTLConfig struct {
	ValueMinutes int
	MinMinutes   int
	MaxMinutes   int
	UpdatedAt    time.Time
}

// DefaultTokenTTLConfig is the BR-AC20 out-of-box setting: a 15-minute value
// within a 15–30 minute range. Migrate seeds a row with exactly these values,
// and Store.GetTokenTTLConfig falls back to it if the row is ever missing.
func DefaultTokenTTLConfig() TokenTTLConfig {
	return TokenTTLConfig{
		ValueMinutes: DefaultTokenTTLMinutes,
		MinMinutes:   MinTTLMinutes,
		MaxMinutes:   MaxTTLMinutes,
	}
}

// Validate enforces BR-AC21: the configured range must lie within the hard
// [MinTTLMinutes, MaxTTLMinutes] envelope, min must not exceed max, and the
// issued value must fall within the configured range. Because the range is
// envelope-bounded, a valid value is transitively guaranteed to sit inside
// the BR-UA03 15–30 minute window.
func (c TokenTTLConfig) Validate() error {
	if c.MinMinutes < MinTTLMinutes || c.MinMinutes > MaxTTLMinutes ||
		c.MaxMinutes < MinTTLMinutes || c.MaxMinutes > MaxTTLMinutes {
		return fmt.Errorf("token TTL range [%d, %d] must lie within the %d–%d minute envelope (BR-UA03)",
			c.MinMinutes, c.MaxMinutes, MinTTLMinutes, MaxTTLMinutes)
	}
	if c.MinMinutes > c.MaxMinutes {
		return fmt.Errorf("token TTL range minimum %d must not exceed maximum %d", c.MinMinutes, c.MaxMinutes)
	}
	if c.ValueMinutes < c.MinMinutes || c.ValueMinutes > c.MaxMinutes {
		return fmt.Errorf("token TTL value %d must lie within the configured range [%d, %d]",
			c.ValueMinutes, c.MinMinutes, c.MaxMinutes)
	}
	return nil
}

// TTL is ValueMinutes as a time.Duration, ready to stamp on a JWT's Expires.
func (c TokenTTLConfig) TTL() time.Duration {
	return time.Duration(c.ValueMinutes) * time.Minute
}

// GetTokenTTLConfig reads the singleton system-config row. It falls back to
// DefaultTokenTTLConfig if the row is somehow absent, so a token mint never
// fails just because the config lookup did (BR-AC20).
func (s *Store) GetTokenTTLConfig(ctx context.Context) (TokenTTLConfig, error) {
	var c TokenTTLConfig
	err := s.db.QueryRowContext(ctx, `
		SELECT token_ttl_minutes, token_ttl_min_minutes, token_ttl_max_minutes, updated_at
		FROM accounts.system_config WHERE singleton = true`).
		Scan(&c.ValueMinutes, &c.MinMinutes, &c.MaxMinutes, &c.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return DefaultTokenTTLConfig(), nil
	}
	if err != nil {
		return TokenTTLConfig{}, err
	}
	return c, nil
}

// SetTokenTTLConfig upserts the singleton system-config row. Callers must
// Validate the config first; this method only persists. Upsert (rather than a
// bare UPDATE) keeps it correct even if the seed row is missing.
func (s *Store) SetTokenTTLConfig(ctx context.Context, c TokenTTLConfig) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO accounts.system_config (singleton, token_ttl_minutes, token_ttl_min_minutes, token_ttl_max_minutes, updated_at)
		VALUES (true, $1, $2, $3, now())
		ON CONFLICT (singleton) DO UPDATE SET
			token_ttl_minutes = EXCLUDED.token_ttl_minutes,
			token_ttl_min_minutes = EXCLUDED.token_ttl_min_minutes,
			token_ttl_max_minutes = EXCLUDED.token_ttl_max_minutes,
			updated_at = now()`,
		c.ValueMinutes, c.MinMinutes, c.MaxMinutes)
	return err
}
