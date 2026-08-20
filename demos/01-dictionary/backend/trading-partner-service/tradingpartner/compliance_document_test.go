package tradingpartner_test

import (
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/trading-partner-service/tradingpartner/internal/domain"
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

		// Superseded is terminal — every transition off it is rejected,
		// including a second Supersede.
		It("rejects every transition on a superseded document", func() {
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
})
