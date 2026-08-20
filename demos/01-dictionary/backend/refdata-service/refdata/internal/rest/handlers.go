// Package rest exposes the reference-data module's admin/operator surface
// over HTTP.
//
// BR-D43 (Phase 33): every business (browser-facing) read this package used
// to serve — item get, type/locales list, completeness, cache-status,
// item localizations/references, versioned reads, and the /api/refdata-watch
// SSE stream — was retired here. Each has full parity on api.* (see
// internal/browserrpc), which every live frontend already used exclusively;
// none of them had any remaining REST caller. What remains below is
// /api/refdata/admin/* — a deliberate, permanent exemption (not un-migrated
// business REST): accounts-service's server-to-server RefdataClient
// (accounts-service/accounts/refdata.go) calls these routes directly and has
// no NATS path of its own for refdata admin writes yet.
//
// Routes:
//
//	POST   /api/refdata/admin/types                       register a dictionary type
//	POST   /api/refdata/admin/items                        register an item (BR-D01)
//	POST   /api/refdata/admin/items/{type}/{context}/{code}/deprecate  deprecate an item
//	POST   /api/refdata/admin/items/{type}/{context}/{code}/reactivate reactivate a deprecated item (BR-D12)
//	PATCH  /api/refdata/admin/items/{type}/{context}/{code}/attrs      replace an item's attrs map (BR-D18)
//	DELETE /api/refdata/admin/items/{type}/{context}/{code} delete an unreferenced item (BR-D02)
//	POST   /api/refdata/admin/references                   create a typed reference (BR-D05)
//	POST   /api/refdata/admin/regions                      register a region + its country relation (BR-D46-BR-D48)
//	POST   /api/refdata/admin/locales                      register a locale for a context
//	POST   /api/refdata/admin/localizations                 set an item's label/description in one locale
//	POST   /api/refdata/admin/{type}/{code}/translate        draft AI-assisted translations, unsaved (BR-D07)
//	POST   /api/refdata/admin/contexts                      register a context
//	GET    /api/refdata/admin/contexts                      list contexts (optional ?tenant=)
//	GET    /api/refdata/admin/contexts/{context}/detail      get one context, with ancestors/descendants
//	PATCH  /api/refdata/admin/contexts/{context}/visible     toggle a context's visibility (BR-D38)
//	POST   /api/refdata/admin/corpus/{context}/draft         open a corpus draft
//	GET    /api/refdata/admin/corpus/{context}/draft         read the open draft's contents
//	PUT    /api/refdata/admin/corpus/{context}/draft/items         upsert one draft item
//	PUT    /api/refdata/admin/corpus/{context}/draft/localizations upsert one draft localization
//	POST   /api/refdata/admin/corpus/{context}/publish       publish the open draft
//	POST   /api/refdata/admin/corpus/{context}/rollback/{version}  roll back to a prior published version
//	GET    /api/refdata/admin/corpus/{context}/versions      list corpus versions
//	GET    /api/refdata/admin/corpus/{context}/versions/{version}  get one corpus version's contents
//	GET    /api/refdata/admin/corpus/{context}/diff/{from}/{to}    diff two corpus versions
package rest

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	httpSwagger "github.com/swaggo/http-swagger"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata/internal/application/commands"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata/internal/domain"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata/internal/kvcache"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata/internal/kvstore"
)

type errorResponse struct {
	Error string `json:"error"`
}

type typeRequest struct {
	TypeKey     string              `json:"typeKey"`
	Name        string              `json:"name"`
	Description string              `json:"description"`
	Category    domain.TypeCategory `json:"category"`
}

type contextRequest struct {
	Context     string `json:"context"`
	Parent      string `json:"parent"`
	Name        string `json:"name"`
	Description string `json:"description"`
	// Tenant is optional governance/ownership metadata (Phase 16d, BR-D34) —
	// not enforced, see domain.Context's doc comment.
	Tenant string `json:"tenant"`
}

type contextsResponse struct {
	Contexts []domain.Context `json:"contexts"`
}

