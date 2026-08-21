package postgres_test

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	profiledomain "github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/transporterprofile/domain"
	profilepostgres "github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/transporterprofile/postgres"
)

var _ = Describe("TransporterProfile projection", func() {
	// The projection's status CHECK constraint and the domain's Status values
	// are two lists of the same thing, in two languages, with nothing keeping
	// them in step. This spec is that link.
	//
	// It was written after BR-TP63 added CoverLapsed and the constraint was
	// not migrated with it. The failure was silent and expensive to read: the
	// projector's Upsert was rejected, so it Nak'd, so JetStream redelivered
	// the same event forever — ack floor frozen one message behind, consumer
	// sequence into the tens of thousands, nothing in the service log. And
	// because the drop suspends the organization before the projection is
	// updated, what an operator saw was a SUSPENDED organization whose profile
	// still read Vetted with the fleet gate open.
	Context("BR-TP63: every status the domain can produce is writable", func() {
		It("accepts each Status value, so the CHECK constraint cannot drift from the domain", func() {
			db := testDB()
			projection := profilepostgres.NewProjection(db)
			ctx := context.Background()
			Expect(projection.Migrate(ctx)).To(Succeed())

			for _, status := range []profiledomain.Status{
				profiledomain.StatusAwaitingDocumentation,
				profiledomain.StatusDocumentsInReview,
				profiledomain.StatusVetted,
				profiledomain.StatusRejected,
				profiledomain.StatusCoverLapsed,
			} {
				partnerID := freshPartner(db, "TRANSPORTER")
				err := projection.Upsert(ctx, profiledomain.State{
					Context: "acme", ID: partnerID, Status: status, UpdatedAt: time.Now().UTC(),
				})
				Expect(err).NotTo(HaveOccurred(), "status %q is a value the domain produces but the projection rejects", status)

				stored, err := projection.Get(ctx, partnerID)
				Expect(err).NotTo(HaveOccurred())
				Expect(stored.Status).To(Equal(status))
			}
		})
	})
})
