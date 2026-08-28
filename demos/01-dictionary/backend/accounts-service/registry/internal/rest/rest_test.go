package rest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/accounts-service/registry/internal/domain"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/accounts-service/registry/internal/postgres"
)

type stubService struct {
	doc     domain.Document
	applied domain.Write
	err     error
}

func (s *stubService) Read(context.Context) domain.Document             { return s.doc }
func (s *stubService) Curated(context.Context) (domain.Document, error) { return s.doc, nil }
func (s *stubService) Allowlist() domain.Allowlist                      { return domain.NewAllowlist(nil) }
func (s *stubService) Apply(_ context.Context, w domain.Write) (domain.Document, error) {
	s.applied = w
	if s.err != nil {
		return domain.Document{}, s.err
	}
	return s.doc, nil
}

type stubAudit struct{}

func (stubAudit) Audit(context.Context, int) ([]postgres.AuditPage, error) {
	return []postgres.AuditPage{}, nil
}

func mount(svc Service) *http.ServeMux {
	mux := http.NewServeMux()
	Mount(mux, svc, stubAudit{}, nil)
	return mux
}

// TestMountRoutes pins the transport surface exactly. BR-AS21 and BR-AS24
// are claims about what a caller *cannot* reach, so they are only checkable
// against an exhaustive list: a route added here without a rule fails this
// test rather than review.
func TestMountRoutes(t *testing.T) {
	g := NewWithT(t)

	routes := Mount(http.NewServeMux(), &stubService{}, stubAudit{}, nil)

	g.Expect(routes).To(ConsistOf(
		"GET /api/registry/frontend-plugins",
		"GET /api/registry/entries",
		"POST /api/registry/entries",
		"POST /api/registry/entries/{id}/enabled",
		"GET /api/registry/audit",
	))
	for _, r := range routes {
		g.Expect(r).NotTo(HavePrefix("DELETE "), "BR-AS24: a curated entry is disabled, never removed")
	}
}

// BR-AS24 again, this time against a live mux: DELETE is not merely absent
// from the list, it is unreachable.
func TestDeleteIsNotServed(t *testing.T) {
	g := NewWithT(t)
	mux := mount(&stubService{})

	for _, target := range []string{"/api/registry/entries", "/api/registry/entries/example-plugin"} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, target, nil))
		g.Expect(rec.Code).To(BeNumerically(">=", 400), target)
		g.Expect(rec.Code).NotTo(Equal(http.StatusOK), target)
	}
}

// BR-AS18: a write that names no revision has nothing to be checked against,
// so it is refused before it reaches the store.
func TestWriteWithoutIfMatchIsRefused(t *testing.T) {
	g := NewWithT(t)
	svc := &stubService{}
	mux := mount(svc)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/registry/entries", strings.NewReader(`{"entry":{"id":"x"}}`))
	mux.ServeHTTP(rec, req)

	g.Expect(rec.Code).To(Equal(http.StatusPreconditionRequired))
	g.Expect(svc.applied.Op).To(BeEmpty(), "the store must not be reached")
}

// BR-AS18: a stale write is refused with the revision to reapply on top of,
// and nothing is merged.
func TestStaleWriteIsRefusedWithTheCurrentRevision(t *testing.T) {
	g := NewWithT(t)
	svc := &stubService{err: domain.StaleRevisionError{Current: 50, Supplied: 47}}
	mux := mount(svc)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/registry/entries/example-plugin/enabled", strings.NewReader(`{"enabled":false}`))
	req.Header.Set("If-Match", `"47"`)
	mux.ServeHTTP(rec, req)

	g.Expect(rec.Code).To(Equal(http.StatusConflict))
	g.Expect(rec.Body.String()).To(ContainSubstring(`"currentRevision":50`))
	g.Expect(rec.Header().Get("ETag")).To(Equal(`"50"`))
}

// BR-AS04: a refusal carries stage and cause only — never the URL it
// refused, and never the configured origins.
func TestOriginRefusalNamesNoURL(t *testing.T) {
	g := NewWithT(t)
	svc := &stubService{err: domain.ErrOriginNotAllowed}
	mux := mount(svc)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/registry/entries",
		strings.NewReader(`{"entry":{"id":"x","remote":{"kind":"federated","url":"https://evil.example.com/remoteEntry.js"}}}`))
	req.Header.Set("If-Match", `"3"`)
	mux.ServeHTTP(rec, req)

	g.Expect(rec.Code).To(Equal(http.StatusUnprocessableEntity))
	g.Expect(rec.Body.String()).NotTo(ContainSubstring("evil.example.com"))
}

// BR-AS19: the shell's read is conditional, so a re-read that finds nothing
// changed costs a 304.
func TestConditionalReadReportsNotModified(t *testing.T) {
	g := NewWithT(t)
	svc := &stubService{doc: domain.Document{SchemaVersion: domain.SchemaVersion, Revision: 12}}
	mux := mount(svc)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/registry/frontend-plugins", nil))
	g.Expect(rec.Code).To(Equal(http.StatusOK))
	g.Expect(rec.Header().Get("ETag")).To(Equal(`"12"`))

	again := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/registry/frontend-plugins", nil)
	req.Header.Set("If-None-Match", `"12"`)
	mux.ServeHTTP(again, req)
	g.Expect(again.Code).To(Equal(http.StatusNotModified))
}

// BR-AS22: a degraded document is served whole, never as a 304 — a shell
// holding revision 12 must not be told "unchanged" by an outage whose
// reserved revision is 0.
func TestDegradedReadIsNeverAnswered304(t *testing.T) {
	g := NewWithT(t)
	svc := &stubService{doc: domain.Degraded()}
	mux := mount(svc)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/registry/frontend-plugins", nil)
	req.Header.Set("If-None-Match", `"0"`)
	mux.ServeHTTP(rec, req)

	g.Expect(rec.Code).To(Equal(http.StatusOK))
	g.Expect(rec.Body.String()).To(ContainSubstring(`"degraded":true`))
	g.Expect(rec.Header().Get("ETag")).To(BeEmpty())
}
