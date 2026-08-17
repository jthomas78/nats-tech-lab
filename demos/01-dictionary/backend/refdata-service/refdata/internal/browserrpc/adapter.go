// Package browserrpc is refdata-service's api.* frontend-to-service adapter
// (Phase 32) — a second transport onto the same application-layer methods
// internal/rest and internal/natsrpc already call, mirroring
// pricing-service's and trading-partner-service's own browserrpc packages
// (see ARCHITECTURE-COMMUNICATIONS.md §4 for what nats.go/micro provides).
//
// Unlike internal/natsrpc, which always runs on refdata-service's single
// permanent PLATFORM connection, an Adapter here is registered once per
// TENANT connection (see internal/tenants, BR-D40) — a browser authenticated
// into one tenant's account must reach refdata-service's handlers on that
// same account. Every tenant connection's Adapter shares the exact same
// command handlers built once at Startup; only the NATS connection itself is
// per-tenant.
//
// Subjects split into two namespaces, not just two conventionally-named
// handler groups (BR-D41): api.*.refdata.{item,type,locales,completeness,
// cache-status,context}.* are reads, mirroring the same query methods
// internal/natsrpc's rpc.* handlers already call — including the KV-first
// item/type resolution (BR-D08) natsrpc established. api.*.refdata.admin.*
// covers every corpus/context-registration/type/locale/item/reference/
// localization mutation internal/rest's /api/refdata/admin/* routes expose.
// The split exists so accounts-service's MintBrowserToken can deny the
// admin prefix by subject pattern alone (BR-D41) — a server-enforced
// boundary, not a handler-level check.
package browserrpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/micro"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata/internal/application/commands"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata/internal/domain"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata/internal/kvcache"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata/internal/natstrace"
)

// Business subjects (BR-D41) — {context} is a real subject token, read off
// the concrete subject a request arrived on via contextFromSubject, exactly
// like pricing-service's and trading-partner-service's browserrpc adapters
// (NOT a body field — internal/natsrpc's rpc.* adapter carries context in
// the body instead, for its own Phase 21 reasons specific to rpc.*; this
// package follows the api.* convention, not natsrpc's). ContextGet is a read
// (browsing one context in the hierarchy) — grouped here rather than under
// the admin namespace below, unlike REST's /api/refdata/admin/contexts
// routing, which nests it there only to avoid a Go ServeMux path-ambiguity,
// not because reading a context is a privileged operation.
//
// ContextListSubject is the one exception to the wildcard convention: "list
// every context I can see" has no single context to route by, so — exactly
// like internal/natsrpc's own ContextListSubject — it uses the fixed literal
// "_platform" token rather than a wildcard.
const (
	ItemGetSubject          = "api.*.refdata.item.get.v1"
	ItemGetVersionedSubject = "api.*.refdata.item.get-versioned.v1"
	TypeListSubject         = "api.*.refdata.type.list.v1"
	LocalesListSubject      = "api.*.refdata.locales.list.v1"
	CompletenessSubject     = "api.*.refdata.completeness.v1"
	CacheStatusSubject      = "api.*.refdata.cache-status.v1"
	ContextListSubject      = "api._platform.refdata.context.list.v1"
	ContextGetSubject       = "api.*.refdata.context.get.v1"
	// ItemLocalizationsListSubject/ItemReferencesListSubject are the api.*
	// counterparts of REST's listItemLocalizations/listItemReferences (the
	// item detail panel's Localizations/References tabs) — reads, so
	// business, not admin.
	ItemLocalizationsListSubject = "api.*.refdata.item.localizations-list.v1"
	ItemReferencesListSubject    = "api.*.refdata.item.references-list.v1"
	// TypesListSubject lists registered dictionary TYPES (currency, country,
	// incoterm, …) — REST's listTypes/Types.ListTypes. Fixed-literal
	// "_platform", not a wildcard: types are global, never scoped by
	// context (REST's own GET /api/refdata/{context}/types takes a
	// {context} path segment purely for URL symmetry with every other
	// per-context route — Types.ListTypes itself takes no context
	// argument). Named plural ("types.list") to stay distinct from
	// TypeListSubject above, which — confusingly, but matching
	// internal/natsrpc's own precedent — lists an existing type's ITEMS,
	// not the type registry.
	TypesListSubject = "api._platform.refdata.types.list.v1"
)

// Admin subjects (BR-D41) — never granted to a MintBrowserToken credential
// (accounts-service/auth/token.go denies the api.*.refdata.admin.> prefix
// explicitly). Reachable only by an admin/operator credential.
//
// ContextRegisterSubject is fixed-literal "_platform", not a wildcard, for
// the same reason ContextListSubject is above: registering a brand-new
// context has no existing context to route by (REST's own
// POST /api/refdata/admin/contexts carries no {context} path segment
// either) — the new context's name travels in the body
// (ContextRegisterRequest.Context), same as every other field on that
// request.
const (
	ContextRegisterSubject            = "api._platform.refdata.admin.context.register.v1"
	ContextSetVisibleSubject          = "api.*.refdata.admin.context.set-visible.v1"
	CorpusCreateDraftSubject          = "api.*.refdata.admin.corpus.create-draft.v1"
	CorpusGetDraftSubject             = "api.*.refdata.admin.corpus.get-draft.v1"
	CorpusPutDraftItemSubject         = "api.*.refdata.admin.corpus.put-draft-item.v1"
	CorpusPutDraftLocalizationSubject = "api.*.refdata.admin.corpus.put-draft-localization.v1"
	CorpusPublishSubject              = "api.*.refdata.admin.corpus.publish.v1"
	CorpusRollbackSubject             = "api.*.refdata.admin.corpus.rollback.v1"
	CorpusListVersionsSubject         = "api.*.refdata.admin.corpus.list-versions.v1"
	CorpusGetVersionSubject           = "api.*.refdata.admin.corpus.get-version.v1"
	CorpusDiffSubject                 = "api.*.refdata.admin.corpus.diff.v1"
	// TypeRegisterSubject is fixed-literal "_platform" for the same reason
	// TypesListSubject above is: registering a dictionary type has no
	// context to route by — types are global (handleTypeRegister never
	// calls contextFromSubject).
	TypeRegisterSubject     = "api._platform.refdata.admin.type.register.v1"
	LocaleAddSubject        = "api.*.refdata.admin.locale.add.v1"
	ItemRegisterSubject     = "api.*.refdata.admin.item.register.v1"
	ItemDeprecateSubject    = "api.*.refdata.admin.item.deprecate.v1"
	ItemReactivateSubject   = "api.*.refdata.admin.item.reactivate.v1"
	ItemUpdateAttrsSubject  = "api.*.refdata.admin.item.update-attrs.v1"
	ItemDeleteSubject       = "api.*.refdata.admin.item.delete.v1"
	ReferenceCreateSubject  = "api.*.refdata.admin.reference.create.v1"
	LocalizationSetSubject  = "api.*.refdata.admin.localization.set.v1"
	TranslationDraftSubject = "api.*.refdata.admin.translation.draft.v1"
)

