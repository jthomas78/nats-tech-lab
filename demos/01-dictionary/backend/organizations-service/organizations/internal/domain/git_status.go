package domain

import "time"

// GitStatus is the derived goods-in-transit cover status shown on a
// Transporter (BR-TP38). Five values, matching V2's real GitStatusType enum
// — the screenshot this design was drawn from only showed four, and the
// missing one (None) is the state a freshly-registered Transporter is
// actually in, so it matters.
type GitStatus string

const (
	GitStatusNone     GitStatus = "None"
	GitStatusPending  GitStatus = "Pending"
	GitStatusActive   GitStatus = "Active"
	GitStatusExpired  GitStatus = "Expired"
	GitStatusRejected GitStatus = "Rejected"
)

// gitSeverity ranks the document-derived values worst-first, so
// DeriveGitStatus can take a max without a chain of if/else. None is absent
// deliberately: it is not a candidate a document can produce, it is the
// answer when there are no candidates at all. Active is present but never
// consulted — only an approved certificate can produce it, and DeriveGitStatus
// returns on that certificate rather than ranking it. It is listed so the
// table reads as the full enum rather than looking like an omission.
var gitSeverity = map[GitStatus]int{
	GitStatusActive:   1,
	GitStatusPending:  2,
	GitStatusExpired:  3,
	GitStatusRejected: 4,
}

// DeriveGitStatus implements BR-TP38 — the approved certificate's own status
// when the partner has one, and otherwise the worst status across its
// remaining current GOODS_IN_TRANSIT documents, evaluated against now.
//
// Three things about this are deliberate:
//
//   - It is derived on every read and never stored. There is no column and no
//     setter, so the badge cannot drift from the documents it describes, and
//     no code path can hand-set it to something the documents don't support.
//   - now is a parameter rather than a time.Now() call inside, because expiry
//     is the one input that changes without anything being written. Passing it
//     in makes the rule testable at a fixed instant instead of only "today".
//   - Expiry is read here but the document's own status is left alone. This is
//     the first rule to read ExpiresAt at all (BR-TP07-BR-TP11 stored it and
//     noted nothing consumed it). Mutating a document to EXPIRED would need a
//     scheduled job and would quietly convert a named deferred exploration
//     into shipped behaviour, so this derives a view and writes nothing.
//
// docs may contain documents of any type; non-GIT and superseded ones are
// skipped, so callers can pass a partner's whole document list.
func DeriveGitStatus(docs []ComplianceDocument, now time.Time) GitStatus {
	worst, approved := GitStatusNone, GitStatus("")
	for _, doc := range docs {
		if doc.Type != DocumentTypeGoodsInTransit || doc.Status == DocumentStatusSuperseded {
			continue
		}
		candidate := gitStatusOf(doc, now)
		// The approved certificate answers on its own, and everything else is
		// only allowed to speak when there isn't one. At most one can exist —
		// approval supersedes every earlier certificate (BR-TP69), backed by a
		// unique partial index — so this is not a "best of several", it is the
		// one document that carries cover speaking for the transporter.
		//
		// Before Phase 39 moved supersede from on-upload to on-approval there
		// was exactly one current certificate, and worst-of-all was the same
		// rule as this one. Now that renewals coexist with live cover by
		// design, worst-of-all reports a rejected or in-review renewal as the
		// transporter's cover status — wrong twice over, because the badge
		// then contradicts the certificate carrying cover, and IsGitActive
		// (the same call, feeding BR-TP28's suspension) would revoke fleet
		// availability from a transporter that is covered.
		if doc.Status == DocumentStatusApproved {
			// Written as a fold rather than an early return so the answer
			// cannot depend on slice order if that index is ever missing:
			// where two approved certificates somehow coexist, the one
			// carrying cover wins over the lapsed one.
			if approved != GitStatusActive {
				approved = candidate
			}
			continue
		}
		if gitSeverity[candidate] > gitSeverity[worst] {
			worst = candidate
		}
	}
	if approved != "" {
		return approved
	}
	return worst
}

// gitStatusOf maps one current GIT document to its contribution. Expiry is
// checked before status: an expired document tells you nothing useful about
// whether it was once approved — either way there is no cover today.
func gitStatusOf(doc ComplianceDocument, now time.Time) GitStatus {
	if doc.ExpiresAt != nil && !time.Unix(*doc.ExpiresAt, 0).After(now) {
		return GitStatusExpired
	}
	switch doc.Status {
	case DocumentStatusApproved:
		return GitStatusActive
	case DocumentStatusRejected:
		return GitStatusRejected
	case DocumentStatusForReview:
		// BR-TP68: a file in a review queue is not cover, and deliberately
		// derives to Pending rather than relying on default fall-through.
		return GitStatusPending
	case DocumentStatusPending:
		return GitStatusPending
	default:
		return GitStatusNone
	}
}

// CoverByGoodsType implements BR-TP65 — a transporter's goods-in-transit
// cover for each goods type is the *maximum* across its approved, unexpired
// certificates. A certificate's single cover amount applies to every goods
// type listed on it (the amount is certificate-scoped, not per type), so one
// certificate contributes the same figure to each of its types.
//
// Two things about this are deliberate, and both follow BR-TP65's own text:
//
//   - Nothing consumes this to *enforce* anything. There is no load
//     allocation in this backend, so there is no decision path to refuse. It
//     is reported, and the rule says so explicitly.
//   - Like DeriveGitStatus, it is derived on every read against a
//     caller-supplied now and never stored, so it cannot drift from the
//     certificates it describes.
//
// A goods type on an approved certificate with no cover amount is present in
// the result at 0 rather than absent: "covered for nothing" and "not covered"
// are different answers, and only the first has a certificate behind it.
func CoverByGoodsType(docs []ComplianceDocument, now time.Time) map[string]int64 {
	cover := map[string]int64{}
	for _, doc := range docs {
		if doc.Type != DocumentTypeGoodsInTransit || doc.Status != DocumentStatusApproved {
			continue
		}
		if doc.ExpiresAt != nil && !time.Unix(*doc.ExpiresAt, 0).After(now) {
			continue
		}
		var amount int64
		if doc.CoverageCents != nil {
			amount = *doc.CoverageCents
		}
		for _, goodsType := range doc.GoodsTypes {
			if existing, ok := cover[goodsType]; !ok || amount > existing {
				cover[goodsType] = amount
			}
		}
	}
	return cover
}
