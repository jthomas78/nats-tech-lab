package accounts_test

// Phase 52 — the session reaper's loop and its configuration (BR-AC45).
//
// The DELETE itself is covered against real Postgres in users_store_test.go;
// what is covered here is everything around it — the cadence, the start-up
// run, the disabled case, and the rule that a failing tick must not stop the
// reaper. None of that needs a database, and asserting it against one would
// only make the timing flaky.

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/accounts-service/accounts"
)

// fakeReapStore counts reap calls and can be told to fail a given number of
// them, so "a failed tick is retried on the next one" is observable.
type fakeReapStore struct {
	mu         sync.Mutex
	calls      []time.Duration
	failFirstN int
	removed    int64
}

func (f *fakeReapStore) ReapExpiredSessions(_ context.Context, retention time.Duration) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, retention)
	if f.failFirstN > 0 {
		f.failFirstN--
		return 0, errors.New("postgres is away")
	}
	return f.removed, nil
}

func (f *fakeReapStore) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *fakeReapStore) retentions() []time.Duration {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]time.Duration(nil), f.calls...)
}

var _ = Describe("Session reaper", func() {
	quietLog := slog.New(slog.NewTextHandler(io.Discard, nil))

	Context("BR-AC45 — the reaper runs at start-up and then on a fixed interval", func() {
		It("reaps once immediately rather than waiting out the first interval", func() {
			store := &fakeReapStore{}
			reaper := accounts.NewSessionReaper(store, accounts.ReaperConfig{
				Retention: 24 * time.Hour,
				Interval:  time.Hour, // long enough that a tick cannot be what we observe
			}, quietLog)

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			go reaper.Run(ctx)

			Eventually(store.count).Should(Equal(1),
				"a stack that has been down for a week must not wait out an interval before cleaning up")
			Expect(store.retentions()[0]).To(Equal(24 * time.Hour))
		})

		It("keeps reaping on the interval", func() {
			store := &fakeReapStore{}
			reaper := accounts.NewSessionReaper(store, accounts.ReaperConfig{
				Retention: 24 * time.Hour,
				Interval:  10 * time.Millisecond,
			}, quietLog)

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			go reaper.Run(ctx)

			Eventually(store.count).Should(BeNumerically(">=", 3))
		})

		It("stops when its context is cancelled", func() {
			store := &fakeReapStore{}
			reaper := accounts.NewSessionReaper(store, accounts.ReaperConfig{
				Retention: 24 * time.Hour,
				Interval:  10 * time.Millisecond,
			}, quietLog)

			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan struct{})
			go func() { reaper.Run(ctx); close(done) }()
			Eventually(store.count).Should(BeNumerically(">=", 1))
			cancel()
			Eventually(done).Should(BeClosed())

			settled := store.count()
			Consistently(store.count, 50*time.Millisecond).Should(Equal(settled))
		})

		It("retries on the next tick after a failed one, rather than giving up", func() {
			store := &fakeReapStore{failFirstN: 2}
			reaper := accounts.NewSessionReaper(store, accounts.ReaperConfig{
				Retention: 24 * time.Hour,
				Interval:  10 * time.Millisecond,
			}, quietLog)

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			go reaper.Run(ctx)

			Eventually(store.count).Should(BeNumerically(">=", 4),
				"a reaper that dies on the first Postgres blip is a reaper that silently stops reaping")
		})

		It("does not run at all when retention is disabled", func() {
			store := &fakeReapStore{}
			reaper := accounts.NewSessionReaper(store, accounts.ReaperConfig{
				Retention: 0,
				Interval:  10 * time.Millisecond,
			}, quietLog)

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan struct{})
			go func() { reaper.Run(ctx); close(done) }()

			Eventually(done).Should(BeClosed(), "a disabled reaper returns instead of ticking forever")
			Expect(store.count()).To(BeZero())
		})
	})

	Context("BR-AC45 — configuration", func() {
		It("defaults to 24h retention on a 15m cadence when nothing is set", func() {
			cfg, err := accounts.ReaperConfigFromEnv("", "")
			Expect(err).NotTo(HaveOccurred())
			Expect(cfg.Retention).To(Equal(24 * time.Hour))
			Expect(cfg.Interval).To(Equal(15 * time.Minute))
			Expect(cfg.Enabled()).To(BeTrue())
		})

		It("treats an explicit zero retention as OFF, not as reap-on-expiry", func() {
			cfg, err := accounts.ReaperConfigFromEnv("0", "")
			Expect(err).NotTo(HaveOccurred())
			Expect(cfg.Enabled()).To(BeFalse(),
				"the destructive reading must never be the one an operator gets by typing the obvious value")
		})

		It("accepts Go durations for both knobs", func() {
			cfg, err := accounts.ReaperConfigFromEnv("168h", "1h")
			Expect(err).NotTo(HaveOccurred())
			Expect(cfg.Retention).To(Equal(168 * time.Hour))
			Expect(cfg.Interval).To(Equal(time.Hour))
		})

		It("rejects a negative retention rather than reaping the future", func() {
			_, err := accounts.ReaperConfigFromEnv("-1h", "")
			Expect(err).To(HaveOccurred())
		})

		It("rejects a non-positive interval", func() {
			_, err := accounts.ReaperConfigFromEnv("", "0")
			Expect(err).To(HaveOccurred())
		})

		It("rejects an unparseable value rather than silently falling back to the default", func() {
			_, err := accounts.ReaperConfigFromEnv("a day", "")
			Expect(err).To(HaveOccurred())
			_, err = accounts.ReaperConfigFromEnv("", "often")
			Expect(err).To(HaveOccurred())
		})
	})
})
