// Package natsrpc is the rpc.* dual-transport adapter (Phase 12.10) — a
// second, internal-only transport onto the same application-layer methods
// the rest/ adapter already calls, built on the NATS Micro/Services
// framework (github.com/nats-io/nats.go/micro —
// https://docs.nats.io/using-nats/developer/services; what it provides —
// endpoint registration, $SRV.* runtime discovery, the
// Nats-Service-Error/-Code header convention used by respondError below —
// is explained in ARCHITECTURE-COMMUNICATIONS.md §4 "What nats.go/micro
// provides"). See obsidian/V3-Platform/Architecture/Dictionary-POC/
// ARCHITECTURE-COMMUNICATIONS.md for the full design.
package natsrpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/nats-io/nats.go/micro"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata/internal/application/commands"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata/internal/domain"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata/internal/kvcache"
)

// ItemGetSubject is the rpc.* subject pattern for the item.get operation —
// {context} is a wildcard token, resolved per-request from the concrete
// subject the request arrived on (mirrors evt.*'s
// evt.{context}.{service}.{entity}.{id}.{event} grammar; see
// ARCHITECTURE-COMMUNICATIONS.md §2).
const ItemGetSubject = "rpc.*.refdata.item.get.v1"

// TypeListSubject serves ItemHandler.ListAssignable + per-item label
// resolution — the rpc.* counterpart of REST's listItems (Phase 12.11,
// BR-D28).
const TypeListSubject = "rpc.*.refdata.type.list.v1"

// ItemGetVersionedSubject serves a pinned-corpus-version item read — the
// rpc.* counterpart of REST's getVersionedItem (Phase 12.11, BR-D28). The
// corpus version travels in the request body, not the subject: unlike the
// tenant/region {context} token, it's a per-call parameter, not a routing
// concern.
const ItemGetVersionedSubject = "rpc.*.refdata.item.get-versioned.v1"

// LocalesListSubject serves LocalizationHandler.ListLocales +
// DefaultLocale — the rpc.* counterpart of REST's listLocales (Phase 12.11,
// BR-D28).
const LocalesListSubject = "rpc.*.refdata.locales.list.v1"

// ContextListSubject serves ContextHandler.List/ListByTenant — the rpc.*
// counterpart of REST's listContexts (Phase 16f). Unlike the other
// endpoints above, its {context} token is the fixed literal "_platform"
// rather than a wildcard: "list the contexts I (a tenant) can see" has no
// single company context to route on — it's the same precedent as
// rpc._platform.refdata.* being the subject family for
// steward/tooling-style corpus-wide operations (Main-POC-Plan.md § Phase
// 16, decision 10). The tenant to filter by travels in the request body
// (ContextListRequest), not the subject — refdata-service runs on one
// shared NATS account and has no server-supplied caller identity to read
// it from otherwise (decision 13).
const ContextListSubject = "rpc._platform.refdata.context.list.v1"

// ObsSubjectWildcard is the subject filter for the obs.rpc.* observability
// side-channel (BR-D26) — the RPCTRACE stream (BR-D29) is provisioned
// against this same wildcard so every event publishObs emits is retained,
// not just the ones a live subscriber happens to catch.
const ObsSubjectWildcard = "obs.rpc.>"

// Deps are Adapter's collaborators. Items and VersionReader are optional
// (nil-safe): a deployment that never wires the corresponding REST routes
// (e.g. VersionReader is nil until JetStream is configured) simply can't
// serve the matching rpc.* endpoint either, mirroring the REST layer's own
// "not configured" responses.
type Deps struct {
	Localizations *commands.LocalizationHandler
	Items         *commands.ItemHandler
	VersionReader *kvcache.VersionReader
	Projector     *kvcache.Projector
	// Contexts is optional (nil-safe, Phase 16f) — a deployment that never
	// wires the REST context routes (h.deps.Contexts == nil there too)
	// simply can't serve context.list.v1 either.
	Contexts *commands.ContextHandler
	// JS is optional (nil-safe, mirroring VersionReader/Projector): when
	// configured, publishObs retains events on RPCTRACE (BR-D29) instead of
	// just fire-and-forget core-NATS publishing.
	JS  jetstream.JetStream
	Log *slog.Logger
}

