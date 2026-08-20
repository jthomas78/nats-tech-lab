// Command seed-regions seeds the `region` corpus (BR-D46-BR-D48) — the
// Country -> Region hierarchy behind Phase 38d-ii's Operating Areas.
//
// It talks to a running refdata-service over its REST admin API (never
// Postgres directly), so it exercises the same validation path a human admin
// would, including BR-D47's country requirement. Safe to re-run: every call
// is an upsert server-side except item/region registration, which returns
// 409 on a repeat (BR-D01) and is treated as "already there".
//
// Regions are seeded into _platform, not a tenant context: ISO 3166-2 is an
// external standard (BR-D46's `standards` category), so every business unit
// should inherit one corpus rather than maintain its own.
//
// The corpus is sourced from the live Linebooker V2 database rather than
// invented — see ARCHITECTURE-ORGANIZATIONS.md § "Operating Areas — region
// seed". Its codes must stay in step with the map overlay at
// frontend/refdata/public/geo/operating-areas.geojson; the two artifacts are
// joined only by ISO code.
//
// Usage:
//
//	go run ./cmd/seed-regions [-base-url http://localhost:7201] [-dry-run]
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

const platformContext = "_platform"

type countryRow struct {
	Code string
	Name string
}

// countrySeed covers only the countries this seed needs that are not already
// in the corpus. ZA is already seeded (52 countries exist); BW and NA are
// not, and BR-D47 refuses a region whose country is missing, so they must
// land first.
var countrySeed = []countryRow{
	{"BW", "Botswana"},
	{"NA", "Namibia"},
}

type regionRow struct {
	Code    string
	Country string
	Name    string
	// Afrikaans label where V2 carried one as a *separate region row*.
	// BR-D48 makes these localizations of one canonical item instead — this
	// field is the concrete demonstration of that rule.
	AfZA string
}

// regionSeed — 32 first-level subdivisions across ZA (9), BW (10), NA (13).
//
// Namibia is 13, not the ISO 14: NA-OK Kavango is seeded undivided under its
// retired pre-2013 code because the post-2013 NA-KE/NA-KW split geometry was
// not obtainable, and V2's own corpus likewise carries a single "Kavango
// Region". Fabricating a boundary would have been worse than an honest
// retired code. Reversible later — the corpus and the GeoJSON are joined
// only by code.
var regionSeed = []regionRow{
	// South Africa — all 9 provinces are in live use in V2 (2,366-2,720
	// transporters each). The AfZA labels are the exact strings V2 stored as
	// duplicate region rows with their own independent assignments.
	{"ZA-EC", "ZA", "Eastern Cape", "Oos-Kaap"},
	{"ZA-FS", "ZA", "Free State", "Vrystaat"},
	{"ZA-GP", "ZA", "Gauteng", "Gauteng"},
	{"ZA-KZN", "ZA", "KwaZulu-Natal", "KwaZulu-Natal"},
	{"ZA-LP", "ZA", "Limpopo", "Limpopo"},
	{"ZA-MP", "ZA", "Mpumalanga", "Mpumalanga"},
	{"ZA-NC", "ZA", "Northern Cape", "Noord-Kaap"},
	{"ZA-NW", "ZA", "North West", "Noordwes"},
	{"ZA-WC", "ZA", "Western Cape", "Wes-Kaap"},

	// Botswana — the 10 ISO districts. V2 additionally carries 5 city/town
	// rows (Gaborone City, Francistown City, Lobatse Town, Jwaneng Town,
	// Selibe Phikwe Town); those are a different ISO level and are excluded
	// from a two-level Country -> Region corpus.
	{"BW-CE", "BW", "Central", ""},
	{"BW-CH", "BW", "Chobe", ""},
	{"BW-GH", "BW", "Ghanzi", ""},
	{"BW-KG", "BW", "Kgalagadi", ""},
	{"BW-KL", "BW", "Kgatleng", ""},
	{"BW-KW", "BW", "Kweneng", ""},
	{"BW-NE", "BW", "North-East", ""},
	{"BW-NW", "BW", "North-West", ""},
	{"BW-SE", "BW", "South-East", ""},
	{"BW-SO", "BW", "Southern", ""},

	// Namibia — 13 regions. NA-CA is "Zambezi" (renamed from Caprivi in
	// 2013) and NA-KA is "ǁKaras", whose first character is U+01C1 LATIN
	// LETTER LATERAL CLICK — not two ASCII pipes. Both spellings are
	// deliberate and are asserted by the GeoJSON validator too.
	{"NA-CA", "NA", "Zambezi", ""},
	{"NA-ER", "NA", "Erongo", ""},
	{"NA-HA", "NA", "Hardap", ""},
	{"NA-KA", "NA", "ǁKaras", ""},
	{"NA-KH", "NA", "Khomas", ""},
	{"NA-KU", "NA", "Kunene", ""},
	{"NA-OD", "NA", "Otjozondjupa", ""},
	{"NA-OH", "NA", "Omaheke", ""},
	{"NA-OK", "NA", "Kavango", ""},
	{"NA-ON", "NA", "Oshana", ""},
	{"NA-OS", "NA", "Omusati", ""},
	{"NA-OT", "NA", "Oshikoto", ""},
	{"NA-OW", "NA", "Ohangwena", ""},
}

