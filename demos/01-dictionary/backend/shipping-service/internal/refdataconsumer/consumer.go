// Package refdataconsumer is the consuming side of refdata-service's rpc.*
// dual-transport adapter (Phase 12.10/12.11) — this package has no
// dependency on refdata-service's Go code; it only agrees on NATS wire
// shapes, exactly as two independent services would in the real platform.
//
// Every read goes through rpc.* (BR-D08): refdata-service's own KV cache is
// an internal read-through layer behind its RPC handler, not something a
// consumer reaches into directly — reaching into another bounded context's
// datastore couples this service to that store's shape (previously this
// consumer read the `refdata-{context}` KV bucket directly, which meant a
// refdata-service-internal cache shape change forced a coordinated update
// here too). Label resolution for the plain (non-versioned) protocol is
// resolved server-side by refdata-service's ResolveLabel and returned
// pre-resolved in the RPC response. The versioned protocol
// (LookupAtVersion) is the one exception: it always returns every locale
// rather than a pre-resolved label, so this consumer still applies the
// BR-D03 fallback chain locally against that response (resolveLabel below).
// defaultLocale there mirrors the locale refdata-service seeds as the
// context default (see refdata-service Seed / BR-D03); this consumer is
// demo-scoped to that context.
//
// Phase 12.11 (BR-D28): this consumer is NATS-only. There is no REST
// fallback and no REST-client coupling of any kind (no base URL, no
// net/http client, no hostname/port config pointing at refdata-service) —
// backend-to-backend calls in this repo go through rpc.* exclusively. A
// bounded number of retries (with backoff) is made against rpc.* on every
// call; if every retry fails, ErrRPCUnavailable is returned to the caller.
// See obsidian/V3-Platform/Architecture/Dictionary-POC/
// ARCHITECTURE-COMMUNICATIONS.md §7 for the full design.
package refdataconsumer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nuid"

	"github.com/jthomas78/nats-tech-lab/shared/natstrace"
)

var ErrNotFound = errors.New("refdata item not found")

// ErrRPCUnavailable is returned when every retry against rpc.* fails (no
// responder, timeout, or a malformed reply) — there is no REST fallback to
// fall through to (BR-D28). Wrapped with the underlying NATS error via %w,
// so callers can still inspect the cause.
var ErrRPCUnavailable = errors.New("refdata rpc: unavailable after retries")

// defaultLocale is the fallback locale in the BR-D03 chain — it mirrors the
// locale refdata-service registers as the context default at seed time.
const defaultLocale = "en"

// localization is one locale's label/description, mirroring the fields this
// consumer needs from refdata-service's domain.LocalizationValue.
type localization struct {
	Locale      string `json:"locale"`
	Label       string `json:"label"`
	Description string `json:"description"`
}

// versionedCacheEntry mirrors refdata-service's kvcache.VersionedEntry
// (Phase 12.5) — the wire shape of the rpc.* item.get-versioned response
// (Phase 12.11), returned when this consumer is pinned to a specific corpus
// version rather than always tracking the latest published one. No
// Item.Label/Description — BR-D30 guarantees the default locale's entry
// exists in Localizations whenever any exist, so this consumer resolves
// both fields from there instead of a duplicated item-level field.
type versionedCacheEntry struct {
	Item struct {
		Code   string `json:"code"`
		Status string `json:"status"`
	} `json:"item"`
	Localizations map[string]localization `json:"localizations"`
	SourceContext string                  `json:"sourceContext"`
	IsOverride    bool                    `json:"isOverride"`
	Version       int                     `json:"version"`
}

// Result is a resolved dictionary item. Source is always "rpc" — retained
// as a field (rather than removed outright) so the REST demo layer that
// surfaces it doesn't need a shape change; before this refactor it
// distinguished a direct-KV-read hit from an rpc.* refetch, but every read
// now goes through rpc.* (BR-D08).
type Result struct {
	Code        string
	Status      string
	Label       string
	Description string
	Source      string
}

// rpcSource is the constant Source value every Result now carries.
const rpcSource = "rpc"

// Bounded retry defaults for rpc.* calls (Phase 12.11, BR-D28) — no REST
// fallback exists, so these bound how long a caller waits before getting
// ErrRPCUnavailable rather than hanging indefinitely. Tests override these
// via WithRPCTimeout/WithRPCRetries/WithRPCBackoff to stay fast.
const (
	defaultRPCRequestTimeout = 3 * time.Second
	defaultRPCRetries        = 2 // total attempts = 1 + retries
	defaultRPCBackoff        = 150 * time.Millisecond
)

