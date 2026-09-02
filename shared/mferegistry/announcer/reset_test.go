package announcer

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"path/filepath"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/shared/mferegistry"
	"github.com/nats-io/nats.go"
)

// Phase 15d — what a plugin does with a catalogue-reset notice (BR-AS73).
//
// The notice is a statement of fact, so every rule below is about the
// PLUGIN's decision, never about obedience. The two that matter most are the
// ones a passing deployment would never reveal: a window taken raw off the
// wire is a lever that turns the backstop into a stampede, and any path from
// a notice to unregister would let a message take running code off an
// operator's screen — which BR-AS54 says only SIGTERM may do.

type recordingBus struct {
	mu        sync.Mutex
	subjects  []string
	handler   nats.MsgHandler
	subscribe error
}

func (b *recordingBus) Subscribe(subject string, handler nats.MsgHandler) (*nats.Subscription, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.subjects = append(b.subjects, subject)
	b.handler = handler
	return nil, b.subscribe
}

var _ = Describe("BR-AS73 — a publisher's response to a catalogue-reset notice", func() {
	const pluginID = "plugin-a"
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))

	notice := func(n mferegistry.ResetNotice) []byte {
		raw, err := json.Marshal(n)
		Expect(err).NotTo(HaveOccurred())
		return raw
	}

	// watcher with an announce that only counts, and a draw that reports the
	// window it was handed rather than a random point inside it — the bounds
	// are the rule, the sample is not.
	newWatcher := func() (*resetWatcher, *int, *time.Duration) {
		announced := 0
		var offered time.Duration
		w := newResetWatcher(pluginID, func(context.Context) error {
			announced++
			return nil
		}, quiet)
		w.draw = func(window time.Duration) time.Duration {
			offered = window
			return 0
		}
		return w, &announced, &offered
	}

	Context("the jitter window is the registry's to widen and nobody's to close", func() {
		It("draws the delay from the clamped window, never from the raw field", func() {
			w, _, offered := newWatcher()
			_, ok := w.Notice(notice(mferegistry.ResetNotice{JitterMillis: 1}))
			Expect(ok).To(BeTrue())
			Expect(*offered).To(Equal(mferegistry.ResetJitterFloor))
		})

		It("honours a widened window, which is the whole reason the field is on the wire", func() {
			w, _, offered := newWatcher()
			widened := 3 * time.Minute
			_, ok := w.Notice(notice(mferegistry.ResetNotice{JitterMillis: widened.Milliseconds()}))
			Expect(ok).To(BeTrue())
			Expect(*offered).To(Equal(widened))
		})

		It("never returns a delay outside the window it drew from", func() {
			w, _, _ := newWatcher()
			w.draw = newResetWatcher(pluginID, func(context.Context) error { return nil }, quiet).draw
			for i := 0; i < 200; i++ {
				delay, ok := w.Notice(notice(mferegistry.ResetNotice{JitterMillis: (30 * time.Second).Milliseconds()}))
				Expect(ok).To(BeTrue())
				Expect(delay).To(And(BeNumerically(">=", time.Duration(0)), BeNumerically("<", 30*time.Second)))
				w.done()
			}
		})
	})

	Context("what a notice can and cannot make a plugin do", func() {
		It("ignores an undecodable notice rather than re-announcing on a guess", func() {
			w, announced, _ := newWatcher()
			_, ok := w.Notice([]byte("{not json"))
			Expect(ok).To(BeFalse())
			Expect(*announced).To(Equal(0))
		})

		It("coalesces a burst into one pending re-announce, so a retrying registry costs one announce per plugin", func() {
			w, _, _ := newWatcher()
			first, ok := w.Notice(notice(mferegistry.ResetNotice{}))
			Expect(ok).To(BeTrue())
			for i := 0; i < 5; i++ {
				_, again := w.Notice(notice(mferegistry.ResetNotice{}))
				Expect(again).To(BeFalse())
			}
			// And the burst does not move the first deadline: a plugin held
			// permanently on the point of announcing would never announce.
			Expect(first).To(Equal(time.Duration(0)))
		})

		It("honours a later reset once the pending one has been attempted", func() {
			w, _, _ := newWatcher()
			_, ok := w.Notice(notice(mferegistry.ResetNotice{}))
			Expect(ok).To(BeTrue())
			w.done()
			_, ok = w.Notice(notice(mferegistry.ResetNotice{}))
			Expect(ok).To(BeTrue())
		})

		It("is not addressed to a curated publisher, which holds no announce grant to use", func() {
			w := newResetWatcher(pluginID, nil, quiet)
			_, ok := w.Notice(notice(mferegistry.ResetNotice{}))
			Expect(ok).To(BeFalse())
		})

		It("takes no instruction from the notice beyond the window, so a reason cannot branch behaviour", func() {
			w, _, offered := newWatcher()
			_, ok := w.Notice(notice(mferegistry.ResetNotice{Reason: "restore from backup", JitterMillis: (20 * time.Second).Milliseconds()}))
			Expect(ok).To(BeTrue())
			Expect(*offered).To(Equal(20 * time.Second))
		})
	})

	Context("BR-AS54 — silence is inert and a notice never withdraws", func() {
		It("never unregisters, whatever the notice says or does not say", func() {
			publisher := &recordingPublisher{}
			r := &resident{pluginID: pluginID, manifest: json.RawMessage(`{"id":"plugin-a"}`), publisher: publisher, log: quiet}
			w := newResetWatcher(pluginID, func(ctx context.Context) error { return nil }, quiet)
			for _, payload := range [][]byte{
				notice(mferegistry.ResetNotice{}),
				[]byte("{not json"),
				notice(mferegistry.ResetNotice{JitterMillis: -5, Reason: "unregister"}),
			} {
				w.Notice(payload)
				w.done()
			}
			Expect(publisher.unregisters).To(BeEmpty())
			Expect(r.pluginID).To(Equal(pluginID))
		})

		It("ignoring a notice leaves the plugin exactly as it was — not re-announced, not withdrawn", func() {
			path := filepath.Join(GinkgoT().TempDir(), "release.json")
			publisher := &recordingPublisher{}
			releases := newReleaseStore(path, pluginID, 0)
			spent, _, err := releases.PrepareAnnounce()
			Expect(err).NotTo(HaveOccurred())
			w := newResetWatcher(pluginID, nil, quiet)
			_, ok := w.Notice(notice(mferegistry.ResetNotice{}))
			Expect(ok).To(BeFalse())
			Expect(publisher.announcements).To(BeEmpty())
			Expect(publisher.unregisters).To(BeEmpty())
			// A dropped notice must be indistinguishable from no notice. If it
			// spent a release anyway, the next ordinary announce would be a
			// protocol action caused by a message the publisher ignored.
			retry, _, err := newReleaseStore(path, pluginID, 0).PrepareAnnounce()
			Expect(err).NotTo(HaveOccurred())
			Expect(retry).To(Equal(spent))
		})
	})

	Context("the subscription itself", func() {
		It("subscribes to exactly the reset subject, with no wildcard on the plugin side", func() {
			bus := &recordingBus{}
			stop, err := newResetWatcher(pluginID, func(context.Context) error { return nil }, quiet).Watch(context.Background(), bus)
			Expect(err).NotTo(HaveOccurred())
			defer stop()
			Expect(bus.subjects).To(Equal([]string{mferegistry.EntriesReset}))
			Expect(mferegistry.EntriesReset).NotTo(ContainSubstring("*"))
			Expect(mferegistry.EntriesReset).NotTo(ContainSubstring(">"))
		})

		It("re-announces once the drawn delay elapses, through the same call start-up uses", func() {
			announced := make(chan struct{}, 4)
			w := newResetWatcher(pluginID, func(context.Context) error {
				announced <- struct{}{}
				return nil
			}, quiet)
			w.draw = func(time.Duration) time.Duration { return time.Millisecond }
			bus := &recordingBus{}
			stop, err := w.Watch(context.Background(), bus)
			Expect(err).NotTo(HaveOccurred())
			defer stop()
			bus.handler(&nats.Msg{Subject: mferegistry.EntriesReset, Data: notice(mferegistry.ResetNotice{})})
			Eventually(announced).Should(Receive())
			Consistently(announced, 50*time.Millisecond).ShouldNot(Receive())
		})

		It("does not re-announce when the process is shutting down mid-wait", func() {
			announced := make(chan struct{}, 1)
			w := newResetWatcher(pluginID, func(context.Context) error {
				announced <- struct{}{}
				return nil
			}, quiet)
			w.draw = func(time.Duration) time.Duration { return time.Hour }
			ctx, cancel := context.WithCancel(context.Background())
			bus := &recordingBus{}
			stop, err := w.Watch(ctx, bus)
			Expect(err).NotTo(HaveOccurred())
			defer stop()
			bus.handler(&nats.Msg{Subject: mferegistry.EntriesReset, Data: notice(mferegistry.ResetNotice{})})
			cancel()
			Consistently(announced, 50*time.Millisecond).ShouldNot(Receive())
		})
	})
})
