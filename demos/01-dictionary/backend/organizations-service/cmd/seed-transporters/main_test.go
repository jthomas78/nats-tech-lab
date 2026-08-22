package main

import "testing"

// The ladder is the seeder's whole contract: rung N is a known state, and a
// reader should be able to tell what a seeded row represents from its name.
// These guard the two properties that are easy to break by editing the table
// and hard to notice afterwards.
func TestLadderIsCumulative(t *testing.T) {
	// Each rung does everything the previous one did. A gap would make a
	// higher rung a *different* shape rather than a more complete one, which
	// quietly breaks "rung N is rung N-1 plus one step".
	for i := 1; i < len(ladder); i++ {
		prev, cur := ladder[i-1], ladder[i]
		for _, f := range []struct {
			name      string
			prev, cur bool
		}{
			{"companyInfo", prev.companyInfo, cur.companyInfo},
			{"operatingArea", prev.operatingArea, cur.operatingArea},
			{"fleetAsset", prev.fleetAsset, cur.fleetAsset},
			{"document", prev.document, cur.document},
			{"trackingCreds", prev.trackingCreds, cur.trackingCreds},
		} {
			if f.prev && !f.cur {
				t.Errorf("rung %d (%s) drops %s, which rung %d (%s) had; the ladder must only add",
					i+1, cur.label, f.name, i, prev.label)
			}
		}
	}
}

func TestLadderRungsAreDistinctlyLabelled(t *testing.T) {
	seen := map[string]bool{}
	for i, r := range ladder {
		if r.label == "" {
			t.Errorf("rung %d has no label; the label is what makes a seeded row's stage readable", i+1)
		}
		if seen[r.label] {
			t.Errorf("rung %d reuses the label %q", i+1, r.label)
		}
		seen[r.label] = true
	}
}

// A rung that reviews a document without one to review would submit for
// vetting with nothing pending and be refused by BR-TP56.
func TestReviewingRungsAlwaysCarryADocument(t *testing.T) {
	for i, r := range ladder {
		if r.review != "" && !r.document {
			t.Errorf("rung %d (%s) reviews %q but adds no document", i+1, r.label, r.review)
		}
		switch r.review {
		case "", "approve", "reject":
		default:
			t.Errorf("rung %d (%s) has unknown review %q", i+1, r.label, r.review)
		}
	}
}

// Suspend is only legal from Active (BR-TP04), which is only reachable through
// a Vetted profile (BR-TP19) — so a suspending rung must both activate and
// have been approved.
func TestSuspendingRungsCanReachActiveFirst(t *testing.T) {
	for i, r := range ladder {
		if !r.suspend {
			continue
		}
		if !r.activate {
			t.Errorf("rung %d (%s) suspends without activating; BR-TP04 refuses that", i+1, r.label)
		}
		if r.review != "approve" {
			t.Errorf("rung %d (%s) activates without a vetted profile; BR-TP19 refuses that", i+1, r.label)
		}
	}
}

func TestSeededGoodsTypesArePresentAndUnique(t *testing.T) {
	seen := map[string]bool{}
	for i, code := range goodsTypes {
		if code == "" {
			t.Errorf("goods type %d is empty", i+1)
		}
		if seen[code] {
			t.Errorf("goods type %d duplicates %q", i+1, code)
		}
		seen[code] = true
	}
}