// Adapter is the natsrpc/ dual-transport adapter — a second transport onto
// the same commands/queries methods the rest/ adapter already calls
// (BR-D25), with a best-effort obs.rpc.* observability side-channel
// (BR-D26).
type Adapter struct {
	nc            *nats.Conn
	localizations *commands.LocalizationHandler
	items         *commands.ItemHandler
	versionReader *kvcache.VersionReader
	projector     *kvcache.Projector
	contexts      *commands.ContextHandler
	js            jetstream.JetStream
	log           *slog.Logger
	svc           micro.Service
}

// ItemGetRequest is the rpc.{context}.refdata.item.get.v1 request payload.
// Context travels in the body (Phase 21: subject position 2 carries the
// caller's NATS account public key, not the context name).
type ItemGetRequest struct {
	Context string `json:"context"`
	TypeKey string `json:"typeKey"`
	Code    string `json:"code"`
	Locale  string `json:"locale"`
}

// ItemGetResponse mirrors the REST get-item response shape closely enough to
// demonstrate BR-D25 (same underlying call, two transports).
type ItemGetResponse struct {
	Item        domain.DictionaryItem `json:"item"`
	Locale      string                `json:"locale,omitempty"`
	Label       string                `json:"label,omitempty"`
	Description string                `json:"description,omitempty"`
	// IsFallback is false only when the requested locale (or its bare
	// language) matched exactly; true for a default-locale substitution or
	// the terminal code-echo (BR-D03) — a bogus locale that happens to fall
	// through to the default must not look identical to an exact match.
	// Unlike the REST resolvedItemResponse, this call always resolves a
	// locale (even ""), so there is no "not attempted" case to distinguish.
	IsFallback bool `json:"isFallback"`
}

// errorResponse is the wire shape for every failed rpc.* call. NotFound lets
// a consumer distinguish "this item/context/version genuinely doesn't
// exist" from any other business error, without depending on REST's HTTP
// status codes (Phase 12.11, BR-D28 — the consumer no longer has a REST leg
// to fall back to for a categorized error).
type errorResponse struct {
	Error    string `json:"error"`
	NotFound bool   `json:"notFound,omitempty"`
}

// isNotFoundErr mirrors the "not found" branch of REST's own writeError
// status-code switch (§ writeError in rest/handlers.go), minus the
// HTTP-specific status code — just the boolean a consumer needs.
func isNotFoundErr(err error) bool {
	return errors.Is(err, domain.ErrItemNotFound) ||
		errors.Is(err, domain.ErrTypeNotFound) ||
		errors.Is(err, domain.ErrReferenceNotFound) ||
		errors.Is(err, domain.ErrContextNotFound) ||
		errors.Is(err, domain.ErrDraftNotFound) ||
		errors.Is(err, kvcache.ErrVersionedKeyNotFound)
}

// TypeListRequest is the rpc.{context}.refdata.type.list.v1 request payload.
type TypeListRequest struct {
	Context string `json:"context"`
	TypeKey string `json:"typeKey"`
	Locale  string `json:"locale"`
}

// TypeListResponse reuses ItemGetResponse per item, so a caller resolving a
// whole type gets the identical per-item shape a single item.get call
// returns.
type TypeListResponse struct {
	Items []ItemGetResponse `json:"items"`
}

// ItemGetVersionedRequest is the rpc.{context}.refdata.item.get-versioned.v1
// request payload — Version is the pinned corpus version, not a subject
// version token.
type ItemGetVersionedRequest struct {
	Context string `json:"context"`
	TypeKey string `json:"typeKey"`
	Code    string `json:"code"`
	Version int    `json:"version"`
}

