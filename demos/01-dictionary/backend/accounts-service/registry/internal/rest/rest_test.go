package rest

import (
	"net/http"
	"net/http/httptest"
	"testing"

	. "github.com/onsi/gomega"
)

// BR-AS21/24: exhaustive even when empty. The former payload tests now live
// in internal/browserrpc/adapter_test.go. The former BR-AS25 mount split is
// re-expressed as TestShellReadIsUngatedAndEverythingElseIsNot in auth.
func TestMountRoutes(t *testing.T) {
	NewWithT(t).Expect(Mount(http.NewServeMux())).To(BeEmpty())
}

func TestRetiredRegistryRoutesAreUnreachable(t *testing.T) {
	g := NewWithT(t)
	mux := http.NewServeMux()
	Mount(mux)
	for _, path := range []string{
		"/api/registry/frontend-plugins", "/api/registry/entries",
		"/api/registry/entries/example-plugin/enabled", "/api/registry/audit",
	} {
		for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodDelete} {
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
			g.Expect(rec.Code).To(Equal(http.StatusNotFound), "%s %s", method, path)
		}
	}
}
