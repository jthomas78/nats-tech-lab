// Package refdataconsumer demonstrates the consuming side of the Q5
// versioned-read protocol (Phase 11.3, Dictionary-Service-Plan.md): the
// shipping backend reads the refdata-service's `refdata-{context}` KV cache
// directly for the hot path, and on a miss/stale entry calls
// refdata-service's rpc.* dual-transport adapter. This package has no
// dependency on the refdata-service's Go code; it only agrees on the KV
// bucket naming convention and NATS wire shapes, exactly as two independent
// services would in the real platform.
//
// Label resolution (Phase 11.6, BR-D08) is KV-first: the cached entry
// already carries the per-locale localizations map, so the label is
// resolved locally applying the BR-D03 fallback chain (requested locale →
// bare language → default locale → code). Only a KV miss/stale entry calls
// rpc.*, which resolves the label server-side via the authoritative
// ResolveLabel. defaultLocale mirrors the locale refdata-service seeds as
// the context default (see refdata-service Seed / BR-D03); this consumer is
// demo-scoped to that context.
//
// Phase 12.11 (BR-D28): this consumer is NATS-only. There is no REST
// fallback and no REST-client coupling of any kind (no base URL, no
// net/http client, no hostname/port config pointing at refdata-service) —
// backend-to-backend calls in this repo go through rpc.* exclusively. On a
// cache miss, a bounded number of retries (with backoff) is made against
// rpc.*; if every retry fails, ErrRPCUnavailable is returned to the caller.
// See obsidian/V3-Platform/Architecture/Dictionary-POC/
// ARCHITECTURE-COMMUNICATIONS.md §7 for the full design.
package refdataconsumer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/shipping-service/internal/kvstore"
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

// localization is one locale's label, mirroring the fields this consumer
// needs from refdata-service's domain.Localization.
type localization struct {
	Locale string `json:"locale"`
	Label  string `json:"label"`
}

// cacheEntry mirrors refdata-service's kvcache.Entry — just the fields this
// consumer needs. Localizations is keyed by locale (e.g. "en", "es").
type cacheEntry struct {
	Item struct {
		Code   string         `json:"code"`
		Status string         `json:"status"`
		Attrs  map[string]any `json:"attrs"`
	} `json:"item"`
	Localizations map[string]localization `json:"localizations"`
	Version       int                     `json:"version"`
}

// versionedCacheEntry mirrors refdata-service's kvcache.VersionedEntry
// (Phase 12.5) — the per-corpus-version materialized item read from a
// refdata-{context}-v{N} bucket when this consumer is pinned to a specific
// version rather than always tracking the latest published one. It also
// doubles as the wire shape for the rpc.* item.get-versioned response,
// which returns this exact structure (Phase 12.11).
type versionedCacheEntry struct {
	Item struct {
		Code   string         `json:"code"`
		Status string         `json:"status"`
		Attrs  map[string]any `json:"attrs"`
	} `json:"item"`
	Localizations map[string]localization `json:"localizations"`
	SourceContext string                  `json:"sourceContext"`
	IsOverride    bool                    `json:"isOverride"`
	Version       int                     `json:"version"`
}

// metaEntry mirrors refdata-service's kvcache.MetaEntry.
type metaEntry struct {
	Version int `json:"version"`
}

// Result is a resolved dictionary item plus which path served it — the
// consumer-side counterpart to Shape B's cache hit/miss demonstration.
type Result struct {
	Code   string
	Status string
	Label  string
	Attrs  map[string]any
	Source string // "kv-cache" | "rpc-refetch"
}

// Bounded retry defaults for rpc.* calls (Phase 12.11, BR-D28) — no REST
// fallback exists, so these bound how long a caller waits before getting
// ErrRPCUnavailable rather than hanging indefinitely. Tests override these
// via WithRPCTimeout/WithRPCRetries/WithRPCBackoff to stay fast.
const (
	defaultRPCRequestTimeout = 3 * time.Second
	defaultRPCRetries        = 2 // total attempts = 1 + retries
	defaultRPCBackoff        = 150 * time.Millisecond
)