type corpusRequest struct {
	Notes string `json:"notes"`
}
type corpusVersionsResponse struct {
	Versions []domain.CorpusVersion `json:"versions"`
}
type corpusItemRequest struct {
	TypeKey       string            `json:"typeKey"`
	Code          string            `json:"code"`
	Status        domain.ItemStatus `json:"status"`
	Attrs         map[string]any    `json:"attrs"`
	SourceContext string            `json:"sourceContext"`
	IsOverride    bool              `json:"isOverride"`
}
type corpusLocalizationRequest struct {
	TypeKey       string `json:"typeKey"`
	Code          string `json:"code"`
	Locale        string `json:"locale"`
	Label         string `json:"label"`
	Description   string `json:"description"`
	Source        string `json:"source"`
	SourceContext string `json:"sourceContext"`
}
type corpusContentsResponse struct {
	Version       domain.CorpusVersion        `json:"version"`
	Items         []domain.CorpusItem         `json:"items"`
	Localizations []domain.CorpusLocalization `json:"localizations"`
}
type corpusDiffResponse struct {
	Entries []domain.CorpusDiffEntry `json:"entries"`
}
type itemRequest struct {
	TypeKey string         `json:"typeKey"`
	Code    string         `json:"code"`
	Context string         `json:"context"`
	Attrs   map[string]any `json:"attrs"`
}

// regionRequest is the payload for BR-D46's region registration. countryCode
// is required (BR-D47) — a region with no parent country is refused rather
// than stored incomplete, so it is part of the create payload and not a
// follow-up reference call.
type regionRequest struct {
	Context     string `json:"context"`
	Code        string `json:"code"`
	CountryCode string `json:"countryCode"`
	Name        string `json:"name"`
}

type itemResponse struct {
	Item domain.DictionaryItem `json:"item"`
}

type updateAttrsRequest struct {
	Attrs map[string]any `json:"attrs"`
}

type referenceRequest struct {
	Context            string `json:"context"`
	FromTypeKey        string `json:"fromTypeKey"`
	FromCode           string `json:"fromCode"`
	Relation           string `json:"relation"`
	DeclaredTargetType string `json:"declaredTargetType"`
	ToTypeKey          string `json:"toTypeKey"`
	ToCode             string `json:"toCode"`
}

type localizationRequest struct {
	TypeKey     string `json:"typeKey"`
	Code        string `json:"code"`
	Context     string `json:"context"`
	Locale      string `json:"locale"`
	Label       string `json:"label"`
	Description string `json:"description"`
	// Source is "manual" or "ai" (BR-D07); omitted/empty defaults to "manual".
	Source string `json:"source,omitempty"`
}

// translateRequest requests AI-drafted translations for one item across one
// or more target locales (BR-D07). Nothing is persisted by this call.
type translateRequest struct {
	Context       string   `json:"context"`
	TargetLocales []string `json:"targetLocales"`
}

type translateDraftResponse struct {
	Locale      string `json:"locale"`
	Label       string `json:"label,omitempty"`
	Description string `json:"description,omitempty"`
	// Error is set (and Label/Description empty) when drafting failed for
	// this locale specifically — other locales in the same request are
	// unaffected.
	Error string `json:"error,omitempty"`
}

type translateResponse struct {
	Drafts []translateDraftResponse `json:"drafts"`
}

type localeRequest struct {
	Context   string `json:"context"`
	Locale    string `json:"locale"`
	IsDefault bool   `json:"isDefault"`
}

// Deps bundles everything the HTTP layer needs.
type Deps struct {
	Types         *commands.TypeHandler
	Items         *commands.ItemHandler
	References    *commands.ReferenceHandler
	Regions       *commands.RegionHandler
	Localizations *commands.LocalizationHandler
	KV            *kvstore.Store           // nil in tests that don't wire NATS
	Projector     *kvcache.Projector       // nil in tests that don't wire NATS
	Versions      domain.VersionRepository // nil in tests that don't wire NATS
	Contexts      *commands.ContextHandler
	Corpus        *commands.CorpusHandler
	VersionReader *kvcache.VersionReader       // nil in tests that don't wire NATS
	Translations  *commands.TranslationHandler // nil when ANTHROPIC_API_KEY is unset
	Log           *slog.Logger
}

