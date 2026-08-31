package registry_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/application"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/browserrpc"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/domain"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/manifesthttp"
)

type driftCatalog struct{ doc domain.Document }

func (c driftCatalog) Curated(context.Context) (domain.Document, error) { return c.doc, nil }
func (c driftCatalog) Sources(context.Context) map[string]domain.Registration {
	out := map[string]domain.Registration{}
	for _, e := range c.doc.Entries {
		out[e.ID] = domain.Registration{Source: domain.SourcePreload}
	}
	return out
}

type manifestFetch func(context.Context, string) ([]byte, error)

func (f manifestFetch) Fetch(ctx context.Context, target string) ([]byte, error) {
	return f(ctx, target)
}

var _ = Describe("Phase 8c — manifest drift", func() {
	var entry domain.Entry
	var body []byte
	var allowed domain.Allowlist
	var origins domain.FetchOrigins
	var schedule application.DriftSchedule
	BeforeEach(func() {
		entry = federated("example", "http://localhost:7111/remoteEntry.js")
		entry.Name = "Example"
		entry.Contributions = []domain.Contribution{}
		body, _ = json.Marshal(entry)
		allowed = registry.ParseAllowedOrigins("http://localhost:7111")
		var warnings []string
		origins, warnings = registry.ParseFetchOrigins(`{"http://localhost:7111":"http://plugin:80"}`, allowed)
		Expect(warnings).To(BeEmpty())
		schedule = application.DriftSchedule{Timeout: 40 * time.Millisecond, RetryDelay: time.Millisecond, PollInterval: time.Hour}
	})

	start := func(fetch application.ManifestFetcher) *application.DriftChecker {
		checker := application.NewDriftChecker(driftCatalog{domain.Document{Revision: 7, Entries: []domain.Entry{entry}}}, origins, fetch, schedule)
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() { defer close(done); checker.Run(ctx) }()
		DeferCleanup(func() { cancel(); Eventually(done).Should(BeClosed()) })
		return checker
	}

	Context("BR-AS45 — failure is not agreement", func() {
		DescribeTable("an unknown field cannot be silently discarded into agreement", func(extra map[string]any) {
			var served map[string]any
			Expect(json.Unmarshal(body, &served)).To(Succeed())
			for field, value := range extra {
				served[field] = value
			}
			raw, err := json.Marshal(served)
			Expect(err).NotTo(HaveOccurred())
			result := domain.CompareManifest(entry, raw)
			Expect(result.State).To(Equal("not checked"))
			Expect(result.Cause).To(Equal("invalid-manifest"))
			encoded, _ := json.Marshal(result)
			Expect(string(encoded)).NotTo(ContainSubstring("private.example"))
		}, Entry("top level", map[string]any{"https://private.example/new-field": true}),
			Entry("inside remote", map[string]any{"remote": map[string]any{"kind": "federated", "url": "http://localhost:7111/remoteEntry.js", "module": "./plugin", "newField": true}}),
			Entry("inside a contribution", map[string]any{"contributions": []any{map[string]any{"kind": "route", "id": "extra", "newField": true}}}))

		It("replaces a successful check with not checked when the next fetch fails", func() {
			schedule.PollInterval = 10 * time.Millisecond
			var failing atomic.Bool
			checker := start(manifestFetch(func(context.Context, string) ([]byte, error) {
				if failing.Load() {
					return nil, errors.New("dial http://private-origin:80: refused")
				}
				return body, nil
			}))
			Eventually(func() string { return checker.Snapshot(entry, domain.SourcePreload).State }).Should(Equal("checked"))
			failing.Store(true)
			Eventually(func() string { return checker.Snapshot(entry, domain.SourcePreload).State }).Should(Equal("not checked"))
			Expect(checker.Snapshot(entry, domain.SourcePreload).Cause).To(Equal("fetch-failed"))
		})
		It("times out a hanging origin and stops after one retry", func() {
			var calls atomic.Int32
			checker := start(manifestFetch(func(ctx context.Context, _ string) ([]byte, error) {
				calls.Add(1)
				<-ctx.Done()
				return nil, ctx.Err()
			}))
			Eventually(func() string { return checker.Snapshot(entry, domain.SourcePreload).Cause }).Should(Equal("timeout"))
			Eventually(calls.Load).Should(Equal(int32(2)))
			Consistently(calls.Load, 100*time.Millisecond).Should(Equal(int32(2)))
		})
		It("recovers on the one bounded retry", func() {
			var calls atomic.Int32
			checker := start(manifestFetch(func(context.Context, string) ([]byte, error) {
				if calls.Add(1) == 1 {
					return nil, errors.New("offline")
				}
				return body, nil
			}))
			Eventually(func() string { return checker.Snapshot(entry, domain.SourcePreload).State }).Should(Equal("checked"))
			Expect(calls.Load()).To(Equal(int32(2)))
		})
		DescribeTable("unparsable or incomplete bodies are not checked", func(raw string) {
			result := domain.CompareManifest(entry, []byte(raw))
			Expect(result.State).To(Equal("not checked"))
			Expect(result.Cause).To(Equal("invalid-manifest"))
		}, Entry("broken JSON", `{`), Entry("null", `null`), Entry("empty object", `{}`), Entry("array", `[]`), Entry("wrong field type", `{"id":123}`), Entry("trailing JSON", `{} {}`))
	})

	Context("BR-AS20/BR-AS45 — translation never grants an origin", func() {
		It("leaves an unmapped origin not checked without fetching its browser address", func() {
			origins, _ = registry.ParseFetchOrigins("", allowed)
			var calls atomic.Int32
			checker := start(manifestFetch(func(context.Context, string) ([]byte, error) { calls.Add(1); return body, nil }))
			Expect(checker.Snapshot(entry, domain.SourcePreload).State).To(Equal("not checked"))
			Expect(checker.Snapshot(entry, domain.SourcePreload).Cause).To(Equal("origin-unmapped"))
			Consistently(calls.Load, 50*time.Millisecond).Should(BeZero())
		})
		It("ignores and warns about an unallowlisted mapping without widening configuration", func() {
			mapped, warnings := registry.ParseFetchOrigins(`{"http://localhost:7111":"http://plugin:80","https://untrusted.example":"http://private:80"}`, allowed)
			Expect(warnings).To(HaveLen(1))
			entry.Remote.URL = "https://untrusted.example/remoteEntry.js"
			target, result := mapped.Target(entry, domain.SourcePreload)
			Expect(target).To(BeEmpty())
			Expect(result.State).To(Equal("not checked"))
			Expect(allowed.Origins()).To(Equal([]string{"http://localhost:7111"}))
		})
		It("uses only the mapped manifest address, never the remote's path or query", func() {
			entry.Remote.URL = "http://localhost:7111/private/code.js?token=secret"
			target, _ := origins.Target(entry, domain.SourcePreload)
			Expect(target).To(Equal("http://plugin:80/manifest.json"))
		})
		DescribeTable("refuses unsafe mapping addresses", func(address string) {
			raw, _ := json.Marshal(map[string]string{"http://localhost:7111": address})
			mapped, warnings := registry.ParseFetchOrigins(string(raw), allowed)
			Expect(warnings).NotTo(BeEmpty())
			target, result := mapped.Target(entry, domain.SourcePreload)
			Expect(target).To(BeEmpty())
			Expect(result.State).To(Equal("not checked"))
		}, Entry("credentials", "http://user:password@plugin"), Entry("non-HTTP", "file:///private/file"), Entry("query", "http://plugin?secret=1"), Entry("path", "http://plugin/private"), Entry("fragment", "http://plugin/#secret"))
		It("fails closed on malformed configuration", func() {
			mapped, warnings := registry.ParseFetchOrigins("not json", allowed)
			Expect(warnings).NotTo(BeEmpty())
			target, _ := mapped.Target(entry, domain.SourcePreload)
			Expect(target).To(BeEmpty())
		})
		It("checks only preloaded entries, independent of their lifecycle or enabled state", func() {
			for _, source := range []string{domain.SourceCurated, domain.SourceAnnounced, domain.SourceUnknown} {
				target, result := origins.Target(entry, source)
				Expect(target).To(BeEmpty())
				Expect(result.State).To(Equal("not checked"))
			}
			entry.Enabled = false
			entry.Lifecycle = "dynamic"
			target, _ := origins.Target(entry, domain.SourcePreload)
			Expect(target).NotTo(BeEmpty())
		})
	})

	Context("decisions 77/85 — display only, with differing fields named", func() {
		It("does not round release numbers into agreement", func() {
			entry.Release = 9007199254740992
			served := entry
			served.Release++
			raw, err := json.Marshal(served)
			Expect(err).NotTo(HaveOccurred())
			Expect(domain.CompareManifest(entry, raw).Fields).To(Equal([]string{"release"}))
		})
		It("compares manifest content, ignoring platform-owned state and JSON formatting", func() {
			entry.Enabled = !entry.Enabled
			entry.Lifecycle = "static"
			entry.Withheld = true
			entry.AnnouncedAt = "yesterday"
			entry.LastAnnouncedAt = "today"
			entry.Manifest = &domain.Manifest{Bytes: []byte("signed")}
			Expect(domain.CompareManifest(entry, body).State).To(Equal("checked"))
		})
		It("names changed fields without mutating the curated copy or echoing values", func() {
			served := entry
			served.Version = "new"
			served.Remote.URL = "https://private.example/secret"
			served.Contributions = []domain.Contribution{{Kind: "route", ID: "extra"}}
			raw, _ := json.Marshal(served)
			result := domain.CompareManifest(entry, raw)
			Expect(result.State).To(Equal("drift"))
			Expect(result.Fields).To(Equal([]string{"contributions", "remote", "version"}))
			Expect(entry.Remote.URL).To(Equal("http://localhost:7111/remoteEntry.js"))
			encoded, _ := json.Marshal(result)
			Expect(string(encoded)).NotTo(ContainSubstring("https://"))
		})
		It("does not attach an old agreement to an edited curated entry", func() {
			checker := start(manifestFetch(func(context.Context, string) ([]byte, error) { return body, nil }))
			Eventually(func() string { return checker.Snapshot(entry, domain.SourcePreload).State }).Should(Equal("checked"))
			entry.Version = "edited"
			Expect(checker.Snapshot(entry, domain.SourcePreload).State).To(Equal("not checked"))
		})
		It("keeps diagnostics out of the shell's entry contract", func() {
			view := browserrpc.EntryView{Entry: entry, Drift: domain.CompareManifest(entry, body)}
			operator, _ := json.Marshal(view)
			shell, _ := json.Marshal(entry)
			Expect(string(operator)).To(ContainSubstring(`"drift"`))
			Expect(string(shell)).NotTo(ContainSubstring(`"drift"`))
		})
	})

	Context("BR-AS45 — read-only, bounded HTTP away from request handlers", func() {
		It("bounds a response that sends headers but never finishes its body", func() {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(`{"id":`))
				w.(http.Flusher).Flush()
				<-r.Context().Done()
			}))
			DeferCleanup(server.Close)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
			defer cancel()
			client := manifesthttp.New()
			DeferCleanup(client.Close)
			_, err := client.Fetch(ctx, server.URL+"/manifest.json")
			Expect(domain.FailedDrift(err).State).To(Equal("not checked"))
			Expect(domain.FailedDrift(err).Cause).To(Equal("timeout"))
		})
		It("lets a read return while the background fetch is hanging", func() {
			started := make(chan struct{})
			checker := start(manifestFetch(func(ctx context.Context, _ string) ([]byte, error) {
				select {
				case <-started:
				default:
					close(started)
				}
				<-ctx.Done()
				return nil, ctx.Err()
			}))
			Eventually(started).Should(BeClosed())
			done := make(chan domain.Drift, 1)
			go func() { done <- checker.Snapshot(entry, domain.SourcePreload) }()
			Eventually(done, 20*time.Millisecond).Should(Receive(HaveField("State", "not checked")))
		})
		It("makes a GET for manifest.json and refuses every redirect", func() {
			var redirected atomic.Int32
			destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirected.Add(1) }))
			DeferCleanup(destination.Close)
			var method, path string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				method, path = r.Method, r.URL.Path
				http.Redirect(w, r, destination.URL, http.StatusFound)
			}))
			DeferCleanup(server.Close)
			_, err := manifesthttp.New().Fetch(context.Background(), server.URL+"/manifest.json")
			Expect(err).To(MatchError(domain.ErrDriftHTTPStatus))
			Expect(method).To(Equal("GET"))
			Expect(path).To(Equal("/manifest.json"))
			Expect(redirected.Load()).To(BeZero())
		})
		DescribeTable("non-200 is never a successful fetch", func(status int) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status); _, _ = w.Write(body) }))
			DeferCleanup(server.Close)
			_, err := manifesthttp.New().Fetch(context.Background(), server.URL+"/manifest.json")
			Expect(domain.FailedDrift(err).State).To(Equal("not checked"))
			Expect(err).To(MatchError(domain.ErrDriftHTTPStatus))
		}, Entry("not found", 404), Entry("unavailable", 503), Entry("no content", 204))
		It("caps the response body", func() {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(strings.Repeat("x", 1024*1024+1))) }))
			DeferCleanup(server.Close)
			_, err := manifesthttp.New().Fetch(context.Background(), server.URL+"/manifest.json")
			Expect(err).To(MatchError(domain.ErrDriftBodyTooLarge))
		})
	})

	Context("BR-AS04 — a refusal states stage and cause, never addresses", func() {
		It("redacts transport and parser errors", func() {
			for _, result := range []domain.Drift{domain.FailedDrift(errors.New("GET http://internal:80?secret: refused")), domain.CompareManifest(entry, []byte(`{"http://internal:80":`))} {
				Expect(result.Stage).To(Equal("manifest-drift"))
				Expect(result.Cause).NotTo(BeEmpty())
				raw, _ := json.Marshal(result)
				Expect(string(raw)).NotTo(ContainSubstring("http"))
			}
		})
	})
})
