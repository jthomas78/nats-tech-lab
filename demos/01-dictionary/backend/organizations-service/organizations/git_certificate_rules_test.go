package organizations_test

// BR-TP64-BR-TP72 (Phase 39a) — the GIT certificate rules, one Context per
// rule, as the rule text itself requires.
//
// Where a rule is already fully covered elsewhere, this file does not repeat
// it and says where it lives instead: BR-TP64's cardinality guard, BR-TP66's
// approval-time requirements and BR-TP67's approval-side expiry check are in
// compliance_document_test.go, and BR-TP68's FOR_REVIEW -> Pending derivation
// is in git_status_test.go. What is here is everything those leave uncovered
// — chiefly the write-side rules, which only exist once the aggregate, the
// command and the vocabulary port are wired together.

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/internal/application/commands"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/internal/domain"
	profiledomain "github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/transporterprofile/domain"
)

// recordingGoodsTypes is BR-TP64's fake refdata vocabulary (decision 26 —
// 39a's specs run against a fake; 39b supplies the live corpus). It records
// the tenant and context each lookup was performed as, because "in the
// certificate's own context" is half of what the rule says.
type recordingGoodsTypes struct {
	mu       sync.Mutex
	known    map[string]bool
	tenants  []string
	contexts []string
	codes    []string
}

func newRecordingGoodsTypes(known ...string) *recordingGoodsTypes {
	set := map[string]bool{}
	for _, code := range known {
		set[code] = true
	}
	return &recordingGoodsTypes{known: set}
}

func (g *recordingGoodsTypes) GoodsTypeExists(_ context.Context, tenant, contextKey, code string) (bool, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.tenants = append(g.tenants, tenant)
	g.contexts = append(g.contexts, contextKey)
	g.codes = append(g.codes, code)
	return g.known[code], nil
}

// recordingAppender wraps fakeCertificateAppender and keeps every event the
// aggregate produced, which is what BR-TP69/BR-TP71/BR-TP72 are actually about
// — the returned State says what survived, not what was written to the log.
type recordingAppender struct {
	*fakeCertificateAppender
	mu     sync.Mutex
	events []profiledomain.Event
}

func newRecordingAppender(docs *fakeDocRepo) *recordingAppender {
	return &recordingAppender{fakeCertificateAppender: newFakeCertificateAppender(docs)}
}

func (r *recordingAppender) CertificateCommands(string) (commands.CertificateAppender, error) {
	return r, nil
}

// capture re-derives the events the underlying fake appended, by asking the
// aggregate for them through the same commands. Rather than duplicate that,
// each wrapper below records the events it built.
func (r *recordingAppender) record(events ...profiledomain.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, events...)
}

func (r *recordingAppender) recorded() []profiledomain.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]profiledomain.Event(nil), r.events...)
}

func (r *recordingAppender) RegisterCertificate(ctx context.Context, contextKey, organizationID string, doc domain.ComplianceDocument, actorName, sourceIP string) (profiledomain.State, error) {
	r.replay(contextKey, organizationID, func(agg *profiledomain.TransporterProfile) ([]profiledomain.Event, error) {
		event, err := agg.RegisterCertificate(doc, actorName, sourceIP)
		return []profiledomain.Event{event}, err
	})
	return r.fakeCertificateAppender.RegisterCertificate(ctx, contextKey, organizationID, doc, actorName, sourceIP)
}

func (r *recordingAppender) ApproveCertificate(ctx context.Context, contextKey, organizationID, documentID, insurerName, actorName, sourceIP string) (profiledomain.State, error) {
	r.replay(contextKey, organizationID, func(agg *profiledomain.TransporterProfile) ([]profiledomain.Event, error) {
		return agg.ApproveCertificate(documentID, insurerName, actorName, sourceIP)
	})
	return r.fakeCertificateAppender.ApproveCertificate(ctx, contextKey, organizationID, documentID, insurerName, actorName, sourceIP)
}