// Consumer reads dictionary items via the Q5 versioned-read protocol,
// re-fetching from refdata-service over rpc.* on a cache miss/stale entry.
// It holds a NATS connection and nothing else (BR-D28: backend services
// should only be aware of NATS for inter-service calls) — no HTTP client,
// no base URL, no hostname/port config pointing at refdata-service.
type Consumer struct {
	kv *kvstore.Store // bucket prefix "refdata"
	nc *nats.Conn

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

func New(kv *kvstore.Store, nc *nats.Conn, opts ...Option) *Consumer {
	c := &Consumer{
		kv:                kv,
		nc:                nc,
		rpcRequestTimeout: defaultRPCRequestTimeout,
		rpcRetries:        defaultRPCRetries,
		rpcBackoff:        defaultRPCBackoff,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Lookup resolves one item and its label for the requested locale. It reads
// the KV cache entry and the type's _meta in the same call; if the entry's
// stamped version doesn't match the set's current version (or either key is
// missing), it re-fetches via rpc.* — the source of truth, which also
// backfills the cache for the next reader (BR-D27). On a KV hit the label is
// resolved locally from the cached localizations map (BR-D03 fallback); an
// empty locale skips label resolution.
func (c *Consumer) Lookup(ctx context.Context, itemContext, typeKey, code, locale string) (Result, error) {
	if entry, ok := c.readCache(ctx, itemContext, typeKey, code); ok {
		return Result{
			Code:   entry.Item.Code,
			Status: entry.Item.Status,
			Label:  resolveLabel(entry.Localizations, locale, code),
			Attrs:  entry.Item.Attrs,
			Source: "kv-cache",
		}, nil
	}
	return c.fetchViaRPC(ctx, itemContext, typeKey, code, locale)
}

// ResolveType resolves every item of a type for the requested locale. It
// enumerates the bucket's keys (KV-first), resolving each item through
// Lookup so per-item cache-hit/refetch semantics still apply. If the bucket
// is absent or empty it falls back to the rpc.* type.list endpoint.
func (c *Consumer) ResolveType(ctx context.Context, itemContext, typeKey, locale string) ([]Result, error) {
	keys, err := c.kv.Keys(ctx, itemContext)
	if err == nil {
		prefix := typeKey + "."
		metaKey := typeKey + "._meta"
		var results []Result
		for _, k := range keys {
			if k == metaKey || !strings.HasPrefix(k, prefix) {
				continue
			}
			code := strings.TrimPrefix(k, prefix)
			res, lookupErr := c.Lookup(ctx, itemContext, typeKey, code, locale)
			if lookupErr != nil {
				continue
			}
			results = append(results, res)
		}
		if len(results) > 0 {
			return results, nil
		}
	}
	return c.fetchTypeViaRPC(ctx, itemContext, typeKey, locale)
}

// LookupAtVersion resolves one item at a PINNED corpus version — the
// version-pinning half of cross-service consumption (old and new corpus
// versions coexist indefinitely; a consumer pinned to version N keeps
// reading exactly that snapshot until it explicitly re-pins to a newer
// one). KV-first against the refdata-{context}-v{N} bucket refdata-service
// eagerly materializes on publish (Phase 12.5); a miss calls rpc.*'s
// item.get-versioned endpoint, which also keeps the bucket warm via the
// server's own rewrite-on-read.
func (c *Consumer) LookupAtVersion(ctx context.Context, itemContext string, version int, typeKey, code, locale string) (Result, error) {
	if entry, ok := c.readVersionedCache(ctx, itemContext, version, typeKey, code); ok {
		return Result{
			Code:   entry.Item.Code,
			Status: entry.Item.Status,
			Label:  resolveLabel(entry.Localizations, locale, code),
			Attrs:  entry.Item.Attrs,
			Source: "kv-cache",
		}, nil
	}
	return c.fetchVersionedViaRPC(ctx, itemContext, version, typeKey, code, locale)
}

func (c *Consumer) readVersionedCache(ctx context.Context, itemContext string, version int, typeKey, code string) (versionedCacheEntry, bool) {
	var entry versionedCacheEntry
	raw, _, err := c.kv.GetVersioned(ctx, itemContext, version, typeKey+"."+code)
	if err != nil || json.Unmarshal(raw, &entry) != nil {
		return versionedCacheEntry{}, false
	}
	return entry, true
}

// rpcItemGetVersionedRequest is the rpc.{context}.refdata.item.get-versioned.v1
// request payload — the corpus version is a per-call parameter, not part of
// the subject.
type rpcItemGetVersionedRequest struct {
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
	reqBody, err := json.Marshal(rpcItemGetVersionedRequest{TypeKey: typeKey, Code: code, Version: version})
	if err != nil {
		return Result{}, err
	}
	subject := fmt.Sprintf("rpc.%s.refdata.item.get-versioned.v1", itemContext)
	data, err := c.requestRPC(ctx, subject, reqBody)
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
	return Result{
		Code:   entry.Item.Code,
		Status: entry.Item.Status,
		Label:  resolveLabel(entry.Localizations, locale, entry.Item.Code),
		Attrs:  entry.Item.Attrs,
		Source: "rpc-refetch",
	}, nil
}

// rpcLocalesListResponse mirrors refdata-service's natsrpc.LocalesListResponse.
type rpcLocalesListResponse struct {
	Locales       []string `json:"locales"`
	DefaultLocale string   `json:"defaultLocale"`
}

// Locales returns the locales registered for the context. Locales live in
// refdata-service's Postgres (not the KV cache), so this always calls
// rpc.*'s locales.list endpoint — it is config, not a hot-path lookup, and
// has no KV-cache tier of its own.
func (c *Consumer) Locales(ctx context.Context, itemContext string) ([]string, error) {
	subject := fmt.Sprintf("rpc.%s.refdata.locales.list.v1", itemContext)
	data, err := c.requestRPC(ctx, subject, nil)
	if err != nil {
		return nil, err
	}
	if err := checkRPCError(data); err != nil {
		return nil, err
	}
	var resp rpcLocalesListResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}
	return resp.Locales, nil
}

// resolveLabel implements the BR-D03 fallback chain locally against a cached
// localizations map: requested locale → bare language (es-ES → es) → default
// locale → the code itself. It deliberately mirrors refdata-service's
// ResolveLabel rather than importing it (the two services share only a wire
// shape); BR-D08's test guards against divergence.
func resolveLabel(locs map[string]localization, requested, code string) string {
	if requested != "" {
		if l, ok := locs[requested]; ok && l.Label != "" {
			return l.Label
		}
		if lang := languageOf(requested); lang != requested {
			if l, ok := locs[lang]; ok && l.Label != "" {
				return l.Label
			}
		}
	}
	if l, ok := locs[defaultLocale]; ok && l.Label != "" {
		return l.Label
	}
	return code
}

func languageOf(locale string) string {
	if i := strings.Index(locale, "-"); i >= 0 {
		return locale[:i]
	}
	return locale
}

// readCache returns the cache entry only if it exists AND its stamped
// version matches the type's current _meta version — a stale entry (the
// versioned-read protocol's mismatch case) is treated the same as a miss.
func (c *Consumer) readCache(ctx context.Context, itemContext, typeKey, code string) (cacheEntry, bool) {
	var entry cacheEntry
	entryRaw, _, err := c.kv.Get(ctx, itemContext, typeKey+"."+code)
	if err != nil || json.Unmarshal(entryRaw, &entry) != nil {
		return cacheEntry{}, false
	}

	var meta metaEntry
	metaRaw, _, err := c.kv.Get(ctx, itemContext, typeKey+"._meta")
	if err != nil || json.Unmarshal(metaRaw, &meta) != nil {
		return cacheEntry{}, false
	}

	if meta.Version != entry.Version {
		return cacheEntry{}, false // stale — the set moved on since this entry was cached
	}
	return entry, true
}

// rpcItemGetRequest/rpcItemGetResponse mirror refdata-service's
// natsrpc.ItemGetRequest/ItemGetResponse wire shape (Phase 12.10) — this
// consumer has no dependency on refdata-service's Go code, so it agrees on
// the JSON shape only, same convention as the rest of this file.
type rpcItemGetRequest struct {
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

// fetchViaRPC is the rpc.* path (BR-D25): it calls
// rpc.{context}.refdata.item.get.v1, the NATS Micro/Services adapter wired to
// the exact same commands.LocalizationHandler.ResolveItem() method the REST
// GET /api/refdata/{context}/{type}/{code} endpoint calls.
func (c *Consumer) fetchViaRPC(ctx context.Context, itemContext, typeKey, code, locale string) (Result, error) {
	reqBody, err := json.Marshal(rpcItemGetRequest{TypeKey: typeKey, Code: code, Locale: locale})
	if err != nil {
		return Result{}, err
	}
	subject := fmt.Sprintf("rpc.%s.refdata.item.get.v1", itemContext)
	data, err := c.requestRPC(ctx, subject, reqBody)
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
		Code:   resp.Item.Code,
		Status: resp.Item.Status,
		Label:  labelOrCode(resp.Label, resp.Item.Code),
		Attrs:  resp.Item.Attrs,
		Source: "rpc-refetch",
	}, nil
}

// rpcTypeListRequest/rpcTypeListItem/rpcTypeListResponse mirror
// refdata-service's natsrpc.TypeListRequest/TypeListResponse wire shape
// (Phase 12.11).
type rpcTypeListRequest struct {
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
func (c *Consumer) fetchTypeViaRPC(ctx context.Context, itemContext, typeKey, locale string) ([]Result, error) {
	reqBody, err := json.Marshal(rpcTypeListRequest{TypeKey: typeKey, Locale: locale})
	if err != nil {
		return nil, err
	}
	subject := fmt.Sprintf("rpc.%s.refdata.type.list.v1", itemContext)
	data, err := c.requestRPC(ctx, subject, reqBody)
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
			Attrs:  item.Item.Attrs,
			Source: "rpc-refetch",
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

// requestRPC makes a bounded number of attempts against subject (1 +
// rpcRetries total), waiting rpcBackoff*attempt between them, and returns
// ErrRPCUnavailable (wrapping the last underlying error) if every attempt
// fails. There is no REST fallback to fall through to (BR-D28) — this is
// the only transport this consumer has.
func (c *Consumer) requestRPC(ctx context.Context, subject string, body []byte) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt <= c.rpcRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-time.After(c.rpcBackoff * time.Duration(attempt)):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		rctx, cancel := context.WithTimeout(ctx, c.rpcRequestTimeout)
		msg, err := c.nc.RequestWithContext(rctx, subject, body)
		cancel()
		if err == nil {
			return msg.Data, nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("%w (subject %s): %v", ErrRPCUnavailable, subject, lastErr)
}
