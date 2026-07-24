// Package rest exposes the reference-data module over HTTP.
//
// Routes:
//
//	GET    /api/refdata/{context}/types                  list registered dictionary types
//	POST   /api/refdata/admin/types                       register a dictionary type
//	GET    /api/refdata/{context}/{type}                  list items, optionally locale-resolved (?locale=), assignable only; ?all=true includes deprecated (BR-D06)
//	GET    /api/refdata/{context}/{type}/{code}            get one item, any status (BR-D06); ?locale= resolves a label (BR-D03); ?expand= inlines a reference target
//	GET    /api/refdata/{context}/{type}/{code}/localizations  every locale's localization recorded for the item
//	GET    /api/refdata/{context}/{type}/{code}/references     every outbound reference recorded from the item
//	GET    /api/refdata/{context}/{type}/completeness      localization completeness for a locale (?locale=)
//	GET    /api/refdata/{context}/{type}/cache-status      Postgres set version vs KV _meta version (cache status widget)
//	POST   /api/refdata/admin/items                        register an item (BR-D01)
//	POST   /api/refdata/admin/items/{type}/{context}/{code}/deprecate  deprecate an item
//	POST   /api/refdata/admin/items/{type}/{context}/{code}/reactivate reactivate a deprecated item (BR-D12)
//	PATCH  /api/refdata/admin/items/{type}/{context}/{code}/attrs      replace an item's attrs map (BR-D18)
//	DELETE /api/refdata/admin/items/{type}/{context}/{code} delete an unreferenced item (BR-D02)
//	POST   /api/refdata/admin/references                   create a typed reference (BR-D05)
//	GET    /api/refdata/{context}/locales                  locales known to this context
//	POST   /api/refdata/admin/locales                      register a locale for a context
//	POST   /api/refdata/admin/localizations                 set an item's label/description in one locale
//	POST   /api/refdata/admin/{type}/{code}/translate        draft AI-assisted translations, unsaved (BR-D07)
//	GET    /api/refdata-watch/{context}                     SSE stream of refdata-{context} KV cache changes
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
type versionedItemsResponse struct {
	Items []kvcache.VersionedEntry `json:"items"`
}

type typesResponse struct {
	Types []domain.DictionaryType `json:"types"`
}

type itemRequest struct {
	TypeKey string         `json:"typeKey"`
	Code    string         `json:"code"`
	Context string         `json:"context"`
	Attrs   map[string]any `json:"attrs"`
}

type itemResponse struct {
	Item domain.DictionaryItem `json:"item"`
}

type updateAttrsRequest struct {
	Attrs map[string]any `json:"attrs"`
}

type itemsResponse struct {
	Items []domain.DictionaryItem `json:"items"`
}

type localizationsResponse struct {
	Localizations []domain.Localization `json:"localizations"`
}