// Consumer reads dictionary items exclusively via refdata-service's rpc.*
// dual-transport adapter (BR-D08). It holds a NATS connection and nothing
// else (BR-D28: backend services should only be aware of NATS for
// inter-service calls) — no HTTP client, no base URL, no hostname/port
// config pointing at refdata-service, and no direct dependency on
// refdata-service's KV cache (that cache is internal to refdata-service).
type Consumer struct {
	nc *nats.Conn

	// requestorID is this Consumer's Nats-Requestor value,
	// "<nats.Name>/<NUID>" — fixed at construction so every call from this
	// process carries the same instance identity (see requestorHeader).
	requestorID string

	tracer *natstrace.Tracer

	rpcRequestTimeout time.Duration
	rpcRetries        int
	rpcBackoff        time.Duration
}

// Option configures optional Consumer behavior — currently only the rpc.*
// retry/timeout knobs tests need to override to stay fast.
type Option func(*Consumer)

// WithRPCTimeout overrides the per-attempt rpc.* request timeout (default
// 3s). Tests use this to keep a deliberate no-responder case fast.
func WithRPCTimeout(d time.Duration) Option { return func(c *Consumer) { c.rpcRequestTimeout = d } }

// WithRPCRetries overrides the number of retries after the first rpc.*
// attempt (default 2, i.e. 3 total attempts). 0 means a single attempt with
// no retry.
func WithRPCRetries(n int) Option { return func(c *Consumer) { c.rpcRetries = n } }

// WithRPCBackoff overrides the backoff between rpc.* retry attempts
// (default 150ms, scaled linearly by attempt number).
func WithRPCBackoff(d time.Duration) Option { return func(c *Consumer) { c.rpcBackoff = d } }

func New(nc *nats.Conn, opts ...Option) *Consumer {
	c := &Consumer{
		nc:                nc,
		requestorID:       fmt.Sprintf("%s/%s", nc.Opts.Name, nuid.Next()),
		tracer:            natstrace.New(nc),
		rpcRequestTimeout: defaultRPCRequestTimeout,
		rpcRetries:        defaultRPCRetries,
		rpcBackoff:        defaultRPCBackoff,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Lookup resolves one item and its label for the requested locale via
// rpc.*.refdata.item.get.v1 — refdata-service resolves the label
// server-side (BR-D08).
func (c *Consumer) Lookup(ctx context.Context, itemContext, typeKey, code, locale string) (Result, error) {
	return c.fetchViaRPC(ctx, itemContext, typeKey, code, locale)
}

// ResolveType resolves every item of a type for the requested locale via
// rpc.*.refdata.type.list.v1.
func (c *Consumer) ResolveType(ctx context.Context, itemContext, typeKey, locale string) ([]Result, error) {
	return c.fetchTypeViaRPC(ctx, itemContext, typeKey, locale)
}

// LookupAtVersion resolves one item at a PINNED corpus version via
// rpc.*.refdata.item.get-versioned.v1 — the version-pinning half of
// cross-service consumption (old and new corpus versions coexist
// indefinitely; a consumer pinned to version N keeps reading exactly that
// snapshot until it explicitly re-pins to a newer one).
func (c *Consumer) LookupAtVersion(ctx context.Context, itemContext string, version int, typeKey, code, locale string) (Result, error) {
	return c.fetchVersionedViaRPC(ctx, itemContext, version, typeKey, code, locale)
}

// rpcItemGetVersionedRequest is the rpc.{context}.refdata.item.get-versioned.v1
// request payload — the corpus version is a per-call parameter, not part of
// the subject.
type rpcItemGetVersionedRequest struct {
	Context string `json:"context"`
	TypeKey string `json:"typeKey"`
	Code    string `json:"code"`
	Version int    `json:"version"`
}

// fetchVersionedViaRPC hits the versioned rpc.* endpoint (BR-D25), which
// returns the full materialized entry — unlike the plain protocol's ?locale=
// server-side resolution, the versioned protocol always returns every locale
// and leaves resolution to the caller, so this uses the same local
// resolveLabel as the KV-hit path for consistency.
func (c *Consumer) fetchVersionedViaRPC(ctx context.Context, itemContext string, version int, typeKey, code, locale string) (Result, error) {
	reqBody, err := json.Marshal(rpcItemGetVersionedRequest{Context: itemContext, TypeKey: typeKey, Code: code, Version: version})
	if err != nil {
		return Result{}, err
	}
	subject := "refdata.item.get-versioned.v1"
	data, err := c.requestRPC(ctx, subject, itemContext, "item", "get-versioned", reqBody)
	if err != nil {
		return Result{}, err
	}
	if err := checkRPCError(data); err != nil {
		return Result{}, err
	}
	var entry versionedCacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return Result{}, err
	}
	resolved := resolveLocalization(entry.Localizations, locale, entry.Item.Code)
	return Result{
		Code:        entry.Item.Code,
		Status:      entry.Item.Status,
		Label:       resolved.Label,
		Description: resolved.Description,
		Source:      rpcSource,
	}, nil
}

// rpcLocalesListRequest mirrors refdata-service's natsrpc.LocalesListRequest.
type rpcLocalesListRequest struct {
	Context string `json:"context"`
}

// rpcLocalesListResponse mirrors refdata-service's natsrpc.LocalesListResponse.
type rpcLocalesListResponse struct {
	Locales       []string `json:"locales"`
	DefaultLocale string   `json:"defaultLocale"`
}

// LocalesResult is the context's locale registry as this service sees it. The
// default locale is carried alongside the list, not dropped: BR-D32 requires
// every UI locale list to sort the default first and mark it as the default,
// which a bare []string can't express.
type LocalesResult struct {
	Locales       []string
	DefaultLocale string
}

// Locales returns the locales registered for the context. Locales live in
// refdata-service's Postgres (not the KV cache), so this always calls
// rpc.*'s locales.list endpoint — it is config, not a hot-path lookup, and
// has no KV-cache tier of its own.
func (c *Consumer) Locales(ctx context.Context, itemContext string) (LocalesResult, error) {
	subject := "refdata.locales.list.v1"
	reqBody, err := json.Marshal(rpcLocalesListRequest{Context: itemContext})
	if err != nil {
		return LocalesResult{}, err
	}
	data, err := c.requestRPC(ctx, subject, itemContext, "locales", "list", reqBody)
	if err != nil {
		return LocalesResult{}, err
	}
	if err := checkRPCError(data); err != nil {
		return LocalesResult{}, err
	}
	var resp rpcLocalesListResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return LocalesResult{}, err
	}
	return LocalesResult{Locales: resp.Locales, DefaultLocale: resp.DefaultLocale}, nil
}

