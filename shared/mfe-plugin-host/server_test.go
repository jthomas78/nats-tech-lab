package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/jthomas78/nats-tech-lab/shared/mferegistry/announcer"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestPluginHost(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "MFE Plugin Host Suite")
}

var _ = Describe("MFE plugin static host", func() {
	Context("BR-AS61 — the bounded frontend probe", func() {
		It("serves no-store JSON from /healthz with no CORS header", func() {
			host, err := NewStaticHost(GinkgoT().TempDir(), "http://localhost:7110")
			Expect(err).NotTo(HaveOccurred())
			response := httptest.NewRecorder()
			host.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/healthz", nil))

			Expect(response.Code).To(Equal(http.StatusOK))
			Expect(response.Header().Get("Content-Type")).To(MatchRegexp(`^application/json`))
			Expect(response.Header().Get("Cache-Control")).To(Equal("no-store"))
			Expect(response.Header().Get("Access-Control-Allow-Origin")).To(BeEmpty())
			Expect(response.Body.String()).To(MatchJSON(`{"status":"ok"}`))
		})

		It("answers while the asset root is empty", func() {
			host, err := NewStaticHost(GinkgoT().TempDir(), "http://localhost:7110")
			Expect(err).NotTo(HaveOccurred())
			response := httptest.NewRecorder()
			host.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/healthz", nil))
			Expect(response.Code).To(Equal(http.StatusOK))
		})
	})

	Context("decision 3 — nginx's static-serving contract", func() {
		It("returns 404 for a missing asset without an index fallback", func() {
			root := GinkgoT().TempDir()
			Expect(os.WriteFile(filepath.Join(root, "index.html"), []byte("fallback"), 0o600)).To(Succeed())
			host, err := NewStaticHost(root, "http://localhost:7110")
			Expect(err).NotTo(HaveOccurred())
			response := httptest.NewRecorder()
			host.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/missing.js", nil))
			Expect(response.Code).To(Equal(http.StatusNotFound))
			Expect(response.Body.String()).NotTo(ContainSubstring("fallback"))
		})

		It("serves an existing asset with one named CORS origin and Vary", func() {
			root := GinkgoT().TempDir()
			Expect(os.WriteFile(filepath.Join(root, "remoteEntry.js"), []byte("export {}"), 0o600)).To(Succeed())
			host, err := NewStaticHost(root, "http://localhost:7110")
			Expect(err).NotTo(HaveOccurred())
			response := httptest.NewRecorder()
			host.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/remoteEntry.js", nil))
			Expect(response.Code).To(Equal(http.StatusOK))
			Expect(response.Header().Get("Access-Control-Allow-Origin")).To(Equal("http://localhost:7110"))
			Expect(response.Header().Values("Vary")).To(ContainElement("Origin"))
		})

		It("requires a named allowed origin and never emits wildcard CORS", func() {
			_, err := NewStaticHost(GinkgoT().TempDir(), "")
			Expect(err).To(MatchError("ASSET_ALLOWED_ORIGIN is required"))
			_, err = NewStaticHost(GinkgoT().TempDir(), "*")
			Expect(err).To(MatchError("ASSET_ALLOWED_ORIGIN must be one named HTTP(S) origin"))
		})

		It("cannot traverse outside the asset root", func() {
			parent := GinkgoT().TempDir()
			root := filepath.Join(parent, "srv")
			Expect(os.Mkdir(root, 0o700)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(parent, "signing.nk"), []byte("secret"), 0o600)).To(Succeed())
			host, err := NewStaticHost(root, "http://localhost:7110")
			Expect(err).NotTo(HaveOccurred())
			for _, target := range []string{"/../signing.nk", "/%2e%2e/signing.nk"} {
				response := httptest.NewRecorder()
				host.ServeHTTP(response, httptest.NewRequest(http.MethodGet, target, nil))
				Expect(response.Code).To(Equal(http.StatusNotFound))
				Expect(response.Body.String()).NotTo(ContainSubstring("secret"))
			}
		})

		It("registers exactly /healthz and the asset root", func() {
			host, err := NewStaticHost(GinkgoT().TempDir(), "http://localhost:7110")
			Expect(err).NotTo(HaveOccurred())
			Expect(host.RoutePatterns()).To(ConsistOf("/healthz", "/"))
		})
	})

	Context("BR-AS54 — serving failure is not withdrawal", func() {
		It("refuses an unreadable asset root before starting the announcer", func() {
			called := false
			err := runHost(context.Background(), HostConfig{AssetRoot: filepath.Join(GinkgoT().TempDir(), "missing"), AllowedOrigin: "http://localhost:7110"}, announcer.Config{}, func(context.Context, announcer.Config) error {
				called = true
				return nil
			}, nil)
			Expect(err).To(MatchError(ContainSubstring("asset root")))
			Expect(called).To(BeFalse())
		})

		It("cancels the announcer without SIGTERM when the listener cannot bind", func() {
			cancelled := make(chan struct{})
			start := func(ctx context.Context, _ announcer.Config) error {
				<-ctx.Done()
				close(cancelled)
				return nil
			}
			serve := func(context.Context, *StaticHost) error {
				return errors.New("listen tcp :8080: bind: address already in use")
			}
			err := runHost(context.Background(), HostConfig{AssetRoot: GinkgoT().TempDir(), AllowedOrigin: "http://localhost:7110"}, announcer.Config{}, start, serve)
			Expect(err).To(MatchError(ContainSubstring("address already in use")))
			Eventually(cancelled).Should(BeClosed())
		})

		It("turns a handler panic into process failure and cancels the announcer", func() {
			cancelled := make(chan struct{})
			start := func(ctx context.Context, _ announcer.Config) error {
				<-ctx.Done()
				close(cancelled)
				return nil
			}
			serve := func(ctx context.Context, host *StaticHost) error {
				host.assetHandler = http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("boom") })
				host.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/remoteEntry.js", nil))
				<-ctx.Done()
				return nil
			}
			err := runHost(context.Background(), HostConfig{AssetRoot: GinkgoT().TempDir(), AllowedOrigin: "http://localhost:7110"}, announcer.Config{}, start, serve)
			Expect(err).To(MatchError("asset handler panic: boom"))
			Eventually(cancelled).Should(BeClosed())
		})
	})
})

func readBody(response *http.Response) string {
	defer response.Body.Close()
	raw, _ := io.ReadAll(response.Body)
	return string(raw)
}
