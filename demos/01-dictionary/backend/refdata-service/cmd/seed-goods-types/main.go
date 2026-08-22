// Command seed-goods-types is a standalone preview seeder. It is NOT wired
// into refdata-service's Seed() and is not run at startup. It uses only the
// REST admin API, so every write follows the same validation path as a human
// admin-UI user. Re-running is safe: 409 item conflicts mean "already there"
// and are accepted while the remaining upserts continue.
//
// Usage:
//
//	go run ./cmd/seed-goods-types [-base-url http://localhost:7201] [-context acme] [-tenant acme] [-dry-run] [-publish=true]
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

type goodsTypeRow struct {
	Code, Label, Description string
}

const (
	platformContext = "_platform"
	targetTenant    = "linebooker"
)

func main() {
	baseURL := flag.String("base-url", "http://localhost:7201", "refdata-service base URL")
	contextKey := flag.String("context", targetTenant, "context to seed goods-type data into (sibling of _platform)")
	tenant := flag.String("tenant", "", "tenant that owns the context (default: same as -context)")
	dryRun := flag.Bool("dry-run", false, "print requests instead of sending them")
	publish := flag.Bool("publish", true, "create and publish an initial corpus version after seeding")
	flag.Parse()
	if *tenant == "" {
		*tenant = *contextKey
	}

	c := &client{baseURL: *baseURL, dryRun: *dryRun, httpc: &http.Client{Timeout: 10 * time.Second}}
	log.Printf("seeding goods-type into context %q against %s (dry-run=%v)", *contextKey, *baseURL, *dryRun)
	must(c.registerContext(*contextKey, *tenant))
	must(c.addLocale(*contextKey, "en", true))
	must(c.registerType("goods-type", "Goods Type", "Representative goods and commodity categories for GIT certificate cover", "domain-enum"))
	for _, row := range goodsTypeSeed {
		must(c.registerItem("goods-type", row.Code, *contextKey))
		must(c.setLocalization("goods-type", row.Code, *contextKey, "en", row.Label, row.Description))
	}
	if *publish {
		must(c.createDraft(*contextKey, "seed-goods-types representative corpus"))
		must(c.publish(*contextKey))
	}
	log.Printf("done: %d goods-type items, context %q", len(goodsTypeSeed), *contextKey)
}

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

type client struct {
	baseURL string
	dryRun  bool
	httpc   *http.Client
}

func (c *client) post(path string, body any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	if c.dryRun {
		fmt.Printf("POST %s\n  %s\n", path, b)
		return nil
	}
	resp, err := c.httpc.Post(c.baseURL+path, "application/json", bytes.NewReader(b))
	if err != nil {
		return fmt.Errorf("POST %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 && resp.StatusCode != http.StatusConflict {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("POST %s: %s: %s", path, resp.Status, body)
	}
	return nil
}

func (c *client) registerContext(contextKey, tenant string) error {
	name, description := "Linebooker", "Preview context for linebooker-sourced refdata candidates (seed-goods-types)"
	if contextKey != targetTenant {
		name, description = contextKey, "Representative goods-type corpus seeded by seed-goods-types"
	}
	return c.post("/api/refdata/admin/contexts", map[string]any{
		"context": contextKey, "parent": platformContext, "name": name,
		"description": description, "tenant": tenant,
	})
}

func (c *client) addLocale(contextKey, locale string, isDefault bool) error {
	return c.post("/api/refdata/admin/locales", map[string]any{"context": contextKey, "locale": locale, "isDefault": isDefault})
}

func (c *client) registerType(typeKey, name, description, category string) error {
	return c.post("/api/refdata/admin/types", map[string]any{"typeKey": typeKey, "name": name, "description": description, "category": category})
}

func (c *client) registerItem(typeKey, code, contextKey string) error {
	return c.post("/api/refdata/admin/items", map[string]any{"typeKey": typeKey, "code": code, "context": contextKey, "attrs": map[string]any{}})
}

func (c *client) setLocalization(typeKey, code, contextKey, locale, label, description string) error {
	return c.post("/api/refdata/admin/localizations", map[string]any{
		"typeKey": typeKey, "code": code, "context": contextKey,
		"locale": locale, "label": label, "description": description,
	})
}

func (c *client) createDraft(contextKey, notes string) error {
	return c.post(fmt.Sprintf("/api/refdata/admin/corpus/%s/draft", contextKey), map[string]any{"notes": notes})
}

func (c *client) publish(contextKey string) error {
	return c.post(fmt.Sprintf("/api/refdata/admin/corpus/%s/publish", contextKey), map[string]any{})
}