// contextListSubject is fixed, not templated by a company context, unlike
// every other rpc.* call this consumer makes — "list the contexts I (a
// tenant) can see" has no single company context to route on. This mirrors
// refdata-service's own rpc._platform.refdata.* precedent for
// steward/tooling-style, corpus-wide operations (Main-POC-Plan.md § Phase
// 16, decision 10; Phase 16f).
const contextListSubject = "rpc._platform.refdata.context.list.v1"

// rpcContextListRequest/rpcContext/rpcContextListResponse mirror
// refdata-service's natsrpc.ContextListRequest/ContextListResponse wire
// shape (Phase 16f).
type rpcContextListRequest struct {
	Tenant string `json:"tenant"`
}

type rpcContext struct {
	Context string `json:"context"`
}

type rpcContextListResponse struct {
	Contexts []rpcContext `json:"contexts"`
}

// ListContexts returns the context values visible to tenant — its own
// registered contexts plus the shared "_"-reserved platform roots every
// tenant inherits from (Phase 16f). Calls
// rpc._platform.refdata.context.list.v1, the natsrpc counterpart of
// refdata-service's REST GET /api/refdata/admin/contexts?tenant=.
func (c *Consumer) ListContexts(ctx context.Context, tenant string) ([]string, error) {
	reqBody, err := json.Marshal(rpcContextListRequest{Tenant: tenant})
	if err != nil {
		return nil, err
	}
	data, err := c.requestRPC(ctx, contextListSubject, "_platform", "context", "list", reqBody)
	if err != nil {
		return nil, err
	}
	if err := checkRPCError(data); err != nil {
		return nil, err
	}
	var resp rpcContextListResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}
	out := make([]string, len(resp.Contexts))
	for i, rc := range resp.Contexts {
		out[i] = rc.Context
	}
	return out, nil
}

// resolvedLocalization is the label+description resolveLocalization returns
// after applying the BR-D03 fallback chain.
type resolvedLocalization struct {
	Label       string
	Description string
}

// resolveLocalization implements the BR-D03 fallback chain locally against a
// cached localizations map: requested locale → bare language (es-ES → es) →
// default locale → the code itself (with no description). It deliberately
// mirrors refdata-service's ResolveLabel rather than importing it (the two
// services share only a wire shape); BR-D08's test guards against
// divergence.
func resolveLocalization(locs map[string]localization, requested, code string) resolvedLocalization {
	if requested != "" {
		if l, ok := locs[requested]; ok && l.Label != "" {
			return resolvedLocalization{Label: l.Label, Description: l.Description}
		}
		if lang := languageOf(requested); lang != requested {
			if l, ok := locs[lang]; ok && l.Label != "" {
				return resolvedLocalization{Label: l.Label, Description: l.Description}
			}
		}
	}
	if l, ok := locs[defaultLocale]; ok && l.Label != "" {
		return resolvedLocalization{Label: l.Label, Description: l.Description}
	}
	return resolvedLocalization{Label: code}
}

