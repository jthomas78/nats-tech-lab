package main

import (
	"regexp"
	"testing"
)

func TestGoodsTypeSeedIntegrity(t *testing.T) {
	seen := map[string]bool{}
	codePattern := regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)
	for i, row := range goodsTypeSeed {
		if row.Code == "" || row.Label == "" || row.Description == "" {
			t.Errorf("row %d must have a code, label and description: %+v", i+1, row)
		}
		if seen[row.Code] {
			t.Errorf("row %d duplicates code %q", i+1, row.Code)
		}
		if !codePattern.MatchString(row.Code) {
			t.Errorf("row %d code %q is not SCREAMING_SNAKE", i+1, row.Code)
		}
		seen[row.Code] = true
	}
	if len(goodsTypeSeed) != 10 {
		t.Errorf("representative corpus has %d items, want 10", len(goodsTypeSeed))
	}
}
