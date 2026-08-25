package organizations_test

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations"
)

// The vetting worker used to die on a cold `docker compose up -d` after
// `down -v`. Temporal has two distinct not-ready-yet stages and both were
// observed on 2026-08-25: the frontend not listening ("connection refused",
// from client.Dial), and the frontend listening before temporal-auto-setup
// has created the default namespace ("Namespace default is not found", from
// worker.Start — it appeared one second after the service gave up).

func discard() *slog.Logger { return slog.New(slog.DiscardHandler) }

func TestRetryUntilReadySurvivesADependencyThatArrivesLate(t *testing.T) {
	defer organizations.TemporalProbeIntervalForTest(time.Millisecond)()

	for _, tc := range []struct {
		name string
		err  error
	}{
		{"frontend not listening yet", errors.New("connection error: connect: connection refused")},
		{"namespace not created yet", errors.New("Namespace default is not found.")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			attempt := func(context.Context) error {
				calls++
				if calls < 3 {
					return tc.err
				}
				return nil
			}

			if err := organizations.RetryUntilReady(context.Background(), "temporal", discard(), attempt); err != nil {
				t.Fatalf("want nil once the dependency arrives, got %v", err)
			}
			if calls != 3 {
				t.Fatalf("want 3 attempts, got %d", calls)
			}
		})
	}
}

func TestRetryUntilReadyStillFailsWhenTheDependencyNeverArrives(t *testing.T) {
	defer organizations.TemporalProbeIntervalForTest(time.Millisecond)()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	attempt := func(context.Context) error { return errors.New("Namespace default is not found.") }

	err := organizations.RetryUntilReady(ctx, "temporal namespace default", discard(), attempt)
	if err == nil {
		t.Fatal("want an error when the dependency never arrives — the service must not come up worker-less")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want the deadline wrapped, got %v", err)
	}
	if !strings.Contains(err.Error(), "temporal namespace default") {
		t.Fatalf("want the error to name what it waited for, got %v", err)
	}
}

// The retry loop is failure-mode-agnostic by design, which means a genuinely
// wrong address would otherwise burn the whole budget before failing. The
// preflight check is the fast path for that case only — it must never
// shortcut a Temporal that is merely still starting.

func TestPermanentDialErrorFailsFastOnlyOnAddressesThatCanNeverWork(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"malformed host:port", &net.AddrError{Err: "missing port in address", Addr: "temporal"}, true},
		{"host does not resolve", &net.DNSError{Err: "no such host", Name: "temporl", IsNotFound: true}, true},
		{"dns lookup timed out", &net.DNSError{Err: "i/o timeout", Name: "temporal", IsTimeout: true}, false},
		{"connection refused", syscall.ECONNREFUSED, false},
		{"anything else", errors.New("some transient thing"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := organizations.PermanentDialError(tc.err); got != tc.want {
				t.Fatalf("want %v, got %v", tc.want, got)
			}
		})
	}
}

func TestPreflightLetsANotListeningYetTemporalThrough(t *testing.T) {
	defer organizations.PreflightDialTimeoutForTest(200 * time.Millisecond)()

	// A port nothing is listening on: refused, which is exactly the cold-start
	// shape. Preflight must not turn that into a startup failure.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()

	if err := organizations.PreflightTemporalAddr(addr); err != nil {
		t.Fatalf("want nil for a refused connection — the retry loop owns that case, got %v", err)
	}
}

func TestPreflightRejectsAnAddressThatCannotResolve(t *testing.T) {
	defer organizations.PreflightDialTimeoutForTest(2 * time.Second)()

	err := organizations.PreflightTemporalAddr("temporl.invalid:7233")
	if err == nil {
		t.Fatal("want an error for a host that does not resolve")
	}
	if !strings.Contains(err.Error(), "temporl.invalid:7233") {
		t.Fatalf("want the bad address named in the error, got %v", err)
	}
}