// Deps are Adapter's collaborators — the exact same command handlers
// composition.go's Startup already builds once and shares across every
// tenant connection (see this package's doc comment). Optional fields keep
// the same nil-safety contract internal/rest and internal/natsrpc already
// established: a deployment that never wires JetStream/KV or the AI
// translation drafter simply can't serve the matching subject either.
type Deps struct {
	Types         *commands.TypeHandler
	Items         *commands.ItemHandler
	References    *commands.ReferenceHandler
	Localizations *commands.LocalizationHandler
	Contexts      *commands.ContextHandler
	Corpus        *commands.CorpusHandler
	VersionReader *kvcache.VersionReader
	Projector     *kvcache.Projector
	Versions      domain.VersionRepository
	Translations  *commands.TranslationHandler
	Log           *slog.Logger
	// Tenant is the friendly tenant name this connection belongs to,
	// attached to the micro registration as metadata purely for an admin
	// Services panel to label same-named instances by tenant (mirrors
	// pricing-service's/shipping-service's browserrpc.Deps.Tenant).
	Tenant string
}

// Adapter is refdata-service's api.* frontend-to-service adapter for one
// tenant connection.
type Adapter struct {
	nc            *nats.Conn
	types         *commands.TypeHandler
	items         *commands.ItemHandler
	references    *commands.ReferenceHandler
	localizations *commands.LocalizationHandler
	contexts      *commands.ContextHandler
	corpus        *commands.CorpusHandler
	versionReader *kvcache.VersionReader
	projector     *kvcache.Projector
	versions      domain.VersionRepository
	translations  *commands.TranslationHandler
	log           *slog.Logger
	svc           micro.Service
	tracer        *natstrace.Tracer
}

// errorResponse is the wire shape for every failed api.* call — same shape
// every adapter in this repo uses, so a browser client handles every
// service's errors identically.
type errorResponse struct {
	Error    string `json:"error"`
	NotFound bool   `json:"notFound,omitempty"`
}

func isNotFoundErr(err error) bool {
	return errors.Is(err, domain.ErrItemNotFound) ||
		errors.Is(err, domain.ErrTypeNotFound) ||
		errors.Is(err, domain.ErrReferenceNotFound) ||
		errors.Is(err, domain.ErrContextNotFound) ||
		errors.Is(err, domain.ErrDraftNotFound) ||
		errors.Is(err, kvcache.ErrVersionedKeyNotFound)
}

var errCorpusNotConfigured = errors.New("corpus versioning is not configured")
var errContextsNotConfigured = errors.New("context hierarchy is not configured")
var errVersionedReadsNotConfigured = errors.New("versioned reads are not configured")
var errTranslationsNotConfigured = errors.New("AI-assisted translation drafting is not configured")
var errTargetLocalesRequired = errors.New("targetLocales is required")

// New starts the browserrpc microservice and registers every business and
// admin endpoint on nc — one tenant connection's worth. Callers should
// Stop() the returned Adapter on shutdown/teardown.
func New(nc *nats.Conn, deps Deps) (*Adapter, error) {
	a := &Adapter{
		nc:            nc,
		types:         deps.Types,
		items:         deps.Items,
		references:    deps.References,
		localizations: deps.Localizations,
		contexts:      deps.Contexts,
		corpus:        deps.Corpus,
		versionReader: deps.VersionReader,
		projector:     deps.Projector,
		versions:      deps.Versions,
		translations:  deps.Translations,
		log:           deps.Log,
		tracer:        natstrace.New(nc),
	}

	svc, err := micro.AddService(nc, micro.Config{
		// Matches this connection's own nats.Name("refdata-service") — see
		// this package's doc comment and BR-D37.
		Name:        "refdata-service",
		Version:     "1.0.0",
		Description: "refdata-service api.* business + admin endpoints (Phase 32)",
		Metadata:    map[string]string{"tenant": deps.Tenant},
	})
	if err != nil {
		return nil, err
	}
	a.svc = svc

	for _, ep := range a.endpoints() {
		if err := svc.AddEndpoint(ep.name, a.tracer.Middleware(ep.handler), micro.WithEndpointSubject(ep.subject)); err != nil {
			_ = svc.Stop()
			return nil, err
		}
	}
	return a, nil
}

type endpoint struct {
	name    string
	handler micro.HandlerFunc
	subject string
}

// endpoints is the single registration table — New() iterates it, and
// adapter_test.go's BR-D41 permission tests read its subjects, so the
// permission classification can never drift from what is actually served.
func (a *Adapter) endpoints() []endpoint {
	return []endpoint{
		{"item-get", a.handleItemGet, ItemGetSubject},
		{"item-localizations-list", a.handleItemLocalizationsList, ItemLocalizationsListSubject},
		{"item-references-list", a.handleItemReferencesList, ItemReferencesListSubject},
		{"item-get-versioned", a.handleItemGetVersioned, ItemGetVersionedSubject},
		{"type-list", a.handleTypeList, TypeListSubject},
		{"types-list", a.handleTypesList, TypesListSubject},
		{"locales-list", a.handleLocalesList, LocalesListSubject},
		{"completeness", a.handleCompleteness, CompletenessSubject},
		{"cache-status", a.handleCacheStatus, CacheStatusSubject},

		{"context-register", a.handleContextRegister, ContextRegisterSubject},
		{"context-list", a.handleContextList, ContextListSubject},
		{"context-get", a.handleContextGet, ContextGetSubject},
		{"context-set-visible", a.handleContextSetVisible, ContextSetVisibleSubject},
		{"corpus-create-draft", a.handleCorpusCreateDraft, CorpusCreateDraftSubject},
		{"corpus-get-draft", a.handleCorpusGetDraft, CorpusGetDraftSubject},
		{"corpus-put-draft-item", a.handleCorpusPutDraftItem, CorpusPutDraftItemSubject},
		{"corpus-put-draft-localization", a.handleCorpusPutDraftLocalization, CorpusPutDraftLocalizationSubject},
		{"corpus-publish", a.handleCorpusPublish, CorpusPublishSubject},
		{"corpus-rollback", a.handleCorpusRollback, CorpusRollbackSubject},
		{"corpus-list-versions", a.handleCorpusListVersions, CorpusListVersionsSubject},
		{"corpus-get-version", a.handleCorpusGetVersion, CorpusGetVersionSubject},
		{"corpus-diff", a.handleCorpusDiff, CorpusDiffSubject},
		{"type-register", a.handleTypeRegister, TypeRegisterSubject},
		{"locale-add", a.handleLocaleAdd, LocaleAddSubject},
		{"item-register", a.handleItemRegister, ItemRegisterSubject},
		{"item-deprecate", a.handleItemDeprecate, ItemDeprecateSubject},
		{"item-reactivate", a.handleItemReactivate, ItemReactivateSubject},
		{"item-update-attrs", a.handleItemUpdateAttrs, ItemUpdateAttrsSubject},
		{"item-delete", a.handleItemDelete, ItemDeleteSubject},
		{"reference-create", a.handleReferenceCreate, ReferenceCreateSubject},
		{"localization-set", a.handleLocalizationSet, LocalizationSetSubject},
		{"translation-draft", a.handleTranslationDraft, TranslationDraftSubject},
	}
}