// LocalesListRequest is the rpc.{context}.refdata.locales.list.v1 request
// payload. Context travels in the body (Phase 21: subject position 2 carries
// the caller's NATS account public key, not the context name).
type LocalesListRequest struct {
	Context string `json:"context"`
}

// ItemGetVersionedResponse is exactly kvcache.VersionedEntry — no separate
// wire shape, since the versioned-read protocol always returns every locale
// and leaves resolution to the caller (see REST's getVersionedItem).
type ItemGetVersionedResponse = kvcache.VersionedEntry

// LocalesListResponse is the rpc.{context}.refdata.locales.list.v1 response
// payload.
type LocalesListResponse struct {
	Locales []string `json:"locales"`
	// DefaultLocale is "" when no locale is marked default for the context.
	DefaultLocale string `json:"defaultLocale"`
}

var errVersionedReadsNotConfigured = errors.New("versioned reads are not configured")

// errContextsNotConfigured mirrors REST's own nil-Contexts guard (Phase 16f).
var errContextsNotConfigured = errors.New("context hierarchy is not configured")

// New starts the natsrpc microservice and registers its endpoints.
// deps.Projector may be nil (e.g. in tests, or when JetStream/KV isn't
// wired) — the cache backfill in handleItemGet/handleTypeList is then simply
// skipped, mirroring the REST layer's own nil check for its Projector
// dependency. deps.VersionReader may also be nil (mirroring REST's own
// "versioned reads are not configured" response) when JetStream isn't
// wired. Callers should Stop() the returned Adapter on shutdown.
func New(nc *nats.Conn, deps Deps) (*Adapter, error) {
	a := &Adapter{
		nc:            nc,
		localizations: deps.Localizations,
		items:         deps.Items,
		versionReader: deps.VersionReader,
		projector:     deps.Projector,
		contexts:      deps.Contexts,
		js:            deps.JS,
		log:           deps.Log,
	}

	svc, err := micro.AddService(nc, micro.Config{
		// Matches this connection's own nats.Name("refdata-service") (cmd/main.go)
		// rather than a family-derived name like "refdata-rpc" — Nats-Responder
		// (responderIdentity below) and Nats-Requestor (refdataconsumer's
		// requestorHeader) must agree on one identity string per service, or the
		// Admin UI's Request/Reply panel reads as if they're different entities.
		Name:        "refdata-service",
		Version:     "1.0.0",
		Description: "refdata-service rpc.* dual-transport endpoints (Phase 12.10/12.11)",
	})
	if err != nil {
		return nil, err
	}
	a.svc = svc

	endpoints := []struct {
		name    string
		handler micro.HandlerFunc
		subject string
	}{
		{"item-get", a.handleItemGet, ItemGetSubject},
		{"type-list", a.handleTypeList, TypeListSubject},
		{"item-get-versioned", a.handleItemGetVersioned, ItemGetVersionedSubject},
		{"locales-list", a.handleLocalesList, LocalesListSubject},
		{"context-list", a.handleContextList, ContextListSubject},
	}
	for _, ep := range endpoints {
		if err := svc.AddEndpoint(ep.name, ep.handler, micro.WithEndpointSubject(ep.subject)); err != nil {
			_ = svc.Stop()
			return nil, err
		}
	}
	return a, nil
}

// Stop drains the adapter's subscriptions.
func (a *Adapter) Stop() error {
	if a.svc == nil {
		return nil
	}
	return a.svc.Stop()
}

