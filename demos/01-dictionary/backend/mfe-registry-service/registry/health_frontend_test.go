package registry_test

// Phase 5d — the frontend probe, against a real HTTP server (BR-AS61).
//
// This is the one place the registry reaches out of the process on health's
// behalf, so the interesting specs are all about what it REFUSES: an origin
// nobody mapped, a redirect to somewhere else, a body big enough to be a
// payload rather than a status, a 200 carrying something that is not the
// agreed shape. Every one of those is a failed probe and none of them is
// health — "we could not tell" and "it is fine" must never be spelled the
// same way.
//
// A passing probe is also deliberately narrow: it says an HTTP endpoint
// answered, and nothing at all about whether a browser can fetch
// remoteEntry.js from that origin. The loader still owns that.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/domain"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/healthhttp"
)

var _ = Describe("BR-AS61 — a frontend is probed through a bounded, mapped endpoint", func() {
	var (
		client *healthhttp.Client
		ctx    context.Context
	)

	BeforeEach(func() {
		client = healthhttp.New()
		DeferCleanup(client.Close)
		ctx = context.Background()
	})

	// serving stands up a /healthz that answers however the spec says.
	serving := func(handler http.HandlerFunc) *httptest.Server {
		mux := http.NewServeMux()
		mux.HandleFunc("/healthz", handler)
		s := httptest.NewServer(mux)
		DeferCleanup(s.Close)
		return s
	}

	ok := func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}

	Context("the mapping", func() {
		allowed := registry.ParseAllowedOrigins("http://localhost:7111")

		targets := func(mapping map[string]string) domain.HealthOrigins {
			raw, err := json.Marshal(mapping)
			Expect(err).NotTo(HaveOccurred())
			origins, warnings := registry.ParseHealthOrigins(string(raw), allowed)
			Expect(warnings).To(BeEmpty())
			return origins
		}

		entry := func() domain.Entry { return federated("fleet-ops", "http://localhost:7111/remoteEntry.js") }

		It("probes the address the deployment mapped, at /healthz", func() {
			target, signal := targets(map[string]string{"http://localhost:7111": "http://service:7111"}).Target(entry())

			Expect(target).To(Equal("http://service:7111/healthz"))
			Expect(signal.State).To(BeEmpty(), "a mapped target has nothing to report yet")
		})

		It("says not configured for an origin nobody mapped, and never falls back to the browser URL", func() {
			// In Docker, localhost names THIS service, not the plugin. Guessing
			// would turn missing config into outbound reach the operator never
			// granted (BR-AS20/AS45).
			target, signal := targets(map[string]string{}).Target(entry())

			Expect(target).To(BeEmpty())
			Expect(signal.State).To(Equal(domain.HealthNotConfigured))
		})

		It("refuses a mapping for an origin that is not allowlisted", func() {
			raw, err := json.Marshal(map[string]string{"http://evil.test": "http://evil.test"})
			Expect(err).NotTo(HaveOccurred())
			origins, warnings := registry.ParseHealthOrigins(string(raw), allowed)

			Expect(warnings).ToNot(BeEmpty())
			target, signal := origins.Target(entry())
			Expect(target).To(BeEmpty())
			Expect(signal.State).To(Equal(domain.HealthNotConfigured))
		})

		It("refuses an address carrying a path, query or credentials", func() {
			raw, err := json.Marshal(map[string]string{"http://localhost:7111": "http://user:pw@service:7111/deep?x=1"})
			Expect(err).NotTo(HaveOccurred())
			_, warnings := registry.ParseHealthOrigins(string(raw), allowed)

			Expect(warnings).ToNot(BeEmpty())
		})

		It("does not check an entry whose own remote is not allowlisted", func() {
			outside := federated("fleet-ops", "http://elsewhere.test/remoteEntry.js")
			target, signal := targets(map[string]string{"http://localhost:7111": "http://service:7111"}).Target(outside)

			Expect(target).To(BeEmpty())
			Expect(signal.State).To(Equal(domain.HealthNotConfigured))
		})
	})

	Context("the probe itself", func() {
		It("reports a well-formed 200 as healthy", func() {
			origin := serving(ok)

			probe := client.Probe(ctx, origin.URL+"/healthz", time.Now())

			Expect(probe.OK).To(BeTrue())
		})

		It("refuses a 200 whose body is not the agreed shape", func() {
			origin := serving(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{"status":"probably"}`))
			})

			probe := client.Probe(ctx, origin.URL+"/healthz", time.Now())

			Expect(probe.OK).To(BeFalse())
			Expect(probe.Cause).To(Equal("invalid-response"))
		})

		It("refuses a body that is not JSON at all", func() {
			origin := serving(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`<html>service is up</html>`))
			})

			Expect(client.Probe(ctx, origin.URL+"/healthz", time.Now()).Cause).To(Equal("invalid-response"))
		})

		It("refuses a non-success status", func() {
			origin := serving(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) })

			probe := client.Probe(ctx, origin.URL+"/healthz", time.Now())

			Expect(probe.OK).To(BeFalse())
			Expect(probe.Cause).To(Equal("http-status"))
		})

		It("refuses an oversized body instead of reading it", func() {
			// A status endpoint answers in bytes, not megabytes. Without the
			// cap, a hostile or broken origin could spend this service's
			// memory once every five seconds.
			origin := serving(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{"status":"ok","pad":"` + strings.Repeat("x", 70000) + `"}`))
			})

			Expect(client.Probe(ctx, origin.URL+"/healthz", time.Now()).Cause).To(Equal("body-too-large"))
		})

		It("does not follow a redirect to another destination", func() {
			elsewhere := serving(ok)
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, elsewhere.URL+"/healthz", http.StatusFound)
			}))
			DeferCleanup(origin.Close)

			probe := client.Probe(ctx, origin.URL+"/healthz", time.Now())

			Expect(probe.OK).To(BeFalse(), "the configured address answers or nothing does")
			Expect(probe.Cause).To(Equal("http-status"))
		})

		It("gives up at the timeout rather than hanging the schedule", func() {
			origin := serving(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() })

			started := time.Now()
			probe := client.Probe(ctx, origin.URL+"/healthz", started)

			Expect(probe.OK).To(BeFalse())
			Expect(probe.Cause).To(Equal("timeout"))
			Expect(time.Since(started)).To(BeNumerically("<", 2*domain.HealthProbeTimeout))
		})

		It("reports a dead address as unreachable, without saying which address", func() {
			// The cause vocabulary is closed for the same reason drift's is: a
			// transport error carries the host and port that were dialled, and
			// that is deployment topology, not news for a browser (BR-AS60).
			probe := client.Probe(ctx, "http://127.0.0.1:1/healthz", time.Now())

			Expect(probe.OK).To(BeFalse())
			Expect(probe.Cause).To(Equal("unreachable"))
			Expect(probe.Cause).ToNot(ContainSubstring("127.0.0.1"))
		})

		It("stamps the probe with the time it was started", func() {
			origin := serving(ok)
			at := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

			Expect(client.Probe(ctx, origin.URL+"/healthz", at).At).To(Equal(at))
		})
	})
})