// registeredSubjects is every subject New() serves — the authority
// adapter_test.go's classification check compares its BR-D41 business/admin
// lists against. Reads off a zero-value Adapter: only the subject strings
// are needed, and the method values in the table don't require a
// constructed adapter to enumerate.
func registeredSubjects() []string {
	eps := (&Adapter{}).endpoints()
	out := make([]string, 0, len(eps))
	for _, ep := range eps {
		out = append(out, ep.subject)
	}
	return out
}

// Stop drains the adapter's subscriptions.
func (a *Adapter) Stop() error {
	if a.svc == nil {
		return nil
	}
	return a.svc.Stop()
}

func contextFromSubject(subject string) string {
	parts := strings.Split(subject, ".")
	if len(parts) < 2 {
		return ""
	}
	return parts[1]
}

// --- reply plumbing (mirrors internal/natsrpc/adapter.go) ---

const responderHeader = "Nats-Responder"

func (a *Adapter) responderIdentity() string {
	info := a.svc.Info()
	return fmt.Sprintf("%s/%s", info.Name, info.ID)
}

func (a *Adapter) respondOK(req micro.Request, subject, correlationID string, data []byte) {
	headers := map[string][]string{responderHeader: {a.responderIdentity()}}
	natstrace.SpanFrom(req).End(data, headers)
	if err := req.Respond(data, micro.WithHeaders(micro.Headers(headers))); err != nil && a.log != nil {
		a.log.Error("browserrpc: respond failed", "subject", subject, "err", err)
	}
}

func (a *Adapter) respondError(req micro.Request, subject, correlationID string, err error) {
	data, _ := json.Marshal(errorResponse{Error: err.Error(), NotFound: isNotFoundErr(err)})
	code := "500"
	if isNotFoundErr(err) {
		code = "404"
	}
	headers := map[string][]string{
		micro.ErrorHeader:     {err.Error()},
		micro.ErrorCodeHeader: {code},
		responderHeader:       {a.responderIdentity()},
	}
	natstrace.SpanFrom(req).Fail(err, data, headers)
	if respErr := req.Respond(data, micro.WithHeaders(micro.Headers(headers))); respErr != nil && a.log != nil {
		a.log.Error("browserrpc: respond failed", "subject", subject, "err", respErr)
	}
}

// reply marshals out and replies OK, or replies with err if non-nil —
// shared tail every handler below funnels through, so only the
// business-logic call itself differs per endpoint.
func (a *Adapter) reply(req micro.Request, subject, correlationID string, out any, err error) {
	if err != nil {
		a.respondError(req, subject, correlationID, err)
		return
	}
	data, merr := json.Marshal(out)
	if merr != nil {
		a.respondError(req, subject, correlationID, merr)
		return
	}
	a.respondOK(req, subject, correlationID, data)
}

func spanCtx(req micro.Request) context.Context {
	return natstrace.ContextWithSpan(context.Background(), natstrace.SpanFrom(req))
}

func decode[T any](req micro.Request) (T, error) {
	var in T
	if len(req.Data()) > 0 {
		if err := json.Unmarshal(req.Data(), &in); err != nil {
			return in, err
		}
	}
	return in, nil
}

// --- business handlers (BR-D41) ---

// ItemGetRequest is the api.{context}.refdata.item.get.v1 request payload —
// {context} itself travels in the subject (contextFromSubject), not here.
type ItemGetRequest struct {
	TypeKey string `json:"typeKey"`
	Code    string `json:"code"`
	Locale  string `json:"locale"`
}

// ItemGetResponse mirrors internal/natsrpc's ItemGetResponse shape.
type ItemGetResponse struct {
	Item        domain.DictionaryItem `json:"item"`
	Locale      string                `json:"locale,omitempty"`
	Label       string                `json:"label,omitempty"`
	Description string                `json:"description,omitempty"`
	IsFallback  bool                  `json:"isFallback"`
}

func entryItem(item kvcache.CacheItem) domain.DictionaryItem {
	return domain.DictionaryItem{TypeKey: item.TypeKey, Code: item.Code, Context: item.Context, Status: item.Status}
}

func resolveEntryLabel(entry kvcache.Entry, itemContext, requestedLocale, defaultLocale string) domain.Localization {
	locs := make([]domain.Localization, 0, len(entry.Localizations))
	for locale, v := range entry.Localizations {
		locs = append(locs, domain.Localization{
			TypeKey: entry.Item.TypeKey, Code: entry.Item.Code, Context: itemContext,
			Locale: locale, Label: v.Label, Description: v.Description, Source: v.Source,
		})
	}
	return domain.ResolveLabel(entry.Item.TypeKey, entry.Item.Code, itemContext, requestedLocale, defaultLocale, locs)
}

func (a *Adapter) entryResolvedResponse(ctx context.Context, itemContext string, entry kvcache.Entry, requestedLocale string) (ItemGetResponse, error) {
	item := entryItem(entry.Item)
	defaultLocale, err := a.localizations.DefaultLocale(ctx, itemContext)
	if err != nil {
		return ItemGetResponse{}, err
	}
	resolved := resolveEntryLabel(entry, itemContext, requestedLocale, defaultLocale)
	return ItemGetResponse{Item: item, Locale: resolved.Locale, Label: resolved.Label, Description: resolved.Description, IsFallback: resolved.IsFallback}, nil
}

// resolveItemKVFirst mirrors internal/natsrpc's resolveItemKVFirst — the KV
// cache is refdata-service's own read-through cache (BR-D08), never
// reachable directly by a caller; a Postgres-served response backfills it.
func (a *Adapter) resolveItemKVFirst(ctx context.Context, itemContext, typeKey, code, locale string) (ItemGetResponse, error) {
	if a.projector != nil {
		if entry, err := a.projector.ReadEntry(ctx, itemContext, typeKey, code); err == nil && entry != nil {
			return a.entryResolvedResponse(ctx, itemContext, *entry, locale)
		}
	}
	resolved, err := a.localizations.ResolveItem(ctx, typeKey, itemContext, code, locale)
	if err != nil {
		return ItemGetResponse{}, err
	}
	if a.projector != nil {
		if err := a.projector.Backfill(ctx, itemContext, typeKey, code); err != nil && a.log != nil {
			a.log.Warn("browserrpc: cache backfill failed", "type", typeKey, "code", code, "err", err)
		}
	}
	return ItemGetResponse{
		Item: resolved.Item, Locale: resolved.Localization.Locale,
		Label: resolved.Localization.Label, Description: resolved.Localization.Description,
		IsFallback: resolved.Localization.IsFallback,
	}, nil
}

