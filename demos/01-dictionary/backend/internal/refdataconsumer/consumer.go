// Package refdataconsumer demonstrates the consuming side of the Q5
// versioned-read protocol (Phase 11.3, Dictionary-Service-Plan.md): the
// shipping backend reads the refdata-service's `refdata-{context}` KV cache
// directly for the hot path, and falls through to its REST API — which also
// backfills the cache — on a miss or a stale (version-mismatched) entry.
// This package has no dependency on the refdata-service's Go code; it only
// agrees on the KV bucket naming convention and a JSON wire shape, exactly
// as two independent services would in the real platform.
package refdataconsumer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/internal/kvstore"
)

var ErrNotFound = errors.New("refdata item not found")

// cacheEntry mirrors refdata-service's kvcache.Entry — just the fields this
// consumer needs.
type cacheEntry struct {
	Item struct {
		Code   string         `json:"code"`
		Status string         `json:"status"`
		Attrs  map[string]any `json:"attrs"`
	} `json:"item"`
	Version int `json:"version"`
}

// metaEntry mirrors refdata-service's kvcache.MetaEntry.
type metaEntry struct {
	Version int `json:"version"`
}

// apiItemResponse mirrors refdata-service's REST resolvedItemResponse.
type apiItemResponse struct {
	Item struct {
		Code   string         `json:"code"`
		Status string         `json:"status"`
		Attrs  map[string]any `json:"attrs"`
	} `json:"item"`
}

// Result is a resolved dictionary item plus which path served it — the
// consumer-side counterpart to Shape B's cache hit/miss demonstration.
type Result struct {
	Code   string
	Status string
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

// Lookup resolves one item. It reads the KV cache entry and the type's
// _meta in the same call; if the entry's stamped version doesn't match the
// set's current version (or either key is missing), it re-fetches from the
// refdata-service's REST API — which is the source of truth and also
// backfills the cache for the next reader.
func (c *Consumer) Lookup(ctx context.Context, itemContext, typeKey, code string) (Result, error) {
	if entry, ok := c.readCache(ctx, itemContext, typeKey, code); ok {
		return Result{Code: entry.Item.Code, Status: entry.Item.Status, Attrs: entry.Item.Attrs, Source: "kv-cache"}, nil
	}
	return c.fetchViaAPI(ctx, itemContext, typeKey, code)
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

func (c *Consumer) fetchViaAPI(ctx context.Context, itemContext, typeKey, code string) (Result, error) {
	url := fmt.Sprintf("%s/api/refdata/%s/%s/%s", c.baseURL, itemContext, typeKey, code)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
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
	return Result{Code: api.Item.Code, Status: api.Item.Status, Attrs: api.Item.Attrs, Source: "api-refetch"}, nil
}
