package postgres_test

// Phase 39a's schema half: the columns, constraint and index that carry the
// GIT certificate rules into Postgres. These specs exist because the schema
// changes and the projection write are the part of 39a that no in-memory fake
// can prove — and because this suite skips silently without
// ORGANIZATIONS_TEST_DATABASE_URL, which is exactly how two of these bugs
// survived their first "green" run.

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/internal/identity"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/internal/domain"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/internal/postgres"
)

var _ = Describe("GIT certificate projection (Phase 39a schema)", func() {
	// Phase 40 (BR-TP74): the document name is unique per organization, so a
	// helper that minted a fixed one would trip that index instead of the rule
	// each spec is about. The name rides along with the ID.
	certificate := func(status domain.DocumentStatus, goodsTypes ...string) domain.ProjectedCertificate {
		id := identity.New()
		return domain.ProjectedCertificate{
			ID: id, Status: status,
			DocumentName: id + ".pdf", GoodsTypes: goodsTypes,
			InsurerName: "Acme Insurance",
		}
	}

	Context("BR-TP64: goods types survive the round trip to Postgres", func() {
		// JSONB, not TEXT[]: pgx's database/sql path hands a Postgres array
		// back as a string and cannot scan it into []string. That failure is
		// invisible on write and only appears on the read.
		It("writes and reads back several goods types", func() {
			db := testDB()
			repo := postgres.NewComplianceDocumentRepository(db)
			partnerID := freshPartner(db, "TRANSPORTER")
			ctx := context.Background()

			cert := certificate(domain.DocumentStatusForReview, "FOOD", "CHEMICALS")
			Expect(repo.UpsertCertificate(ctx, partnerID, cert)).To(Succeed())

			stored, err := repo.GetDocument(ctx, partnerID, cert.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(stored.GoodsTypes).To(Equal([]string{"FOOD", "CHEMICALS"}),
				"order is part of the value — the projection replays what the event carried")
		})

		It("defaults a non-certificate document's goods types to empty rather than null", func() {
			// The column is NOT NULL with a default, and a default only
			// applies when the column is *omitted* from the INSERT. Sending an
			// explicit nil instead broke every non-GIT insert in the service.
			db := testDB()
			repo := postgres.NewComplianceDocumentRepository(db)
			partnerID := freshPartner(db, "SHIPPER")

			doc, err := repo.AddDocument(context.Background(), partnerID,
				mustAdd("SHIPPER", domain.DocumentTypeCIPC, "s3://cipc.pdf"))
			Expect(err).NotTo(HaveOccurred())
			Expect(doc.GoodsTypes).To(BeEmpty())
		})
	})

	Context("BR-TP68: FOR_REVIEW is a storable status", func() {
		It("accepts a certificate in FOR_REVIEW", func() {
			// The CHECK constraint predates this status; a certificate that
			// cannot be stored in review is one the reviewer never sees.
			db := testDB()
			repo := postgres.NewComplianceDocumentRepository(db)
			partnerID := freshPartner(db, "TRANSPORTER")
			ctx := context.Background()

			cert := certificate(domain.DocumentStatusForReview, "FOOD")
			Expect(repo.UpsertCertificate(ctx, partnerID, cert)).To(Succeed())

			stored, err := repo.GetDocument(ctx, partnerID, cert.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(stored.Status).To(Equal(domain.DocumentStatusForReview))
		})
	})

	Context("BR-TP69: at most one approved certificate per type, enforced by the database", func() {
		It("refuses a second approved certificate of the same type for one transporter", func() {
			// The write-side replay invariant is the primary enforcement; this
			// index is the backstop, because the existing
			// compliance_documents_current_idx is non-unique and two live
			// approved rows would otherwise be a silent bug.
			db := testDB()
			repo := postgres.NewComplianceDocumentRepository(db)
			partnerID := freshPartner(db, "TRANSPORTER")
			ctx := context.Background()

			Expect(repo.UpsertCertificate(ctx, partnerID, certificate(domain.DocumentStatusApproved, "FOOD"))).To(Succeed())
			err := repo.UpsertCertificate(ctx, partnerID, certificate(domain.DocumentStatusApproved, "FOOD"))
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("compliance_documents_one_approved_idx"))
		})

		It("permits any number of superseded and in-review certificates alongside the approved one", func() {
			db := testDB()
			repo := postgres.NewComplianceDocumentRepository(db)
			partnerID := freshPartner(db, "TRANSPORTER")
			ctx := context.Background()

			Expect(repo.UpsertCertificate(ctx, partnerID, certificate(domain.DocumentStatusApproved, "FOOD"))).To(Succeed())
			Expect(repo.UpsertCertificate(ctx, partnerID, certificate(domain.DocumentStatusSuperseded, "FOOD"))).To(Succeed())
			Expect(repo.UpsertCertificate(ctx, partnerID, certificate(domain.DocumentStatusSuperseded, "FOOD"))).To(Succeed())
			Expect(repo.UpsertCertificate(ctx, partnerID, certificate(domain.DocumentStatusForReview, "FOOD"))).To(Succeed(),
				"a renewal in review must be able to sit beside live cover (BR-TP68)")
		})

		It("scopes the constraint to one transporter", func() {
			db := testDB()
			repo := postgres.NewComplianceDocumentRepository(db)
			ctx := context.Background()

			Expect(repo.UpsertCertificate(ctx, freshPartner(db, "TRANSPORTER"), certificate(domain.DocumentStatusApproved, "FOOD"))).To(Succeed())
			Expect(repo.UpsertCertificate(ctx, freshPartner(db, "TRANSPORTER"), certificate(domain.DocumentStatusApproved, "FOOD"))).To(Succeed())
		})
	})

	Context("BR-TP72/decision 25: the projection is the contact pair's only home", func() {
		It("does not blank the contact columns when a later event replays over the row", func() {
			// The values are not on the stream, so a projection write that
			// included them would overwrite the only copy that exists with
			// nothing — and a replay writes every certificate, every time.
			db := testDB()
			repo := postgres.NewComplianceDocumentRepository(db)
			partnerID := freshPartner(db, "TRANSPORTER")
			ctx := context.Background()

			cert := certificate(domain.DocumentStatusForReview, "FOOD")
			Expect(repo.UpsertCertificate(ctx, partnerID, cert)).To(Succeed())
			Expect(repo.SetInsuranceContact(ctx, partnerID, cert.ID,
				"Acme Insurance", "Jane Reviewer", "+27 11 555 0000")).To(Succeed())

			approved := cert
			approved.Status = domain.DocumentStatusApproved
			expiry := time.Now().Add(72 * time.Hour).Truncate(time.Second).UTC().Unix()
			approved.ExpiresAt = &expiry
			Expect(repo.UpsertCertificate(ctx, partnerID, approved)).To(Succeed())

			stored, err := repo.GetDocument(ctx, partnerID, cert.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(stored.Status).To(Equal(domain.DocumentStatusApproved), "the replayed fields did move")
			Expect(stored.InsuranceContactName).To(Equal("Jane Reviewer"))
			Expect(stored.InsuranceContactNumber).To(Equal("+27 11 555 0000"))
		})

		It("restores the contacts as NULL when a row is rebuilt from replay alone", func() {
			// The named exception, asserted rather than assumed: a projection
			// rebuilt from the stream has no contact values to restore, and
			// that is the accepted cost of keeping them off an immutable log.
			db := testDB()
			repo := postgres.NewComplianceDocumentRepository(db)
			partnerID := freshPartner(db, "TRANSPORTER")
			ctx := context.Background()

			cert := certificate(domain.DocumentStatusApproved, "FOOD")
			Expect(repo.UpsertCertificate(ctx, partnerID, cert)).To(Succeed())

			stored, err := repo.GetDocument(ctx, partnerID, cert.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(stored.InsurerName).To(Equal("Acme Insurance"), "the insurer name IS on the stream")
			Expect(stored.InsuranceContactName).To(BeEmpty())
			Expect(stored.InsuranceContactNumber).To(BeEmpty())
		})
	})

	Context("BR-TP66/BR-TP72: the contact write refuses to lose the only copy", func() {
		It("reports a missing row instead of updating nothing", func() {
			// SetInsuranceContact is an UPDATE, and the two columns it writes
			// are never on the stream — so if the projection row has not landed
			// yet, a silent no-op discards the contacts BR-TP66 required in
			// order to approve, permanently and with no error anywhere. Found
			// during the 39b verification pass, where a certificate could be
			// approved in the window before its own registration projected.
			db := testDB()
			repo := postgres.NewComplianceDocumentRepository(db)
			partnerID := freshPartner(db, "TRANSPORTER")

			err := repo.SetInsuranceContact(context.Background(), partnerID, identity.New(),
				"Acme Insurance", "Jane Reviewer", "+27 11 555 0000")
			Expect(err).To(MatchError(domain.ErrDocumentNotFound))
		})

		It("still writes them when the row is there", func() {
			db := testDB()
			repo := postgres.NewComplianceDocumentRepository(db)
			partnerID := freshPartner(db, "TRANSPORTER")
			ctx := context.Background()

			cert := certificate(domain.DocumentStatusForReview, "FOOD")
			Expect(repo.UpsertCertificate(ctx, partnerID, cert)).To(Succeed())
			Expect(repo.SetInsuranceContact(ctx, partnerID, cert.ID,
				"Acme Insurance", "Jane Reviewer", "+27 11 555 0000")).To(Succeed())

			stored, err := repo.GetDocument(ctx, partnerID, cert.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(stored.InsuranceContactName).To(Equal("Jane Reviewer"))
		})
	})
})
