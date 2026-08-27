package accounts

// Phase 52 — the session reaper (BR-AC44/BR-AC45).
//
// accounts.users grows on every browser connect. BR-AC38 makes recording
// part of minting, so a 15-minute session (BR-AC20) writes a row that
// outlives the credential it describes by forever, and nothing in the stack
// deleted one: the only sweep that existed ran once at start-up and touched
// `pending` rows only — the mint-crash case, not the ordinary one. Measured
// on the running stack after a day's uptime: 56 rows, 44 of them expired
// sessions.
//
// The reaper is deliberately two knobs rather than one, because "how often
// does it run" and "how long does a row live" are different questions and
// conflating them is how a reaper ends up either useless or destructive:
//
//   - Retention is the OPERATOR knob — how long a row survives past its own
//     expires_at. A session that expired four minutes ago is exactly the row
//     someone is reading when they ask "why did my tab drop", so reaping on
//     expiry destroys the answer at the moment it is wanted. 24h is chosen
//     because it is roughly how long the row stays EXPLAINABLE: past that
//     the /connz closed ring feeding the panel's Last outcome column (a
//     bounded ~59 entries, measured) has long since rolled over, and a row
//     with no outcome left to pair with is not worth keeping.
//   - Interval is a LOAD knob and nothing else. It only has to be frequent
//     enough that between-sweep accumulation is small next to the retained
//     set; at 15m against a 24h window that is ~1% overshoot.
//
// What it may delete is stated as a whitelist in ReapExpiredSessions, not as
// an exclusion list, so the failure mode of a future edit is a row that
// survives rather than a credential that does not.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

const (
	// DefaultSessionRetention is how long an expired session row survives
	// past its own expires_at before the reaper removes it.
	DefaultSessionRetention = 24 * time.Hour
	// DefaultReapInterval is the sweep cadence — one minimum-TTL period
	// (accounts/config.go's MinTTLMinutes).
	DefaultReapInterval = 15 * time.Minute
)

// ReaperConfig is the reaper's two knobs. Retention <= 0 means the reaper is
// OFF; see ReaperConfigFromEnv for why that reading and not the other one.
type ReaperConfig struct {
	Retention time.Duration
	Interval  time.Duration
}

// Enabled reports whether the reaper should run at all.
func (c ReaperConfig) Enabled() bool { return c.Retention > 0 }

// ReaperConfigFromEnv parses the two env values, falling back to the
// defaults above when either is empty.
//
// An explicit "0" retention DISABLES the reaper rather than meaning "reap
// the instant a session expires". Both readings are defensible in the
// abstract; only one of them is safe to get wrong. An operator who types the
// obvious value and means "don't keep them" gets a no-op they can see in the
// logs, not a table emptied of everything that could have explained the last
// hour. A negative value is rejected outright rather than clamped — it can
// only be a mistake, and clamping would hide which mistake.
//
// An unparseable value is an error, never a silent fallback to the default:
// a deployment that meant to set 7 days and typed it wrong must find out at
// boot, not a week later when the rows it wanted are gone.
func ReaperConfigFromEnv(retention, interval string) (ReaperConfig, error) {
	cfg := ReaperConfig{Retention: DefaultSessionRetention, Interval: DefaultReapInterval}

	if retention != "" {
		d, err := time.ParseDuration(retention)
		if err != nil {
			return ReaperConfig{}, fmt.Errorf("ACCOUNTS_SESSION_RETENTION: %w", err)
		}
		if d < 0 {
			return ReaperConfig{}, errors.New("ACCOUNTS_SESSION_RETENTION must not be negative (use 0 to disable the reaper)")
		}
		cfg.Retention = d
	}

	if interval != "" {
		d, err := time.ParseDuration(interval)
		if err != nil {
			return ReaperConfig{}, fmt.Errorf("ACCOUNTS_REAPER_INTERVAL: %w", err)
		}
		if d <= 0 {
			return ReaperConfig{}, errors.New("ACCOUNTS_REAPER_INTERVAL must be positive")
		}
		cfg.Interval = d
	}

	return cfg, nil
}

// ExpiredSessionReaper is the one store call the loop needs. Narrow on
// purpose, the same way UserRegistry is: the cadence, the start-up run and
// the retry-on-failure rule are all assertable without a database, and
// asserting them against one would only make them flaky.
type ExpiredSessionReaper interface {
	ReapExpiredSessions(ctx context.Context, retention time.Duration) (int64, error)
}

// SessionReaper periodically removes expired session rows from the registry.
type SessionReaper struct {
	store ExpiredSessionReaper
	cfg   ReaperConfig
	log   *slog.Logger
}

// NewSessionReaper wires a reaper. Nothing starts until Run is called.
func NewSessionReaper(store ExpiredSessionReaper, cfg ReaperConfig, log *slog.Logger) *SessionReaper {
	return &SessionReaper{store: store, cfg: cfg, log: log}
}

// Run reaps once immediately and then on the configured interval, until ctx
// is cancelled. Intended to be called in its own goroutine.
//
// The immediate run is not an optimisation: a stack that has been down for a
// week must not wait out an interval before cleaning up, and the start-up
// run is also what surfaces a misconfiguration now rather than a quarter of
// an hour from now.
//
// A failing tick logs and waits for the next one (BR-AC45). A reaper that
// returns on the first Postgres blip is a reaper that has silently stopped
// reaping, which is worse than one that is noisily failing — the table goes
// on growing either way, but only one of them says so.
func (r *SessionReaper) Run(ctx context.Context) {
	if !r.cfg.Enabled() {
		r.log.Info("session reaper disabled", "reason", "ACCOUNTS_SESSION_RETENTION is 0")
		return
	}
	r.log.Info("session reaper started", "retention", r.cfg.Retention, "interval", r.cfg.Interval)

	r.reapOnce(ctx)

	ticker := time.NewTicker(r.cfg.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			r.log.Info("session reaper stopped")
			return
		case <-ticker.C:
			r.reapOnce(ctx)
		}
	}
}

// reapOnce runs a single sweep. Silent when it removes nothing — a heartbeat
// every 15 minutes saying "0" is how a log stops being read.
func (r *SessionReaper) reapOnce(ctx context.Context) {
	n, err := r.store.ReapExpiredSessions(ctx, r.cfg.Retention)
	switch {
	case err != nil:
		r.log.Warn("reap expired sessions", "err", err)
	case n > 0:
		r.log.Info("reaped expired sessions", "count", n, "retention", r.cfg.Retention)
	}
}