func (r *recordingAppender) RejectCertificate(ctx context.Context, contextKey, organizationID, documentID, actorName, sourceIP string) (profiledomain.State, error) {
	r.replay(contextKey, organizationID, func(agg *profiledomain.TransporterProfile) ([]profiledomain.Event, error) {
		event, err := agg.RejectCertificate(documentID, actorName, sourceIP)
		return []profiledomain.Event{event}, err
	})
	return r.fakeCertificateAppender.RejectCertificate(ctx, contextKey, organizationID, documentID, actorName, sourceIP)
}

func (r *recordingAppender) AttachCertificateFile(ctx context.Context, contextKey, organizationID, documentID string, file domain.DocumentFile, actorName, sourceIP string) (profiledomain.State, error) {
	r.replay(contextKey, organizationID, func(agg *profiledomain.TransporterProfile) ([]profiledomain.Event, error) {
		event, err := agg.AttachCertificateFile(documentID, file, actorName, sourceIP)
		return []profiledomain.Event{event}, err
	})
	return r.fakeCertificateAppender.AttachCertificateFile(ctx, contextKey, organizationID, documentID, file, actorName, sourceIP)
}

func (r *recordingAppender) SetCertificateExpiry(ctx context.Context, contextKey, organizationID, documentID string, expiresAt *int64, now time.Time, actorName, sourceIP string) (profiledomain.State, error) {
	r.replay(contextKey, organizationID, func(agg *profiledomain.TransporterProfile) ([]profiledomain.Event, error) {
		event, err := agg.SetCertificateExpiry(documentID, expiresAt, now, actorName, sourceIP)
		return []profiledomain.Event{event}, err
	})
	return r.fakeCertificateAppender.SetCertificateExpiry(ctx, contextKey, organizationID, documentID, expiresAt, now, actorName, sourceIP)
}

// replay runs the command against a *copy* of the profile's history so the
// events can be inspected without applying them twice.
func (r *recordingAppender) replay(contextKey, organizationID string, fn func(*profiledomain.TransporterProfile) ([]profiledomain.Event, error)) {
	r.fakeCertificateAppender.mu.Lock()
	agg := r.fakeCertificateAppender.aggregate(contextKey, organizationID)
	r.fakeCertificateAppender.mu.Unlock()
	events, err := fn(agg)
	if err != nil {
		return
	}
	r.record(events...)
}

