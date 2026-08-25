package organizations

import (
	"context"
	"log/slog"
	"time"
)

// RetryUntilReady exposes the Temporal cold-start retry loop to the external
// test package; TemporalProbeIntervalForTest shortens it so exercising a
// retry does not cost a second per attempt.
func RetryUntilReady(ctx context.Context, what string, log *slog.Logger, attempt func(context.Context) error) error {
	return retryUntilReady(ctx, what, log, attempt)
}

func TemporalProbeIntervalForTest(d time.Duration) func() {
	prev := temporalProbeInterval
	temporalProbeInterval = d
	return func() { temporalProbeInterval = prev }
}

// PermanentDialError and PreflightTemporalAddr expose the fast-path config
// check to the external test package.
func PermanentDialError(err error) bool { return permanentDialError(err) }

func PreflightTemporalAddr(addr string) error { return preflightTemporalAddr(addr) }

func PreflightDialTimeoutForTest(d time.Duration) func() {
	prev := preflightDialTimeout
	preflightDialTimeout = d
	return func() { preflightDialTimeout = prev }
}
