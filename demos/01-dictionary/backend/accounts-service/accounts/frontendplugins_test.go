package accounts_test

// BR-AS01: the application shell discovers its plugins from this endpoint and
// loads code only from the remotes it lists.
//
// Deliberately DB-free — the curated registry is Go data, so these specs need
// no Postgres fixture and therefore never Skip. That matters more than usual
// here: a silently skipped spec on the endpoint that decides which code the
// browser is allowed to fetch would be the worst place in this service to
// have a green run prove nothing.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/accounts-service/accounts"
)

type frontendPluginRegistryJSON struct {
	SchemaVersion int `json:"schemaVersion"`
	Plugins       []struct {
		ID              string `json:"id"`
		Name            string `json:"name"`
		SchemaVersion   int    `json:"schemaVersion"`
		ShellAPIVersion int    `json:"shellApiVersion"`
		Enabled         bool   `json:"enabled"`
		Remote          struct {
			Kind   string `json:"kind"`
			URL    string `json:"url"`
			Module string `json:"module"`
		} `json:"remote"`
		Contributions []struct {
			Kind string `json:"kind"`
			ID   string `json:"id"`
		} `json:"contributions"`
	} `json:"plugins"`
}

var _ = Describe("GET /api/accounts/frontend-plugins", func() {
	const authSecret = "test-secret"

	var mux *http.ServeMux

	BeforeEach(func() {
		mux = http.NewServeMux()
		(&accounts.Handlers{}).Mount(mux, authSecret)
	})

	get := func(user, pass string) *httptest.ResponseRecorder {
		GinkgoHelper()
		req := httptest.NewRequest(http.MethodGet, "/api/accounts/frontend-plugins", nil)
		if user != "" {
			req.SetBasicAuth(user, pass)
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	decode := func(rec *httptest.ResponseRecorder) frontendPluginRegistryJSON {
		GinkgoHelper()
		var doc frontendPluginRegistryJSON
		Expect(json.Unmarshal(rec.Body.Bytes(), &doc)).To(Succeed())
		return doc
	}

	Context("the document the shell validates before it renders", func() {
		It("declares the registry schema version the shell checks first", func() {
			rec := get(accounts.BasicAuthUser, authSecret)

			Expect(rec.Code).To(Equal(http.StatusOK))
			Expect(decode(rec).SchemaVersion).To(Equal(accounts.RegistrySchemaVersion))
		})

		It("returns an empty plugins array, not null, when nothing is curated", func() {
			// The shell distinguishes "curates nothing" from "unreadable"
			// (BR-AS04); `null` would land in the second bucket and the
			// Plugins screen would report a registry fault that is not one.
			Expect(get(accounts.BasicAuthUser, authSecret).Body.String()).To(ContainSubstring(`"plugins":[]`))
		})

		It("serves JSON", func() {
			Expect(get(accounts.BasicAuthUser, authSecret).Header().Get("Content-Type")).To(Equal("application/json"))
		})
	})

	Context("every curated plugin is loadable by the shell", func() {
		It("declares both contract versions and a remote for each entry", func() {
			// Empty today; this asserts the invariant rather than the count,
			// so the first federated plugin added to the curated list is
			// checked the moment it lands rather than at the next review.
			for _, plugin := range decode(get(accounts.BasicAuthUser, authSecret)).Plugins {
				Expect(plugin.ID).NotTo(BeEmpty())
				Expect(plugin.Name).NotTo(BeEmpty())
				Expect(plugin.SchemaVersion).To(Equal(accounts.RegistrySchemaVersion))
				Expect(plugin.ShellAPIVersion).To(Equal(accounts.ShellAPIVersion))
				Expect(plugin.Remote.Kind).To(BeElementOf("federated", "builtin"))
				Expect(plugin.Remote.Module).NotTo(BeEmpty())
				if plugin.Remote.Kind == "federated" {
					Expect(plugin.Remote.URL).NotTo(BeEmpty())
				}
				Expect(plugin.Contributions).NotTo(BeEmpty())
			}
		})
	})

	Context("the registry is not anonymously readable", func() {
		It("refuses a request with no credentials", func() {
			// It names every remote the shell will fetch. That inventory is
			// reconnaissance, and it is gated like the rest of /api/accounts
			// rather than being the one route that is not.
			Expect(get("", "").Code).To(Equal(http.StatusUnauthorized))
		})

		It("refuses a wrong secret", func() {
			Expect(get(accounts.BasicAuthUser, "wrong").Code).To(Equal(http.StatusUnauthorized))
		})
	})

	Context("the endpoint is read-only", func() {
		It("is not mounted for writes", func() {
			req := httptest.NewRequest(http.MethodPost, "/api/accounts/frontend-plugins", nil)
			req.SetBasicAuth(accounts.BasicAuthUser, authSecret)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			Expect(rec.Code).To(Equal(http.StatusMethodNotAllowed))
		})
	})

	Context("the registry carries no {context} token", func() {
		It("is reachable without one", func() {
			// The curated set is platform-wide. Per-user visibility is decided
			// in the shell from the caller's own claims — a context in this
			// path would imply the plugin *inventory* is per-business-unit,
			// which it is not.
			Expect(get(accounts.BasicAuthUser, authSecret).Code).To(Equal(http.StatusOK))
		})
	})
})