func (a *Adapter) handleItemGet(req micro.Request) {
	subject := req.Subject()
	// The reply-to inbox is unique per request, so it doubles as a
	// correlation ID pairing this request's obs.rpc.* event with its reply's
	// (ARCHITECTURE-COMMUNICATIONS.md §6).
	correlationID := req.Reply()

	a.publishObs(subject, correlationID, "request", map[string][]string(req.Headers()), req.Data(), "")

	var in ItemGetRequest
	if err := json.Unmarshal(req.Data(), &in); err != nil {
		a.respondError(req, subject, correlationID, err)
		return
	}

	out, err := a.resolveItemKVFirst(context.Background(), in.Context, in.TypeKey, in.Code, in.Locale)
	if err != nil {
		a.respondError(req, subject, correlationID, err)
		return
	}

	data, err := json.Marshal(out)
	if err != nil {
		a.respondError(req, subject, correlationID, err)
		return
	}
	a.respondOK(req, subject, correlationID, data)
}

// entryItem converts a kvcache.CacheItem into the domain.DictionaryItem shape
// the RPC response embeds. Attrs is left empty — the KV cache intentionally
// omits attrs (a Postgres/REST-only concern, see kvcache.CacheItem) — so a
// cache-served reply and a Postgres-served reply agree on every field except
// this one. No rpc.* consumer currently reads Item.Attrs from this response.
func entryItem(item kvcache.CacheItem) domain.DictionaryItem {
	return domain.DictionaryItem{
		TypeKey: item.TypeKey,
		Code:    item.Code,
		Context: item.Context,
		Status:  item.Status,
	}
}

// resolveEntryLabel applies BR-D03's fallback chain against a cache entry's
// already-cached localizations map — the KV-hit counterpart of
// LocalizationHandler.ResolveItem's Postgres-backed resolution.
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

// entryResolvedResponse always resolves requestedLocale (even "") against a
// cache entry — rpc.*.item.get.v1's contract always returns a resolved
// label, unlike type.list's optional per-item resolution below.
func (a *Adapter) entryResolvedResponse(ctx context.Context, itemContext string, entry kvcache.Entry, requestedLocale string) (ItemGetResponse, error) {
	item := entryItem(entry.Item)
	defaultLocale, err := a.localizations.DefaultLocale(ctx, itemContext)
	if err != nil {
		return ItemGetResponse{}, err
	}
	resolved := resolveEntryLabel(entry, itemContext, requestedLocale, defaultLocale)
	return ItemGetResponse{
		Item:        item,
		Locale:      resolved.Locale,
		Label:       resolved.Label,
		Description: resolved.Description,
		IsFallback:  resolved.IsFallback,
	}, nil
}

// resolveItemKVFirst serves rpc.*.refdata.item.get.v1 from refdata-service's
// internal KV cache when warm, falling through to Postgres (ResolveItem) on
// a miss/stale entry (BR-D08) — the KV bucket is refdata-service's own
// read-through cache, never something a caller reaches into directly. A
// Postgres-served response also backfills the cache so the next call (KV or
// Postgres) finds it warm.
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

	// Best-effort cache backfill (Q5's miss path, BR-D08) — mirrors REST's
	// getItem handler: an rpc.* consumer hitting a cache miss should find it
	// warm next time too, regardless of which transport served the read that
	// repaired it. Never fails the reply — Postgres already has the
	// authoritative answer this response is built from.
	if a.projector != nil {
		if err := a.projector.Backfill(ctx, itemContext, typeKey, code); err != nil && a.log != nil {
			a.log.Warn("natsrpc: cache backfill failed", "type", typeKey, "code", code, "err", err)
		}
	}

	return ItemGetResponse{
		Item:        resolved.Item,
		Locale:      resolved.Localization.Locale,
		Label:       resolved.Localization.Label,
		Description: resolved.Localization.Description,
		IsFallback:  resolved.Localization.IsFallback,
	}, nil
}

