package announcer

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"time"

	"github.com/jthomas78/nats-tech-lab/shared/mferegistry"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type recordingHealthBus struct {
	subjects []string
	reports  []mferegistry.HealthReport
	err      error
}

func (b *recordingHealthBus) Publish(subject string, payload []byte) error {
	if b.err != nil {
		return b.err
	}
	var report mferegistry.HealthReport
	if err := json.Unmarshal(payload, &report); err != nil {
		return err
	}
	b.subjects = append(b.subjects, subject)
	b.reports = append(b.reports, report)
	return nil
}

var _ = Describe("Plugin frontend health reporter", func() {
	const pluginID = "plugin-a"
	quiet := slog.New(slog.NewTextHandler(GinkgoWriter, nil))
	at := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)

	newReporter := func(bus *recordingHealthBus, check selfCheck) *healthReporter {
		return newHealthReporter(pluginID, check, bus, quiet)
	}

	// A self-check that answers without touching the network, so the specs
	// drive the decision machine rather than an HTTP stack.
	answering := func(cause string) selfCheck {
		return func(context.Context) string { return cause }
	}

	Context("BR-AS61 — a plugin reports its own health, and each report costs a real local request", func() {
		It("publishes on the subject derived from its own plugin id, and on no other", func() {
			bus := &recordingHealthBus{}
			newReporter(bus, answering("")).Step(context.Background(), at)
			Expect(bus.subjects).To(ConsistOf("notify._platform.health.frontend.plugin-a.v1"))
			Expect(bus.subjects[0]).To(Equal(mferegistry.FrontendHealth(pluginID)))
		})

		It("carries its own plugin id in the body as well as the subject", func() {
			bus := &recordingHealthBus{}
			newReporter(bus, answering("")).Step(context.Background(), at)
			Expect(bus.reports[0].PluginID).To(Equal(pluginID))
		})

		It("reports healthy only when the local request actually succeeded", func() {
			bus := &recordingHealthBus{}
			newReporter(bus, answering("")).Step(context.Background(), at)
			Expect(bus.reports[0].State).To(Equal(mferegistry.HealthReportHealthy))
			Expect(bus.reports[0].Cause).To(BeEmpty())
		})

		It("never reports a state or a cause outside the closed vocabulary", func() {
			for _, cause := range []string{"", mferegistry.HealthCauseTimeout, mferegistry.HealthCauseUnreachable, mferegistry.HealthCauseHTTPStatus, mferegistry.HealthCauseInvalidResponse} {
				bus := &recordingHealthBus{}
				r := newReporter(bus, answering(cause))
				r.Step(context.Background(), at)
				r.Step(context.Background(), at.Add(mferegistry.HealthHeartbeat))
				last := bus.reports[len(bus.reports)-1]
				Expect(mferegistry.ValidHealthReportState(last.State)).To(BeTrue())
				if last.Cause != "" {
					Expect(mferegistry.ValidHealthCause(last.Cause)).To(BeTrue())
				}
			}
		})

		It("never claims the registry's absent cause, which only a receiver may conclude", func() {
			bus := &recordingHealthBus{}
			r := newReporter(bus, answering(mferegistry.HealthCauseUnreachable))
			for i := 0; i < 5; i++ {
				r.Step(context.Background(), at.Add(time.Duration(i)*mferegistry.HealthHeartbeat))
			}
			for _, report := range bus.reports {
				Expect(report.Cause).NotTo(Equal(mferegistry.HealthCauseAbsent))
			}
		})

		It("keeps publishing on a heartbeat even when nothing changed, so silence is a fact and not an inference", func() {
			bus := &recordingHealthBus{}
			r := newReporter(bus, answering(""))
			for i := 0; i < 4; i++ {
				r.Step(context.Background(), at.Add(time.Duration(i)*mferegistry.HealthHeartbeat))
			}
			Expect(bus.reports).To(HaveLen(4))
			Expect(bus.reports[3].At).To(Equal(at.Add(3 * mferegistry.HealthHeartbeat).UnixMilli()))
		})

		It("keeps reporting after a publish fails, rather than latching off", func() {
			bus := &recordingHealthBus{err: errors.New("no responders")}
			r := newReporter(bus, answering(""))
			r.Step(context.Background(), at)
			bus.err = nil
			r.Step(context.Background(), at.Add(mferegistry.HealthHeartbeat))
			Expect(bus.reports).To(HaveLen(1))
		})
	})

	Context("BR-AS63 — the failure threshold is decided by the plugin, about itself", func() {
		It("does not call itself unhealthy on one failed check", func() {
			bus := &recordingHealthBus{}
			r := newReporter(bus, answering(""))
			r.Step(context.Background(), at)
			r.check = answering(mferegistry.HealthCauseTimeout)
			r.Step(context.Background(), at.Add(mferegistry.HealthHeartbeat))
			Expect(bus.reports[1].State).To(Equal(mferegistry.HealthReportHealthy))
		})

		It("reports unhealthy on the second consecutive failure, with the cause of that check", func() {
			bus := &recordingHealthBus{}
			r := newReporter(bus, answering(mferegistry.HealthCauseTimeout))
			r.Step(context.Background(), at)
			r.Step(context.Background(), at.Add(mferegistry.HealthHeartbeat))
			Expect(bus.reports[1].State).To(Equal(mferegistry.HealthReportUnhealthy))
			Expect(bus.reports[1].Cause).To(Equal(mferegistry.HealthCauseTimeout))
		})

		It("resets the run on any success, so one failure per heartbeat forever never reaches the threshold", func() {
			bus := &recordingHealthBus{}
			r := newReporter(bus, answering(""))
			// Starts from a proved-healthy baseline on purpose: a run that
			// begins on a failure is the separate case below, where nothing
			// has ever proved the plugin good.
			for i := 0; i < 6; i++ {
				if i%2 == 0 {
					r.check = answering("")
				} else {
					r.check = answering(mferegistry.HealthCauseTimeout)
				}
				r.Step(context.Background(), at.Add(time.Duration(i)*mferegistry.HealthHeartbeat))
			}
			for _, report := range bus.reports {
				Expect(report.State).To(Equal(mferegistry.HealthReportHealthy))
			}
		})

		It("starts unhealthy rather than healthy when the very first checks fail, because nothing ever proved it good", func() {
			bus := &recordingHealthBus{}
			r := newReporter(bus, answering(mferegistry.HealthCauseUnreachable))
			r.Step(context.Background(), at)
			Expect(bus.reports[0].State).To(Equal(mferegistry.HealthReportUnhealthy))
		})
	})

	Context("BR-AS61 — the self-check deadline expires strictly before the heartbeat", func() {
		It("relates the two rather than pinning either number, so moving one cannot silently overlap the other", func() {
			Expect(mferegistry.HealthSelfCheckTimeout).To(BeNumerically("<", mferegistry.HealthHeartbeat))
		})

		It("bounds the loopback request with that deadline and not with the caller's", func() {
			var deadline time.Time
			var ok bool
			bus := &recordingHealthBus{}
			r := newReporter(bus, func(ctx context.Context) string {
				deadline, ok = ctx.Deadline()
				return ""
			})
			r.Step(context.Background(), at)
			Expect(ok).To(BeTrue())
			Expect(deadline).To(BeTemporally("~", time.Now().Add(mferegistry.HealthSelfCheckTimeout), time.Second))
		})
	})

	Context("BR-AS61 — the loopback check refuses anything that is not a bounded local GET", func() {
		It("accepts only a loopback target, so the check can never become arbitrary egress", func() {
			_, err := newLoopbackCheck("http://plugin-a-frontend:8080/healthz")
			Expect(err).To(HaveOccurred())
			_, err = newLoopbackCheck("http://127.0.0.1:8080/healthz")
			Expect(err).NotTo(HaveOccurred())
			_, err = newLoopbackCheck("http://localhost:8080/healthz")
			Expect(err).NotTo(HaveOccurred())
		})

		It("refuses a target that is not plain HTTP(S) with no credentials, query or fragment", func() {
			for _, bad := range []string{"", "ftp://127.0.0.1/healthz", "http://user:pw@127.0.0.1/healthz", "http://127.0.0.1/healthz?x=1", "http://127.0.0.1/healthz#f"} {
				_, err := newLoopbackCheck(bad)
				Expect(err).To(HaveOccurred(), bad)
			}
		})

		It("calls a real endpoint and accepts only the small validated response", func() {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"status":"ok"}`))
			}))
			DeferCleanup(server.Close)
			check, err := newLoopbackCheck(server.URL + "/healthz")
			Expect(err).NotTo(HaveOccurred())
			Expect(check(context.Background())).To(BeEmpty())
		})

		It("cannot claim health on a non-success status", func() {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, "nope", http.StatusServiceUnavailable)
			}))
			DeferCleanup(server.Close)
			check, _ := newLoopbackCheck(server.URL + "/healthz")
			Expect(check(context.Background())).To(Equal(mferegistry.HealthCauseHTTPStatus))
		})

		It("cannot claim health on a body that is not the expected shape", func() {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{"status":"degraded"}`))
			}))
			DeferCleanup(server.Close)
			check, _ := newLoopbackCheck(server.URL + "/healthz")
			Expect(check(context.Background())).To(Equal(mferegistry.HealthCauseInvalidResponse))
		})

		It("reports unreachable rather than healthy when nothing is listening", func() {
			server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
			url := server.URL + "/healthz"
			server.Close()
			check, _ := newLoopbackCheck(url)
			Expect(check(context.Background())).To(Equal(mferegistry.HealthCauseUnreachable))
		})

		It("does not follow a redirect, so a local server cannot send the check somewhere else", func() {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, "http://example.test/elsewhere", http.StatusFound)
			}))
			DeferCleanup(server.Close)
			check, _ := newLoopbackCheck(server.URL + "/healthz")
			Expect(check(context.Background())).To(Equal(mferegistry.HealthCauseHTTPStatus))
		})
	})
})