type Handlers struct {
	deps Deps
}

func NewHandlers(deps Deps) *Handlers { return &Handlers{deps: deps} }

// Mount registers every route this service exposes on mux and returns the
// exact list of patterns registered, in registration order — BR-040/BR-D44's
// mux allowlist test (handlers_allowlist_test.go) asserts this list
// ConsistOf a hardcoded allowlist, so a future business route added here
// fails that test rather than only a code review.
func (h *Handlers) Mount(mux *http.ServeMux) []string {
	var routes []string
	handle := func(pattern string, fn http.HandlerFunc) {
		routes = append(routes, pattern)
		mux.HandleFunc(pattern, fn)
	}
	handle("POST /api/refdata/admin/contexts", h.registerContext)
	handle("GET /api/refdata/admin/contexts", h.listContexts)
	// Trailing /detail (not bare .../contexts/{context}) is deliberate: a
	// bare 3-segment admin/contexts/{context} is structurally ambiguous
	// against the pre-existing {context}/{type}/completeness and
	// .../cache-status routes — Go's ServeMux can't tell "admin" apart from
	// a real context key at the same wildcard position, so it panics at
	// startup rather than silently misrouting. A 4th literal segment can
	// never collide with any 3-segment {context}/{type}/action pattern.
	handle("GET /api/refdata/admin/contexts/{context}/detail", h.getContext)
	// Phase 22: visibility toggle called by accounts-service when the operator
	// hides or shows _default_bu via the Admin UI (BR-D38).
	handle("PATCH /api/refdata/admin/contexts/{context}/visible", h.setContextVisible)
	handle("POST /api/refdata/admin/corpus/{context}/draft", h.createDraft)
	handle("GET /api/refdata/admin/corpus/{context}/draft", h.getDraft)
	handle("PUT /api/refdata/admin/corpus/{context}/draft/items", h.putDraftItem)
	handle("PUT /api/refdata/admin/corpus/{context}/draft/localizations", h.putDraftLocalization)
	handle("POST /api/refdata/admin/corpus/{context}/publish", h.publishCorpus)
	handle("POST /api/refdata/admin/corpus/{context}/rollback/{version}", h.rollbackCorpus)
	handle("GET /api/refdata/admin/corpus/{context}/versions", h.listCorpusVersions)
	handle("GET /api/refdata/admin/corpus/{context}/versions/{version}", h.getCorpusVersion)
	handle("GET /api/refdata/admin/corpus/{context}/diff/{from}/{to}", h.diffCorpus)
	handle("POST /api/refdata/admin/types", h.registerType)
	handle("POST /api/refdata/admin/locales", h.addLocale)
	handle("POST /api/refdata/admin/items", h.registerItem)
	handle("POST /api/refdata/admin/regions", h.registerRegion)
	handle("POST /api/refdata/admin/items/{type}/{context}/{code}/deprecate", h.deprecateItem)
	handle("POST /api/refdata/admin/items/{type}/{context}/{code}/reactivate", h.reactivateItem)
	handle("PATCH /api/refdata/admin/items/{type}/{context}/{code}/attrs", h.updateItemAttrs)
	handle("DELETE /api/refdata/admin/items/{type}/{context}/{code}", h.deleteItem)
	handle("POST /api/refdata/admin/references", h.createReference)
	handle("POST /api/refdata/admin/localizations", h.setLocalization)
	handle("POST /api/refdata/admin/{type}/{code}/translate", h.draftTranslation)
	mux.Handle("/swagger/", httpSwagger.WrapHandler)
	routes = append(routes, "/swagger/")
	return routes
}

