package organizations_test

import (
	"errors"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/internal/domain"
)

var _ = Describe("ComplianceDocument Rules", func() {
	Context("BR-TP07: document type is drawn from a controlled vocabulary, restricted by partner type", func() {
		It("allows CIPC/DIRECTOR_ID/BANK_CONFIRMATION_LETTER/TERMS_AND_CONDITIONS for either partner type", func() {
			for _, docType := range []domain.DocumentType{
				domain.DocumentTypeCIPC,
				domain.DocumentTypeDirectorID,
				domain.DocumentTypeBankConfirmationLetter,
				domain.DocumentTypeTermsAndConditions,
			} {
				Expect(domain.ValidateDocumentType(domain.PartnerTypeShipper, docType)).To(Succeed())
				Expect(domain.ValidateDocumentType(domain.PartnerTypeTransporter, docType)).To(Succeed())
			}
		})

		It("allows GOODS_IN_TRANSIT only for Transporter", func() {
			Expect(domain.ValidateDocumentType(domain.PartnerTypeTransporter, domain.DocumentTypeGoodsInTransit)).To(Succeed())

			err := domain.ValidateDocumentType(domain.PartnerTypeShipper, domain.DocumentTypeGoodsInTransit)
			Expect(errors.Is(err, domain.ErrDocumentTypeNotAllowedForPartnerType)).To(BeTrue())
		})

		It("rejects an unknown document type", func() {
			err := domain.ValidateDocumentType(domain.PartnerTypeTransporter, domain.DocumentType("PASSPORT"))
			Expect(errors.Is(err, domain.ErrInvalidDocumentType)).To(BeTrue())
		})
	})

	Context("BR-TP08: AddDocument requires a reference and a type valid for the partner, always starts Pending", func() {
		It("creates a document in Pending status", func() {
			doc, err := domain.AddDocument(domain.PartnerTypeShipper, domain.DocumentTypeCIPC, "s3://docs/cipc-123.pdf")
			Expect(err).NotTo(HaveOccurred())
			Expect(doc.Type).To(Equal(domain.DocumentTypeCIPC))
			Expect(doc.Reference).To(Equal("s3://docs/cipc-123.pdf"))
			Expect(doc.Status).To(Equal(domain.DocumentStatusPending))
		})

		It("rejects an empty reference", func() {
			_, err := domain.AddDocument(domain.PartnerTypeShipper, domain.DocumentTypeCIPC, "")
			Expect(errors.Is(err, domain.ErrReferenceRequired)).To(BeTrue())
		})

		It("rejects a document type not valid for the partner's type", func() {
			_, err := domain.AddDocument(domain.PartnerTypeShipper, domain.DocumentTypeGoodsInTransit, "s3://docs/git-123.pdf")
			Expect(errors.Is(err, domain.ErrDocumentTypeNotAllowedForPartnerType)).To(BeTrue())
		})

		It("leaves CoverageCents/ExpiresAt unset and unenforced (BR-TP07-11's note: no domain-level restriction on which types may carry them)", func() {
			doc, err := domain.AddDocument(domain.PartnerTypeShipper, domain.DocumentTypeCIPC, "s3://docs/cipc-123.pdf")
			Expect(err).NotTo(HaveOccurred())
			Expect(doc.CoverageCents).To(BeNil())
			Expect(doc.ExpiresAt).To(BeNil())

			coverage := int64(500000)
			doc.CoverageCents = &coverage
			Expect(doc.CoverageCents).NotTo(BeNil(), "setting coverageCents on a non-GOODS_IN_TRANSIT document is a convention, not a domain-enforced rule")
		})
	})

	Context("BR-TP09/BR-TP10/BR-TP11: the 3x3 document-status transition legality matrix", func() {
		pending := func() domain.ComplianceDocument {
			doc, err := domain.AddDocument(domain.PartnerTypeTransporter, domain.DocumentTypeGoodsInTransit, "s3://docs/git-1.pdf")
			Expect(err).NotTo(HaveOccurred())
			return doc
		}
		approved := func() domain.ComplianceDocument {
			doc, err := pending().Approve()
			Expect(err).NotTo(HaveOccurred())
			return doc
		}
		rejected := func() domain.ComplianceDocument {
			doc, err := pending().Reject()
			Expect(err).NotTo(HaveOccurred())
			return doc
		}

		// The three legal edges.
		It("Pending -> Approved via Approve", func() {
			doc, err := pending().Approve()
			Expect(err).NotTo(HaveOccurred())
			Expect(doc.Status).To(Equal(domain.DocumentStatusApproved))
		})

		It("Pending -> Rejected via Reject", func() {
			doc, err := pending().Reject()
			Expect(err).NotTo(HaveOccurred())
			Expect(doc.Status).To(Equal(domain.DocumentStatusRejected))
		})

		It("Rejected -> Pending via Resubmit", func() {
			doc, err := rejected().Resubmit()
			Expect(err).NotTo(HaveOccurred())
			Expect(doc.Status).To(Equal(domain.DocumentStatusPending))
		})

		// The six illegal edges.
		It("rejects Approve from Approved (already approved)", func() {
			_, err := approved().Approve()
			Expect(errors.Is(err, domain.ErrDocumentNotPending)).To(BeTrue())
		})

		It("rejects Approve from Rejected (must resubmit first)", func() {
			_, err := rejected().Approve()
			Expect(errors.Is(err, domain.ErrDocumentNotPending)).To(BeTrue())
		})

		It("rejects Reject from Approved", func() {
			_, err := approved().Reject()
			Expect(errors.Is(err, domain.ErrDocumentNotPending)).To(BeTrue())
		})

		It("rejects Reject from Rejected (already rejected)", func() {
			_, err := rejected().Reject()
			Expect(errors.Is(err, domain.ErrDocumentNotPending)).To(BeTrue())
		})

		It("rejects Resubmit from Pending (nothing to resubmit)", func() {
			_, err := pending().Resubmit()
			Expect(errors.Is(err, domain.ErrDocumentNotRejected)).To(BeTrue())
		})

		It("rejects Resubmit from Approved (no un-approving in v1)", func() {
			_, err := approved().Resubmit()
			Expect(errors.Is(err, domain.ErrDocumentNotRejected)).To(BeTrue())
		})
	})

	Context("BR-TP59: a document may carry an optional expiry, which must be future-dated when set", func() {
		now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
		pending := func() domain.ComplianceDocument {
			doc, err := domain.AddDocument(domain.PartnerTypeTransporter, domain.DocumentTypeGoodsInTransit, "s3://docs/git-1.pdf")
			Expect(err).NotTo(HaveOccurred())
			return doc
		}

		It("sets a future expiry", func() {
			at := now.Add(24 * time.Hour).Unix()
			doc, err := pending().SetExpiry(&at, now)
			Expect(err).NotTo(HaveOccurred())
			Expect(doc.ExpiresAt).NotTo(BeNil())
			Expect(*doc.ExpiresAt).To(Equal(at))
		})

		// A past date on a write is a data-entry error, not a lapse. Accepting
		// it would arm BR-TP60's timer against an instant that has already
		// gone by, producing an immediate suspension that looks like a
		// business event and is actually a typo.
		It("rejects an expiry in the past", func() {
			at := now.Add(-time.Second).Unix()
			_, err := pending().SetExpiry(&at, now)
			Expect(errors.Is(err, domain.ErrDocumentExpiryInPast)).To(BeTrue())
		})

		It("rejects an expiry exactly at the current instant", func() {
			at := now.Unix()
			_, err := pending().SetExpiry(&at, now)
			Expect(errors.Is(err, domain.ErrDocumentExpiryInPast)).To(BeTrue())
		})

		// Clearing is not the same as never having had one, but it produces
		// the same state: no expiry means cover cannot lapse by time
		// (38h-ii's D5), so nil is always legal and never checked against now.
		It("clears an expiry when passed nil", func() {
			at := now.Add(24 * time.Hour).Unix()
			doc, err := pending().SetExpiry(&at, now)
			Expect(err).NotTo(HaveOccurred())

			cleared, err := doc.SetExpiry(nil, now)
			Expect(err).NotTo(HaveOccurred())
			Expect(cleared.ExpiresAt).To(BeNil())
		})

		// Deliberately permissive about Approved: a renewal is a new document
		// (BR-TP30), but correcting a mistyped date on an approved one is not
		// a review decision and must not require re-approval.
		It("allows an expiry change on an approved document", func() {
			approved, err := pending().Approve()
			Expect(err).NotTo(HaveOccurred())

			at := now.Add(72 * time.Hour).Unix()
			doc, err := approved.SetExpiry(&at, now)
			Expect(err).NotTo(HaveOccurred())
			Expect(doc.Status).To(Equal(domain.DocumentStatusApproved), "setting an expiry is not a review transition")
			Expect(*doc.ExpiresAt).To(Equal(at))
		})

		It("allows an expiry correction on a superseded document", func() {
			superseded, err := pending().Supersede()
			Expect(err).NotTo(HaveOccurred())

			at := now.Add(24 * time.Hour).Unix()
			corrected, err := superseded.SetExpiry(&at, now)
			Expect(err).NotTo(HaveOccurred())
			Expect(corrected.Status).To(Equal(domain.DocumentStatusSuperseded))
			Expect(*corrected.ExpiresAt).To(Equal(at))
		})

		It("does not mutate the receiver", func() {
			doc := pending()
			at := now.Add(24 * time.Hour).Unix()
			_, err := doc.SetExpiry(&at, now)
			Expect(err).NotTo(HaveOccurred())
			Expect(doc.ExpiresAt).To(BeNil(), "SetExpiry returns a copy, like every other transition here")
		})
	})

	Context("BR-TP30: supersession is an explicit terminal transition from any non-terminal status", func() {
		pending := func() domain.ComplianceDocument {
			doc, err := domain.AddDocument(domain.PartnerTypeTransporter, domain.DocumentTypeGoodsInTransit, "s3://docs/git-1.pdf")
			Expect(err).NotTo(HaveOccurred())
			return doc
		}

		// All three non-terminal statuses may be superseded. Approved is
		// included deliberately: BR-TP30 amends BR-TP11's "no Approved ->
		// anything" rule, because retiring the record an approval applied to
		// is not the same as un-approving the work.
		It("supersedes a Pending document", func() {
			doc, err := pending().Supersede()
			Expect(err).NotTo(HaveOccurred())
			Expect(doc.Status).To(Equal(domain.DocumentStatusSuperseded))
		})

		It("supersedes an Approved document without un-approving it", func() {
			approved, err := pending().Approve()
			Expect(err).NotTo(HaveOccurred())

			doc, err := approved.Supersede()
			Expect(err).NotTo(HaveOccurred())
			Expect(doc.Status).To(Equal(domain.DocumentStatusSuperseded))
		})

		It("supersedes a Rejected document", func() {
			rejected, err := pending().Reject()
			Expect(err).NotTo(HaveOccurred())

			doc, err := rejected.Supersede()
			Expect(err).NotTo(HaveOccurred())
			Expect(doc.Status).To(Equal(domain.DocumentStatusSuperseded))
		})

		It("rejects every transition except review resolution and SetExpiry on a superseded document", func() {
			superseded, err := pending().Supersede()
			Expect(err).NotTo(HaveOccurred())

			_, err = superseded.Approve()
			Expect(errors.Is(err, domain.ErrDocumentSuperseded)).To(BeTrue())

			_, err = superseded.Reject()
			Expect(errors.Is(err, domain.ErrDocumentSuperseded)).To(BeTrue())

			_, err = superseded.Resubmit()
			Expect(errors.Is(err, domain.ErrDocumentSuperseded)).To(BeTrue())

			_, err = superseded.Supersede()
			Expect(errors.Is(err, domain.ErrDocumentSuperseded)).To(BeTrue())
		})
	})

	Context("BR-TP64: GIT certificates require goods types", func() {
		It("rejects a GIT certificate with no goods types", func() {
			doc := domain.ComplianceDocument{Type: domain.DocumentTypeGoodsInTransit}
			Expect(errors.Is(doc.ValidateGitCertificate(), domain.ErrGoodsTypesRequired)).To(BeTrue())
		})

		It("accepts one or more goods types", func() {
			doc := domain.ComplianceDocument{Type: domain.DocumentTypeGoodsInTransit, GoodsTypes: []string{"FOOD", "CHEMICALS"}}
			Expect(doc.ValidateGitCertificate()).To(Succeed())
		})
	})

	Context("BR-TP66/BR-TP67: GIT approval requires insurance details and live cover", func() {
		It("refuses a missing insurer or contact at the domain boundary", func() {
			now := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
			doc := domain.ComplianceDocument{Type: domain.DocumentTypeGoodsInTransit, Status: domain.DocumentStatusForReview}
			_, err := doc.ApproveWithInsuranceDetails("", "Jane", "123", now)
			Expect(errors.Is(err, domain.ErrInsurerNameRequired)).To(BeTrue())
			_, err = doc.ApproveWithInsuranceDetails("Insurer", "", "123", now)
			Expect(errors.Is(err, domain.ErrInsuranceContactNameRequired)).To(BeTrue())
			_, err = doc.ApproveWithInsuranceDetails("Insurer", "Jane", "", now)
			Expect(errors.Is(err, domain.ErrInsuranceContactNumberRequired)).To(BeTrue())
		})

		It("refuses an approval after the certificate has expired", func() {
			now := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
			expired := now.Add(-time.Second).Unix()
			doc := domain.ComplianceDocument{Type: domain.DocumentTypeGoodsInTransit, Status: domain.DocumentStatusForReview, ExpiresAt: &expired}
			_, err := doc.ApproveWithInsuranceDetails("Insurer", "Jane", "123", now)
			Expect(errors.Is(err, domain.ErrDocumentExpiryInPast)).To(BeTrue())
		})

		It("approves a reviewed GIT certificate with complete, live insurance details", func() {
			now := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
			doc := domain.ComplianceDocument{Type: domain.DocumentTypeGoodsInTransit, Status: domain.DocumentStatusForReview}
			approved, err := doc.ApproveWithInsuranceDetails("Insurer", "Jane", "123", now)
			Expect(err).NotTo(HaveOccurred())
			Expect(approved.Status).To(Equal(domain.DocumentStatusApproved))
		})
	})

	Context("BR-TP68: attaching GIT bytes moves a minted row into review", func() {
		It("moves Pending to FOR_REVIEW without changing other document types", func() {
			git := domain.ComplianceDocument{Type: domain.DocumentTypeGoodsInTransit, Status: domain.DocumentStatusPending}
			attached, err := git.AttachFile(domain.DocumentFile{FileName: "git.pdf", ContentType: "application/pdf", SizeBytes: 1})
			Expect(err).NotTo(HaveOccurred())
			Expect(attached.Status).To(Equal(domain.DocumentStatusForReview))
		})
	})
})