// handleTypeList is the rpc.* counterpart of REST's listItems — same
// ItemHandler.ListAssignable + per-item LocalizationHandler.ResolveItem
// calls listItems makes, over every item of a type (BR-D25, Phase 12.11).
func (a *Adapter) handleTypeList(req micro.Request) {
	subject := req.Subject()
	correlationID := req.Reply()
	a.publishObs(subject, correlationID, "request", map[string][]string(req.Headers()), req.Data(), "")

	var in TypeListRequest
	if err := json.Unmarshal(req.Data(), &in); err != nil {
		a.respondError(req, subject, correlationID, err)
		return
	}

	out, err := a.resolveTypeKVFirst(context.Background(), in.Context, in.TypeKey, in.Locale)
	if err != nil {
		a.respondError(req, subject, correlationID, err)
		return
	}

	data, err := json.Marshal(out)
	if err != nil {
		a.respondError(req, subject, correlationID, err)
		return
	}
	a.respondOK(req, subject, correlationID, data)
}

// resolveTypeKVFirst serves rpc.*.refdata.type.list.v1 from the internal KV
// cache when the whole type's cache is warm and complete (BR-D08), falling
// through to Postgres (ListAssignable + per-item ResolveItem, same as REST's
// listItems) otherwise. A Postgres-served response backfills every item's
// cache entry so the type is warm for the next call.
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
			Item:        resolved.Item,
			Locale:      resolved.Localization.Locale,
			Label:       resolved.Localization.Label,
			Description: resolved.Localization.Description,
			IsFallback:  resolved.Localization.IsFallback,
		})
	}

	// Best-effort cache backfill per item (BR-D08) — mirrors
	// resolveItemKVFirst's miss-path repair so a type.list Postgres fallback
	// also warms the cache for the next call (KV or Postgres), not just
	// item.get misses.
	if a.projector != nil {
		for _, item := range items {
			if err := a.projector.Backfill(ctx, itemContext, typeKey, item.Code); err != nil && a.log != nil {
				a.log.Warn("natsrpc: cache backfill failed", "type", typeKey, "code", item.Code, "err", err)
			}
		}
	}

	return out, nil
}

// handleItemGetVersioned is the rpc.* counterpart of REST's
// getVersionedItem — a pinned-corpus-version read via VersionReader.Get
// (BR-D25, Phase 12.11). Unlike REST, it never resolves the "latest" literal
// version — refdataconsumer.LookupAtVersion always calls with an explicit,
// already-pinned version number.
func (a *Adapter) handleItemGetVersioned(req micro.Request) {
	subject := req.Subject()
	correlationID := req.Reply()
	a.publishObs(subject, correlationID, "request", map[string][]string(req.Headers()), req.Data(), "")

	if a.versionReader == nil {
		a.respondError(req, subject, correlationID, errVersionedReadsNotConfigured)
		return
	}

	var in ItemGetVersionedRequest
	if err := json.Unmarshal(req.Data(), &in); err != nil {
		a.respondError(req, subject, correlationID, err)
		return
	}

	entry, err := a.versionReader.Get(context.Background(), in.Context, in.Version, in.TypeKey, in.Code)
	if err != nil {
		a.respondError(req, subject, correlationID, err)
		return
	}

	data, err := json.Marshal(entry)
	if err != nil {
		a.respondError(req, subject, correlationID, err)
		return
	}
	a.respondOK(req, subject, correlationID, data)
}

// handleLocalesList is the rpc.* counterpart of REST's listLocales — the
// same LocalizationHandler.ListLocales + DefaultLocale calls listLocales
// makes (BR-D25, Phase 12.11). Context travels in the body (Phase 21:
// subject position 2 carries the caller's NATS account public key).
func (a *Adapter) handleLocalesList(req micro.Request) {
	subject := req.Subject()
	correlationID := req.Reply()
	a.publishObs(subject, correlationID, "request", map[string][]string(req.Headers()), req.Data(), "")

	var in LocalesListRequest
	if err := json.Unmarshal(req.Data(), &in); err != nil {
		a.respondError(req, subject, correlationID, err)
		return
	}

	locales, err := a.localizations.ListLocales(context.Background(), in.Context)
	if err != nil {
		a.respondError(req, subject, correlationID, err)
		return
	}
	defaultLocale, err := a.localizations.DefaultLocale(context.Background(), in.Context)
	if err != nil {
		a.respondError(req, subject, correlationID, err)
		return
	}

	data, err := json.Marshal(LocalesListResponse{Locales: locales, DefaultLocale: defaultLocale})
	if err != nil {
		a.respondError(req, subject, correlationID, err)
		return
	}
	a.respondOK(req, subject, correlationID, data)
}