func (h *Handlers) createDraft(w http.ResponseWriter, r *http.Request) {
	if h.deps.Corpus == nil {
		h.writeJSON(w, http.StatusNotImplemented, errorResponse{Error: "corpus versioning is not configured"})
		return
	}
	var req corpusRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	version, err := h.deps.Corpus.CreateDraft(r.Context(), r.PathValue("context"), req.Notes)
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.writeJSON(w, http.StatusCreated, version)
}
func (h *Handlers) publishCorpus(w http.ResponseWriter, r *http.Request) {
	if h.deps.Corpus == nil {
		h.writeJSON(w, http.StatusNotImplemented, errorResponse{Error: "corpus versioning is not configured"})
		return
	}
	version, err := h.deps.Corpus.Publish(r.Context(), r.PathValue("context"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, version)
}
func (h *Handlers) rollbackCorpus(w http.ResponseWriter, r *http.Request) {
	if h.deps.Corpus == nil {
		h.writeJSON(w, http.StatusNotImplemented, errorResponse{Error: "corpus versioning is not configured"})
		return
	}
	var req corpusRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	var version int
	if _, err := fmt.Sscanf(r.PathValue("version"), "%d", &version); err != nil {
		h.writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid version"})
		return
	}
	result, err := h.deps.Corpus.Rollback(r.Context(), r.PathValue("context"), version, req.Notes)
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, result)
}
func (h *Handlers) listCorpusVersions(w http.ResponseWriter, r *http.Request) {
	if h.deps.Corpus == nil {
		h.writeJSON(w, http.StatusOK, corpusVersionsResponse{Versions: []domain.CorpusVersion{}})
		return
	}
	versions, err := h.deps.Corpus.Versions(r.Context(), r.PathValue("context"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, corpusVersionsResponse{Versions: versions})
}

func (h *Handlers) getDraft(w http.ResponseWriter, r *http.Request) {
	if h.deps.Corpus == nil {
		h.writeJSON(w, http.StatusNotImplemented, errorResponse{Error: "corpus versioning is not configured"})
		return
	}
	contextKey := r.PathValue("context")
	versions, err := h.deps.Corpus.Versions(r.Context(), contextKey)
	if err != nil {
		h.writeError(w, err)
		return
	}
	var draft *domain.CorpusVersion
	for i := range versions {
		if versions[i].Status == domain.CorpusDraft {
			draft = &versions[i]
			break
		}
	}
	if draft == nil {
		h.writeError(w, domain.ErrDraftNotFound)
		return
	}
	h.writeCorpusContents(w, r, contextKey, *draft)
}

func (h *Handlers) getCorpusVersion(w http.ResponseWriter, r *http.Request) {
	if h.deps.Corpus == nil {
		h.writeJSON(w, http.StatusNotImplemented, errorResponse{Error: "corpus versioning is not configured"})
		return
	}
	contextKey := r.PathValue("context")
	var number int
	if _, err := fmt.Sscanf(r.PathValue("version"), "%d", &number); err != nil {
		h.writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid version"})
		return
	}
	version, err := h.deps.Corpus.GetVersion(r.Context(), contextKey, number)
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.writeCorpusContents(w, r, contextKey, version)
}

func (h *Handlers) writeCorpusContents(w http.ResponseWriter, r *http.Request, contextKey string, version domain.CorpusVersion) {
	items, err := h.deps.Corpus.ItemsAtVersion(r.Context(), contextKey, version.Version)
	if err != nil {
		h.writeError(w, err)
		return
	}
	locs, err := h.deps.Corpus.LocalizationsAtVersion(r.Context(), contextKey, version.Version)
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, corpusContentsResponse{Version: version, Items: items, Localizations: locs})
}

