// Package rest is the registry's HTTP adapter.
//
// There is no DELETE route in this package, and its absence is a rule, not
// an omission: `active` has no exit transition, so a curated entry is
// disabled, never torn out from under a shell that already loaded it
// (BR-AS19, BR-AS24).
package rest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/accounts-service/registry/internal/domain"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/accounts-service/registry/internal/postgres"
)

// Service is what the adapter drives — the module's interface, not its parts.
type Service interface {
	Read(ctx context.Context) domain.Document
	Curated(ctx context.Context) (domain.Document, error)
	Apply(ctx context.Context, w domain.Write) (domain.Document, error)
	Allowlist() domain.Allowlist
}

// Auditor is the admin surface's read of the audit trail. Separate from
// Service because the shell never sees it.
type Auditor interface {
	Audit(ctx context.Context, limit int) ([]postgres.AuditPage, error)
}

// Middleware wraps each handler — supplied by the composition root so this
// package does not import the accounts module for its BasicAuth.
type Middleware func(http.HandlerFunc) http.Handler

// Mount wires the routes onto mux and returns the exact "METHOD /pattern"
// list it registered, so a test can assert the surface hasn't grown a write
// transport nobody wrote a rule for.
func Mount(mux *http.ServeMux, svc Service, audit Auditor, mw Middleware) []string {
	var routes []string
	handle := func(pattern string, fn http.HandlerFunc) {
		if mw != nil {
			mux.Handle(pattern, mw(fn))
		} else {
			mux.HandleFunc(pattern, fn)
		}
		routes = append(routes, pattern)
	}

	h := &handlers{svc: svc, audit: audit}
	handle("GET /api/registry/frontend-plugins", h.read)
	handle("GET /api/registry/entries", h.curated)
	handle("POST /api/registry/entries", h.upsert)
	handle("POST /api/registry/entries/{id}/enabled", h.setEnabled)
	handle("GET /api/registry/audit", h.auditTrail)
	return routes
}

type handlers struct {
	svc   Service
	audit Auditor
}

// read is the shell's endpoint. Conditional: the shell re-reads on a slow
// interval and on tab focus, and a 304 makes that cheap enough to be boring
// (BR-AS19).
func (h *handlers) read(w http.ResponseWriter, r *http.Request) {
	doc := h.svc.Read(r.Context())
	etag := revisionETag(doc.Revision)
	if !doc.Degraded && matchesETag(r.Header.Get("If-None-Match"), etag) {
		w.Header().Set("ETag", etag)
		w.WriteHeader(http.StatusNotModified)
		return
	}
	if !doc.Degraded {
		w.Header().Set("ETag", etag)
	}
	writeJSON(w, http.StatusOK, doc)
}

func (h *handlers) curated(w http.ResponseWriter, r *http.Request) {
	doc, err := h.svc.Curated(r.Context())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "the registry could not be read")
		return
	}
	allowed := h.svc.Allowlist()
	type view struct {
		domain.Entry
		// Conforming says whether this entry would be served to a shell
		// today. An entry can stop conforming without being edited, when the
		// configured origins narrow (BR-AS20) — the admin surface has to be
		// able to say so, or the entry just silently stops appearing.
		Conforming bool `json:"conforming"`
	}
	out := struct {
		SchemaVersion int      `json:"schemaVersion"`
		Revision      int64    `json:"revision"`
		Origins       []string `json:"allowedOrigins"`
		Entries       []view   `json:"plugins"`
	}{SchemaVersion: doc.SchemaVersion, Revision: doc.Revision, Origins: allowed.Origins(), Entries: []view{}}
	for _, e := range doc.Entries {
		out.Entries = append(out.Entries, view{Entry: e, Conforming: allowed.Check(e) == nil})
	}
	w.Header().Set("ETag", revisionETag(doc.Revision))
	writeJSON(w, http.StatusOK, out)
}

type upsertRequest struct {
	Entry domain.Entry `json:"entry"`
}

func (h *handlers) upsert(w http.ResponseWriter, r *http.Request) {
	var req upsertRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "the request body is not a registry entry")
		return
	}
	rev, ok := suppliedRevision(w, r)
	if !ok {
		return
	}
	h.apply(w, r, domain.Write{
		Op:         domain.OpUpsert,
		EntryID:    req.Entry.ID,
		Actor:      domain.SharedAdminActor,
		Entry:      &req.Entry,
		IfRevision: rev,
	})
}

func (h *handlers) setEnabled(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "the request body must say enabled true or false")
		return
	}
	rev, ok := suppliedRevision(w, r)
	if !ok {
		return
	}
	h.apply(w, r, domain.Write{
		Op:         domain.OpSetEnabled,
		EntryID:    r.PathValue("id"),
		Actor:      domain.SharedAdminActor,
		Enabled:    req.Enabled,
		IfRevision: rev,
	})
}

func (h *handlers) apply(w http.ResponseWriter, r *http.Request, write domain.Write) {
	doc, err := h.svc.Apply(r.Context(), write)
	switch {
	case err == nil:
		w.Header().Set("ETag", revisionETag(doc.Revision))
		writeJSON(w, http.StatusOK, doc)
	case errors.Is(err, domain.ErrStaleRevision):
		// 409, and the refusal names the revision to reapply on top of.
		// Nothing is merged: two curation decisions are not something a
		// server should guess at.
		var stale domain.StaleRevisionError
		errors.As(err, &stale)
		w.Header().Set("ETag", revisionETag(stale.Current))
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":           "the registry moved on while you were editing",
			"currentRevision": stale.Current,
			"yourRevision":    stale.Supplied,
		})
	case errors.Is(err, domain.ErrOriginNotAllowed):
		// Stage and cause only — never the URL or the configured origins
		// (BR-AS04).
		writeError(w, http.StatusUnprocessableEntity, "the plugin's remote origin is not one this platform is configured to load code from")
	case errors.Is(err, domain.ErrUnknownOp), errors.Is(err, domain.ErrEntryIDMismatch),
		errors.Is(err, domain.ErrNoEntry), errors.Is(err, domain.ErrNoEntryID):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "the write could not be applied")
	}
}

func (h *handlers) auditTrail(w http.ResponseWriter, r *http.Request) {
	if h.audit == nil {
		writeJSON(w, http.StatusOK, []postgres.AuditPage{})
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	entries, err := h.audit.Audit(r.Context(), limit)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "the audit trail could not be read")
		return
	}
	writeJSON(w, http.StatusOK, entries)
}

// suppliedRevision reads the revision the writer is keyed on from If-Match.
// Required: a write with no revision is a write whose author did not read
// the registry, and BR-AS18 has nothing to compare it against.
func suppliedRevision(w http.ResponseWriter, r *http.Request) (int64, bool) {
	raw := strings.Trim(strings.TrimSpace(r.Header.Get("If-Match")), `"`)
	if raw == "" {
		writeError(w, http.StatusPreconditionRequired, "the write must carry the revision it was made against in If-Match")
		return 0, false
	}
	rev, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "If-Match must be a registry revision")
		return 0, false
	}
	return rev, true
}

func revisionETag(rev int64) string { return fmt.Sprintf("%q", strconv.FormatInt(rev, 10)) }

func matchesETag(header, etag string) bool {
	for _, candidate := range strings.Split(header, ",") {
		if strings.TrimSpace(candidate) == etag {
			return true
		}
	}
	return false
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
