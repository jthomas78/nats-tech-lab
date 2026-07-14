// Package refdataconsumer demonstrates the consuming side of the Q5
// versioned-read protocol (Phase 11.3, Dictionary-Service-Plan.md): the
// shipping backend reads the refdata-service's `refdata-{context}` KV cache
// directly for the hot path, and falls through to its REST API — which also
// backfills the cache — on a miss or a stale (version-mismatched) entry.
// This package has no dependency on the refdata-service's Go code; it only
// agrees on the KV bucket naming convention and a JSON wire shape, exactly
// as two independent services would in the real platform.
//
// Label resolution (Phase 11.6, BR-D08) is KV-first: the cached entry already
// carries the per-locale localizations map, so the label is resolved locally
// applying the BR-D03 fallback chain (requested locale → bare language →
// default locale → code). Only a KV miss/stale entry hits REST (with
// ?locale=), which resolves the label server-side via the authoritative
// ResolveLabel. defaultLocale mirrors the locale refdata-service seeds as the
// context default (see refdata-service Seed / BR-D03); this consumer is demo-
// scoped to that context.
package refdataconsumer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/internal/kvstore"
)

var ErrNotFound = errors.New("refdata item not found")

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

// metaEntry mirrors refdata-service's kvcache.MetaEntry.
type metaEntry struct {
	Version int `json:"version"`
}

// apiItemResponse mirrors refdata-service's REST resolvedItemResponse — the
// resolved label sits at the top level (not inside item) when ?locale= is set.
type apiItemResponse struct {
	Item struct {
		Code   string         `json:"code"`
		Status string         `json:"status"`
		Attrs  map[string]any `json:"attrs"`
	} `json:"item"`
	Label string `json:"label"`
}

// apiListResponse mirrors refdata-service's REST list response for a type.
type apiListResponse struct {
	Items []apiItemResponse `json:"items"`
}

// apiLocalesResponse mirrors refdata-service's localesResponse.
type apiLocalesResponse struct {
	Locales []string `json:"locales"`
}

// Result is a resolved dictionary item plus which path served it — the
// consumer-side counterpart to Shape B's cache hit/miss demonstration.
type Result struct {
	Code   string
	Status string
	Label  string
	Attrs  map[string]any
	Source string // "kv-cache" | "api-refetch"
}

// Consumer reads dictionary items via the Q5 versioned-read protocol.
type Consumer struct {
	kv      *kvstore.Store // bucket prefix "refdata"
	baseURL string
	httpc   *http.Client
}

func New(kv *kvstore.Store, baseURL string) *Consumer {
	return &Consumer{kv: kv, baseURL: baseURL, httpc: &http.Client{Timeout: 5 * time.Second}}
}

// Lookup resolves one item and its label for the requested locale. It reads
// the KV cache entry and the type's _meta in the same call; if the entry's
// stamped version doesn't match the set's current version (or either key is
// missing), it re-fetches from the refdata-service's REST API — which is the
// source of truth and also backfills the cache for the next reader. On a KV
// hit the label is resolved locally from the cached localizations map
// (BR-D03 fallback); an empty locale skips label resolution.
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
	return c.fetchViaAPI(ctx, itemContext, typeKey, code, locale)
}

// ResolveType resolves every item of a type for the requested locale. It
// enumerates the bucket's keys (KV-first), resolving each item through
// Lookup so per-item cache-hit/refetch semantics still apply. If the bucket
// is absent or empty it falls back to the REST list endpoint.
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
	return c.fetchTypeViaAPI(ctx, itemContext, typeKey, locale)
}

// Locales returns the locales registered for the context. Locales live in
// refdata-service's Postgres (not the KV cache), so this is a thin REST
// passthrough — it is config, not a hot-path lookup.
func (c *Consumer) Locales(ctx context.Context, itemContext string) ([]string, error) {
	u := fmt.Sprintf("%s/api/refdata/%s/locales", c.baseURL, itemContext)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("refdata API returned %d", resp.StatusCode)
	}
	var out apiLocalesResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out.Locales, nil
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

func (c *Consumer) fetchViaAPI(ctx context.Context, itemContext, typeKey, code, locale string) (Result, error) {
	u := fmt.Sprintf("%s/api/refdata/%s/%s/%s", c.baseURL, itemContext, typeKey, code)
	if locale != "" {
		u += "?locale=" + url.QueryEscape(locale)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return Result{}, err
	}
	resp, err := c.httpc.Do(req)
	if err != nil {
		return Result{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return Result{}, ErrNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return Result{}, fmt.Errorf("refdata API returned %d", resp.StatusCode)
	}

	var api apiItemResponse
	if err := json.NewDecoder(resp.Body).Decode(&api); err != nil {
		return Result{}, err
	}
	return Result{
		Code:   api.Item.Code,
		Status: api.Item.Status,
		Label:  labelOrCode(api.Label, api.Item.Code),
		Attrs:  api.Item.Attrs,
		Source: "api-refetch",
	}, nil
}

func (c *Consumer) fetchTypeViaAPI(ctx context.Context, itemContext, typeKey, locale string) ([]Result, error) {
	u := fmt.Sprintf("%s/api/refdata/%s/%s", c.baseURL, itemContext, typeKey)
	if locale != "" {
		u += "?locale=" + url.QueryEscape(locale)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("refdata API returned %d", resp.StatusCode)
	}
	var list apiListResponse
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return nil, err
	}
	results := make([]Result, 0, len(list.Items))
	for _, item := range list.Items {
		results = append(results, Result{
			Code:   item.Item.Code,
			Status: item.Item.Status,
			Label:  labelOrCode(item.Label, item.Item.Code),
			Attrs:  item.Item.Attrs,
			Source: "api-refetch",
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
