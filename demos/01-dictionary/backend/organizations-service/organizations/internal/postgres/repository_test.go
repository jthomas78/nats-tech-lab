package postgres_test

import (
	"context"
	"errors"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/internal/domain"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/internal/postgres"
)

var _ = Describe("ComplianceDocumentRepository", func() {
	Context("BR-TP29/BR-TP30: AddDocument inserts and supersedes the incumbent", func() {
		It("mints a distinct ID per document rather than upserting", func() {
			db := testDB()
			repo := postgres.NewComplianceDocumentRepository(db)
			partnerID := freshPartner(db, "SHIPPER")
			ctx := context.Background()

			first, err := repo.AddDocument(ctx, partnerID, mustAdd("SHIPPER", domain.DocumentTypeCIPC, "s3://one.pdf"))
			Expect(err).NotTo(HaveOccurred())
			Expect(first.ID).NotTo(BeEmpty(), "BR-TP29: the service mints the ID and returns it")

			second, err := repo.AddDocument(ctx, partnerID, mustAdd("SHIPPER", domain.DocumentTypeCIPC, "s3://two.pdf"))
			Expect(err).NotTo(HaveOccurred())
			Expect(second.ID).NotTo(Equal(first.ID), "a second document of the same type is a new row, not an overwrite")
		})

		It("supersedes the previous document of that type, keeping it for audit", func() {
			db := testDB()
			repo := postgres.NewComplianceDocumentRepository(db)
			partnerID := freshPartner(db, "SHIPPER")
			ctx := context.Background()

			first, err := repo.AddDocument(ctx, partnerID, mustAdd("SHIPPER", domain.DocumentTypeCIPC, "s3://one.pdf"))
			Expect(err).NotTo(HaveOccurred())
			approved, err := repo.ApproveDocument(ctx, partnerID, first.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(approved.Status).To(Equal(domain.DocumentStatusApproved))

			second, err := repo.AddDocument(ctx, partnerID, mustAdd("SHIPPER", domain.DocumentTypeCIPC, "s3://two.pdf"))
			Expect(err).NotTo(HaveOccurred())

			// BR-TP31: only the current document is listed...
			current, err := repo.ListDocuments(ctx, partnerID)
			Expect(err).NotTo(HaveOccurred())
			Expect(current).To(HaveLen(1))
			Expect(current[0].ID).To(Equal(second.ID))
			Expect(current[0].Reference).To(Equal("s3://two.pdf"))

			// ...but BR-TP30 keeps the old row, superseded, rather than
			// destroying it the way BR-TP08's upsert did.
			var status string
			Expect(db.QueryRow(`
				SELECT status FROM organizations.compliance_documents WHERE id = $1`,
				first.ID).Scan(&status)).To(Succeed())
			Expect(status).To(Equal(string(domain.DocumentStatusSuperseded)))
		})

		It("rejects a review transition on a superseded document", func() {
			db := testDB()
			repo := postgres.NewComplianceDocumentRepository(db)
			partnerID := freshPartner(db, "SHIPPER")
			ctx := context.Background()

			first, err := repo.AddDocument(ctx, partnerID, mustAdd("SHIPPER", domain.DocumentTypeCIPC, "s3://one.pdf"))
			Expect(err).NotTo(HaveOccurred())
			_, err = repo.AddDocument(ctx, partnerID, mustAdd("SHIPPER", domain.DocumentTypeCIPC, "s3://two.pdf"))
			Expect(err).NotTo(HaveOccurred())

			_, err = repo.ApproveDocument(ctx, partnerID, first.ID)
			Expect(errors.Is(err, domain.ErrDocumentSuperseded)).To(BeTrue())
		})

		It("keeps documents of different types independent", func() {
			db := testDB()
			repo := postgres.NewComplianceDocumentRepository(db)
			partnerID := freshPartner(db, "TRANSPORTER")
			ctx := context.Background()

			_, err := repo.AddDocument(ctx, partnerID, mustAdd("TRANSPORTER", domain.DocumentTypeCIPC, "s3://cipc.pdf"))
			Expect(err).NotTo(HaveOccurred())
			_, err = repo.AddDocument(ctx, partnerID, mustAdd("TRANSPORTER", domain.DocumentTypeGoodsInTransit, "s3://git.pdf"))
			Expect(err).NotTo(HaveOccurred())

			// Adding a second CIPC must not supersede the GIT document.
			_, err = repo.AddDocument(ctx, partnerID, mustAdd("TRANSPORTER", domain.DocumentTypeCIPC, "s3://cipc-2.pdf"))
			Expect(err).NotTo(HaveOccurred())

			current, err := repo.ListDocuments(ctx, partnerID)
			Expect(err).NotTo(HaveOccurred())
			types := make([]domain.DocumentType, 0, len(current))
			for _, d := range current {
				types = append(types, d.Type)
			}
			Expect(types).To(ConsistOf(domain.DocumentTypeCIPC, domain.DocumentTypeGoodsInTransit))
		})
	})

	Context("BR-TP28: a document's expiry survives the round trip to Postgres", func() {
		// expires_at is TIMESTAMPTZ but ExpiresAt is *int64, so an unconverted
		// scan fails outright. That matters beyond tidiness: DeriveGitStatus
		// reads ExpiresAt to decide whether goods-in-transit cover is still
		// active, so a broken round trip takes BR-TP28's expiry-driven
		// suspension with it — which is exactly how this was found, with a
		// live monitor run failing on the scan rather than suspending.
		It("writes and reads back an expiry rather than failing to scan it", func() {
			db := testDB()
			repo := postgres.NewComplianceDocumentRepository(db)
			partnerID := freshPartner(db, "TRANSPORTER")
			ctx := context.Background()

			expiry := time.Now().Add(24 * time.Hour).Truncate(time.Second).UTC().Unix()
			doc := mustAdd("TRANSPORTER", domain.DocumentTypeGoodsInTransit, "s3://git.pdf")
			doc.ExpiresAt = &expiry

			inserted, err := repo.AddDocument(ctx, partnerID, doc)
			Expect(err).NotTo(HaveOccurred())
			Expect(inserted.ExpiresAt).NotTo(BeNil())
			Expect(*inserted.ExpiresAt).To(Equal(expiry))

			listed, err := repo.ListDocuments(ctx, partnerID)
			Expect(err).NotTo(HaveOccurred(), "an unconverted scan fails here with a driver.Value type error")
			Expect(listed).To(HaveLen(1))
			Expect(listed[0].ExpiresAt).NotTo(BeNil())
			Expect(*listed[0].ExpiresAt).To(Equal(expiry))
		})

		It("persists an expiry set after the fact through SetDocumentExpiry", func() {
			db := testDB()
			repo := postgres.NewComplianceDocumentRepository(db)
			partnerID := freshPartner(db, "TRANSPORTER")
			ctx := context.Background()

			added, err := repo.AddDocument(ctx, partnerID,
				mustAdd("TRANSPORTER", domain.DocumentTypeGoodsInTransit, "s3://git.pdf"))
			Expect(err).NotTo(HaveOccurred())
			Expect(added.ExpiresAt).To(BeNil())

			expiry := time.Now().Add(48 * time.Hour).Truncate(time.Second).UTC().Unix()
			updated, err := repo.SetDocumentExpiry(ctx, partnerID, added.ID, &expiry)
			Expect(err).NotTo(HaveOccurred())
			Expect(*updated.ExpiresAt).To(Equal(expiry))

			listed, err := repo.ListDocuments(ctx, partnerID)
			Expect(err).NotTo(HaveOccurred())
			Expect(*listed[0].ExpiresAt).To(Equal(expiry))
		})

		It("clears an expiry through SetDocumentExpiry", func() {
			db := testDB()
			repo := postgres.NewComplianceDocumentRepository(db)
			partnerID := freshPartner(db, "TRANSPORTER")
			ctx := context.Background()

			expiry := time.Now().Add(48 * time.Hour).Truncate(time.Second).UTC().Unix()
			doc := mustAdd("TRANSPORTER", domain.DocumentTypeGoodsInTransit, "s3://git.pdf")
			doc.ExpiresAt = &expiry
			added, err := repo.AddDocument(ctx, partnerID, doc)
			Expect(err).NotTo(HaveOccurred())

			cleared, err := repo.SetDocumentExpiry(ctx, partnerID, added.ID, nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(cleared.ExpiresAt).To(BeNil())
		})

		It("rejects a past expiry at the repository boundary, not only in the domain", func() {
			db := testDB()
			repo := postgres.NewComplianceDocumentRepository(db)
			partnerID := freshPartner(db, "TRANSPORTER")
			ctx := context.Background()

			added, err := repo.AddDocument(ctx, partnerID,
				mustAdd("TRANSPORTER", domain.DocumentTypeGoodsInTransit, "s3://git.pdf"))
			Expect(err).NotTo(HaveOccurred())

			past := time.Now().Add(-time.Hour).Unix()
			_, err = repo.SetDocumentExpiry(ctx, partnerID, added.ID, &past)
			Expect(errors.Is(err, domain.ErrDocumentExpiryInPast)).To(BeTrue())
		})

		It("leaves ExpiresAt nil when the column is null", func() {
			db := testDB()
			repo := postgres.NewComplianceDocumentRepository(db)
			partnerID := freshPartner(db, "TRANSPORTER")

			inserted, err := repo.AddDocument(context.Background(), partnerID,
				mustAdd("TRANSPORTER", domain.DocumentTypeGoodsInTransit, "s3://none.pdf"))
			Expect(err).NotTo(HaveOccurred())
			Expect(inserted.ExpiresAt).To(BeNil())
		})
	})
})

var _ = Describe("OrganizationRepository", func() {
	Context("BR-TP33: every successful write bumps version", func() {
		It("starts at 1 and bumps on a lifecycle transition", func() {
			db := testDB()
			repo := postgres.NewOrganizationRepository(db)
			partnerID := freshPartner(db, "SHIPPER")
			ctx := context.Background()

			loaded, err := repo.Get(ctx, partnerID)
			Expect(err).NotTo(HaveOccurred())
			Expect(loaded.Version).To(Equal(1))

			// A lifecycle transition bumps but does not check (BR-TP34).
			activated, err := repo.Activate(ctx, partnerID)
			Expect(err).NotTo(HaveOccurred())
			Expect(activated.Version).To(Equal(2))
			Expect(activated.Status).To(Equal(domain.StatusActive))
		})
	})

	Context("BR-TP34: the version predicate rejects a lost update", func() {
		It("rejects a stale writer and leaves the winner's values intact", func() {
			db := testDB()
			repo := postgres.NewOrganizationRepository(db)
			partnerID := freshPartner(db, "SHIPPER")
			ctx := context.Background()

			loaded, err := repo.Get(ctx, partnerID)
			Expect(err).NotTo(HaveOccurred())

			// Both editors hold version 1 — the think-time window.
			_, err = repo.UpdateDetails(ctx, partnerID, loaded.Version, domain.Details{
				Name: loaded.Name, CompanyName: "Set by Alice",
			})
			Expect(err).NotTo(HaveOccurred())

			_, err = repo.UpdateDetails(ctx, partnerID, loaded.Version, domain.Details{
				Name: loaded.Name, CompanyName: "Set by Bob",
			})
			Expect(errors.Is(err, domain.ErrVersionConflict)).To(BeTrue())

			after, err := repo.Get(ctx, partnerID)
			Expect(err).NotTo(HaveOccurred())
			Expect(after.CompanyName).To(Equal("Set by Alice"))
			Expect(after.Version).To(Equal(2), "the rejected write must not have bumped the version")
		})

		// The reason this suite needs a real database rather than a fake: the
		// domain guard alone cannot make the check atomic. Two goroutines both
		// holding version 1 both pass it, and only the SQL predicate decides a
		// winner. Exactly one must succeed.
		It("lets exactly one of two simultaneous writers win", func() {
			db := testDB()
			repo := postgres.NewOrganizationRepository(db)
			partnerID := freshPartner(db, "SHIPPER")
			ctx := context.Background()

			loaded, err := repo.Get(ctx, partnerID)
			Expect(err).NotTo(HaveOccurred())

			const writers = 8
			var (
				wg        sync.WaitGroup
				mu        sync.Mutex
				succeeded int
				conflicts int
				other     []error
			)
			start := make(chan struct{})
			for i := 0; i < writers; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					<-start
					_, err := repo.UpdateDetails(ctx, partnerID, loaded.Version, domain.Details{
						Name: loaded.Name, CompanyName: "concurrent",
					})
					mu.Lock()
					defer mu.Unlock()
					switch {
					case err == nil:
						succeeded++
					case errors.Is(err, domain.ErrVersionConflict):
						conflicts++
					default:
						other = append(other, err)
					}
				}()
			}
			close(start)
			wg.Wait()

			Expect(other).To(BeEmpty(), "a conflict must surface as ErrVersionConflict, not a driver error")
			Expect(succeeded).To(Equal(1), "exactly one writer may win")
			Expect(conflicts).To(Equal(writers-1), "every other writer must be told it conflicted")

			after, err := repo.Get(ctx, partnerID)
			Expect(err).NotTo(HaveOccurred())
			Expect(after.Version).To(Equal(2), "only one write landed, so version moved by exactly one")
		})
	})
})

func mustAdd(partnerType domain.PartnerType, docType domain.DocumentType, reference string) domain.ComplianceDocument {
	doc, err := domain.AddDocument(partnerType, docType, reference)
	Expect(err).NotTo(HaveOccurred())
	return doc
}
