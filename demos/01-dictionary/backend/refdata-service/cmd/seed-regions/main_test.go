package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// geojsonPath is the Operating Areas map overlay. The region corpus and the
// overlay are separate artifacts joined only by ISO code (BR-D46's Test
// note), so nothing but a test stops them drifting apart — a region with no
// polygon renders as an unclickable checklist row, and a polygon with no
// region is a shape nothing can be assigned to.
const geojsonPath = "../../../../frontend/refdata/public/geo/operating-areas.geojson"

func TestSeedCodesMatchGeoJSONOverlay(t *testing.T) {
	raw, err := os.ReadFile(filepath.Clean(geojsonPath))
	if err != nil {
		t.Fatalf("reading overlay: %v", err)
	}

	var fc struct {
		Features []struct {
			Properties struct {
				Code    string `json:"code"`
				Country string `json:"country"`
				Name    string `json:"name"`
			} `json:"properties"`
		} `json:"features"`
	}
	if err := json.Unmarshal(raw, &fc); err != nil {
		t.Fatalf("parsing overlay: %v", err)
	}

	geo := map[string]string{} // code -> name
	for _, f := range fc.Features {
		geo[f.Properties.Code] = f.Properties.Name
	}
	seed := map[string]string{}
	for _, r := range regionSeed {
		seed[r.Code] = r.Name
	}

	if len(seed) != len(regionSeed) {
		t.Fatalf("regionSeed contains duplicate codes: %d rows, %d unique", len(regionSeed), len(seed))
	}

	for _, code := range sortedKeys(seed) {
		if _, ok := geo[code]; !ok {
			t.Errorf("region %q is seeded but has no polygon in the overlay", code)
		}
	}
	for _, code := range sortedKeys(geo) {
		if _, ok := seed[code]; !ok {
			t.Errorf("overlay carries polygon %q with no seeded region", code)
		}
	}

	// Names must agree too, not just codes. The two artifacts were authored
	// separately and both carry the deliberate spellings (ǁKaras with
	// U+01C1, Zambezi not Caprivi, Northern Cape not the source's typo) —
	// comparing them is what keeps those corrections from regressing on one
	// side only.
	for _, code := range sortedKeys(seed) {
		if g, ok := geo[code]; ok && g != seed[code] {
			t.Errorf("region %q: seed name %q != overlay name %q", code, seed[code], g)
		}
	}
}

// TestSeedRegionCodesAgreeWithTheirCountry is a consistency check on the
// seed table above, not a business rule: every code in it happens to be ISO
// 3166-2, so a mistyped Country column contradicts its own Code and would
// otherwise surface only as a confusing 422 partway through a live run.
// Nothing stops a future non-ISO region code — this test would need
// relaxing then, and that is the correct place for the trade-off to sit
// rather than in the domain layer.
func TestSeedRegionCodesAgreeWithTheirCountry(t *testing.T) {
	for _, r := range regionSeed {
		if len(r.Code) < 3 || r.Code[:2] != r.Country || r.Code[2] != '-' {
			t.Errorf("region %q declares country %q — code prefix and country disagree", r.Code, r.Country)
		}
	}
}

// TestNamibiaShipsThirteenRegions pins the deliberate Kavango decision. If
// someone later sources the post-2013 split geometry and adds NA-KE/NA-KW,
// this test failing is the prompt to update the seed comment and the
// architecture doc together rather than silently changing the corpus.
func TestNamibiaShipsThirteenRegions(t *testing.T) {
	counts := map[string]int{}
	for _, r := range regionSeed {
		counts[r.Country]++
	}
	for country, want := range map[string]int{"ZA": 9, "BW": 10, "NA": 13} {
		if counts[country] != want {
			t.Errorf("country %s: got %d regions, want %d", country, counts[country], want)
		}
	}
	for _, r := range regionSeed {
		if r.Code == "NA-KE" || r.Code == "NA-KW" {
			t.Errorf("NA-KE/NA-KW present: the Kavango split geometry was unobtainable when this seed "+
				"was authored (got %q). If it is now available, update the overlay, the seed comment, "+
				"and ARCHITECTURE-ORGANIZATIONS.md § \"Operating Areas — region seed\" together.", r.Code)
		}
	}
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
