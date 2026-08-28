package accounts_test

// BR-AS03: curation is an operator decision, and the file that carries it is
// what lets that decision be made without rebuilding this service — the same
// argument the registry itself makes for the shell.
//
// DB-free, like frontendplugins_test.go, for the same reason: these specs
// cover the path that decides which remotes a browser may fetch, so a silent
// Skip here would be the worst kind of green run.

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/accounts-service/accounts"
)

var _ = Describe("the curated frontend plugin registry file", func() {
	const authSecret = "test-secret"

	write := func(body string) string {
		GinkgoHelper()
		path := filepath.Join(GinkgoT().TempDir(), "registry.json")
		Expect(os.WriteFile(path, []byte(body), 0o600)).To(Succeed())
		return path
	}

	serve := func() frontendPluginRegistryJSON {
		GinkgoHelper()
		mux := http.NewServeMux()
		(&accounts.Handlers{}).Mount(mux, authSecret)
		req := httptest.NewRequest(http.MethodGet, "/api/accounts/frontend-plugins", nil)
		req.SetBasicAuth(accounts.BasicAuthUser, authSecret)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		Expect(rec.Code).To(Equal(http.StatusOK))
		return decodeRegistry(rec)
	}

	// The curated set is package state, so every spec that installs one puts
	// the compiled-in (empty) set back.
	BeforeEach(func() { DeferCleanup(func() { accounts.SetCuratedFrontendPlugins(nil) }) })

	Context("a well-formed document", func() {
		const doc = `{
		  "schemaVersion": 1,
		  "plugins": [
		    {
		      "id": "example-plugin",
		      "name": "Example Plugin",
		      "schemaVersion": 1,
		      "shellApiVersion": 1,
		      "routePrefix": "example",
		      "remote": {
		        "kind": "federated",
		        "url": "http://localhost:7110/remoteEntry.js",
		        "name": "example_plugin",
		        "module": "plugin"
		      },
		      "contributions": [
		        { "kind": "route", "id": "overview", "path": "/example", "title": "Example", "component": "overview" },
		        { "kind": "shell-control", "id": "toggle", "region": "shell/topbar-controls/v1",
		          "component": "topbar-control", "routes": ["/example"] }
		      ]
		    }
		  ]
		}`

		It("is served by the endpoint the shell reads", func() {
			plugins, err := accounts.LoadCuratedFrontendPlugins(write(doc))
			Expect(err).NotTo(HaveOccurred())
			accounts.SetCuratedFrontendPlugins(plugins)

			served := serve()
			Expect(served.Plugins).To(HaveLen(1))
			Expect(served.Plugins[0].ID).To(Equal("example-plugin"))
			Expect(served.Plugins[0].Remote.URL).To(Equal("http://localhost:7110/remoteEntry.js"))
		})

		It("treats an entry that omits `enabled` as enabled", func() {
			// The flag is a pointer for exactly this: a plain bool would turn
			// every entry that omits it into a disabled one, and the shell
			// would show an operator a registry that curates nothing.
			plugins, err := accounts.LoadCuratedFrontendPlugins(write(doc))
			Expect(err).NotTo(HaveOccurred())
			accounts.SetCuratedFrontendPlugins(plugins)

			Expect(serve().Plugins[0].Enabled).NotTo(BeNil())
			Expect(*serve().Plugins[0].Enabled).To(BeTrue())
		})

		It("keeps an explicit `enabled: false`", func() {
			plugins, err := accounts.LoadCuratedFrontendPlugins(write(`{
			  "schemaVersion": 1,
			  "plugins": [{ "id": "p", "name": "P", "schemaVersion": 1, "shellApiVersion": 1,
			    "enabled": false,
			    "remote": { "kind": "federated", "url": "http://localhost:7110/remoteEntry.js", "module": "plugin" },
			    "contributions": [] }]
			}`))
			Expect(err).NotTo(HaveOccurred())
			accounts.SetCuratedFrontendPlugins(plugins)

			Expect(*serve().Plugins[0].Enabled).To(BeFalse())
		})
	})

	Context("a document this service will not serve", func() {
		It("refuses a schemaVersion it does not know rather than guessing at the fields", func() {
			_, err := accounts.LoadCuratedFrontendPlugins(write(`{"schemaVersion": 99, "plugins": []}`))

			Expect(err).To(MatchError(ContainSubstring("schemaVersion 99")))
		})

		It("refuses a malformed document", func() {
			_, err := accounts.LoadCuratedFrontendPlugins(write(`{ not json`))

			Expect(err).To(MatchError(ContainSubstring("parse frontend plugin registry")))
		})

		It("refuses a file that is not there", func() {
			_, err := accounts.LoadCuratedFrontendPlugins(filepath.Join(GinkgoT().TempDir(), "absent.json"))

			Expect(err).To(MatchError(ContainSubstring("read frontend plugin registry")))
		})

		It("leaves the previously curated set serving, so a bad file degrades nothing", func() {
			// main.go logs and carries on rather than failing startup; this is
			// the half of that contract that lives in this package.
			plugins, err := accounts.LoadCuratedFrontendPlugins(write(`{
			  "schemaVersion": 1,
			  "plugins": [{ "id": "kept", "name": "Kept", "schemaVersion": 1, "shellApiVersion": 1,
			    "remote": { "kind": "federated", "url": "http://localhost:7110/remoteEntry.js", "module": "plugin" },
			    "contributions": [] }]
			}`))
			Expect(err).NotTo(HaveOccurred())
			accounts.SetCuratedFrontendPlugins(plugins)

			_, err = accounts.LoadCuratedFrontendPlugins(write(`{"schemaVersion": 99, "plugins": []}`))
			Expect(err).To(HaveOccurred())

			Expect(serve().Plugins).To(HaveLen(1))
			Expect(serve().Plugins[0].ID).To(Equal("kept"))
		})
	})

	Context("the registry that ships for local review", func() {
		It("is a document this service accepts", func() {
			// The five curated entries behind task 1b-4's failure switches.
			// Four of them are deliberately broken *plugins*; none of them is
			// a broken registry document, and that distinction is the one the
			// shell's own states depend on.
			path, err := filepath.Abs(
				"../../../../../lab-shell/plugins/example-plugin/registry.dev.json")
			Expect(err).NotTo(HaveOccurred())

			plugins, err := accounts.LoadCuratedFrontendPlugins(path)
			Expect(err).NotTo(HaveOccurred())
			Expect(plugins).To(HaveLen(5))
		})
	})
})
