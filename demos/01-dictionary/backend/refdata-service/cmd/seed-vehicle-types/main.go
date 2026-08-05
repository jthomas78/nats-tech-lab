// Command seed-vehicle-types is a one-off, standalone preview seeder — NOT
// wired into refdata-service's own Seed() (refdata/seed.go), and not run on
// service startup. It exists to answer one question: "what would
// linebooker's vehicle-type data look like as a refdata-service dictionary
// type?" before any decision is made about a real migration.
//
// It talks to a running refdata-service purely over its REST admin API
// (never touching Postgres directly), so it exercises the exact same
// validation path a human admin-UI user would. Safe to re-run: context,
// type, locale, localization, and reference registrations are all upserts
// server-side; only item registration (BR-D01, one code per type+context)
// rejects a repeat with 409, which this tool treats as "already there" and
// continues rather than failing the run.
//
// Usage:
//
//	go run ./cmd/seed-vehicle-types [-base-url http://localhost:7201] [-context linebooker] [-dry-run]
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

func sp(s string) *string { return &s }

type vehicleTypeRow struct {
	Code        string
	Display     string
	FullDisplay string
	Icon        *string
	OrderIndex  float64
	ParentCode  *string
	Category    string // one of vehicleTypeCategorySeed's codes
}

// vehicleTypeCategorySeed mirrors VehicleType.java's VehicleTypeCategory
// enum (DOUBLE_TRAILER, SINGLE_TRAILER, RIGID, PARENT_GROUP) as its own
// small domain-enum type, so a vehicle-type's category is a typed
// reference (BR-D05) instead of a hardcoded Java switch statement.
var vehicleTypeCategorySeed = []struct{ code, label string }{
	{"DOUBLE_TRAILER", "Double Trailer"},
	{"SINGLE_TRAILER", "Single Trailer"},
	{"RIGID", "Rigid"},
	{"PARENT_GROUP", "Parent Group"},
}

const (
	platformContext = "_platform"
	targetTenant    = "linebooker"
)

func main() {
	baseURL := flag.String("base-url", "http://localhost:7201", "refdata-service base URL")
	context := flag.String("context", targetTenant, "context to seed vehicle-type data into (sibling of _platform)")
	dryRun := flag.Bool("dry-run", false, "print requests instead of sending them")
	publish := flag.Bool("publish", true, "create+publish an initial corpus version for the context after seeding")
	flag.Parse()

	c := &client{baseURL: *baseURL, dryRun: *dryRun, httpc: &http.Client{Timeout: 10 * time.Second}}

	log.Printf("seeding context %q against %s (dry-run=%v)", *context, *baseURL, *dryRun)

	must(c.registerContext(*context))
	must(c.addLocale(*context, "en", true))

	must(c.registerType("vehicle-type-category", "Vehicle Type Category", "Derived from VehicleType.java's getCategory() switch", "domain-enum"))
	for _, cat := range vehicleTypeCategorySeed {
		must(c.registerItem("vehicle-type-category", cat.code, *context, nil))
		must(c.setLocalization("vehicle-type-category", cat.code, *context, "en", cat.label, ""))
	}

	must(c.registerType("vehicle-type", "Vehicle Type", "linebooker's fleet vehicle-type vocabulary (vehicle_type_entity + VehicleType.java)", "domain-enum"))
	for _, row := range vehicleTypeSeed {
		attrs := map[string]any{"orderIndex": row.OrderIndex}
		if row.Icon != nil {
			attrs["icon"] = *row.Icon
		}
		must(c.registerItem("vehicle-type", row.Code, *context, attrs))
		must(c.setLocalization("vehicle-type", row.Code, *context, "en", row.Display, row.FullDisplay))
	}

	// Second pass: references need both ends to already exist.
	for _, row := range vehicleTypeSeed {
		if row.ParentCode != nil {
			must(c.createReference(*context, "vehicle-type", row.Code, "childOf", "vehicle-type", *row.ParentCode))
		}
		if row.Category != "" {
			must(c.createReference(*context, "vehicle-type", row.Code, "hasCategory", "vehicle-type-category", row.Category))
		}
	}

	if *publish {
		must(c.createDraft(*context, "seed-vehicle-types preview"))
		must(c.publish(*context))
	}

	log.Printf("done: %d vehicle-type items, %d category items, context %q", len(vehicleTypeSeed), len(vehicleTypeCategorySeed), *context)
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

// post treats 2xx and 409 (already exists — BR-D01 on repeat item
// registration) as success, so the whole seed is safely re-runnable.
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
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("POST %s: %s: %s", path, resp.Status, respBody)
	}
	return nil
}

func (c *client) registerContext(ctxKey string) error {
	return c.post("/api/refdata/admin/contexts", map[string]any{
		"context":     ctxKey,
		"parent":      platformContext,
		"name":        "Linebooker",
		"description": "Preview context for linebooker-sourced refdata candidates (seed-vehicle-types)",
		"tenant":      targetTenant,
	})
}

func (c *client) addLocale(ctxKey, locale string, isDefault bool) error {
	return c.post("/api/refdata/admin/locales", map[string]any{
		"context": ctxKey, "locale": locale, "isDefault": isDefault,
	})
}

func (c *client) registerType(typeKey, name, description, category string) error {
	return c.post("/api/refdata/admin/types", map[string]any{
		"typeKey": typeKey, "name": name, "description": description, "category": category,
	})
}

func (c *client) registerItem(typeKey, code, ctxKey string, attrs map[string]any) error {
	if attrs == nil {
		attrs = map[string]any{}
	}
	return c.post("/api/refdata/admin/items", map[string]any{
		"typeKey": typeKey, "code": code, "context": ctxKey, "attrs": attrs,
	})
}

func (c *client) setLocalization(typeKey, code, ctxKey, locale, label, description string) error {
	return c.post("/api/refdata/admin/localizations", map[string]any{
		"typeKey": typeKey, "code": code, "context": ctxKey,
		"locale": locale, "label": label, "description": description,
	})
}

func (c *client) createReference(ctxKey, fromType, fromCode, relation, toType, toCode string) error {
	return c.post("/api/refdata/admin/references", map[string]any{
		"context": ctxKey, "fromTypeKey": fromType, "fromCode": fromCode,
		"relation": relation, "declaredTargetType": toType,
		"toTypeKey": toType, "toCode": toCode,
	})
}

func (c *client) createDraft(ctxKey, notes string) error {
	return c.post(fmt.Sprintf("/api/refdata/admin/corpus/%s/draft", ctxKey), map[string]any{"notes": notes})
}

func (c *client) publish(ctxKey string) error {
	return c.post(fmt.Sprintf("/api/refdata/admin/corpus/%s/publish", ctxKey), map[string]any{})
}