func languageOf(locale string) string {
	if i := strings.Index(locale, "-"); i >= 0 {
		return locale[:i]
	}
	return locale
}

// rpcItemGetRequest/rpcItemGetResponse mirror refdata-service's
// natsrpc.ItemGetRequest/ItemGetResponse wire shape (Phase 12.10) — this
// consumer has no dependency on refdata-service's Go code, so it agrees on
// the JSON shape only, same convention as the rest of this file.
type rpcItemGetRequest struct {
	Context string `json:"context"`
	TypeKey string `json:"typeKey"`
	Code    string `json:"code"`
	Locale  string `json:"locale"`
}

type rpcItemGetResponse struct {
	Item struct {
		Code   string         `json:"code"`
		Status string         `json:"status"`
		Attrs  map[string]any `json:"attrs"`
	} `json:"item"`
	Label       string `json:"label,omitempty"`
	Description string `json:"description,omitempty"`
}

// fetchViaRPC publishes the tenant-local refdata.item.get.v1 import. NATS
// remaps it to the PLATFORM export and inserts the caller account identity at
// rpc.* token 2, so a caller-controlled context can never select another
// tenant's corpus.
func (c *Consumer) fetchViaRPC(ctx context.Context, itemContext string, typeKey, code, locale string) (Result, error) {
	reqBody, err := json.Marshal(rpcItemGetRequest{Context: itemContext, TypeKey: typeKey, Code: code, Locale: locale})
	if err != nil {
		return Result{}, err
	}
	subject := "refdata.item.get.v1"
	data, err := c.requestRPC(ctx, subject, itemContext, "item", "get", reqBody)
	if err != nil {
		return Result{}, err
	}
	if err := checkRPCError(data); err != nil {
		return Result{}, err
	}
	var resp rpcItemGetResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return Result{}, err
	}
	return Result{
		Code:        resp.Item.Code,
		Status:      resp.Item.Status,
		Label:       labelOrCode(resp.Label, resp.Item.Code),
		Description: resp.Description,
		Source:      rpcSource,
	}, nil
}

// rpcTypeListRequest/rpcTypeListItem/rpcTypeListResponse mirror
// refdata-service's natsrpc.TypeListRequest/TypeListResponse wire shape
// (Phase 12.11).
type rpcTypeListRequest struct {
	Context string `json:"context"`
	TypeKey string `json:"typeKey"`
	Locale  string `json:"locale"`
}

type rpcTypeListItem struct {
	Item struct {
		Code   string         `json:"code"`
		Status string         `json:"status"`
		Attrs  map[string]any `json:"attrs"`
	} `json:"item"`
	Label string `json:"label,omitempty"`
}

type rpcTypeListResponse struct {
	Items []rpcTypeListItem `json:"items"`
}

// fetchTypeViaRPC hits rpc.*'s type.list endpoint (BR-D25) — the rpc.*
// counterpart of REST's list-a-type endpoint, called when the KV bucket for
// this type is absent/empty.
func (c *Consumer) fetchTypeViaRPC(ctx context.Context, itemContext string, typeKey, locale string) ([]Result, error) {
	reqBody, err := json.Marshal(rpcTypeListRequest{Context: itemContext, TypeKey: typeKey, Locale: locale})
	if err != nil {
		return nil, err
	}
	subject := "refdata.type.list.v1"
	data, err := c.requestRPC(ctx, subject, itemContext, "type", "list", reqBody)
	if err != nil {
		return nil, err
	}
	if err := checkRPCError(data); err != nil {
		return nil, err
	}
	var resp rpcTypeListResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}
	results := make([]Result, 0, len(resp.Items))
	for _, item := range resp.Items {
		results = append(results, Result{
			Code:   item.Item.Code,
			Status: item.Item.Status,
			Label:  labelOrCode(item.Label, item.Item.Code),
			Source: rpcSource,
		})
	}
	return results, nil
}

func labelOrCode(label, code string) string {
	if label != "" {
		return label
	}
	return code
}

