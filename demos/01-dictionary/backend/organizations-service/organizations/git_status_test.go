package organizations_test

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/internal/domain"
)

var _ = Describe("Derived GIT status (BR-TP38)", func() {
	// at is a fixed evaluation instant, so these specs never depend on the
	// wall clock — expiry is evaluated against a caller-supplied "now", which
	// is also what makes the rule testable at all.
	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	past := at.Add(-24 * time.Hour).Unix()
	future := at.Add(24 * time.Hour).Unix()

	git := func(status domain.DocumentStatus, expiresAt *int64) domain.ComplianceDocument {
		return domain.ComplianceDocument{
			Type:      domain.DocumentTypeGoodsInTransit,
			Status:    status,
			ExpiresAt: expiresAt,
		}
	}

	Context("the five values", func() {
		It("is None when the partner has no GIT document at all", func() {
			Expect(domain.DeriveGitStatus(nil, at)).To(Equal(domain.GitStatusNone))
			Expect(domain.DeriveGitStatus([]domain.ComplianceDocument{}, at)).To(Equal(domain.GitStatusNone))
		})

		It("is None when the partner has documents, but none of them GIT", func() {
			docs := []domain.ComplianceDocument{
				{Type: domain.DocumentTypeCIPC, Status: domain.DocumentStatusApproved},
				{Type: domain.DocumentTypeDirectorID, Status: domain.DocumentStatusRejected},
			}
			Expect(domain.DeriveGitStatus(docs, at)).To(Equal(domain.GitStatusNone),
				"a rejected CIPC says nothing about goods-in-transit cover")
		})

		It("is Pending for a pending GIT document", func() {
			Expect(domain.DeriveGitStatus([]domain.ComplianceDocument{
				git(domain.DocumentStatusPending, nil),
			}, at)).To(Equal(domain.GitStatusPending))
		})

		It("is Pending for a FOR_REVIEW GIT document", func() {
			Expect(domain.DeriveGitStatus([]domain.ComplianceDocument{
				git(domain.DocumentStatusForReview, nil),
			}, at)).To(Equal(domain.GitStatusPending))
		})

		It("is Active for an approved, unexpired GIT document", func() {
			Expect(domain.DeriveGitStatus([]domain.ComplianceDocument{
				git(domain.DocumentStatusApproved, &future),
			}, at)).To(Equal(domain.GitStatusActive))
		})

		It("is Active for an approved GIT document with no expiry set", func() {
			Expect(domain.DeriveGitStatus([]domain.ComplianceDocument{
				git(domain.DocumentStatusApproved, nil),
			}, at)).To(Equal(domain.GitStatusActive), "no expiry recorded is not the same as expired")
		})

		It("is Rejected for a rejected GIT document", func() {
			Expect(domain.DeriveGitStatus([]domain.ComplianceDocument{
				git(domain.DocumentStatusRejected, nil),
			}, at)).To(Equal(domain.GitStatusRejected))
		})
	})

	Context("expiry is evaluated at read time, not stored", func() {
		It("reads an approved-but-expired document as Expired", func() {
			Expect(domain.DeriveGitStatus([]domain.ComplianceDocument{
				git(domain.DocumentStatusApproved, &past),
			}, at)).To(Equal(domain.GitStatusExpired))
		})

		It("reads a pending-but-expired document as Expired", func() {
			Expect(domain.DeriveGitStatus([]domain.ComplianceDocument{
				git(domain.DocumentStatusPending, &past),
			}, at)).To(Equal(domain.GitStatusExpired))
		})

		// The document's own status is untouched — BR-TP38 derives a badge, it
		// does not run an expiry job. Deriving Expired from a document that
		// still reads APPROVED is the whole point.
		It("does not mutate the document it derives from", func() {
			doc := git(domain.DocumentStatusApproved, &past)
			Expect(domain.DeriveGitStatus([]domain.ComplianceDocument{doc}, at)).To(Equal(domain.GitStatusExpired))
			Expect(doc.Status).To(Equal(domain.DocumentStatusApproved))
		})
	})

	Context("worst-of across several GIT documents", func() {
		// BR-TP29 made several documents of one type possible, so "worst-of"
		// stopped being hypothetical.
		It("prefers Rejected over every other value", func() {
			Expect(domain.DeriveGitStatus([]domain.ComplianceDocument{
				git(domain.DocumentStatusApproved, &future),
				git(domain.DocumentStatusPending, nil),
				git(domain.DocumentStatusRejected, nil),
			}, at)).To(Equal(domain.GitStatusRejected))
		})

		It("prefers Expired over Pending and Active", func() {
			Expect(domain.DeriveGitStatus([]domain.ComplianceDocument{
				git(domain.DocumentStatusApproved, &future),
				git(domain.DocumentStatusPending, nil),
				git(domain.DocumentStatusApproved, &past),
			}, at)).To(Equal(domain.GitStatusExpired))
		})

		It("prefers Pending over Active", func() {
			Expect(domain.DeriveGitStatus([]domain.ComplianceDocument{
				git(domain.DocumentStatusApproved, &future),
				git(domain.DocumentStatusPending, nil),
			}, at)).To(Equal(domain.GitStatusPending))
		})
	})

	Context("superseded documents are excluded (BR-TP31)", func() {
		It("ignores a superseded rejection in favour of the current approval", func() {
			Expect(domain.DeriveGitStatus([]domain.ComplianceDocument{
				git(domain.DocumentStatusSuperseded, nil),
				git(domain.DocumentStatusApproved, &future),
			}, at)).To(Equal(domain.GitStatusActive),
				"a retired document must not hold a partner's badge down")
		})

		It("is None when every GIT document has been superseded", func() {
			Expect(domain.DeriveGitStatus([]domain.ComplianceDocument{
				git(domain.DocumentStatusSuperseded, nil),
			}, at)).To(Equal(domain.GitStatusNone))
		})
	})
})
