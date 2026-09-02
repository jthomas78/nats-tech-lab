package announcer

import (
	"context"
	"encoding/json"
	"log/slog"
	"math/rand"
	"sync"
	"time"

	"github.com/jthomas78/nats-tech-lab/shared/mferegistry"
	"github.com/nats-io/nats.go"
)

/*
	The publisher's half of the catalogue-reset notice (BR-AS73, Phase 15
	decisions 6, 7, 8, 9).

	A notice is a statement of fact, so everything here is the plugin's own
	decision. It decides whether to re-announce, when, and how many times —
	the registry states only that its catalogue was reset.

	The split is the same one the health reporter uses: every DECISION lives
	in a struct stepped by a caller, and the loop owns nothing but a timer and
	a subscription. That is what lets the interesting rules — clamp the
	carried window, coalesce a burst, ignore a malformed notice, and never
	ever reach unregister — be value tests with no broker and no sleeping.
*/

// noticeBus is the one thing the watcher needs from a NATS connection.
// *nats.Conn satisfies it, and so does a spec's recorder.
type noticeBus interface {
	Subscribe(subject string, handler nats.MsgHandler) (*nats.Subscription, error)
}

// resetWatcher turns believed notices into at most one pending re-announce.
type resetWatcher struct {
	pluginID string
	// announce is the SAME call start-up makes, deliberately. A re-announce
	// that took a different path could drift from the signed, release-spending
	// one, and BR-AS73's convergence guarantee assumes they are identical.
	announce func(context.Context) error
	// draw picks the delay inside a window. A seam so a spec can assert the
	// bounds without a random number generator in the assertion.
	draw func(time.Duration) time.Duration
	log  *slog.Logger

	mu      sync.Mutex
	pending bool
}

func newResetWatcher(pluginID string, announce func(context.Context) error, log *slog.Logger) *resetWatcher {
	return &resetWatcher{
		pluginID: pluginID,
		announce: announce,
		draw:     func(window time.Duration) time.Duration { return time.Duration(rand.Int63n(int64(window))) },
		log:      log,
	}
}

// Notice decides what one received message means. It returns the delay to
// wait before re-announcing, and whether to re-announce at all.
//
// Everything it refuses, it refuses silently — this is a notification, and
// there is nobody to answer. None of these paths, and no amount of silence on
// this subject, may reach unregister: withdrawal requires an explicit
// authoritative action and a reset notice is not one (BR-AS54, decision 9).
func (w *resetWatcher) Notice(payload []byte) (time.Duration, bool) {
	// A publisher with no announce path is a curated plugin: it reached the
	// catalogue through the operator's preload file, holds no signing key and
	// holds no announce grant. It has nothing to re-announce, and a notice is
	// simply not addressed to it.
	if w.announce == nil {
		return 0, false
	}

	var notice mferegistry.ResetNotice
	if err := json.Unmarshal(payload, &notice); err != nil {
		w.log.Warn("ignoring an undecodable catalogue-reset notice", "plugin", w.pluginID, "error", err)
		return 0, false
	}

	// One pending re-announce at a time. A burst of notices — a registry
	// retrying, or two operators reaching for the same lever — must cost the
	// fleet one re-announce per plugin, not one per message. Coalescing here
	// rather than at the timer keeps the second notice from moving the first
	// one's deadline, which an attacker could otherwise use to hold a plugin
	// permanently on the point of announcing.
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.pending {
		return 0, false
	}
	w.pending = true

	// The window is read through JitterWindow and never off the struct: the
	// clamp is the rule, and reading the raw field anywhere would be a way
	// around it.
	return w.draw(notice.JitterWindow()), true
}

// done releases the coalescing latch after an attempt, so a later reset is
// still honoured.
func (w *resetWatcher) done() {
	w.mu.Lock()
	w.pending = false
	w.mu.Unlock()
}

// Watch subscribes for the process's lifetime. The returned stop function
// unsubscribes; it is safe to call when the bus never produced one.
func (w *resetWatcher) Watch(ctx context.Context, bus noticeBus) (func(), error) {
	sub, err := bus.Subscribe(mferegistry.EntriesReset, func(msg *nats.Msg) {
		delay, ok := w.Notice(msg.Data)
		if !ok {
			return
		}
		go w.reannounce(ctx, delay)
	})
	if err != nil {
		return func() {}, err
	}
	return func() {
		if sub != nil {
			_ = sub.Unsubscribe()
		}
	}, nil
}

func (w *resetWatcher) reannounce(ctx context.Context, delay time.Duration) {
	defer w.done()
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		// Shutting down mid-wait is an ordinary process end, not a
		// withdrawal. Nothing is published and nothing is unregistered.
		return
	case <-timer.C:
	}
	w.log.Info("re-announcing after a catalogue-reset notice", "plugin", w.pluginID, "waited", delay)
	if err := w.announce(ctx); err != nil {
		// A failed re-announce is left failed. Retrying here would rebuild
		// the storm the jitter window exists to spread out, and the plugin is
		// still running, still reporting health, and still able to answer the
		// next notice.
		w.log.Warn("re-announce after a catalogue-reset notice failed", "plugin", w.pluginID, "error", err)
	}
}