type referencesResponse struct {
	References []domain.DictionaryReference `json:"references"`
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

type localesResponse struct {
	Locales []string `json:"locales"`
	// DefaultLocale is "" when no locale is marked default for the context.
	DefaultLocale string `json:"defaultLocale"`
}

type resolvedItemResponse struct {
	Item        domain.DictionaryItem `json:"item"`
	Locale      string                `json:"locale,omitempty"`
	Label       string                `json:"label,omitempty"`
	Description string                `json:"description,omitempty"`
	// IsFallback is nil when no ?locale= resolution was attempted at all (the
	// plain item branch below) — a pointer so that case omits it entirely,
	// distinct from the zero-value "false" once resolution was attempted.
	// When resolution was attempted (BR-D03): false means the requested
	// locale (or its bare language) matched exactly; true means either a
	// default-locale substitution or the terminal code-echo.
	IsFallback *bool                  `json:"isFallback,omitempty"`
	Expanded   *domain.DictionaryItem `json:"expanded,omitempty"`
}

type completenessResponse struct {
	TypeKey   string `json:"typeKey"`
	Locale    string `json:"locale"`
	Total     int    `json:"total"`
	Localized int    `json:"localized"`
}

type cacheStatusResponse struct {
	TypeKey         string `json:"typeKey"`
	PostgresVersion int    `json:"postgresVersion"`
	KVVersion       int    `json:"kvVersion"`
	KVItemCount     int    `json:"kvItemCount"`
	InSync          bool   `json:"inSync"`
}

// Deps bundles everything the HTTP layer needs.
type Deps struct {
	Types         *commands.TypeHandler
	Items         *commands.ItemHandler
	References    *commands.ReferenceHandler
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

func (h *Handlers) Mount(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/refdata/admin/contexts", h.registerContext)
	mux.HandleFunc("GET /api/refdata/admin/contexts", h.listContexts)
	// Trailing /detail (not bare .../contexts/{context}) is deliberate: a
	// bare 3-segment admin/contexts/{context} is structurally ambiguous
	// against the pre-existing {context}/{type}/completeness and
	// .../cache-status routes — Go's ServeMux can't tell "admin" apart from
	// a real context key at the same wildcard position, so it panics at
	// startup rather than silently misrouting. A 4th literal segment can
	// never collide with any 3-segment {context}/{type}/action pattern.
	mux.HandleFunc("GET /api/refdata/admin/contexts/{context}/detail", h.getContext)
	mux.HandleFunc("POST /api/refdata/admin/corpus/{context}/draft", h.createDraft)
	mux.HandleFunc("GET /api/refdata/admin/corpus/{context}/draft", h.getDraft)
	mux.HandleFunc("PUT /api/refdata/admin/corpus/{context}/draft/items", h.putDraftItem)
	mux.HandleFunc("PUT /api/refdata/admin/corpus/{context}/draft/localizations", h.putDraftLocalization)
	mux.HandleFunc("POST /api/refdata/admin/corpus/{context}/publish", h.publishCorpus)
	mux.HandleFunc("POST /api/refdata/admin/corpus/{context}/rollback/{version}", h.rollbackCorpus)
	mux.HandleFunc("GET /api/refdata/admin/corpus/{context}/versions", h.listCorpusVersions)
	mux.HandleFunc("GET /api/refdata/admin/corpus/{context}/versions/{version}", h.getCorpusVersion)
	mux.HandleFunc("GET /api/refdata/admin/corpus/{context}/diff/{from}/{to}", h.diffCorpus)
	// "latest" is a valid dynamic value of {version}, not a second route —
	// registering it as its own literal-prefixed pattern (`v/latest/...`)
	// alongside `v/{version}/...` created two DIFFERENT ambiguity panics in a
	// row against Go's net/http ServeMux (each only surfaces once a route
	// with a literal in a position the other treats as wildcard is combined
	// with a third route that has ITS literal at yet another position — see
	// the design doc's "Versioned Read" section for the specifics). One
	// pattern per shape, with the handler branching on the literal string
	// "latest", sidesteps the whole class of conflict.
	//
	// "/items" (not a bare .../v/{version}/{context}/{type}) is deliberate
	// too: a bare 4-segment shape there is structurally ambiguous against
	// the pre-existing {context}/{type}/{code}/localizations and
	// .../references routes for the same reason as the contexts/{context}
	// fix above. The trailing literal is a strict specialization of the
	// get-one route's shape (only one position differs, and only one side
	// is a literal there), which Go's mux does allow.
	mux.HandleFunc("GET /api/refdata/v/{version}/{context}/{type}/{code}", h.getVersionedItem)
	mux.HandleFunc("GET /api/refdata/v/{version}/{context}/{type}/items", h.listVersionedItems)
	mux.HandleFunc("GET /api/refdata/{context}/types", h.listTypes)
	mux.HandleFunc("POST /api/refdata/admin/types", h.registerType)
	mux.HandleFunc("GET /api/refdata/{context}/locales", h.listLocales)
	mux.HandleFunc("POST /api/refdata/admin/locales", h.addLocale)
	mux.HandleFunc("GET /api/refdata/{context}/{type}/completeness", h.completeness)
	mux.HandleFunc("GET /api/refdata/{context}/{type}/cache-status", h.cacheStatus)
	mux.HandleFunc("GET /api/refdata/{context}/{type}", h.listItems)
	mux.HandleFunc("GET /api/refdata/{context}/{type}/{code}", h.getItem)
	mux.HandleFunc("GET /api/refdata/{context}/{type}/{code}/localizations", h.listItemLocalizations)
	mux.HandleFunc("GET /api/refdata/{context}/{type}/{code}/references", h.listItemReferences)
	mux.HandleFunc("POST /api/refdata/admin/items", h.registerItem)
	mux.HandleFunc("POST /api/refdata/admin/items/{type}/{context}/{code}/deprecate", h.deprecateItem)
	mux.HandleFunc("POST /api/refdata/admin/items/{type}/{context}/{code}/reactivate", h.reactivateItem)
	mux.HandleFunc("PATCH /api/refdata/admin/items/{type}/{context}/{code}/attrs", h.updateItemAttrs)
	mux.HandleFunc("DELETE /api/refdata/admin/items/{type}/{context}/{code}", h.deleteItem)
	mux.HandleFunc("POST /api/refdata/admin/references", h.createReference)
	mux.HandleFunc("POST /api/refdata/admin/localizations", h.setLocalization)
	mux.HandleFunc("POST /api/refdata/admin/{type}/{code}/translate", h.draftTranslation)
	mux.HandleFunc("GET /api/refdata-watch/{context}", h.watch)
	mux.Handle("/swagger/", httpSwagger.WrapHandler)
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

// getVersionedItem and listVersionedItems are the versioned-read surface
// (§7 "Versioned Read") — the consumer-facing counterpart to the admin
// corpus endpoints above. Every read is rewrite-on-read
// (kvcache.VersionReader), keeping a pinned old version warm. {version}
// accepts either a number or the literal "latest" — see resolveVersion.
func (h *Handlers) getVersionedItem(w http.ResponseWriter, r *http.Request) {
	if h.deps.VersionReader == nil {
		h.writeJSON(w, http.StatusNotImplemented, errorResponse{Error: "versioned reads are not configured"})
		return
	}
	version, err := h.resolveVersion(r)
	if err != nil {
		h.writeError(w, err)
		return
	}
	entry, err := h.deps.VersionReader.Get(r.Context(), r.PathValue("context"), version, r.PathValue("type"), r.PathValue("code"))
	if err != nil {
		h.writeVersionedError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, entry)
}

func (h *Handlers) listVersionedItems(w http.ResponseWriter, r *http.Request) {
	if h.deps.VersionReader == nil {
		h.writeJSON(w, http.StatusNotImplemented, errorResponse{Error: "versioned reads are not configured"})
		return
	}
	version, err := h.resolveVersion(r)
	if err != nil {
		h.writeError(w, err)
		return
	}
	entries, err := h.deps.VersionReader.List(r.Context(), r.PathValue("context"), version, r.PathValue("type"))
	if err != nil {
		h.writeVersionedError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, versionedItemsResponse{Items: entries})
}

var errInvalidVersion = errors.New("invalid version")

// resolveVersion parses {version}. The literal "latest" resolves to the
// highest-numbered version currently in 'published' status — versions
// coexist indefinitely (version pinning), so this is deliberately not just
// MAX(version): a rolled-back version must not win.
func (h *Handlers) resolveVersion(r *http.Request) (int, error) {
	raw := r.PathValue("version")
	if raw != "latest" {
		var version int
		if _, err := fmt.Sscanf(raw, "%d", &version); err != nil {
			return 0, fmt.Errorf("%w: %q", errInvalidVersion, raw)
		}
		return version, nil
	}
	if h.deps.Corpus == nil {
		return 0, domain.ErrContextNotFound
	}
	versions, err := h.deps.Corpus.Versions(r.Context(), r.PathValue("context"))
	if err != nil {
		return 0, err
	}
	latest := -1
	for _, v := range versions {
		if v.Status == domain.CorpusPublished && v.Version > latest {
			latest = v.Version
		}
	}
	if latest < 0 {
		return 0, domain.ErrContextNotFound
	}
	return latest, nil
}

func (h *Handlers) writeVersionedError(w http.ResponseWriter, err error) {
	if errors.Is(err, kvcache.ErrVersionedKeyNotFound) {
		h.writeJSON(w, http.StatusNotFound, errorResponse{Error: err.Error()})
		return
	}
	h.writeError(w, err)
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
	if err := h.deps.Contexts.Register(r.Context(), domain.Context{Context: req.Context, Parent: req.Parent, Name: req.Name, Description: req.Description}); err != nil {
		h.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusCreated)
}

func (h *Handlers) listContexts(w http.ResponseWriter, r *http.Request) {
	if h.deps.Contexts == nil {
		h.writeJSON(w, http.StatusOK, contextsResponse{Contexts: []domain.Context{}})
		return
	}
	contexts, err := h.deps.Contexts.List(r.Context())
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

// @Summary      List dictionary types
// @Description  Lists every registered dictionary type (currency, country, incoterm, …).
// @Tags         types
// @Produce      json
// @Param        context  path      string  true  "tenant/region context"
// @Success      200      {object}  typesResponse
// @Failure      500      {object}  errorResponse
// @Router       /api/refdata/{context}/types [get]
func (h *Handlers) listTypes(w http.ResponseWriter, r *http.Request) {
	types, err := h.deps.Types.ListTypes(r.Context())
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, typesResponse{Types: types})
}

// @Summary      Register a dictionary type
// @Description  Registers (or updates the name/description/category of) a dictionary type, e.g. "currency". Category (BR-D09) must be one of "standards", "domain-enum", "ui-copy", "config".
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

// @Summary      List items
// @Description  Lists items of a dictionary type in a context, optionally locale-resolved (BR-D03). Excludes deprecated items unless ?all=true is passed (BR-D06).
// @Tags         items
// @Produce      json
// @Param        context  path      string  true   "tenant/region context"
// @Param        type     path      string  true   "dictionary type key"
// @Param        all      query     bool    false  "include deprecated items"
// @Param        locale   query     string  false  "resolve each item's label in this locale"
// @Success      200      {object}  itemsResponse
// @Failure      500      {object}  errorResponse
// @Router       /api/refdata/{context}/{type} [get]
func (h *Handlers) listItems(w http.ResponseWriter, r *http.Request) {
	typeKey := r.PathValue("type")
	itemContext := r.PathValue("context")

	var items []domain.DictionaryItem
	var err error
	if r.URL.Query().Get("all") == "true" {
		items, err = h.deps.Items.ListAll(r.Context(), typeKey, itemContext)
	} else {
		items, err = h.deps.Items.ListAssignable(r.Context(), typeKey, itemContext)
	}
	if err != nil {
		h.writeError(w, err)
		return
	}

	locale := r.URL.Query().Get("locale")
	if locale == "" || h.deps.Localizations == nil {
		h.writeJSON(w, http.StatusOK, itemsResponse{Items: items})
		return
	}

	resolved := make([]resolvedItemResponse, 0, len(items))
	for _, item := range items {
		res, err := h.deps.Localizations.ResolveItem(r.Context(), typeKey, itemContext, item.Code, locale)
		if err != nil {
			h.writeError(w, err)
			return
		}
		isFallback := res.Localization.IsFallback
		resolved = append(resolved, resolvedItemResponse{
			Item: res.Item, Locale: res.Localization.Locale,
			Label: res.Localization.Label, Description: res.Localization.Description,
			IsFallback: &isFallback,
		})
	}
	h.writeJSON(w, http.StatusOK, struct {
		Items []resolvedItemResponse `json:"items"`
	}{Items: resolved})
}

// @Summary      Get an item
// @Description  Resolves a single item regardless of status (BR-D06: deprecated items still resolve). ?locale= resolves a label via BR-D03's fallback chain (response includes "isFallback": true/false — false means the exact requested locale matched, true means a default-locale substitution or code-echo); ?expand= inlines the item a named relation points to.
// @Tags         items
// @Produce      json
// @Param        context  path      string  true   "tenant/region context"
// @Param        type     path      string  true   "dictionary type key"
// @Param        code     path      string  true   "item code"
// @Param        locale   query     string  false  "resolve the label in this locale (BR-D03)"
// @Param        expand   query     string  false  "relation name to expand into the response"
// @Success      200      {object}  resolvedItemResponse
// @Failure      404      {object}  errorResponse
// @Router       /api/refdata/{context}/{type}/{code} [get]
func (h *Handlers) getItem(w http.ResponseWriter, r *http.Request) {
	typeKey, itemContext, code := r.PathValue("type"), r.PathValue("context"), r.PathValue("code")

	resp := resolvedItemResponse{}
	locale := r.URL.Query().Get("locale")
	if locale != "" && h.deps.Localizations != nil {
		resolved, err := h.deps.Localizations.ResolveItem(r.Context(), typeKey, itemContext, code, locale)
		if err != nil {
			h.writeError(w, err)
			return
		}
		resp.Item = resolved.Item
		resp.Locale = resolved.Localization.Locale
		resp.Label = resolved.Localization.Label
		resp.Description = resolved.Localization.Description
		isFallback := resolved.Localization.IsFallback
		resp.IsFallback = &isFallback
	} else {
		item, err := h.deps.Items.Get(r.Context(), typeKey, itemContext, code)
		if err != nil {
			h.writeError(w, err)
			return
		}
		resp.Item = item
	}

	if relation := r.URL.Query().Get("expand"); relation != "" {
		expanded, err := h.deps.References.Expand(r.Context(), itemContext, typeKey, code, relation)
		if err != nil {
			h.writeError(w, err)
			return
		}
		resp.Expanded = &expanded
	}

	// Best-effort cache backfill (Q5's miss path): a consumer calling this
	// REST endpoint after a KV miss should find the cache warm next time.
	// Never fails the response — Postgres already has the authoritative answer.
	if h.deps.Projector != nil {
		if err := h.deps.Projector.Backfill(r.Context(), itemContext, typeKey, code); err != nil && h.deps.Log != nil {
			h.deps.Log.Warn("cache backfill failed", "type", typeKey, "code", code, "err", err)
		}
	}

	h.writeJSON(w, http.StatusOK, resp)
}

// @Summary      List an item's localizations
// @Description  Every locale's localization recorded for one item — the editor's Localizations tab.
// @Tags         localization
// @Produce      json
// @Param        context  path      string  true  "tenant/region context"
// @Param        type     path      string  true  "dictionary type key"
// @Param        code     path      string  true  "item code"
// @Success      200      {object}  localizationsResponse
// @Failure      500      {object}  errorResponse
// @Router       /api/refdata/{context}/{type}/{code}/localizations [get]
func (h *Handlers) listItemLocalizations(w http.ResponseWriter, r *http.Request) {
	locs, err := h.deps.Localizations.ListForItem(r.Context(), r.PathValue("type"), r.PathValue("context"), r.PathValue("code"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, localizationsResponse{Localizations: locs})
}

// @Summary      List an item's outbound references
// @Description  Every typed reference recorded from one item — the editor's References tab.
// @Tags         references
// @Produce      json
// @Param        context  path      string  true  "tenant/region context"
// @Param        type     path      string  true  "dictionary type key"
// @Param        code     path      string  true  "item code"
// @Success      200      {object}  referencesResponse
// @Failure      500      {object}  errorResponse
// @Router       /api/refdata/{context}/{type}/{code}/references [get]
func (h *Handlers) listItemReferences(w http.ResponseWriter, r *http.Request) {
	refs, err := h.deps.References.ListFrom(r.Context(), r.PathValue("context"), r.PathValue("type"), r.PathValue("code"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, referencesResponse{References: refs})
}

// @Summary      Localization completeness
// @Description  Reports how many of a type's items have a localization for the given locale.
// @Tags         localization
// @Produce      json
// @Param        context  path      string  true  "tenant/region context"
// @Param        type     path      string  true  "dictionary type key"
// @Param        locale   query     string  true  "locale to check completeness for"
// @Success      200      {object}  completenessResponse
// @Failure      500      {object}  errorResponse
// @Router       /api/refdata/{context}/{type}/completeness [get]
func (h *Handlers) completeness(w http.ResponseWriter, r *http.Request) {
	typeKey, itemContext := r.PathValue("type"), r.PathValue("context")
	locale := r.URL.Query().Get("locale")
	total, localized, err := h.deps.Localizations.Completeness(r.Context(), typeKey, itemContext, locale)
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, completenessResponse{TypeKey: typeKey, Locale: locale, Total: total, Localized: localized})
}

// @Summary      Cache status
// @Description  Compares Postgres's current set version (BR-D04) against the refdata-{context} KV cache's {type}._meta version — the Q5 versioned-read protocol made visible for a cache status widget.
// @Tags         localization
// @Produce      json
// @Param        context  path      string  true  "tenant/region context"
// @Param        type     path      string  true  "dictionary type key"
// @Success      200      {object}  cacheStatusResponse
// @Failure      500      {object}  errorResponse
// @Router       /api/refdata/{context}/{type}/cache-status [get]
func (h *Handlers) cacheStatus(w http.ResponseWriter, r *http.Request) {
	typeKey, itemContext := r.PathValue("type"), r.PathValue("context")

	if h.deps.Versions == nil {
		h.writeJSON(w, http.StatusOK, cacheStatusResponse{TypeKey: typeKey})
		return
	}
	pgVersion, err := h.deps.Versions.Current(r.Context(), itemContext, typeKey)
	if err != nil {
		h.writeError(w, err)
		return
	}

	resp := cacheStatusResponse{TypeKey: typeKey, PostgresVersion: pgVersion}
	if h.deps.KV != nil {
		if raw, _, err := h.deps.KV.Get(r.Context(), itemContext, typeKey+"._meta"); err == nil {
			var meta kvcache.MetaEntry
			if json.Unmarshal(raw, &meta) == nil {
				resp.KVVersion = meta.Version
				resp.KVItemCount = meta.ItemCount
			}
		}
	}
	resp.InSync = resp.KVVersion == resp.PostgresVersion
	h.writeJSON(w, http.StatusOK, resp)
}

// @Summary      List locales
// @Description  Lists locales registered for a context, including which one is the default.
// @Tags         localization
// @Produce      json
// @Param        context  path      string  true  "tenant/region context"
// @Success      200      {object}  localesResponse
// @Failure      500      {object}  errorResponse
// @Router       /api/refdata/{context}/locales [get]
func (h *Handlers) listLocales(w http.ResponseWriter, r *http.Request) {
	locales, err := h.deps.Localizations.ListLocales(r.Context(), r.PathValue("context"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	defaultLocale, err := h.deps.Localizations.DefaultLocale(r.Context(), r.PathValue("context"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, localesResponse{Locales: locales, DefaultLocale: defaultLocale})
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
	case errors.Is(err, domain.ErrInvalidLocaleFormat), errors.Is(err, domain.ErrInvalidSource):
		status = http.StatusBadRequest
	case errors.Is(err, domain.ErrContextCycle), errors.Is(err, domain.ErrCannotDeleteInheritedItem),
		errors.Is(err, domain.ErrDraftAlreadyExists), errors.Is(err, domain.ErrOnlyDraftCanPublish),
		errors.Is(err, domain.ErrRollbackTargetNotPublic):
		status = http.StatusConflict
	case errors.Is(err, domain.ErrDraftNotFound):
		status = http.StatusNotFound
	case errors.Is(err, errInvalidVersion):
		status = http.StatusBadRequest
	}
	if h.deps.Log != nil && status == http.StatusInternalServerError {
		h.deps.Log.Error("refdata request failed", "err", err)
	}
	h.writeJSON(w, status, errorResponse{Error: err.Error()})
}