func (a *Adapter) handleItemGet(req micro.Request) {
	subject, correlationID := req.Subject(), req.Reply()
	in, err := decode[ItemGetRequest](req)
	if err != nil {
		a.respondError(req, subject, correlationID, err)
		return
	}
	out, err := a.resolveItemKVFirst(spanCtx(req), contextFromSubject(subject), in.TypeKey, in.Code, in.Locale)
	a.reply(req, subject, correlationID, out, err)
}

// ItemLocalizationsListRequest is the
// api.{context}.refdata.item.localizations-list.v1 request payload —
// {context} travels in the subject, not here.
type ItemLocalizationsListRequest struct {
	TypeKey string `json:"typeKey"`
	Code    string `json:"code"`
}

// ItemLocalizationsListResponse mirrors REST's localizationsResponse.
type ItemLocalizationsListResponse struct {
	Localizations []domain.Localization `json:"localizations"`
}

func (a *Adapter) handleItemLocalizationsList(req micro.Request) {
	subject, correlationID := req.Subject(), req.Reply()
	in, err := decode[ItemLocalizationsListRequest](req)
	if err != nil {
		a.respondError(req, subject, correlationID, err)
		return
	}
	locs, err := a.localizations.ListForItem(spanCtx(req), in.TypeKey, contextFromSubject(subject), in.Code)
	a.reply(req, subject, correlationID, ItemLocalizationsListResponse{Localizations: locs}, err)
}

// ItemReferencesListRequest is the
// api.{context}.refdata.item.references-list.v1 request payload —
// {context} travels in the subject, not here.
type ItemReferencesListRequest struct {
	TypeKey string `json:"typeKey"`
	Code    string `json:"code"`
}

// ItemReferencesListResponse mirrors REST's referencesResponse.
type ItemReferencesListResponse struct {
	References []domain.DictionaryReference `json:"references"`
}

func (a *Adapter) handleItemReferencesList(req micro.Request) {
	subject, correlationID := req.Subject(), req.Reply()
	in, err := decode[ItemReferencesListRequest](req)
	if err != nil {
		a.respondError(req, subject, correlationID, err)
		return
	}
	refs, err := a.references.ListFrom(spanCtx(req), contextFromSubject(subject), in.TypeKey, in.Code)
	a.reply(req, subject, correlationID, ItemReferencesListResponse{References: refs}, err)
}

// ItemGetVersionedRequest is the api.{context}.refdata.item.get-versioned.v1
// request payload — Version is always an explicit, already-pinned version
// number (mirrors internal/natsrpc's ItemGetVersionedRequest; no "latest"
// resolution here, that's an admin-corpus concern). {context} travels in
// the subject, not here.
type ItemGetVersionedRequest struct {
	TypeKey string `json:"typeKey"`
	Code    string `json:"code"`
	Version int    `json:"version"`
}

func (a *Adapter) handleItemGetVersioned(req micro.Request) {
	subject, correlationID := req.Subject(), req.Reply()
	if a.versionReader == nil {
		a.respondError(req, subject, correlationID, errVersionedReadsNotConfigured)
		return
	}
	in, err := decode[ItemGetVersionedRequest](req)
	if err != nil {
		a.respondError(req, subject, correlationID, err)
		return
	}
	entry, err := a.versionReader.Get(spanCtx(req), contextFromSubject(subject), in.Version, in.TypeKey, in.Code)
	a.reply(req, subject, correlationID, entry, err)
}

// TypeListRequest is the api.{context}.refdata.type.list.v1 request payload
// — {context} travels in the subject, not here. All mirrors REST's
// ?all=true (BR-D06: include deprecated items) — when set, this bypasses
// the KV cache entirely and reads Postgres directly via ListAll, same as
// REST does; the KV cache is warmed only for the assignable-only path.
type TypeListRequest struct {
	TypeKey string `json:"typeKey"`
	Locale  string `json:"locale"`
	All     bool   `json:"all"`
}

type TypeListResponse struct {
	Items []ItemGetResponse `json:"items"`
}

// resolveItemsResponse resolves each item's label (if locale is given) —
// the shared tail of both the KV-cache-miss/Postgres-fallback path in
// resolveTypeKVFirst and the all=true/ListAll path below, so the two don't
// duplicate the same per-item resolution loop.
func (a *Adapter) resolveItemsResponse(ctx context.Context, itemContext, typeKey, locale string, items []domain.DictionaryItem) (TypeListResponse, error) {
	out := TypeListResponse{Items: make([]ItemGetResponse, 0, len(items))}
	for _, item := range items {
		if locale == "" || a.localizations == nil {
			out.Items = append(out.Items, ItemGetResponse{Item: item})
			continue
		}
		resolved, err := a.localizations.ResolveItem(ctx, typeKey, itemContext, item.Code, locale)
		if err != nil {
			return TypeListResponse{}, err
		}
		out.Items = append(out.Items, ItemGetResponse{
			Item: resolved.Item, Locale: resolved.Localization.Locale,
			Label: resolved.Localization.Label, Description: resolved.Localization.Description,
			IsFallback: resolved.Localization.IsFallback,
		})
	}
	return out, nil
}

// resolveTypeKVFirst mirrors internal/natsrpc's resolveTypeKVFirst.
func (a *Adapter) resolveTypeKVFirst(ctx context.Context, itemContext, typeKey, locale string) (TypeListResponse, error) {
	if a.projector != nil {
		if entries, ok := a.projector.ReadType(ctx, itemContext, typeKey); ok {
			out := TypeListResponse{Items: make([]ItemGetResponse, 0, len(entries))}
			for _, entry := range entries {
				if locale == "" || a.localizations == nil {
					out.Items = append(out.Items, ItemGetResponse{Item: entryItem(entry.Item)})
					continue
				}
				resp, err := a.entryResolvedResponse(ctx, itemContext, entry, locale)
				if err != nil {
					return TypeListResponse{}, err
				}
				out.Items = append(out.Items, resp)
			}
			return out, nil
		}
	}

	items, err := a.items.ListAssignable(ctx, typeKey, itemContext)
	if err != nil {
		return TypeListResponse{}, err
	}
	out, err := a.resolveItemsResponse(ctx, itemContext, typeKey, locale, items)
	if err != nil {
		return TypeListResponse{}, err
	}
	if a.projector != nil {
		for _, item := range items {
			if err := a.projector.Backfill(ctx, itemContext, typeKey, item.Code); err != nil && a.log != nil {
				a.log.Warn("browserrpc: cache backfill failed", "type", typeKey, "code", item.Code, "err", err)
			}
		}
	}
	return out, nil
}

