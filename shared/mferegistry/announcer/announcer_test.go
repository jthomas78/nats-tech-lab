package announcer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jthomas78/nats-tech-lab/shared/mferegistry"
	registryclient "github.com/jthomas78/nats-tech-lab/shared/mferegistry/client"
	"github.com/nats-io/nkeys"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestAnnouncer(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "MFE Registry Announcer Suite")
}

type recordingPublisher struct {
	announcements []json.RawMessage
	unregisters   []int64
	announceOut   mferegistry.Response
	unregisterErr error
}

func (p *recordingPublisher) Announce(_ context.Context, manifest json.RawMessage) (mferegistry.Response, error) {
	p.announcements = append(p.announcements, append(json.RawMessage(nil), manifest...))
	return p.announceOut, nil
}

func (p *recordingPublisher) Unregister(_ context.Context, _ string, release int64) (mferegistry.UnregisterResponse, error) {
	p.unregisters = append(p.unregisters, release)
	return mferegistry.UnregisterResponse{OK: true, Outcome: mferegistry.UnregisterWithdrawn}, p.unregisterErr
}

var _ = Describe("Resident plugin announcer", func() {
	const pluginID = "plugin-a"
	manifest := json.RawMessage(`{"id":"plugin-a","remote":{"url":"/remoteEntry.js"},"release":999}`)

	newResident := func(path string, publisher *recordingPublisher, log *slog.Logger, recovery int64) *resident {
		return &resident{pluginID: pluginID, manifest: manifest, publicOrigin: "https://plugins.example.test", publisher: publisher, releases: newReleaseStore(path, pluginID, recovery), log: log}
	}

	Context("BR-AS67 — one persisted monotonic release sequence", func() {
		It("spends N, N+1 and N+2 across announce, unregister and re-announce", func() {
			path := filepath.Join(GinkgoT().TempDir(), "release.json")
			first := newReleaseStore(path, pluginID, 0)
			announce, fresh, err := first.PrepareAnnounce()
			Expect(err).NotTo(HaveOccurred())
			Expect(fresh).To(BeTrue())
			Expect(announce).To(Equal(int64(1)))

			withdraw, err := newReleaseStore(path, pluginID, 0).PrepareUnregister()
			Expect(err).NotTo(HaveOccurred())
			Expect(withdraw).To(Equal(int64(2)))

			reannounce, fresh, err := newReleaseStore(path, pluginID, 0).PrepareAnnounce()
			Expect(err).NotTo(HaveOccurred())
			Expect(fresh).To(BeFalse())
			Expect(reannounce).To(Equal(int64(3)))
		})

		It("retries the current announce release after a crash", func() {
			path := filepath.Join(GinkgoT().TempDir(), "release.json")
			release, _, err := newReleaseStore(path, pluginID, 0).PrepareAnnounce()
			Expect(err).NotTo(HaveOccurred())
			retry, fresh, err := newReleaseStore(path, pluginID, 0).PrepareAnnounce()
			Expect(err).NotTo(HaveOccurred())
			Expect(fresh).To(BeFalse())
			Expect(retry).To(Equal(release))
		})

		It("requires explicit recovery when lost state reuses a spent release", func() {
			publisher := &recordingPublisher{announceOut: mferegistry.Response{OK: true, NoOp: true}}
			r := newResident(filepath.Join(GinkgoT().TempDir(), "lost.json"), publisher, slog.Default(), 0)
			Expect(r.announce(context.Background())).To(MatchError(ErrReleaseRecoveryRequired))

			release, fresh, err := newReleaseStore(filepath.Join(GinkgoT().TempDir(), "recovered.json"), pluginID, 23).PrepareAnnounce()
			Expect(err).NotTo(HaveOccurred())
			Expect(fresh).To(BeTrue())
			Expect(release).To(Equal(int64(23)))
		})

		It("refuses exhausted, mismatched and malformed state", func() {
			path := filepath.Join(GinkgoT().TempDir(), "release.json")
			Expect(persistReleaseState(path, releaseState{SchemaVersion: releaseStateSchemaVersion, Plugin: pluginID, Release: math.MaxInt64, Action: releaseUnregister})).To(Succeed())
			_, _, err := newReleaseStore(path, pluginID, 0).PrepareAnnounce()
			Expect(err).To(MatchError("publisher release counter exhausted"))

			other := filepath.Join(GinkgoT().TempDir(), "other.json")
			Expect(persistReleaseState(other, releaseState{SchemaVersion: releaseStateSchemaVersion, Plugin: "other", Release: 1, Action: releaseAnnounce})).To(Succeed())
			_, _, err = newReleaseStore(other, pluginID, 0).PrepareAnnounce()
			Expect(err).To(MatchError("release state does not match this plugin or schema"))
		})

		It("requires the configured state path instead of falling back inside the image", func() {
			cfg := validConfig()
			cfg.ReleaseStatePath = ""
			Expect(cfg.Validate()).To(MatchError("RELEASE_STATE_PATH is required"))
		})

		It("uses one state schema in both CLI and host directions", func() {
			path := filepath.Join(GinkgoT().TempDir(), "release.json")
			cli := newReleaseStore(path, pluginID, 0)
			_, _, err := cli.PrepareAnnounce()
			Expect(err).NotTo(HaveOccurred())
			host := newReleaseStore(path, pluginID, 0)
			withdraw, err := host.PrepareUnregister()
			Expect(err).NotTo(HaveOccurred())
			Expect(withdraw).To(Equal(int64(2)))

			path = filepath.Join(GinkgoT().TempDir(), "reverse.json")
			host = newReleaseStore(path, pluginID, 0)
			_, _, err = host.PrepareAnnounce()
			Expect(err).NotTo(HaveOccurred())
			_, err = host.PrepareUnregister()
			Expect(err).NotTo(HaveOccurred())
			cli = newReleaseStore(path, pluginID, 0)
			next, _, err := cli.PrepareAnnounce()
			Expect(err).NotTo(HaveOccurred())
			Expect(next).To(Equal(int64(3)))
		})
	})

	Context("BR-AS54 — only SIGTERM authorizes publisher withdrawal", func() {
		It("does not unregister or spend a release for a crash or failed health check", func() {
			for _, reason := range []exitReason{exitCrash, exitHealthCheckFailure} {
				path := filepath.Join(GinkgoT().TempDir(), string(reason)+".json")
				publisher := &recordingPublisher{}
				r := newResident(path, publisher, slog.Default(), 0)
				_, _, err := r.releases.PrepareAnnounce()
				Expect(err).NotTo(HaveOccurred())
				Expect(r.shutdown(context.Background(), reason)).To(Succeed())
				Expect(publisher.unregisters).To(BeEmpty())
				retry, _, err := newReleaseStore(path, pluginID, 0).PrepareAnnounce()
				Expect(err).NotTo(HaveOccurred())
				Expect(retry).To(Equal(int64(1)))
			}
		})

		It("does not self-withdraw when its health connection is down and reports fall silent", func() {
			path := filepath.Join(GinkgoT().TempDir(), "release.json")
			publisher := &recordingPublisher{}
			r := newResident(path, publisher, slog.Default(), 0)
			first, _, err := r.releases.PrepareAnnounce()
			Expect(err).NotTo(HaveOccurred())

			// The reporter has no route to the publisher lifecycle. A NATS
			// outage makes the registry eventually observe absence; it must not
			// make the plugin manufacture an authoritative unregister about
			// itself.
			reporter := newHealthReporter(pluginID, func(context.Context) string { return "" }, &recordingHealthBus{err: errors.New("NATS disconnected")}, slog.Default())
			reporter.Step(context.Background(), time.Now())
			Expect(r.shutdown(context.Background(), exitHealthCheckFailure)).To(Succeed())
			Expect(publisher.unregisters).To(BeEmpty())
			retry, _, err := newReleaseStore(path, pluginID, 0).PrepareAnnounce()
			Expect(err).NotTo(HaveOccurred())
			Expect(retry).To(Equal(first))
		})

		It("publishes unregister on SIGTERM and persists the spent release", func() {
			path := filepath.Join(GinkgoT().TempDir(), "release.json")
			publisher := &recordingPublisher{}
			r := newResident(path, publisher, slog.Default(), 0)
			_, _, err := r.releases.PrepareAnnounce()
			Expect(err).NotTo(HaveOccurred())
			Expect(r.shutdown(context.Background(), exitSIGTERM)).To(Succeed())
			Expect(publisher.unregisters).To(Equal([]int64{2}))
			next, _, err := newReleaseStore(path, pluginID, 0).PrepareAnnounce()
			Expect(err).NotTo(HaveOccurred())
			Expect(next).To(Equal(int64(3)))
		})

		It("warns on a failed SIGTERM unregister and still exits", func() {
			path := filepath.Join(GinkgoT().TempDir(), "release.json")
			publisher := &recordingPublisher{unregisterErr: errors.New("nats unavailable")}
			var logs bytes.Buffer
			r := newResident(path, publisher, slog.New(slog.NewTextHandler(&logs, nil)), 0)
			_, _, err := r.releases.PrepareAnnounce()
			Expect(err).NotTo(HaveOccurred())
			Expect(r.shutdown(context.Background(), exitSIGTERM)).To(Succeed())
			Expect(logs.String()).To(ContainSubstring("unregister failed during SIGTERM shutdown"))
		})

		It("returns on ordinary context cancellation without unregistering", func() {
			publisher := &recordingPublisher{}
			dir := GinkgoT().TempDir()
			manifestPath := filepath.Join(dir, "manifest.json")
			Expect(os.WriteFile(manifestPath, manifest, 0o600)).To(Succeed())
			cfg := validConfig()
			cfg.ManifestPath = manifestPath
			cfg.ReleaseStatePath = filepath.Join(dir, "release.json")
			cfg.publisher = publisher
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			Expect(Start(ctx, cfg)).To(Succeed())
			Expect(publisher.unregisters).To(BeEmpty())
		})
	})

	Context("BR-AS71 — deployment origin is signed", func() {
		It("stamps the configured origin with the release before publishing", func() {
			publisher := &recordingPublisher{announceOut: mferegistry.Response{OK: true}}
			r := newResident(filepath.Join(GinkgoT().TempDir(), "release.json"), publisher, slog.Default(), 0)
			Expect(r.announce(context.Background())).To(Succeed())
			var sent struct {
				Release int64 `json:"release"`
				Remote  struct {
					URL string `json:"url"`
				} `json:"remote"`
			}
			Expect(json.Unmarshal(publisher.announcements[0], &sent)).To(Succeed())
			Expect(sent.Release).To(Equal(int64(1)))
			Expect(sent.Remote.URL).To(Equal("https://plugins.example.test/remoteEntry.js"))
		})

		It("makes a post-signature origin rewrite fail attestation", func() {
			kp, err := nkeys.CreateUser()
			Expect(err).NotTo(HaveOccurred())
			signer, err := registryclient.NewNKeySigner(mustSeed(kp))
			Expect(err).NotTo(HaveOccurred())
			client := registryclient.New(nil, signer, pluginID)
			stamped, err := manifestForAnnouncement(manifest, 1, "https://plugins.example.test")
			Expect(err).NotTo(HaveOccurred())
			req, err := client.BuildAnnounce(stamped)
			Expect(err).NotTo(HaveOccurred())
			mutated := bytes.Replace(req.Payload, []byte("plugins.example.test"), []byte("evil.example.test"), 1)
			sig, err := decodeSignature(req.Signature)
			Expect(err).NotTo(HaveOccurred())
			Expect(kp.Verify(mutated, sig)).To(HaveOccurred())
		})
	})
})

func validConfig() Config {
	return Config{NATSURL: "nats://127.0.0.1:4222", NATSCredsPath: "/creds", ManifestPath: "/manifest", SigningSeedPath: "/seed", ReleaseStatePath: "/state", PublisherID: "plugin-a", ConnectionName: "plugin-a", PublicOrigin: "https://plugins.example.test", SelfCheckURL: "http://127.0.0.1:8080/healthz"}
}

func mustSeed(kp nkeys.KeyPair) []byte {
	seed, err := kp.Seed()
	Expect(err).NotTo(HaveOccurred())
	return seed
}
