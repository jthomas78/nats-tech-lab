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

// gitSeverity ranks the four document-derived values worst-first, so
// DeriveGitStatus can take a max without a chain of if/else. None is absent
// deliberately: it is not a candidate a document can produce, it is the
// answer when there are no candidates at all.
var gitSeverity = map[GitStatus]int{
	GitStatusActive:   1,
	GitStatusPending:  2,
	GitStatusExpired:  3,
	GitStatusRejected: 4,
}

// DeriveGitStatus implements BR-TP38 — the worst status across a partner's
// *current* GOODS_IN_TRANSIT documents, evaluated against now.
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
	worst := GitStatusNone
	for _, doc := range docs {
		if doc.Type != DocumentTypeGoodsInTransit || doc.Status == DocumentStatusSuperseded {
			continue
		}
		if candidate := gitStatusOf(doc, now); gitSeverity[candidate] > gitSeverity[worst] {
			worst = candidate
		}
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
	default:
		return GitStatusPending
	}
}
