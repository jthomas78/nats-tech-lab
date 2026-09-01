package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/jthomas78/nats-tech-lab/shared/mferegistry"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestAnnouncePlugin(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Resident Plugin Announcer Suite")
}

type recordingPublisher struct {
	announcements []json.RawMessage
	unregisters   []int64
	announceOut   mferegistry.Response
	announceErr   error
	unregisterErr error
}

func (p *recordingPublisher) Announce(_ context.Context, manifest json.RawMessage) (mferegistry.Response, error) {
	p.announcements = append(p.announcements, append(json.RawMessage(nil), manifest...))
	return p.announceOut, p.announceErr
}

func (p *recordingPublisher) Unregister(_ context.Context, _ string, release int64) (mferegistry.UnregisterResponse, error) {
	p.unregisters = append(p.unregisters, release)
	return mferegistry.UnregisterResponse{OK: true, Outcome: mferegistry.UnregisterWithdrawn}, p.unregisterErr
}

var _ = Describe("Resident plugin announcer", func() {
	const pluginID = "plugin-a"
	manifest := json.RawMessage(`{"id":"plugin-a","name":"Plugin A","release":999}`)

	newTestResident := func(path string, publisher *recordingPublisher, log *slog.Logger, recovery int64) *resident {
		return &resident{
			pluginID:  pluginID,
			manifest:  manifest,
			publisher: publisher,
			releases:  newReleaseStore(path, pluginID, recovery),
			log:       log,
		}
	}

	Context("BR-AS67 — one persisted monotonic release sequence", func() {
		It("spends N, N+1 and N+2 across announce, unregister and re-announce", func() {
			path := filepath.Join(GinkgoT().TempDir(), "release.json")

			first := newReleaseStore(path, pluginID, 0)
			announce, fresh, err := first.PrepareAnnounce()
			Expect(err).NotTo(HaveOccurred())
			Expect(fresh).To(BeTrue())
			Expect(announce).To(Equal(int64(1)))

			restarted := newReleaseStore(path, pluginID, 0)
			withdraw, err := restarted.PrepareUnregister()
			Expect(err).NotTo(HaveOccurred())
			Expect(withdraw).To(Equal(int64(2)))

			returned := newReleaseStore(path, pluginID, 0)
			reannounce, fresh, err := returned.PrepareAnnounce()
			Expect(err).NotTo(HaveOccurred())
			Expect(fresh).To(BeFalse())
			Expect(reannounce).To(Equal(int64(3)))
		})

		It("retries the current announce release after a crash instead of claiming a new availability action", func() {
			path := filepath.Join(GinkgoT().TempDir(), "release.json")
			first := newReleaseStore(path, pluginID, 0)
			release, _, err := first.PrepareAnnounce()
			Expect(err).NotTo(HaveOccurred())

			afterCrash := newReleaseStore(path, pluginID, 0)
			retry, fresh, err := afterCrash.PrepareAnnounce()
			Expect(err).NotTo(HaveOccurred())
			Expect(fresh).To(BeFalse())
			Expect(retry).To(Equal(release))
		})

		It("requires explicit recovery when lost state reuses a spent release and the server returns NoOp", func() {
			path := filepath.Join(GinkgoT().TempDir(), "lost-release.json")
			publisher := &recordingPublisher{announceOut: mferegistry.Response{OK: true, NoOp: true}}
			announcer := newTestResident(path, publisher, slog.Default(), 0)

			err := announcer.announce(context.Background())

			Expect(err).To(MatchError(ErrReleaseRecoveryRequired))
			Expect(publisher.announcements).To(HaveLen(1))
			var sent struct {
				Release int64 `json:"release"`
			}
			Expect(json.Unmarshal(publisher.announcements[0], &sent)).To(Succeed())
			Expect(sent.Release).To(Equal(int64(1)))

			recovered := newReleaseStore(filepath.Join(GinkgoT().TempDir(), "recovered.json"), pluginID, 23)
			release, fresh, err := recovered.PrepareAnnounce()
			Expect(err).NotTo(HaveOccurred())
			Expect(fresh).To(BeTrue())
			Expect(release).To(Equal(int64(23)))
		})
	})

	Context("BR-AS54 — only SIGTERM authorizes publisher withdrawal", func() {
		It("does not unregister or spend a release for a crash or failed health check", func() {
			for _, reason := range []exitReason{exitCrash, exitHealthCheckFailure} {
				path := filepath.Join(GinkgoT().TempDir(), string(reason)+".json")
				publisher := &recordingPublisher{}
				announcer := newTestResident(path, publisher, slog.Default(), 0)
				_, _, err := announcer.releases.PrepareAnnounce()
				Expect(err).NotTo(HaveOccurred())

				Expect(announcer.shutdown(context.Background(), reason)).To(Succeed())
				Expect(publisher.unregisters).To(BeEmpty(), "reason %s must not unregister", reason)
				retry, _, err := newReleaseStore(path, pluginID, 0).PrepareAnnounce()
				Expect(err).NotTo(HaveOccurred())
				Expect(retry).To(Equal(int64(1)), "reason %s must not spend a release", reason)
			}
		})

		It("publishes unregister on SIGTERM and persists the spent release", func() {
			path := filepath.Join(GinkgoT().TempDir(), "release.json")
			publisher := &recordingPublisher{}
			announcer := newTestResident(path, publisher, slog.Default(), 0)
			_, _, err := announcer.releases.PrepareAnnounce()
			Expect(err).NotTo(HaveOccurred())

			Expect(announcer.shutdown(context.Background(), exitSIGTERM)).To(Succeed())

			Expect(publisher.unregisters).To(Equal([]int64{2}))
			next, _, err := newReleaseStore(path, pluginID, 0).PrepareAnnounce()
			Expect(err).NotTo(HaveOccurred())
			Expect(next).To(Equal(int64(3)))
		})

		It("logs a failed SIGTERM unregister as a warning and still exits", func() {
			path := filepath.Join(GinkgoT().TempDir(), "release.json")
			publisher := &recordingPublisher{unregisterErr: errors.New("nats unavailable")}
			var logs bytes.Buffer
			log := slog.New(slog.NewTextHandler(&logs, nil))
			announcer := newTestResident(path, publisher, log, 0)
			_, _, err := announcer.releases.PrepareAnnounce()
			Expect(err).NotTo(HaveOccurred())

			Expect(announcer.shutdown(context.Background(), exitSIGTERM)).To(Succeed())

			Expect(publisher.unregisters).To(Equal([]int64{2}))
			Expect(logs.String()).To(ContainSubstring("unregister failed during SIGTERM shutdown"))
			next, _, err := newReleaseStore(path, pluginID, 0).PrepareAnnounce()
			Expect(err).NotTo(HaveOccurred())
			Expect(next).To(Equal(int64(3)))
		})
	})
})