func (a *Adapter) handleTypeList(req micro.Request) {
	subject, correlationID := req.Subject(), req.Reply()
	in, err := decode[TypeListRequest](req)
	if err != nil {
		a.respondError(req, subject, correlationID, err)
		return
	}
	itemContext := contextFromSubject(subject)
	if in.All {
		// BR-D06: ?all=true bypasses the KV cache entirely, same as REST —
		// deprecated items aren't guaranteed to be cache-warm.
		items, err := a.items.ListAll(spanCtx(req), in.TypeKey, itemContext)
		if err != nil {
			a.respondError(req, subject, correlationID, err)
			return
		}
		out, err := a.resolveItemsResponse(spanCtx(req), itemContext, in.TypeKey, in.Locale, items)
		a.reply(req, subject, correlationID, out, err)
		return
	}
	out, err := a.resolveTypeKVFirst(spanCtx(req), itemContext, in.TypeKey, in.Locale)
	a.reply(req, subject, correlationID, out, err)
}

// TypesListResponse is the TypesListSubject response — mirrors REST's
// typesResponse. The request carries no body: the type registry is global,
// nothing to filter by.
type TypesListResponse struct {
	Types []domain.DictionaryType `json:"types"`
}

func (a *Adapter) handleTypesList(req micro.Request) {
	subject, correlationID := req.Subject(), req.Reply()
	types, err := a.types.ListTypes(spanCtx(req))
	a.reply(req, subject, correlationID, TypesListResponse{Types: types}, err)
}

// LocalesListResponse is the api.{context}.refdata.locales.list.v1 response
// payload — the request carries no body; {context} travels in the subject.
type LocalesListResponse struct {
	Locales       []string `json:"locales"`
	DefaultLocale string   `json:"defaultLocale"`
}

func (a *Adapter) handleLocalesList(req micro.Request) {
	subject, correlationID := req.Subject(), req.Reply()
	ctx := spanCtx(req)
	itemContext := contextFromSubject(subject)
	locales, err := a.localizations.ListLocales(ctx, itemContext)
	if err != nil {
		a.respondError(req, subject, correlationID, err)
		return
	}
	defaultLocale, err := a.localizations.DefaultLocale(ctx, itemContext)
	a.reply(req, subject, correlationID, LocalesListResponse{Locales: locales, DefaultLocale: defaultLocale}, err)
}

// CompletenessRequest is the api.{context}.refdata.completeness.v1 request
// payload — {context} travels in the subject, not here.
type CompletenessRequest struct {
	TypeKey string `json:"typeKey"`
	Locale  string `json:"locale"`
}

type CompletenessResponse struct {
	TypeKey   string `json:"typeKey"`
	Locale    string `json:"locale"`
	Total     int    `json:"total"`
	Localized int    `json:"localized"`
}

func (a *Adapter) handleCompleteness(req micro.Request) {
	subject, correlationID := req.Subject(), req.Reply()
	in, err := decode[CompletenessRequest](req)
	if err != nil {
		a.respondError(req, subject, correlationID, err)
		return
	}
	total, localized, err := a.localizations.Completeness(spanCtx(req), in.TypeKey, contextFromSubject(subject), in.Locale)
	a.reply(req, subject, correlationID, CompletenessResponse{TypeKey: in.TypeKey, Locale: in.Locale, Total: total, Localized: localized}, err)
}

// CacheStatusRequest is the api.{context}.refdata.cache-status.v1 request
// payload — {context} travels in the subject, not here.
type CacheStatusRequest struct {
	TypeKey string `json:"typeKey"`
}

type CacheStatusResponse struct {
	TypeKey         string `json:"typeKey"`
	PostgresVersion int    `json:"postgresVersion"`
	KVVersion       int    `json:"kvVersion"`
	KVItemCount     int    `json:"kvItemCount"`
	InSync          bool   `json:"inSync"`
}

func (a *Adapter) handleCacheStatus(req micro.Request) {
	subject, correlationID := req.Subject(), req.Reply()
	in, err := decode[CacheStatusRequest](req)
	if err != nil {
		a.respondError(req, subject, correlationID, err)
		return
	}
	if a.versions == nil {
		a.reply(req, subject, correlationID, CacheStatusResponse{TypeKey: in.TypeKey}, nil)
		return
	}
	ctx := spanCtx(req)
	itemContext := contextFromSubject(subject)
	pgVersion, err := a.versions.Current(ctx, itemContext, in.TypeKey)
	if err != nil {
		a.respondError(req, subject, correlationID, err)
		return
	}
	resp := CacheStatusResponse{TypeKey: in.TypeKey, PostgresVersion: pgVersion}
	if a.projector != nil {
		if meta, err := a.projector.ReadMeta(ctx, itemContext, in.TypeKey); err == nil && meta != nil {
			resp.KVVersion = meta.Version
			resp.KVItemCount = meta.ItemCount
		}
	}
	resp.InSync = resp.KVVersion == resp.PostgresVersion
	a.reply(req, subject, correlationID, resp, nil)
}

// --- admin handlers (BR-D41) ---

