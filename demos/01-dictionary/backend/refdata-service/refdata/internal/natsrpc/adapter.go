// Package natsrpc is the rpc.* dual-transport adapter (Phase 12.10) — a
// second, internal-only transport onto the same application-layer methods
// the rest/ adapter already calls, built on the NATS Micro/Services
// framework. See obsidian/V3-Platform/Architecture/Dictionary-POC/
// ARCHITECTURE-COMMUNICATIONS.md for the full design.
package natsrpc

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"regexp"
	"strings"

	"github.com/nats-io/nats.go"
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
	Log           *slog.Logger
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
	log           *slog.Logger
	svc           micro.Service
}

// ItemGetRequest is the rpc.{context}.refdata.item.get.v1 request payload —
// {context} itself travels in the subject, not the body.
type ItemGetRequest struct {
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
	TypeKey string `json:"typeKey"`
	Code    string `json:"code"`
	Version int    `json:"version"`
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
		log:           deps.Log,
	}

	svc, err := micro.AddService(nc, micro.Config{
		Name:        "refdata-rpc",
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
	itemContext := contextFromSubject(subject)
	// The reply-to inbox is unique per request, so it doubles as a
	// correlation ID pairing this request's obs.rpc.* event with its reply's
	// (ARCHITECTURE-COMMUNICATIONS.md §6).
	correlationID := req.Reply()

	a.publishObs(subject, correlationID, "request", req.Data(), "")

	var in ItemGetRequest
	if err := json.Unmarshal(req.Data(), &in); err != nil {
		a.respondError(req, subject, correlationID, err)
		return
	}

	resolved, err := a.localizations.ResolveItem(context.Background(), in.TypeKey, itemContext, in.Code, in.Locale)
	if err != nil {
		a.respondError(req, subject, correlationID, err)
		return
	}

	// Best-effort cache backfill (Q5's miss path, BR-D08) — mirrors REST's
	// getItem handler: an rpc.* consumer hitting a KV miss should find the
	// cache warm next time too, regardless of which transport served the
	// read that repaired it. Never fails the reply — Postgres already has
	// the authoritative answer this response is built from.
	if a.projector != nil {
		if err := a.projector.Backfill(context.Background(), itemContext, in.TypeKey, in.Code); err != nil && a.log != nil {
			a.log.Warn("natsrpc: cache backfill failed", "type", in.TypeKey, "code", in.Code, "err", err)
		}
	}

	out := ItemGetResponse{
		Item:        resolved.Item,
		Locale:      resolved.Localization.Locale,
		Label:       resolved.Localization.Label,
		Description: resolved.Localization.Description,
		IsFallback:  resolved.Localization.IsFallback,
	}
	data, err := json.Marshal(out)
	if err != nil {
		a.respondError(req, subject, correlationID, err)
		return
	}
	a.publishObs(subject, correlationID, "reply", data, "")
	if err := req.Respond(data); err != nil && a.log != nil {
		a.log.Error("natsrpc: respond failed", "subject", subject, "err", err)
	}
}

// handleTypeList is the rpc.* counterpart of REST's listItems — same
// ItemHandler.ListAssignable + per-item LocalizationHandler.ResolveItem
// calls listItems makes, over every item of a type (BR-D25, Phase 12.11).
func (a *Adapter) handleTypeList(req micro.Request) {
	subject := req.Subject()
	itemContext := contextFromSubject(subject)
	correlationID := req.Reply()
	a.publishObs(subject, correlationID, "request", req.Data(), "")

	var in TypeListRequest
	if err := json.Unmarshal(req.Data(), &in); err != nil {
		a.respondError(req, subject, correlationID, err)
		return
	}

	items, err := a.items.ListAssignable(context.Background(), in.TypeKey, itemContext)
	if err != nil {
		a.respondError(req, subject, correlationID, err)
		return
	}

	out := TypeListResponse{Items: make([]ItemGetResponse, 0, len(items))}
	for _, item := range items {
		if in.Locale == "" || a.localizations == nil {
			out.Items = append(out.Items, ItemGetResponse{Item: item})
			continue
		}
		resolved, err := a.localizations.ResolveItem(context.Background(), in.TypeKey, itemContext, item.Code, in.Locale)
		if err != nil {
			a.respondError(req, subject, correlationID, err)
			return
		}
		out.Items = append(out.Items, ItemGetResponse{
			Item:        resolved.Item,
			Locale:      resolved.Localization.Locale,
			Label:       resolved.Localization.Label,
			Description: resolved.Localization.Description,
			IsFallback:  resolved.Localization.IsFallback,
		})
	}

	data, err := json.Marshal(out)
	if err != nil {
		a.respondError(req, subject, correlationID, err)
		return
	}
	a.publishObs(subject, correlationID, "reply", data, "")
	if err := req.Respond(data); err != nil && a.log != nil {
		a.log.Error("natsrpc: respond failed", "subject", subject, "err", err)
	}
}

// handleItemGetVersioned is the rpc.* counterpart of REST's
// getVersionedItem — a pinned-corpus-version read via VersionReader.Get
// (BR-D25, Phase 12.11). Unlike REST, it never resolves the "latest" literal
// version — refdataconsumer.LookupAtVersion always calls with an explicit,
// already-pinned version number.
func (a *Adapter) handleItemGetVersioned(req micro.Request) {
	subject := req.Subject()
	itemContext := contextFromSubject(subject)
	correlationID := req.Reply()
	a.publishObs(subject, correlationID, "request", req.Data(), "")

	if a.versionReader == nil {
		a.respondError(req, subject, correlationID, errVersionedReadsNotConfigured)
		return
	}

	var in ItemGetVersionedRequest
	if err := json.Unmarshal(req.Data(), &in); err != nil {
		a.respondError(req, subject, correlationID, err)
		return
	}

	entry, err := a.versionReader.Get(context.Background(), itemContext, in.Version, in.TypeKey, in.Code)
	if err != nil {
		a.respondError(req, subject, correlationID, err)
		return
	}

	data, err := json.Marshal(entry)
	if err != nil {
		a.respondError(req, subject, correlationID, err)
		return
	}
	a.publishObs(subject, correlationID, "reply", data, "")
	if err := req.Respond(data); err != nil && a.log != nil {
		a.log.Error("natsrpc: respond failed", "subject", subject, "err", err)
	}
}

// handleLocalesList is the rpc.* counterpart of REST's listLocales — the
// same LocalizationHandler.ListLocales + DefaultLocale calls listLocales
// makes (BR-D25, Phase 12.11). Takes no request body beyond the context
// carried in the subject.
func (a *Adapter) handleLocalesList(req micro.Request) {
	subject := req.Subject()
	itemContext := contextFromSubject(subject)
	correlationID := req.Reply()
	a.publishObs(subject, correlationID, "request", req.Data(), "")

	locales, err := a.localizations.ListLocales(context.Background(), itemContext)
	if err != nil {
		a.respondError(req, subject, correlationID, err)
		return
	}
	defaultLocale, err := a.localizations.DefaultLocale(context.Background(), itemContext)
	if err != nil {
		a.respondError(req, subject, correlationID, err)
		return
	}

	data, err := json.Marshal(LocalesListResponse{Locales: locales, DefaultLocale: defaultLocale})
	if err != nil {
		a.respondError(req, subject, correlationID, err)
		return
	}
	a.publishObs(subject, correlationID, "reply", data, "")
	if err := req.Respond(data); err != nil && a.log != nil {
		a.log.Error("natsrpc: respond failed", "subject", subject, "err", err)
	}
}

// respondError also fires the reply-side obs.rpc.* event on failure (BR-D26
// — a failed call must still be visible in the observability view).
func (a *Adapter) respondError(req micro.Request, subject, correlationID string, err error) {
	data, _ := json.Marshal(errorResponse{Error: err.Error(), NotFound: isNotFoundErr(err)})
	a.publishObs(subject, correlationID, "reply", data, err.Error())
	if respErr := req.Respond(data); respErr != nil && a.log != nil {
		a.log.Error("natsrpc: respond failed", "subject", subject, "err", respErr)
	}
}

var versionSuffix = regexp.MustCompile(`\.v\d+$`)

// obsSubjectFor derives the observability subject from a real rpc.* subject
// (e.g. rpc.emea-acme.refdata.item.get.v1 ->
// obs.rpc.emea-acme.refdata.item.get), per ARCHITECTURE-COMMUNICATIONS.md §6.
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

type obsEnvelope struct {
	Direction     string          `json:"direction"`
	CorrelationID string          `json:"correlationId"`
	Subject       string          `json:"subject"`
	Payload       json.RawMessage `json:"payload,omitempty"`
	Error         string          `json:"error,omitempty"`
}

// publishObs fire-and-forget publishes an observability event (BR-D26) — it
// must never block or fail the real RPC reply. nc.Publish is inherently
// non-blocking core-NATS pub/sub (no ack, silently dropped with no
// subscriber), and any marshal/publish error here is swallowed rather than
// propagated to the caller.
func (a *Adapter) publishObs(rpcSubject, correlationID, direction string, payload []byte, errMsg string) {
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
	})
	if err != nil {
		return
	}
	if pubErr := a.nc.Publish(obsSubjectFor(rpcSubject), data); pubErr != nil && a.log != nil {
		a.log.Warn("natsrpc: obs publish failed", "err", pubErr)
	}
}