// orderingPublisher records how many health reports had already been
// published at the moment the announcement was made. Ordering is asserted
// this way, and never as a line ordering in Start, because the contract is
// "known before discoverable" — an implementation may move as long as an
// observer never sees an entry with no health behind it.
type orderingPublisher struct {
	recordingPublisher
	bus               *recordingHealthBus
	reportsAtAnnounce int
}

func (p *orderingPublisher) Announce(ctx context.Context, manifest json.RawMessage) (mferegistry.Response, error) {
	p.reportsAtAnnounce = len(p.bus.reports)
	return p.recordingPublisher.Announce(ctx, manifest)
}

var _ = Describe("Publisher lifecycle with frontend health", func() {
	const pluginID = "plugin-a"
	manifest := json.RawMessage(`{"id":"plugin-a","remote":{"url":"/remoteEntry.js"},"release":999}`)

	healthyServer := func() string {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		}))
		DeferCleanup(server.Close)
		return server.URL + "/healthz"
	}

	startConfig := func(bus *recordingHealthBus) (Config, string) {
		dir := GinkgoT().TempDir()
		manifestPath := filepath.Join(dir, "manifest.json")
		Expect(os.WriteFile(manifestPath, manifest, 0o600)).To(Succeed())
		cfg := validConfig()
		cfg.ManifestPath = manifestPath
		cfg.ReleaseStatePath = filepath.Join(dir, "release.json")
		cfg.SelfCheckURL = healthyServer()
		cfg.healthBus = bus
		return cfg, dir
	}

	Context("BR-AS61 — first health push, then announce", func() {
		It("has already reported its health by the time an announcement reaches the registry", func() {
			bus := &recordingHealthBus{}
			publisher := &orderingPublisher{bus: bus}
			cfg, _ := startConfig(bus)
			cfg.publisher = publisher
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			Expect(Start(ctx, cfg)).To(Succeed())
			Expect(publisher.announcements).To(HaveLen(1))
			Expect(publisher.reportsAtAnnounce).To(BeNumerically(">=", 1))
		})

		It("refuses to start at all when the self-check target is not a bounded loopback GET", func() {
			bus := &recordingHealthBus{}
			cfg, _ := startConfig(bus)
			cfg.publisher = &recordingPublisher{}
			cfg.SelfCheckURL = "http://plugin-a-frontend:8080/healthz"
			Expect(Start(context.Background(), cfg)).To(HaveOccurred())
			Expect(bus.reports).To(BeEmpty())
		})
	})

	Context("BR-AS61 — every plugin reports, curated entries included", func() {
		It("reports health without announcing when the plugin is curated", func() {
			bus := &recordingHealthBus{}
			publisher := &recordingPublisher{}
			cfg := Config{
				NATSURL:        "nats://127.0.0.1:4222",
				NATSCredsPath:  "/creds",
				PublisherID:    pluginID,
				ConnectionName: pluginID,
				HealthOnly:     true,
				SelfCheckURL:   healthyServer(),
				publisher:      publisher,
				healthBus:      bus,
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			Expect(Start(ctx, cfg)).To(Succeed())
			Expect(bus.reports).To(HaveLen(1))
			Expect(bus.reports[0].PluginID).To(Equal(pluginID))
			Expect(publisher.announcements).To(BeEmpty())
			Expect(publisher.unregisters).To(BeEmpty())
		})

		It("does not require a signing seed, a manifest or a release sequence, because it never signs anything", func() {
			cfg := Config{
				NATSCredsPath:  "/creds",
				PublisherID:    pluginID,
				ConnectionName: pluginID,
				HealthOnly:     true,
				SelfCheckURL:   "http://127.0.0.1:8080/healthz",
			}
			Expect(cfg.Validate()).To(Succeed())
		})
	})

	Context("BR-AS61 — the self-check target is deployment configuration", func() {
		It("is required of an announcing publisher, so no plugin is structurally unhealth-checkable", func() {
			cfg := validConfig()
			cfg.SelfCheckURL = ""
			Expect(cfg.Validate()).To(MatchError(ContainSubstring("HEALTH_SELF_URL")))
		})
	})
})