// ContextRegisterRequest is the ContextRegisterSubject (fixed-literal
// "_platform") request payload — mirrors REST's contextRequest. Context
// here is the new context's own name, not a routing token — there is no
// wildcard on this subject to read it from (see ContextRegisterSubject's
// doc comment).
type ContextRegisterRequest struct {
	Context     string `json:"context"`
	Parent      string `json:"parent"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Tenant      string `json:"tenant"`
}

func (a *Adapter) handleContextRegister(req micro.Request) {
	subject, correlationID := req.Subject(), req.Reply()
	if a.contexts == nil {
		a.respondError(req, subject, correlationID, errContextsNotConfigured)
		return
	}
	in, err := decode[ContextRegisterRequest](req)
	if err != nil {
		a.respondError(req, subject, correlationID, err)
		return
	}
	err = a.contexts.Register(spanCtx(req), domain.Context{Context: in.Context, Parent: in.Parent, Name: in.Name, Description: in.Description, Tenant: in.Tenant})
	a.reply(req, subject, correlationID, struct{}{}, err)
}

// ContextListRequest is the ContextListSubject (fixed-literal "_platform")
// request payload. Tenant is optional; empty means "every context".
type ContextListRequest struct {
	Tenant string `json:"tenant"`
}

type ContextsResponse struct {
	Contexts []domain.Context `json:"contexts"`
}

func (a *Adapter) handleContextList(req micro.Request) {
	subject, correlationID := req.Subject(), req.Reply()
	if a.contexts == nil {
		a.reply(req, subject, correlationID, ContextsResponse{Contexts: []domain.Context{}}, nil)
		return
	}
	in, err := decode[ContextListRequest](req)
	if err != nil {
		a.respondError(req, subject, correlationID, err)
		return
	}
	ctx := spanCtx(req)
	var contexts []domain.Context
	if in.Tenant != "" {
		contexts, err = a.contexts.ListByTenant(ctx, in.Tenant)
	} else {
		contexts, err = a.contexts.List(ctx)
	}
	a.reply(req, subject, correlationID, ContextsResponse{Contexts: contexts}, err)
}

// ContextGetResponse is the api.{context}.refdata.admin.context.get.v1
// response payload — the request carries no body; {context} travels in the
// subject.
type ContextGetResponse struct {
	Context     domain.Context   `json:"context"`
	Ancestors   []domain.Context `json:"ancestors"`
	Descendants []domain.Context `json:"descendants"`
}

func (a *Adapter) handleContextGet(req micro.Request) {
	subject, correlationID := req.Subject(), req.Reply()
	if a.contexts == nil {
		a.respondError(req, subject, correlationID, errContextsNotConfigured)
		return
	}
	ctx := spanCtx(req)
	value, err := a.contexts.Get(ctx, contextFromSubject(subject))
	if err != nil {
		a.respondError(req, subject, correlationID, err)
		return
	}
	ancestors, err := a.contexts.Ancestors(ctx, value.Context)
	if err != nil {
		a.respondError(req, subject, correlationID, err)
		return
	}
	descendants, err := a.contexts.Descendants(ctx, value.Context)
	a.reply(req, subject, correlationID, ContextGetResponse{Context: value, Ancestors: ancestors, Descendants: descendants}, err)
}

// ContextSetVisibleRequest is the
// api.{context}.refdata.admin.context.set-visible.v1 request payload —
// {context} travels in the subject, not here.
type ContextSetVisibleRequest struct {
	Visible bool `json:"visible"`
}

func (a *Adapter) handleContextSetVisible(req micro.Request) {
	subject, correlationID := req.Subject(), req.Reply()
	if a.contexts == nil {
		a.respondError(req, subject, correlationID, errContextsNotConfigured)
		return
	}
	in, err := decode[ContextSetVisibleRequest](req)
	if err != nil {
		a.respondError(req, subject, correlationID, err)
		return
	}
	err = a.contexts.SetVisible(spanCtx(req), contextFromSubject(subject), in.Visible)
	a.reply(req, subject, correlationID, struct{}{}, err)
}

// CorpusContextRequest is shared by every corpus admin endpoint that only
// needs (optionally) Notes — {context} travels in the subject, not here.
type CorpusContextRequest struct {
	Notes string `json:"notes"`
}

func (a *Adapter) handleCorpusCreateDraft(req micro.Request) {
	subject, correlationID := req.Subject(), req.Reply()
	if a.corpus == nil {
		a.respondError(req, subject, correlationID, errCorpusNotConfigured)
		return
	}
	in, err := decode[CorpusContextRequest](req)
	if err != nil {
		a.respondError(req, subject, correlationID, err)
		return
	}
	version, err := a.corpus.CreateDraft(spanCtx(req), contextFromSubject(subject), in.Notes)
	a.reply(req, subject, correlationID, version, err)
}

func (a *Adapter) handleCorpusPublish(req micro.Request) {
	subject, correlationID := req.Subject(), req.Reply()
	if a.corpus == nil {
		a.respondError(req, subject, correlationID, errCorpusNotConfigured)
		return
	}
	version, err := a.corpus.Publish(spanCtx(req), contextFromSubject(subject))
	a.reply(req, subject, correlationID, version, err)
}

// CorpusVersionRequest is shared by corpus admin endpoints keyed on
// {context, version} — {context} travels in the subject, not here.
type CorpusVersionRequest struct {
	Version int    `json:"version"`
	Notes   string `json:"notes"`
}

func (a *Adapter) handleCorpusRollback(req micro.Request) {
	subject, correlationID := req.Subject(), req.Reply()
	if a.corpus == nil {
		a.respondError(req, subject, correlationID, errCorpusNotConfigured)
		return
	}
	in, err := decode[CorpusVersionRequest](req)
	if err != nil {
		a.respondError(req, subject, correlationID, err)
		return
	}
	result, err := a.corpus.Rollback(spanCtx(req), contextFromSubject(subject), in.Version, in.Notes)
	a.reply(req, subject, correlationID, result, err)
}

func (a *Adapter) handleCorpusListVersions(req micro.Request) {
	subject, correlationID := req.Subject(), req.Reply()
	if a.corpus == nil {
		a.reply(req, subject, correlationID, CorpusVersionsResponse{Versions: []domain.CorpusVersion{}}, nil)
		return
	}
	versions, err := a.corpus.Versions(spanCtx(req), contextFromSubject(subject))
	a.reply(req, subject, correlationID, CorpusVersionsResponse{Versions: versions}, err)
}

type CorpusVersionsResponse struct {
	Versions []domain.CorpusVersion `json:"versions"`
}

type CorpusContentsResponse struct {
	Version       domain.CorpusVersion        `json:"version"`
	Items         []domain.CorpusItem         `json:"items"`
	Localizations []domain.CorpusLocalization `json:"localizations"`
}

// corpusContents mirrors internal/rest's writeCorpusContents.
func (a *Adapter) corpusContents(ctx context.Context, contextKey string, version domain.CorpusVersion) (CorpusContentsResponse, error) {
	items, err := a.corpus.ItemsAtVersion(ctx, contextKey, version.Version)
	if err != nil {
		return CorpusContentsResponse{}, err
	}
	locs, err := a.corpus.LocalizationsAtVersion(ctx, contextKey, version.Version)
	if err != nil {
		return CorpusContentsResponse{}, err
	}
	return CorpusContentsResponse{Version: version, Items: items, Localizations: locs}, nil
}

func (a *Adapter) handleCorpusGetDraft(req micro.Request) {
	subject, correlationID := req.Subject(), req.Reply()
	if a.corpus == nil {
		a.respondError(req, subject, correlationID, errCorpusNotConfigured)
		return
	}
	ctx := spanCtx(req)
	itemContext := contextFromSubject(subject)
	versions, err := a.corpus.Versions(ctx, itemContext)
	if err != nil {
		a.respondError(req, subject, correlationID, err)
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
		a.respondError(req, subject, correlationID, domain.ErrDraftNotFound)
		return
	}
	out, err := a.corpusContents(ctx, itemContext, *draft)
	a.reply(req, subject, correlationID, out, err)
}

func (a *Adapter) handleCorpusGetVersion(req micro.Request) {
	subject, correlationID := req.Subject(), req.Reply()
	if a.corpus == nil {
		a.respondError(req, subject, correlationID, errCorpusNotConfigured)
		return
	}
	in, err := decode[CorpusVersionRequest](req)
	if err != nil {
		a.respondError(req, subject, correlationID, err)
		return
	}
	ctx := spanCtx(req)
	itemContext := contextFromSubject(subject)
	version, err := a.corpus.GetVersion(ctx, itemContext, in.Version)
	if err != nil {
		a.respondError(req, subject, correlationID, err)
		return
	}
	out, err := a.corpusContents(ctx, itemContext, version)
	a.reply(req, subject, correlationID, out, err)
}

// CorpusItemRequest is the api.{context}.refdata.admin.corpus.put-draft-item.v1
// request payload — mirrors REST's corpusItemRequest. {context} travels in
// the subject, not here.
type CorpusItemRequest struct {
	TypeKey       string            `json:"typeKey"`
	Code          string            `json:"code"`
	Status        domain.ItemStatus `json:"status"`
	Attrs         map[string]any    `json:"attrs"`
	SourceContext string            `json:"sourceContext"`
	IsOverride    bool              `json:"isOverride"`
}

func (a *Adapter) handleCorpusPutDraftItem(req micro.Request) {
	subject, correlationID := req.Subject(), req.Reply()
	if a.corpus == nil {
		a.respondError(req, subject, correlationID, errCorpusNotConfigured)
		return
	}
	in, err := decode[CorpusItemRequest](req)
	if err != nil {
		a.respondError(req, subject, correlationID, err)
		return
	}
	item := domain.CorpusItem{
		DictionaryItem: domain.DictionaryItem{TypeKey: in.TypeKey, Code: in.Code, Status: in.Status, Attrs: in.Attrs},
		SourceContext:  in.SourceContext,
		IsOverride:     in.IsOverride,
	}
	err = a.corpus.PutDraftItem(spanCtx(req), contextFromSubject(subject), item)
	a.reply(req, subject, correlationID, struct{}{}, err)
}

// CorpusLocalizationRequest is the
// api.{context}.refdata.admin.corpus.put-draft-localization.v1 request
// payload — mirrors REST's corpusLocalizationRequest. {context} travels in
// the subject, not here.
type CorpusLocalizationRequest struct {
	TypeKey       string `json:"typeKey"`
	Code          string `json:"code"`
	Locale        string `json:"locale"`
	Label         string `json:"label"`
	Description   string `json:"description"`
	Source        string `json:"source"`
	SourceContext string `json:"sourceContext"`
}

func (a *Adapter) handleCorpusPutDraftLocalization(req micro.Request) {
	subject, correlationID := req.Subject(), req.Reply()
	if a.corpus == nil {
		a.respondError(req, subject, correlationID, errCorpusNotConfigured)
		return
	}
	in, err := decode[CorpusLocalizationRequest](req)
	if err != nil {
		a.respondError(req, subject, correlationID, err)
		return
	}
	loc := domain.CorpusLocalization{
		Localization: domain.Localization{
			TypeKey: in.TypeKey, Code: in.Code, Locale: in.Locale,
			Label: in.Label, Description: in.Description, Source: in.Source,
		},
		SourceContext: in.SourceContext,
	}
	err = a.corpus.PutDraftLocalization(spanCtx(req), contextFromSubject(subject), loc)
	a.reply(req, subject, correlationID, struct{}{}, err)
}

// CorpusDiffRequest is the api.{context}.refdata.admin.corpus.diff.v1
// request payload — {context} travels in the subject, not here.
type CorpusDiffRequest struct {
	From int `json:"from"`
	To   int `json:"to"`
}

type CorpusDiffResponse struct {
	Entries []domain.CorpusDiffEntry `json:"entries"`
}

func (a *Adapter) handleCorpusDiff(req micro.Request) {
	subject, correlationID := req.Subject(), req.Reply()
	if a.corpus == nil {
		a.respondError(req, subject, correlationID, errCorpusNotConfigured)
		return
	}
	in, err := decode[CorpusDiffRequest](req)
	if err != nil {
		a.respondError(req, subject, correlationID, err)
		return
	}
	entries, err := a.corpus.Diff(spanCtx(req), contextFromSubject(subject), in.From, in.To)
	a.reply(req, subject, correlationID, CorpusDiffResponse{Entries: entries}, err)
}

// TypeRegisterRequest is the api.{context}.refdata.admin.type.register.v1
// request payload — mirrors REST's typeRequest.
type TypeRegisterRequest struct {
	TypeKey     string              `json:"typeKey"`
	Name        string              `json:"name"`
	Description string              `json:"description"`
	Category    domain.TypeCategory `json:"category"`
}

func (a *Adapter) handleTypeRegister(req micro.Request) {
	subject, correlationID := req.Subject(), req.Reply()
	in, err := decode[TypeRegisterRequest](req)
	if err != nil {
		a.respondError(req, subject, correlationID, err)
		return
	}
	err = a.types.RegisterType(spanCtx(req), domain.DictionaryType{TypeKey: in.TypeKey, Name: in.Name, Description: in.Description, Category: in.Category})
	a.reply(req, subject, correlationID, struct{}{}, err)
}

// LocaleAddRequest is the api.{context}.refdata.admin.locale.add.v1 request
// payload — mirrors REST's localeRequest. {context} travels in the subject,
// not here.
type LocaleAddRequest struct {
	Locale    string `json:"locale"`
	IsDefault bool   `json:"isDefault"`
}

func (a *Adapter) handleLocaleAdd(req micro.Request) {
	subject, correlationID := req.Subject(), req.Reply()
	in, err := decode[LocaleAddRequest](req)
	if err != nil {
		a.respondError(req, subject, correlationID, err)
		return
	}
	err = a.localizations.AddLocale(spanCtx(req), contextFromSubject(subject), in.Locale, in.IsDefault)
	a.reply(req, subject, correlationID, struct{}{}, err)
}

// ItemRegisterRequest is the api.{context}.refdata.admin.item.register.v1
// request payload — mirrors REST's itemRequest. {context} travels in the
// subject, not here.
type ItemRegisterRequest struct {
	TypeKey string         `json:"typeKey"`
	Code    string         `json:"code"`
	Attrs   map[string]any `json:"attrs"`
}

type ItemResponse struct {
	Item domain.DictionaryItem `json:"item"`
}

func (a *Adapter) handleItemRegister(req micro.Request) {
	subject, correlationID := req.Subject(), req.Reply()
	in, err := decode[ItemRegisterRequest](req)
	if err != nil {
		a.respondError(req, subject, correlationID, err)
		return
	}
	item, err := a.items.RegisterItem(spanCtx(req), commands.ItemInput{TypeKey: in.TypeKey, Code: in.Code, Context: contextFromSubject(subject), Attrs: in.Attrs})
	a.reply(req, subject, correlationID, ItemResponse{Item: item}, err)
}

// ItemKeyRequest is shared by item-deprecate/item-reactivate/item-delete,
// which all take only {type, code} in the body — {context} travels in the
// subject.
type ItemKeyRequest struct {
	TypeKey string `json:"typeKey"`
	Code    string `json:"code"`
}

func (a *Adapter) handleItemDeprecate(req micro.Request) {
	subject, correlationID := req.Subject(), req.Reply()
	in, err := decode[ItemKeyRequest](req)
	if err != nil {
		a.respondError(req, subject, correlationID, err)
		return
	}
	err = a.items.DeprecateItem(spanCtx(req), in.TypeKey, contextFromSubject(subject), in.Code)
	a.reply(req, subject, correlationID, struct{}{}, err)
}

func (a *Adapter) handleItemReactivate(req micro.Request) {
	subject, correlationID := req.Subject(), req.Reply()
	in, err := decode[ItemKeyRequest](req)
	if err != nil {
		a.respondError(req, subject, correlationID, err)
		return
	}
	err = a.items.ReactivateItem(spanCtx(req), in.TypeKey, contextFromSubject(subject), in.Code)
	a.reply(req, subject, correlationID, struct{}{}, err)
}

func (a *Adapter) handleItemDelete(req micro.Request) {
	subject, correlationID := req.Subject(), req.Reply()
	in, err := decode[ItemKeyRequest](req)
	if err != nil {
		a.respondError(req, subject, correlationID, err)
		return
	}
	err = a.items.DeleteItem(spanCtx(req), in.TypeKey, contextFromSubject(subject), in.Code)
	a.reply(req, subject, correlationID, struct{}{}, err)
}

// ItemUpdateAttrsRequest is the
// api.{context}.refdata.admin.item.update-attrs.v1 request payload —
// {context} travels in the subject, not here.
type ItemUpdateAttrsRequest struct {
	TypeKey string         `json:"typeKey"`
	Code    string         `json:"code"`
	Attrs   map[string]any `json:"attrs"`
}

func (a *Adapter) handleItemUpdateAttrs(req micro.Request) {
	subject, correlationID := req.Subject(), req.Reply()
	in, err := decode[ItemUpdateAttrsRequest](req)
	if err != nil {
		a.respondError(req, subject, correlationID, err)
		return
	}
	err = a.items.UpdateItemAttrs(spanCtx(req), in.TypeKey, contextFromSubject(subject), in.Code, in.Attrs)
	a.reply(req, subject, correlationID, struct{}{}, err)
}

// ReferenceCreateRequest is the api.{context}.refdata.admin.reference.create.v1
// request payload — mirrors REST's referenceRequest. {context} travels in
// the subject, not here.
type ReferenceCreateRequest struct {
	FromTypeKey        string `json:"fromTypeKey"`
	FromCode           string `json:"fromCode"`
	Relation           string `json:"relation"`
	DeclaredTargetType string `json:"declaredTargetType"`
	ToTypeKey          string `json:"toTypeKey"`
	ToCode             string `json:"toCode"`
}

func (a *Adapter) handleReferenceCreate(req micro.Request) {
	subject, correlationID := req.Subject(), req.Reply()
	in, err := decode[ReferenceCreateRequest](req)
	if err != nil {
		a.respondError(req, subject, correlationID, err)
		return
	}
	err = a.references.CreateReference(spanCtx(req), commands.ReferenceInput{
		Context: contextFromSubject(subject), FromTypeKey: in.FromTypeKey, FromCode: in.FromCode,
		Relation: in.Relation, DeclaredTargetType: in.DeclaredTargetType,
		ToTypeKey: in.ToTypeKey, ToCode: in.ToCode,
	})
	a.reply(req, subject, correlationID, struct{}{}, err)
}

// LocalizationSetRequest is the
// api.{context}.refdata.admin.localization.set.v1 request payload — mirrors
// REST's localizationRequest. {context} travels in the subject, not here.
type LocalizationSetRequest struct {
	TypeKey     string `json:"typeKey"`
	Code        string `json:"code"`
	Locale      string `json:"locale"`
	Label       string `json:"label"`
	Description string `json:"description"`
	Source      string `json:"source,omitempty"`
}

func (a *Adapter) handleLocalizationSet(req micro.Request) {
	subject, correlationID := req.Subject(), req.Reply()
	in, err := decode[LocalizationSetRequest](req)
	if err != nil {
		a.respondError(req, subject, correlationID, err)
		return
	}
	err = a.localizations.SetLocalization(spanCtx(req), commands.LocalizationInput{
		TypeKey: in.TypeKey, Code: in.Code, Context: contextFromSubject(subject),
		Locale: in.Locale, Label: in.Label, Description: in.Description, Source: in.Source,
	})
	a.reply(req, subject, correlationID, struct{}{}, err)
}

// TranslationDraftRequest is the
// api.{context}.refdata.admin.translation.draft.v1 request payload — mirrors
// REST's translateRequest, with {type, code} folded into the body since
// api.* subjects carry no path segments beyond {context}. {context} itself
// travels in the subject, not here.
type TranslationDraftRequest struct {
	TypeKey       string   `json:"typeKey"`
	Code          string   `json:"code"`
	TargetLocales []string `json:"targetLocales"`
}

type TranslationDraftEntry struct {
	Locale      string `json:"locale"`
	Label       string `json:"label,omitempty"`
	Description string `json:"description,omitempty"`
	Error       string `json:"error,omitempty"`
}

type TranslationDraftResponse struct {
	Drafts []TranslationDraftEntry `json:"drafts"`
}

func (a *Adapter) handleTranslationDraft(req micro.Request) {
	subject, correlationID := req.Subject(), req.Reply()
	if a.translations == nil {
		a.respondError(req, subject, correlationID, errTranslationsNotConfigured)
		return
	}
	in, err := decode[TranslationDraftRequest](req)
	if err != nil {
		a.respondError(req, subject, correlationID, err)
		return
	}
	if len(in.TargetLocales) == 0 {
		a.respondError(req, subject, correlationID, errTargetLocalesRequired)
		return
	}
	drafts, err := a.translations.DraftTranslations(spanCtx(req), commands.DraftInput{
		TypeKey: in.TypeKey, Code: in.Code, Context: contextFromSubject(subject), TargetLocales: in.TargetLocales,
	})
	if err != nil {
		a.respondError(req, subject, correlationID, err)
		return
	}
	out := make([]TranslationDraftEntry, len(drafts))
	for i, d := range drafts {
		out[i] = TranslationDraftEntry{Locale: d.Locale, Label: d.Label, Description: d.Description, Error: d.Error}
	}
	a.reply(req, subject, correlationID, TranslationDraftResponse{Drafts: out}, nil)
}