var _ = Describe("GIT certificates (BR-TP64-BR-TP72)", func() {
	const (
		tenant     = "acme"
		contextKey = "acme-northdiv"
		actorName  = "reviewer@acme"
		sourceIP   = "nats:admin-ui"
	)

	var (
		partners  *fakePartnerRepo
		docs      *fakeDocRepo
		appender  *recordingAppender
		vocab     *recordingGoodsTypes
		handler   *commands.ComplianceDocumentHandler
		partnerID string
		actor     commands.Actor
	)

	BeforeEach(func() {
		partners = newFakePartnerRepo()
		docs = newFakeDocRepo()
		appender = newRecordingAppender(docs)
		vocab = newRecordingGoodsTypes("FOOD", "CHEMICALS")
		handler = commands.NewComplianceDocumentHandler(partners, docs).
			WithGoodsTypeValidator(vocab).
			WithCertificateAppender(appender)
		actor = commands.Actor{Name: actorName, SourceIP: sourceIP}

		tp, err := partners.Register(context.Background(), domain.Organization{
			Name: "Acme Hauliers", Type: domain.PartnerTypeTransporter,
			Context: contextKey, Status: domain.StatusRegistered,
		})
		Expect(err).NotTo(HaveOccurred())
		partnerID = tp.ID
	})

	register := func(goodsTypes []string, coverageCents *int64, expiresAt *int64) (domain.ComplianceDocument, error) {
		return handler.AddGitDocument(context.Background(), tenant, contextKey, partnerID,
			"s3://docs/git.pdf", expiresAt, coverageCents, goodsTypes, actor)
	}

	approve := func(documentID string) (domain.ComplianceDocument, error) {
		return handler.ApproveGitDocument(context.Background(), tenant, contextKey, partnerID,
			documentID, "Acme Insurance", "Jane Reviewer", "+27 11 555 0000", actor)
	}

	Context("BR-TP64: goods types must exist in the vocabulary for this context", func() {
		It("looks each code up in the certificate's own tenant and context", func() {
			_, err := register([]string{"FOOD", "CHEMICALS"}, nil, nil)
			Expect(err).NotTo(HaveOccurred())

			Expect(vocab.codes).To(ConsistOf("FOOD", "CHEMICALS"))
			Expect(vocab.tenants).To(ConsistOf(tenant, tenant))
			Expect(vocab.contexts).To(ConsistOf(contextKey, contextKey),
				"BR-TP14's pattern: the vocabulary is tenant- and context-scoped, so a code valid elsewhere is not valid here")
		})

		It("rejects a code absent from the vocabulary, and appends nothing", func() {
			_, err := register([]string{"FOOD", "PLUTONIUM"}, nil, nil)
			Expect(errors.Is(err, domain.ErrGoodsTypeNotFound)).To(BeTrue())

			// "rejected, not stored and reconciled later" — the certificate
			// must not have reached the log at all.
			Expect(appender.recorded()).To(BeEmpty())
		})

		It("refuses to register at all when no validator is configured", func() {
			bare := commands.NewComplianceDocumentHandler(partners, docs).
				WithCertificateAppender(appender)
			_, err := bare.AddGitDocument(context.Background(), tenant, contextKey, partnerID,
				"s3://docs/git.pdf", nil, nil, []string{"FOOD"}, actor)
			Expect(err).To(HaveOccurred(),
				"an unconfigured vocabulary must fail closed — accepting every code would silently disable the rule")
		})
	})

	Context("BR-TP65: cover for a goods type is the maximum across approved, unexpired certificates", func() {
		at := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
		future := at.Add(24 * time.Hour).Unix()
		past := at.Add(-24 * time.Hour).Unix()

		cert := func(status domain.DocumentStatus, cents int64, expiresAt *int64, goodsTypes ...string) domain.ComplianceDocument {
			return domain.ComplianceDocument{
				Type: domain.DocumentTypeGoodsInTransit, Status: status,
				CoverageCents: &cents, ExpiresAt: expiresAt, GoodsTypes: goodsTypes,
			}
		}

		It("takes the maximum, not the latest, across certificates covering the same type", func() {
			Expect(domain.CoverByGoodsType([]domain.ComplianceDocument{
				cert(domain.DocumentStatusApproved, 500_000, &future, "FOOD"),
				cert(domain.DocumentStatusApproved, 250_000, &future, "FOOD"),
			}, at)).To(Equal(map[string]int64{"FOOD": 500_000}))
		})

		It("applies one certificate's amount to every goods type on it", func() {
			Expect(domain.CoverByGoodsType([]domain.ComplianceDocument{
				cert(domain.DocumentStatusApproved, 500_000, &future, "FOOD", "CHEMICALS"),
			}, at)).To(Equal(map[string]int64{"FOOD": 500_000, "CHEMICALS": 500_000}))
		})

		It("counts only approved certificates", func() {
			Expect(domain.CoverByGoodsType([]domain.ComplianceDocument{
				cert(domain.DocumentStatusForReview, 900_000, &future, "FOOD"),
				cert(domain.DocumentStatusRejected, 900_000, &future, "FOOD"),
				cert(domain.DocumentStatusSuperseded, 900_000, &future, "FOOD"),
				cert(domain.DocumentStatusApproved, 100_000, &future, "FOOD"),
			}, at)).To(Equal(map[string]int64{"FOOD": 100_000}))
		})

		It("drops an expired certificate's contribution", func() {
			Expect(domain.CoverByGoodsType([]domain.ComplianceDocument{
				cert(domain.DocumentStatusApproved, 900_000, &past, "FOOD"),
			}, at)).To(BeEmpty())
		})

		It("ignores non-GIT documents entirely", func() {
			cipc := domain.ComplianceDocument{Type: domain.DocumentTypeCIPC, Status: domain.DocumentStatusApproved, GoodsTypes: []string{"FOOD"}}
			Expect(domain.CoverByGoodsType([]domain.ComplianceDocument{cipc}, at)).To(BeEmpty())
		})
	})

	Context("BR-TP67: expiry is guarded at registration as well as at approval", func() {
		// The approval-side guard is compliance_document_test.go's; this is
		// the registration door, which the rule says is guarded separately.
		It("refuses a registration whose expiry is already past", func() {
			past := time.Now().UTC().Add(-time.Hour).Unix()
			_, err := register([]string{"FOOD"}, nil, &past)
			Expect(errors.Is(err, domain.ErrDocumentExpiryInPast)).To(BeTrue())
			Expect(appender.recorded()).To(BeEmpty())
		})
	})

	Context("BR-TP68: registration is always permitted", func() {
		It("accepts a renewal while cover is live and approved, and moves no cover", func() {
			first, err := register([]string{"FOOD"}, nil, nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(first.Status).To(Equal(domain.DocumentStatusPending), "a registration with no file yet is PENDING (BR-TP68)")
			_, err = approve(first.ID)
			Expect(err).NotTo(HaveOccurred())

			second, err := register([]string{"FOOD"}, nil, nil)
			Expect(err).NotTo(HaveOccurred(), "early renewal is legal in every state (BR-TP68)")

			stored, err := docs.GetDocument(context.Background(), partnerID, first.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(stored.Status).To(Equal(domain.DocumentStatusApproved),
				"registering a renewal must not retire live cover — only approval supersedes (BR-TP69)")
			Expect(second.ID).NotTo(Equal(first.ID))
		})
	})

	Context("BR-TP69: approval is the only thing that locks", func() {
		It("records a GIT rejection on the aggregate so replay preserves the verdict", func() {
			doc, err := register([]string{"FOOD"}, nil, nil)
			Expect(err).NotTo(HaveOccurred())

			rejected, err := handler.RejectGitDocument(context.Background(), tenant, contextKey, partnerID, doc.ID, actor)
			Expect(err).NotTo(HaveOccurred())
			Expect(rejected.Status).To(Equal(domain.DocumentStatusRejected))

			stored, err := docs.GetDocument(context.Background(), partnerID, doc.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(stored.Status).To(Equal(domain.DocumentStatusRejected))
			events := appender.recorded()
			Expect(events).NotTo(BeEmpty())
			Expect(events[len(events)-1].Type).To(Equal(profiledomain.DocumentRejectedEvent))
			Expect(events[len(events)-1].Certificate.Status).To(Equal(domain.DocumentStatusRejected))
		})

		It("supersedes every earlier certificate when a later one is approved", func() {
			first, err := register([]string{"FOOD"}, nil, nil)
			Expect(err).NotTo(HaveOccurred())
			second, err := register([]string{"FOOD"}, nil, nil)
			Expect(err).NotTo(HaveOccurred())

			_, err = approve(second.ID)
			Expect(err).NotTo(HaveOccurred())

			stored, err := docs.GetDocument(context.Background(), partnerID, first.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(stored.Status).To(Equal(domain.DocumentStatusSuperseded))
			approved, err := docs.GetDocument(context.Background(), partnerID, second.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(approved.Status).To(Equal(domain.DocumentStatusApproved))
		})

		It("records a review cancelled by supersession as cancelled, never rejected", func() {
			profile := &profiledomain.TransporterProfile{}
			profile.Apply(profiledomain.NewCreatedEvent(contextKey, "tp-1"))

			first := domain.ComplianceDocument{ID: "first", Type: domain.DocumentTypeGoodsInTransit, Status: domain.DocumentStatusForReview, GoodsTypes: []string{"FOOD"}}
			registered, err := profile.RegisterCertificate(first, actorName, sourceIP)
			Expect(err).NotTo(HaveOccurred())
			profile.Apply(registered)
			// An open review on the earlier certificate: its approval was
			// reverted, which is the path that leaves one pending.
			profile.Apply(profiledomain.Event{
				Type: profiledomain.DocumentApprovalRevertedEvent, Context: contextKey,
				OrganizationID: "tp-1", DocumentReference: "first",
			})
			Expect(profile.State().DocumentReviews["first"]).To(Equal(profiledomain.DocumentPendingReview))

			second := domain.ComplianceDocument{ID: "second", Type: domain.DocumentTypeGoodsInTransit, Status: domain.DocumentStatusForReview, GoodsTypes: []string{"FOOD"}}
			registered, err = profile.RegisterCertificate(second, actorName, sourceIP)
			Expect(err).NotTo(HaveOccurred())
			profile.Apply(registered)

			events, err := profile.ApproveCertificate("second", "Acme Insurance", actorName, sourceIP)
			Expect(err).NotTo(HaveOccurred())
			for _, event := range events {
				profile.Apply(event)
			}

			types := make([]string, 0, len(events))
			for _, event := range events {
				types = append(types, event.Type)
			}
			Expect(types).To(Equal([]string{
				profiledomain.DocumentApprovedEvent,
				profiledomain.DocumentSupersededEvent,
				profiledomain.DocumentReviewCancelledEvent,
			}), "one approval, one supersede per earlier certificate, one cancellation per open review")

			Expect(profile.State().DocumentReviews["first"]).To(Equal(profiledomain.DocumentReviewCancelled),
				"nobody judged that document — recording it as rejected would put a rejection on a transporter's record it never earned")
			Expect(profile.State().DocumentReviews["first"]).NotTo(Equal(profiledomain.DocumentRejected))
		})

		It("emits supersessions in a stable order, so two replays agree", func() {
			profile := func() *profiledomain.TransporterProfile {
				p := &profiledomain.TransporterProfile{}
				p.Apply(profiledomain.NewCreatedEvent(contextKey, "tp-1"))
				for _, id := range []string{"c", "a", "b", "target"} {
					event, err := p.RegisterCertificate(domain.ComplianceDocument{
						ID: id, Type: domain.DocumentTypeGoodsInTransit,
						Status: domain.DocumentStatusForReview, GoodsTypes: []string{"FOOD"},
					}, actorName, sourceIP)
					Expect(err).NotTo(HaveOccurred())
					p.Apply(event)
				}
				return p
			}

			order := func(p *profiledomain.TransporterProfile) []string {
				events, err := p.ApproveCertificate("target", "Acme Insurance", actorName, sourceIP)
				Expect(err).NotTo(HaveOccurred())
				var refs []string
				for _, event := range events {
					if event.Type == profiledomain.DocumentSupersededEvent {
						refs = append(refs, event.DocumentReference)
					}
				}
				return refs
			}

			Expect(order(profile())).To(Equal([]string{"a", "b", "c"}))
			Expect(order(profile())).To(Equal([]string{"a", "b", "c"}),
				"map iteration order would give two replays of the same command two different histories")
		})
	})

	Context("BR-TP70: what a locked certificate still accepts", func() {
		var superseded *profiledomain.TransporterProfile

		BeforeEach(func() {
			superseded = &profiledomain.TransporterProfile{}
			superseded.Apply(profiledomain.NewCreatedEvent(contextKey, "tp-1"))
			for _, id := range []string{"old", "new"} {
				event, err := superseded.RegisterCertificate(domain.ComplianceDocument{
					ID: id, Type: domain.DocumentTypeGoodsInTransit,
					Status: domain.DocumentStatusForReview, GoodsTypes: []string{"FOOD"},
				}, actorName, sourceIP)
				Expect(err).NotTo(HaveOccurred())
				superseded.Apply(event)
			}
			events, err := superseded.ApproveCertificate("new", "Acme Insurance", actorName, sourceIP)
			Expect(err).NotTo(HaveOccurred())
			for _, event := range events {
				superseded.Apply(event)
			}
			Expect(superseded.State().Certificates["old"].Status).To(Equal(domain.DocumentStatusSuperseded))
		})

		It("accepts SetExpiry as a historical correction", func() {
			future := time.Now().UTC().Add(72 * time.Hour).Unix()
			event, err := superseded.SetCertificateExpiry("old", &future, time.Now().UTC(), actorName, sourceIP)
			Expect(err).NotTo(HaveOccurred())
			superseded.Apply(event)
			Expect(superseded.State().Certificates["old"].ExpiresAt).To(Equal(&future))
			Expect(superseded.State().Certificates["old"].Status).To(Equal(domain.DocumentStatusSuperseded),
				"a correction is not a revival — cover is derived from approved certificates alone")
		})

		It("accepts a review resolution, so a workflow left waiting can be driven to rest", func() {
			superseded.Apply(profiledomain.Event{
				Type: profiledomain.DocumentReviewCancelledEvent, Context: contextKey,
				OrganizationID: "tp-1", DocumentReference: "old",
			})
			Expect(superseded.State().DocumentReviews["old"]).To(Equal(profiledomain.DocumentReviewCancelled))
		})

		It("refuses every other mutating command", func() {
			_, err := superseded.ApproveCertificate("old", "Acme Insurance", actorName, sourceIP)
			Expect(errors.Is(err, domain.ErrDocumentSuperseded)).To(BeTrue())

			_, err = superseded.AttachCertificateFile("old", domain.DocumentFile{
				FileName: "git.pdf", ContentType: "application/pdf", SizeBytes: 1,
			}, actorName, sourceIP)
			Expect(errors.Is(err, domain.ErrDocumentSuperseded)).To(BeTrue())
		})

		It("does not lock the current approved certificate", func() {
			// "Locked" means superseded-by-approval, and that is the only
			// thing it means: the incumbent is still the live document.
			Expect(superseded.State().Certificates["new"].Status).To(Equal(domain.DocumentStatusApproved))
			future := time.Now().UTC().Add(72 * time.Hour).Unix()
			_, err := superseded.SetCertificateExpiry("new", &future, time.Now().UTC(), actorName, sourceIP)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("BR-TP71: every command records an actor", func() {
		It("threads the actor through registration, file attachment, expiry and approval", func() {
			doc, err := register([]string{"FOOD"}, nil, nil)
			Expect(err).NotTo(HaveOccurred())

			_, err = appender.AttachCertificateFile(context.Background(), contextKey, partnerID, doc.ID,
				domain.DocumentFile{FileName: "git.pdf", ContentType: "application/pdf", SizeBytes: 1, ObjectName: "obj"},
				actor.Name, actor.SourceIP)
			Expect(err).NotTo(HaveOccurred())

			future := time.Now().UTC().Add(72 * time.Hour).Unix()
			_, err = handler.SetGitDocumentExpiry(context.Background(), tenant, contextKey, partnerID, doc.ID, &future, actor)
			Expect(err).NotTo(HaveOccurred())

			_, err = approve(doc.ID)
			Expect(err).NotTo(HaveOccurred())

			recorded := appender.recorded()
			Expect(recorded).NotTo(BeEmpty())
			for _, event := range recorded {
				Expect(event.ActorName).To(Equal(actorName), "event %q carries no actor", event.Type)
				Expect(event.ActorSourceIP).To(Equal(sourceIP), "event %q carries no source", event.Type)
			}
		})

		It("stamps the same actor on the supersessions and cancellations an approval causes", func() {
			// These are events nobody issued a command for, and they are the
			// ones most likely to be left anonymous.
			profile := &profiledomain.TransporterProfile{}
			profile.Apply(profiledomain.NewCreatedEvent(contextKey, "tp-1"))
			for _, id := range []string{"old", "new"} {
				event, err := profile.RegisterCertificate(domain.ComplianceDocument{
					ID: id, Type: domain.DocumentTypeGoodsInTransit,
					Status: domain.DocumentStatusForReview, GoodsTypes: []string{"FOOD"},
				}, actorName, sourceIP)
				Expect(err).NotTo(HaveOccurred())
				profile.Apply(event)
			}
			profile.Apply(profiledomain.Event{
				Type: profiledomain.DocumentApprovalRevertedEvent, Context: contextKey,
				OrganizationID: "tp-1", DocumentReference: "old",
			})

			events, err := profile.ApproveCertificate("new", "Acme Insurance", "temporal-git-monitor", "")
			Expect(err).NotTo(HaveOccurred())
			Expect(events).To(HaveLen(3))
			for _, event := range events {
				Expect(event.ActorName).To(Equal("temporal-git-monitor"),
					"a system-originated transition records the mechanism, never a person and never an empty actor")
			}
		})
	})

	Context("BR-TP72: the insurance contact never reaches the stream", func() {
		It("has no field on the event-side certificate that could carry one", func() {
			// Structural, not behavioural: the guard is that neither the
			// replayed certificate nor the projection-write struct has the
			// fields at all, so no future caller can populate them by mistake.
			for _, t := range []reflect.Type{
				reflect.TypeOf(profiledomain.Certificate{}),
				reflect.TypeOf(domain.ProjectedCertificate{}),
			} {
				for i := 0; i < t.NumField(); i++ {
					Expect(strings.Contains(t.Field(i).Name, "Contact")).To(BeFalse(),
						"%s.%s would put redactable data on an immutable log", t.Name(), t.Field(i).Name)
				}
			}
		})

		It("records the two contact fields as withheld — changed, with no value", func() {
			profile := &profiledomain.TransporterProfile{}
			profile.Apply(profiledomain.NewCreatedEvent(contextKey, "tp-1"))
			event, err := profile.RegisterCertificate(domain.ComplianceDocument{
				ID: "only", Type: domain.DocumentTypeGoodsInTransit,
				Status: domain.DocumentStatusForReview, GoodsTypes: []string{"FOOD"},
			}, actorName, sourceIP)
			Expect(err).NotTo(HaveOccurred())
			profile.Apply(event)

			events, err := profile.ApproveCertificate("only", "Acme Insurance", actorName, sourceIP)
			Expect(err).NotTo(HaveOccurred())

			var withheld []string
			for _, change := range events[0].Changes {
				if change.Withheld {
					Expect(change.From).To(BeNil())
					Expect(change.To).To(BeNil())
					withheld = append(withheld, change.Field)
				}
			}
			Expect(withheld).To(ConsistOf("insuranceContactName", "insuranceContactNumber"),
				"an auditor asking 'was a contact recorded?' gets an answer, just not the number")
		})

		It("keeps the contact values off the log even when the command was given them", func() {
			doc, err := register([]string{"FOOD"}, nil, nil)
			Expect(err).NotTo(HaveOccurred())
			_, err = handler.ApproveGitDocument(context.Background(), tenant, contextKey, partnerID, doc.ID,
				"Acme Insurance", "Jane Reviewer", "+27 11 555 0000", actor)
			Expect(err).NotTo(HaveOccurred())

			raw, err := json.Marshal(appender.recorded())
			Expect(err).NotTo(HaveOccurred())
			Expect(string(raw)).NotTo(ContainSubstring("Jane Reviewer"))
			Expect(string(raw)).NotTo(ContainSubstring("+27 11 555 0000"))

			// And they are stored, in the one place decision 25 puts them.
			stored, err := docs.GetDocument(context.Background(), partnerID, doc.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(stored.InsuranceContactName).To(Equal("Jane Reviewer"))
			Expect(stored.InsuranceContactNumber).To(Equal("+27 11 555 0000"))
		})
	})
})