// ContextListRequest is the rpc._platform.refdata.context.list.v1 request
// payload — Tenant is optional; empty means "every context" (mirrors REST's
// listContexts with no ?tenant= param).
type ContextListRequest struct {
	Tenant string `json:"tenant"`
}

// ContextListResponse mirrors REST's contextsResponse shape.
type ContextListResponse struct {
	Contexts []domain.Context `json:"contexts"`
}

// handleContextList is the rpc.* counterpart of REST's listContexts
// (Phase 16f) — same ContextHandler.List/ListByTenant calls, over NATS
// instead of HTTP so a backend caller (e.g. shipping-service, BR-D28) never
// needs a REST client to reach it.
func (a *Adapter) handleContextList(req micro.Request) {
	subject := req.Subject()
	correlationID := req.Reply()
	a.publishObs(subject, correlationID, "request", map[string][]string(req.Headers()), req.Data(), "")

	if a.contexts == nil {
		a.respondError(req, subject, correlationID, errContextsNotConfigured)
		return
	}

	var in ContextListRequest
	if len(req.Data()) > 0 {
		if err := json.Unmarshal(req.Data(), &in); err != nil {
			a.respondError(req, subject, correlationID, err)
			return
		}
	}

	var contexts []domain.Context
	var err error
	if in.Tenant != "" {
		contexts, err = a.contexts.ListByTenant(context.Background(), in.Tenant)
	} else {
		contexts, err = a.contexts.List(context.Background())
	}
	if err != nil {
		a.respondError(req, subject, correlationID, err)
		return
	}

	data, err := json.Marshal(ContextListResponse{Contexts: contexts})
	if err != nil {
		a.respondError(req, subject, correlationID, err)
		return
	}
	a.respondOK(req, subject, correlationID, data)
}

// responderHeader identifies which service — and which running instance,
// via micro's own auto-generated per-process instance ID — answered a
// request. Mirrors refdataconsumer's Nats-Requestor (the caller-identity
// header) on the reply side: NATS doesn't propagate responder identity onto
// a reply either, and the subject alone doesn't distinguish which replica
// of a horizontally-scaled service actually handled the call.
const responderHeader = "Nats-Responder"

// responderIdentity is "<service name>/<instance ID>" — svc.Info().ID is a
// fresh, unique value per running process (assigned by micro.AddService),
// so this changes across restarts/replicas without any config of our own.
func (a *Adapter) responderIdentity() string {
	info := a.svc.Info()
	return fmt.Sprintf("%s/%s", info.Name, info.ID)
}

// respondOK sends a successful reply, attaching responderHeader to both the
// real wire reply and its obs.rpc.* observability copy (mirroring how
// respondError attaches the real error headers to both, BR-D36).
func (a *Adapter) respondOK(req micro.Request, subject, correlationID string, data []byte) {
	headers := map[string][]string{responderHeader: {a.responderIdentity()}}
	a.publishObs(subject, correlationID, "reply", headers, data, "")
	if err := req.Respond(data, micro.WithHeaders(micro.Headers(headers))); err != nil && a.log != nil {
		a.log.Error("natsrpc: respond failed", "subject", subject, "err", err)
	}
}

// respondError also fires the reply-side obs.rpc.* event on failure (BR-D26
// — a failed call must still be visible in the observability view). The
// reply carries real Nats-Service-Error/Nats-Service-Error-Code headers
// (micro's own error-header convention, BR-D36) via WithHeaders — additive
// to the existing JSON error body, so no client that reads the body
// (isNotFoundErr's NotFound bool, error string) needs to change.
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
	a.publishObs(subject, correlationID, "reply", headers, data, err.Error())
	if respErr := req.Respond(data, micro.WithHeaders(micro.Headers(headers))); respErr != nil && a.log != nil {
		a.log.Error("natsrpc: respond failed", "subject", subject, "err", respErr)
	}
}