func (h *Handlers) putDraftItem(w http.ResponseWriter, r *http.Request) {
	if h.deps.Corpus == nil {
		h.writeJSON(w, http.StatusNotImplemented, errorResponse{Error: "corpus versioning is not configured"})
		return
	}
	var req corpusItemRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid request body"})
		return
	}
	item := domain.CorpusItem{
		DictionaryItem: domain.DictionaryItem{TypeKey: req.TypeKey, Code: req.Code, Status: req.Status, Attrs: req.Attrs},
		SourceContext:  req.SourceContext,
		IsOverride:     req.IsOverride,
	}
	if err := h.deps.Corpus.PutDraftItem(r.Context(), r.PathValue("context"), item); err != nil {
		h.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// putDraftLocalization overrides a single item-locale pair directly on the
// draft — the mechanism for a child overriding one locale of an item it
// inherited without overriding the item itself (resolved Q3).
func (h *Handlers) putDraftLocalization(w http.ResponseWriter, r *http.Request) {
	if h.deps.Corpus == nil {
		h.writeJSON(w, http.StatusNotImplemented, errorResponse{Error: "corpus versioning is not configured"})
		return
	}
	var req corpusLocalizationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid request body"})
		return
	}
	loc := domain.CorpusLocalization{
		Localization: domain.Localization{
			TypeKey: req.TypeKey, Code: req.Code, Locale: req.Locale,
			Label: req.Label, Description: req.Description, Source: req.Source,
		},
		SourceContext: req.SourceContext,
	}
	if err := h.deps.Corpus.PutDraftLocalization(r.Context(), r.PathValue("context"), loc); err != nil {
		h.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) diffCorpus(w http.ResponseWriter, r *http.Request) {
	if h.deps.Corpus == nil {
		h.writeJSON(w, http.StatusNotImplemented, errorResponse{Error: "corpus versioning is not configured"})
		return
	}
	var from, to int
	if _, err := fmt.Sscanf(r.PathValue("from"), "%d", &from); err != nil {
		h.writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid from version"})
		return
	}
	if _, err := fmt.Sscanf(r.PathValue("to"), "%d", &to); err != nil {
		h.writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid to version"})
		return
	}
	entries, err := h.deps.Corpus.Diff(r.Context(), r.PathValue("context"), from, to)
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, corpusDiffResponse{Entries: entries})
}

func (h *Handlers) registerContext(w http.ResponseWriter, r *http.Request) {
	if h.deps.Contexts == nil {
		h.writeJSON(w, http.StatusNotImplemented, errorResponse{Error: "context hierarchy is not configured"})
		return
	}
	var req contextRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid request body"})
		return
	}
	if err := h.deps.Contexts.Register(r.Context(), domain.Context{Context: req.Context, Parent: req.Parent, Name: req.Name, Description: req.Description, Tenant: req.Tenant}); err != nil {
		h.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusCreated)
}

// listContexts returns the whole corpus by default (unfiltered — the
// existing admin-UI behavior). An optional ?tenant= query param (Phase 16f)
// switches to ListByTenant, scoping the result to that tenant's own
// contexts plus the shared "_"-reserved platform roots — see
// domain.ContextRepository.ListByTenant.
func (h *Handlers) listContexts(w http.ResponseWriter, r *http.Request) {
	if h.deps.Contexts == nil {
		h.writeJSON(w, http.StatusOK, contextsResponse{Contexts: []domain.Context{}})
		return
	}
	var contexts []domain.Context
	var err error
	if tenant := r.URL.Query().Get("tenant"); tenant != "" {
		contexts, err = h.deps.Contexts.ListByTenant(r.Context(), tenant)
	} else {
		contexts, err = h.deps.Contexts.List(r.Context())
	}
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, contextsResponse{Contexts: contexts})
}