func main() {
	baseURL := flag.String("base-url", "http://localhost:7201", "refdata-service base URL")
	dryRun := flag.Bool("dry-run", false, "print requests instead of sending them")
	flag.Parse()

	c := &client{baseURL: *baseURL, dryRun: *dryRun, httpc: &http.Client{Timeout: 10 * time.Second}}
	log.Printf("seeding region corpus into %s against %s (dry-run=%v)", platformContext, *baseURL, *dryRun)

	must(c.registerType("region", "Region",
		"ISO 3166-2 first-level administrative subdivisions; parent country via the `country` relation (BR-D46-BR-D48)",
		"standards"))

	// Countries first — BR-D47 refuses a region whose country is absent.
	for _, ctry := range countrySeed {
		must(c.registerItem("country", ctry.Code, map[string]any{"name": ctry.Name}))
		must(c.setLocalization("country", ctry.Code, "en", ctry.Name))
	}

	var afCount int
	for _, r := range regionSeed {
		must(c.registerRegion(r.Code, r.Country, r.Name))
		must(c.setLocalization("region", r.Code, "en", r.Name))
		if r.AfZA != "" {
			must(c.setLocalization("region", r.Code, "af-za", r.AfZA))
			afCount++
		}
	}

	log.Printf("done: %d regions across %d new countries, %d af-za labels (BR-D48: labels, not duplicate items)",
		len(regionSeed), len(countrySeed), afCount)
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

// post treats 2xx and 409 (already exists — BR-D01 on a repeat item or
// region registration) as success, so the whole seed is re-runnable.
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

func (c *client) registerType(typeKey, name, description, category string) error {
	return c.post("/api/refdata/admin/types", map[string]any{
		"typeKey": typeKey, "name": name, "description": description, "category": category,
	})
}

func (c *client) registerItem(typeKey, code string, attrs map[string]any) error {
	return c.post("/api/refdata/admin/items", map[string]any{
		"typeKey": typeKey, "code": code, "context": platformContext, "attrs": attrs,
	})
}

// registerRegion uses the dedicated BR-D46 route, not the generic item +
// reference pair, so the seed goes through BR-D47's guard rather than around
// it.
func (c *client) registerRegion(code, countryCode, name string) error {
	return c.post("/api/refdata/admin/regions", map[string]any{
		"context": platformContext, "code": code, "countryCode": countryCode, "name": name,
	})
}

func (c *client) setLocalization(typeKey, code, locale, label string) error {
	return c.post("/api/refdata/admin/localizations", map[string]any{
		"typeKey": typeKey, "code": code, "context": platformContext,
		"locale": locale, "label": label, "description": "",
	})
}