var versionSuffix = regexp.MustCompile(`\.v\d+$`)

// obsSubjectFor derives the observability subject from a real rpc.* subject
// (e.g. rpc.acme.refdata.item.get.v1 ->
// obs.rpc.acme.refdata.item.get), per ARCHITECTURE-COMMUNICATIONS.md §6.
// Every derived subject falls under ObsSubjectWildcard.
func obsSubjectFor(rpcSubject string) string {
	return "obs." + versionSuffix.ReplaceAllString(rpcSubject, "")
}

func contextFromSubject(subject string) string {
	parts := strings.Split(subject, ".")
	if len(parts) < 2 {
		return ""
	}
	return parts[1]
}

// obsEnvelope's Headers/Timestamp/PayloadBytes (BR-D36, Phase 17a) are
// additive to the original BR-D26 shape — every field is optional to decode,
// so events retained on RPCTRACE before this change still parse; a consumer
// must treat their absence as "unknown", not an error.
type obsEnvelope struct {
	Direction     string              `json:"direction"`
	CorrelationID string              `json:"correlationId"`
	Subject       string              `json:"subject"`
	Payload       json.RawMessage     `json:"payload,omitempty"`
	Error         string              `json:"error,omitempty"`
	Headers       map[string][]string `json:"headers,omitempty"`
	Timestamp     time.Time           `json:"timestamp"`
	PayloadBytes  int                 `json:"payloadBytes"`
}

// publishObs fire-and-forget publishes an observability event (BR-D26) — it
// must never block or fail the real RPC reply. Any marshal/publish error
// here is swallowed rather than propagated to the caller.
//
// headers is the real headers carried by this message — the caller's
// req.Headers() on the request side, or nil on a success reply (this
// adapter attaches no custom headers there); respondError builds and passes
// the actual Nats-Service-Error/-Code headers it also sends on the wire
// (BR-D36), never fabricated ones. Timestamp is set here, at publish time —
// not left for the SSE consumer to infer from arrival time, which is wrong
// for RPCTRACE-replayed backlog (BR-D29).
//
// When a.js is configured, the event is published onto RPCTRACE via
// PublishAsync (BR-D29) so a reconnecting Admin UI can catch up on the last
// 10 minutes of traffic — PublishAsync only blocks for the send itself, not
// for the server's ack, so this keeps the same non-blocking contract as the
// plain nc.Publish fallback used when JetStream isn't configured (e.g. some
// tests). Core NATS subscribers (the live tail) receive the message
// identically either way — a JetStream publish is still an ordinary NATS
// message on the wire.
func (a *Adapter) publishObs(rpcSubject, correlationID, direction string, headers map[string][]string, payload []byte, errMsg string) {
	defer func() {
		if r := recover(); r != nil && a.log != nil {
			a.log.Error("natsrpc: obs publish panicked", "recovered", r)
		}
	}()
	data, err := json.Marshal(obsEnvelope{
		Direction:     direction,
		CorrelationID: correlationID,
		Subject:       rpcSubject,
		Payload:       payload,
		Error:         errMsg,
		Headers:       headers,
		Timestamp:     time.Now().UTC(),
		PayloadBytes:  len(payload),
	})
	if err != nil {
		return
	}
	subject := obsSubjectFor(rpcSubject)
	if a.js != nil {
		if _, pubErr := a.js.PublishAsync(subject, data); pubErr != nil && a.log != nil {
			a.log.Warn("natsrpc: obs publish failed", "err", pubErr)
		}
		return
	}
	if pubErr := a.nc.Publish(subject, data); pubErr != nil && a.log != nil {
		a.log.Warn("natsrpc: obs publish failed", "err", pubErr)
	}
}