func (h *Handlers) getContext(w http.ResponseWriter, r *http.Request) {
	if h.deps.Contexts == nil {
		h.writeJSON(w, http.StatusNotImplemented, errorResponse{Error: "context hierarchy is not configured"})
		return
	}
	value, err := h.deps.Contexts.Get(r.Context(), r.PathValue("context"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	ancestors, err := h.deps.Contexts.Ancestors(r.Context(), value.Context)
	if err != nil {
		h.writeError(w, err)
		return
	}
	descendants, err := h.deps.Contexts.Descendants(r.Context(), value.Context)
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, struct {
		Context     domain.Context   `json:"context"`
		Ancestors   []domain.Context `json:"ancestors"`
		Descendants []domain.Context `json:"descendants"`
	}{Context: value, Ancestors: ancestors, Descendants: descendants})
}

// setContextVisible toggles the visible flag on a context (Phase 22, BR-D38).
// Called by accounts-service when the operator hides or shows _default_bu via
// the Admin UI. The body must be {"visible": <bool>}.
func (h *Handlers) setContextVisible(w http.ResponseWriter, r *http.Request) {
	if h.deps.Contexts == nil {
		h.writeJSON(w, http.StatusNotImplemented, errorResponse{Error: "context hierarchy is not configured"})
		return
	}
	var req struct {
		Visible bool `json:"visible"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid request body"})
		return
	}
	if err := h.deps.Contexts.SetVisible(r.Context(), r.PathValue("context"), req.Visible); err != nil {
		h.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// @Summary      Register a dictionary type
// @Description  Registers (or updates the name/description/category of) a dictionary type, e.g. "currency". Category (BR-D09) must be one of "standards", "domain-enum", "domain-string", "config".
// @Tags         types
// @Accept       json
// @Produce      json
// @Param        body  body  typeRequest  true  "typeKey, name, description, category"
// @Success      201   "created"
// @Failure      400   {object}  errorResponse
// @Router       /api/refdata/admin/types [post]
func (h *Handlers) registerType(w http.ResponseWriter, r *http.Request) {
	var req typeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid request body"})
		return
	}
	err := h.deps.Types.RegisterType(r.Context(), domain.DictionaryType{
		TypeKey: req.TypeKey, Name: req.Name, Description: req.Description, Category: req.Category,
	})
	if err != nil {
		h.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusCreated)
}

// @Summary      Register a locale
// @Description  Registers a locale as known for a context; at most one locale per context may be marked default.
// @Tags         localization
// @Accept       json
// @Param        body  body  localeRequest  true  "context, locale, isDefault"
// @Success      201   "created"
// @Failure      400   {object}  errorResponse
// @Router       /api/refdata/admin/locales [post]
func (h *Handlers) addLocale(w http.ResponseWriter, r *http.Request) {
	var req localeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid request body"})
		return
	}
	if err := h.deps.Localizations.AddLocale(r.Context(), req.Context, req.Locale, req.IsDefault); err != nil {
		h.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusCreated)
}

// @Summary      Set an item's localization
// @Description  Upserts an item's label/description for one locale. The item must already exist. source ("manual"|"ai", BR-D07) defaults to "manual" when omitted.
// @Tags         localization
// @Accept       json
// @Param        body  body  localizationRequest  true  "typeKey, code, context, locale, label, description, source"
// @Success      204   "no content"
// @Failure      400   {object}  errorResponse
// @Failure      404   {object}  errorResponse
// @Router       /api/refdata/admin/localizations [post]
func (h *Handlers) setLocalization(w http.ResponseWriter, r *http.Request) {
	var req localizationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid request body"})
		return
	}
	err := h.deps.Localizations.SetLocalization(r.Context(), commands.LocalizationInput{
		TypeKey: req.TypeKey, Code: req.Code, Context: req.Context,
		Locale: req.Locale, Label: req.Label, Description: req.Description,
		Source: req.Source,
	})
	if err != nil {
		h.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// @Summary      Draft AI-assisted translations for an item
// @Description  Returns a candidate label/description per requested target locale (BR-D07). Nothing is persisted — save an accepted draft via POST /api/refdata/admin/localizations with source="ai". Returns 501 if ANTHROPIC_API_KEY is not configured.
// @Tags         localization
// @Accept       json
// @Param        type  path  string  true  "type key"
// @Param        code  path  string  true  "item code"
// @Param        body  body  translateRequest  true  "context, targetLocales"
// @Success      200   {object}  translateResponse
// @Failure      400   {object}  errorResponse
// @Failure      404   {object}  errorResponse
// @Failure      501   {object}  errorResponse
// @Router       /api/refdata/admin/{type}/{code}/translate [post]
func (h *Handlers) draftTranslation(w http.ResponseWriter, r *http.Request) {
	if h.deps.Translations == nil {
		h.writeJSON(w, http.StatusNotImplemented, errorResponse{Error: "AI-assisted translation drafting is not configured (ANTHROPIC_API_KEY unset)"})
		return
	}
	var req translateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid request body"})
		return
	}
	if len(req.TargetLocales) == 0 {
		h.writeJSON(w, http.StatusBadRequest, errorResponse{Error: "targetLocales is required"})
		return
	}
	drafts, err := h.deps.Translations.DraftTranslations(r.Context(), commands.DraftInput{
		TypeKey: r.PathValue("type"), Code: r.PathValue("code"), Context: req.Context,
		TargetLocales: req.TargetLocales,
	})
	if err != nil {
		h.writeError(w, err)
		return
	}
	out := make([]translateDraftResponse, len(drafts))
	for i, d := range drafts {
		out[i] = translateDraftResponse{Locale: d.Locale, Label: d.Label, Description: d.Description, Error: d.Error}
	}
	h.writeJSON(w, http.StatusOK, translateResponse{Drafts: out})
}

// @Summary      Register an item
// @Description  Registers a new dictionary item (BR-D01: the code must be free within its type+context).
// @Tags         items
// @Accept       json
// @Produce      json
// @Param        body  body      itemRequest  true  "typeKey, code, context, attrs"
// @Success      201   {object}  itemResponse
// @Failure      400   {object}  errorResponse
// @Failure      409   {object}  errorResponse  "BR-D01: duplicate code"
// @Router       /api/refdata/admin/items [post]
func (h *Handlers) registerItem(w http.ResponseWriter, r *http.Request) {
	var req itemRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid request body"})
		return
	}
	item, err := h.deps.Items.RegisterItem(r.Context(), commands.ItemInput{
		TypeKey: req.TypeKey, Code: req.Code, Context: req.Context, Attrs: req.Attrs,
	})
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.writeJSON(w, http.StatusCreated, itemResponse{Item: item})
}

// @Summary      Register a region
// @Description  Registers a region item and its country relation together (BR-D46-BR-D48). The country must exist and be active (BR-D05).
// @Tags         items
// @Param        request  body      regionRequest  true  "region"
// @Success      201      {object}  itemResponse
// @Failure      409      {object}  errorResponse
// @Failure      422      {object}  errorResponse
// @Router       /api/refdata/admin/regions [post]
func (h *Handlers) registerRegion(w http.ResponseWriter, r *http.Request) {
	var req regionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid request body"})
		return
	}
	item, err := h.deps.Regions.RegisterRegion(r.Context(), commands.RegionInput{
		Context: req.Context, Code: req.Code, CountryCode: req.CountryCode, Name: req.Name,
	})
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.writeJSON(w, http.StatusCreated, itemResponse{Item: item})
}

// @Summary      Deprecate an item
// @Description  Marks an item deprecated. Used for BR-D02's referenced-item path instead of delete.
// @Tags         items
// @Param        type     path  string  true  "dictionary type key"
// @Param        context  path  string  true  "tenant/region context"
// @Param        code     path  string  true  "item code"
// @Success      204      "no content"
// @Failure      404      {object}  errorResponse
// @Router       /api/refdata/admin/items/{type}/{context}/{code}/deprecate [post]
func (h *Handlers) deprecateItem(w http.ResponseWriter, r *http.Request) {
	err := h.deps.Items.DeprecateItem(r.Context(), r.PathValue("type"), r.PathValue("context"), r.PathValue("code"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// @Summary      Reactivate an item
// @Description  Flips a deprecated item back to active. BR-D12: a plain status reversal, symmetric with deprecate.
// @Tags         items
// @Param        type     path  string  true  "dictionary type key"
// @Param        context  path  string  true  "tenant/region context"
// @Param        code     path  string  true  "item code"
// @Success      204      "no content"
// @Failure      404      {object}  errorResponse
// @Router       /api/refdata/admin/items/{type}/{context}/{code}/reactivate [post]
func (h *Handlers) reactivateItem(w http.ResponseWriter, r *http.Request) {
	err := h.deps.Items.ReactivateItem(r.Context(), r.PathValue("type"), r.PathValue("context"), r.PathValue("code"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// @Summary      Replace an item's attrs
// @Description  Replaces an item's entire attrs map (BR-D18) — a full replace, not a per-key merge. Works regardless of item status.
// @Tags         items
// @Accept       json
// @Param        type     path  string              true  "dictionary type key"
// @Param        context  path  string              true  "tenant/region context"
// @Param        code     path  string              true  "item code"
// @Param        body     body  updateAttrsRequest  true  "attrs"
// @Success      204      "no content"
// @Failure      400      {object}  errorResponse
// @Failure      404      {object}  errorResponse
// @Router       /api/refdata/admin/items/{type}/{context}/{code}/attrs [patch]
func (h *Handlers) updateItemAttrs(w http.ResponseWriter, r *http.Request) {
	var req updateAttrsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid request body"})
		return
	}
	err := h.deps.Items.UpdateItemAttrs(r.Context(), r.PathValue("type"), r.PathValue("context"), r.PathValue("code"), req.Attrs)
	if err != nil {
		h.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// @Summary      Delete an item
// @Description  Hard-deletes an unreferenced item. BR-D02: a referenced item cannot be deleted — deprecate it instead.
// @Tags         items
// @Param        type     path  string  true  "dictionary type key"
// @Param        context  path  string  true  "tenant/region context"
// @Param        code     path  string  true  "item code"
// @Success      204      "no content"
// @Failure      404      {object}  errorResponse
// @Failure      409      {object}  errorResponse  "BR-D02: item is referenced"
// @Router       /api/refdata/admin/items/{type}/{context}/{code} [delete]
func (h *Handlers) deleteItem(w http.ResponseWriter, r *http.Request) {
	err := h.deps.Items.DeleteItem(r.Context(), r.PathValue("type"), r.PathValue("context"), r.PathValue("code"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// @Summary      Create a typed reference
// @Description  Links two items via a named relation. BR-D05: the target must exist, be active, and match the relation's declared type.
// @Tags         references
// @Accept       json
// @Produce      json
// @Param        body  body  referenceRequest  true  "context, fromTypeKey, fromCode, relation, declaredTargetType, toTypeKey, toCode"
// @Success      201   "created"
// @Failure      400   {object}  errorResponse
// @Failure      422   {object}  errorResponse  "BR-D05: invalid reference target"
// @Router       /api/refdata/admin/references [post]
func (h *Handlers) createReference(w http.ResponseWriter, r *http.Request) {
	var req referenceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid request body"})
		return
	}
	err := h.deps.References.CreateReference(r.Context(), commands.ReferenceInput{
		Context: req.Context, FromTypeKey: req.FromTypeKey, FromCode: req.FromCode,
		Relation: req.Relation, DeclaredTargetType: req.DeclaredTargetType,
		ToTypeKey: req.ToTypeKey, ToCode: req.ToCode,
	})
	if err != nil {
		h.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusCreated)
}

func (h *Handlers) writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func (h *Handlers) writeError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, domain.ErrItemNotFound), errors.Is(err, domain.ErrTypeNotFound),
		errors.Is(err, domain.ErrReferenceNotFound), errors.Is(err, domain.ErrContextNotFound):
		status = http.StatusNotFound
	case errors.Is(err, domain.ErrDuplicateItemCode), errors.Is(err, domain.ErrItemReferenced):
		status = http.StatusConflict
	case errors.Is(err, domain.ErrReferenceTargetWrongType),
		errors.Is(err, domain.ErrReferenceTargetNotFound),
		errors.Is(err, domain.ErrReferenceTargetNotActive):
		status = http.StatusUnprocessableEntity
	case errors.Is(err, domain.ErrRegionCountryRequired):
		// BR-D47: a well-formed request whose content is contradictory —
		// same 422 class as BR-D05's target failures above.
		status = http.StatusUnprocessableEntity
	case errors.Is(err, domain.ErrInvalidLocaleFormat), errors.Is(err, domain.ErrInvalidSource):
		status = http.StatusBadRequest
	case errors.Is(err, domain.ErrContextCycle), errors.Is(err, domain.ErrCannotDeleteInheritedItem),
		errors.Is(err, domain.ErrDraftAlreadyExists), errors.Is(err, domain.ErrOnlyDraftCanPublish),
		errors.Is(err, domain.ErrRollbackTargetNotPublic):
		status = http.StatusConflict
	case errors.Is(err, domain.ErrDraftNotFound):
		status = http.StatusNotFound
	}
	if h.deps.Log != nil && status == http.StatusInternalServerError {
		h.deps.Log.Error("refdata request failed", "err", err)
	}
	h.writeJSON(w, status, errorResponse{Error: err.Error()})
}