// rpcErrorResponse mirrors every refdata-service natsrpc endpoint's error
// wire shape (Phase 12.11) — Error non-empty means the call reached the
// adapter but the application layer returned an error; NotFound
// distinguishes "genuinely doesn't exist" (mapped to this package's
// ErrNotFound) from any other business error, without HTTP status codes to
// lean on now that REST is gone (BR-D28).
type rpcErrorResponse struct {
	Error    string `json:"error"`
	NotFound bool   `json:"notFound"`
}

// checkRPCError inspects a raw rpc.* reply for the error-response shape. It
// returns nil when the reply doesn't look like an error (the normal case —
// a successful reply's JSON has no non-empty "error" field to match).
func checkRPCError(data []byte) error {
	var errResp rpcErrorResponse
	if err := json.Unmarshal(data, &errResp); err == nil && errResp.Error != "" {
		if errResp.NotFound {
			return ErrNotFound
		}
		return fmt.Errorf("refdata rpc: %s", errResp.Error)
	}
	return nil
}

// requestorHeader carries the calling service's identity on every rpc.*
// request — NATS auth identity lives at the connection level and never
// reaches the handler's Msg, so without this header a handler (and the
// Admin UI's Request/Reply panel) has no way to tell who called it. The
// value is "<nats.Name>/<instance ID>", symmetric with Nats-Responder's
// format: the name half reuses the connection's own nats.Name(...) (the
// existing per-service identity) and the instance half is a NUID generated
// once per Consumer, so replicas of the same service stay distinguishable —
// the same service.name/service.instance.id split OpenTelemetry's resource
// conventions use.
const requestorHeader = "Nats-Requestor"

// requestRPC makes a bounded number of attempts against subject (1 +
// rpcRetries total), waiting rpcBackoff*attempt between them, and returns
// ErrRPCUnavailable (wrapping the last underlying error) if every attempt
// fails. There is no REST fallback to fall through to (BR-D28) — this is
// the only transport this consumer has.
//
// BR-037: exactly one natstrace span covers the whole call — minted before
// the retry loop starts, not one per attempt. The span continues whatever
// parent natstrace.ContextWithSpan attached to ctx, or mints a root span if
// ctx carries none (true of every caller today: no browserrpc handler in
// this service invokes refdataconsumer yet — only REST handlers do, via
// r.Context(), which carries no span until a later phase instruments REST
// too). contextValue/entity/action label the span explicitly rather than
// being parsed from subject: most of this consumer's subjects
// (item.get.v1, item.get-versioned.v1, locales.list.v1, type.list.v1) are
// this tenant account's *local* aliases, remapped to the real
// rpc.{tenant}.refdata.*.v1 subject entirely inside the NATS server
// (accounts-service's provisioner.go) — the local alias itself doesn't
// carry {context} at all, and parsing it positionally would silently
// mislabel the span.
func (c *Consumer) requestRPC(ctx context.Context, subject, contextValue, entity, action string, body []byte) ([]byte, error) {
	sp := c.tracer.StartOutbound(natstrace.SpanFromContext(ctx), subject, body, contextValue, "refdata", entity, action)
	msg := &nats.Msg{
		Subject: subject,
		Data:    body,
		Header: nats.Header{
			requestorHeader:             []string{c.requestorID},
			natstrace.TraceparentHeader: []string{sp.Traceparent()},
		},
	}
	// The outbound span has to be told its own headers: it exists before they
	// do (Traceparent() above is one of the values in them). Without this the
	// client-side span shows "no Nats-Requestor" while publishing exactly that
	// identity on the wire for the callee's span to record.
	sp.SetRequestHeaders(map[string][]string(msg.Header))
	var lastErr error
	attempt := 0
	for ; attempt <= c.rpcRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-time.After(c.rpcBackoff * time.Duration(attempt)):
			case <-ctx.Done():
				sp.SetAttribute("rpc.retry_count", strconv.Itoa(attempt))
				sp.Fail(ctx.Err(), nil, nil)
				return nil, ctx.Err()
			}
		}
		rctx, cancel := context.WithTimeout(ctx, c.rpcRequestTimeout)
		reply, err := c.nc.RequestMsgWithContext(rctx, msg)
		cancel()
		if err == nil {
			sp.SetAttribute("rpc.retry_count", strconv.Itoa(attempt))
			sp.End(reply.Data, map[string][]string(reply.Header))
			return reply.Data, nil
		}
		lastErr = err
	}
	sp.SetAttribute("rpc.retry_count", strconv.Itoa(attempt-1))
	finalErr := fmt.Errorf("%w (subject %s): %v", ErrRPCUnavailable, subject, lastErr)
	sp.Fail(finalErr, nil, nil)
	return nil, finalErr
}
