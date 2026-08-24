package organizations_test

// Wire-level round-trip specs for the api.* adapter (Phase 26h). Separate from
// browserrpc_test.go, which only asserts *registration* ($SRV discovery and
// subject shape) — that a subject is advertised says nothing about whether a
// request over it does the right thing.
//
// These exist for two claims in particular, both of which are the reason the
// api.* path is not merely a translation of the REST path:
//
//  1. {context} is taken from the subject, never from the request body, so a
//     client cannot reach another context by lying in JSON.
//  2. The BR-TP14 tenant is taken from the adapter's own connection, never from
//     the body — the thing REST cannot do, and the reason REST's
//     fleetAssetRequest has a `tenant` field this transport deliberately omits.
//
// Repositories are in-memory fakes: the point here is the transport boundary,
// and the domain rules behind it already have their own 37 specs.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/nats-io/nats.go"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/internal/application/commands"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/internal/browserrpc"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/internal/domain"
	profiledomain "github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/transporterprofile/domain"
)

// --- in-memory fakes ---

type fakePartnerRepo struct {
	mu     sync.Mutex
	nextID int
	items  map[string]domain.Organization
}

func newFakePartnerRepo() *fakePartnerRepo {
	return &fakePartnerRepo{items: map[string]domain.Organization{}}
}

func (f *fakePartnerRepo) Register(_ context.Context, tp domain.Organization) (domain.Organization, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	tp.ID = fmt.Sprintf("tp-%d", f.nextID)
	// BR-TP33: rows start at version 1, mirroring the column default.
	tp.Version = 1
	f.items[tp.ID] = tp
	return tp, nil
}

func (f *fakePartnerRepo) Get(_ context.Context, id string) (domain.Organization, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	tp, ok := f.items[id]
	if !ok {
		return domain.Organization{}, domain.ErrOrganizationNotFound
	}
	return tp, nil
}

func (f *fakePartnerRepo) List(_ context.Context, contextKey string) ([]domain.Organization, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []domain.Organization
	for _, tp := range f.items {
		if tp.Context == contextKey {
			out = append(out, tp)
		}
	}
	return out, nil
}

// transition mirrors what the Postgres repository does: load, run the domain
// transition (value receiver, returns the next state), store the result.
func (f *fakePartnerRepo) transition(
	id string, fn func(domain.Organization) (domain.Organization, error),
) (domain.Organization, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	tp, ok := f.items[id]
	if !ok {
		return domain.Organization{}, domain.ErrOrganizationNotFound
	}
	next, err := fn(tp)
	if err != nil {
		return domain.Organization{}, err
	}
	// BR-TP33: every successful write bumps version, lifecycle transitions
	// included — mirroring the repository's `version = version + 1`.
	next.Version = tp.Version + 1
	f.items[id] = next
	return next, nil
}

func (f *fakePartnerRepo) Activate(_ context.Context, id string) (domain.Organization, error) {
	return f.transition(id, func(tp domain.Organization) (domain.Organization, error) { return tp.Activate() })
}

func (f *fakePartnerRepo) Suspend(_ context.Context, id, reason string) (domain.Organization, error) {
	return f.transition(id, func(tp domain.Organization) (domain.Organization, error) { return tp.Suspend(reason) })
}

func (f *fakePartnerRepo) Reactivate(_ context.Context, id string) (domain.Organization, error) {
	return f.transition(id, func(tp domain.Organization) (domain.Organization, error) { return tp.Reactivate() })
}

// UpdateDetails mirrors the Postgres repository's BR-TP34 path: the domain
// method applies the guard and sets the next version, so this must not bump
// version a second time on top of it.
func (f *fakePartnerRepo) UpdateDetails(_ context.Context, id string, expectedVersion int, details domain.Details) (domain.Organization, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	tp, ok := f.items[id]
	if !ok {
		return domain.Organization{}, domain.ErrOrganizationNotFound
	}
	next, err := tp.UpdateDetails(expectedVersion, details)
	if err != nil {
		return domain.Organization{}, err
	}
	f.items[id] = next
	return next, nil
}

// fakeDocRepo mirrors the Postgres ComplianceDocumentRepository's semantics:
// AddDocument supersedes the incumbent of that type and inserts a new row with
// a minted ID (BR-TP29/BR-TP30), ListDocuments returns current rows only
// (BR-TP31), and the transitions address a document by ID.
//
// It exists because the roundtrip harness previously passed a nil document
// repository — so no document endpoint had ever been exercised over api.* at
// all, and BR-TP38's derived GIT status has nothing to derive from without it.
type fakeDocRepo struct {
	mu     sync.Mutex
	nextID int
	byID   map[string]map[string]domain.ComplianceDocument // partnerID -> docID -> doc
}

func newFakeDocRepo() *fakeDocRepo {
	return &fakeDocRepo{byID: map[string]map[string]domain.ComplianceDocument{}}
}

func (f *fakeDocRepo) AddDocument(_ context.Context, partnerID string, doc domain.ComplianceDocument) (domain.ComplianceDocument, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.byID[partnerID] == nil {
		f.byID[partnerID] = map[string]domain.ComplianceDocument{}
	}
	// BR-TP30: supersede the current document of this type, whatever its status.
	for id, existing := range f.byID[partnerID] {
		if existing.Type == doc.Type && existing.Status != domain.DocumentStatusSuperseded {
			superseded, err := existing.Supersede()
			if err != nil {
				return domain.ComplianceDocument{}, err
			}
			f.byID[partnerID][id] = superseded
		}
	}
	f.nextID++
	doc.ID = fmt.Sprintf("doc-%d", f.nextID)
	f.byID[partnerID][doc.ID] = doc
	return doc, nil
}

// DocumentNameExists is BR-TP74's pre-check (Phase 40) — every status counts,
// superseded rows included, matching the unique index in Postgres.
func (f *fakeDocRepo) DocumentNameExists(_ context.Context, partnerID, documentName string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, doc := range f.byID[partnerID] {
		if doc.DocumentName == documentName {
			return true, nil
		}
	}
	return false, nil
}

func (f *fakeDocRepo) ListDocuments(_ context.Context, partnerID string) ([]domain.ComplianceDocument, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []domain.ComplianceDocument{}
	for _, doc := range f.byID[partnerID] {
		if doc.Status != domain.DocumentStatusSuperseded {
			out = append(out, doc)
		}
	}
	return out, nil
}

func (f *fakeDocRepo) ListGitCertificates(_ context.Context, partnerID string) ([]domain.ComplianceDocument, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []domain.ComplianceDocument{}
	for _, doc := range f.byID[partnerID] {
		if doc.Type == domain.DocumentTypeGoodsInTransit {
			out = append(out, doc)
		}
	}
	return out, nil
}

func (f *fakeDocRepo) transition(
	partnerID, documentID string, fn func(domain.ComplianceDocument) (domain.ComplianceDocument, error),
) (domain.ComplianceDocument, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	doc, ok := f.byID[partnerID][documentID]
	if !ok {
		return domain.ComplianceDocument{}, domain.ErrDocumentNotFound
	}
	next, err := fn(doc)
	if err != nil {
		return domain.ComplianceDocument{}, err
	}
	f.byID[partnerID][documentID] = next
	return next, nil
}

// SetDocumentExpiry mirrors the Postgres adapter: the domain guard runs
// against the stored document, not against the caller's copy (BR-TP59).
func (f *fakeDocRepo) SetDocumentExpiry(_ context.Context, partnerID, documentID string, expiresAt *int64) (domain.ComplianceDocument, error) {
	return f.transition(partnerID, documentID, func(doc domain.ComplianceDocument) (domain.ComplianceDocument, error) {
		return doc.SetExpiry(expiresAt, time.Now().UTC())
	})
}

func (f *fakeDocRepo) ApproveDocument(_ context.Context, partnerID, documentID string) (domain.ComplianceDocument, error) {
	return f.transition(partnerID, documentID, domain.ComplianceDocument.Approve)
}

func (f *fakeDocRepo) RejectDocument(_ context.Context, partnerID, documentID string) (domain.ComplianceDocument, error) {
	return f.transition(partnerID, documentID, domain.ComplianceDocument.Reject)
}

// GetDocument and AttachFile arrived with the port in Phase 38c-ii. Unlike
// ListDocuments, GetDocument returns superseded rows too — BR-TP43 keeps their
// bytes retrievable.
func (f *fakeDocRepo) GetDocument(_ context.Context, partnerID, documentID string) (domain.ComplianceDocument, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	doc, ok := f.byID[partnerID][documentID]
	if !ok {
		return domain.ComplianceDocument{}, domain.ErrDocumentNotFound
	}
	return doc, nil
}

// SetInsuranceContact and UpsertCertificate mirror the Postgres adapter's
// division of labour (decision 25): the first writes the two columns that are
// never on the stream, the second writes everything that is — and pointedly
// leaves those two alone, which is what the roundtrip specs rely on to prove
// a projection write cannot blank them.
func (f *fakeDocRepo) SetInsuranceContact(_ context.Context, partnerID, documentID, insurerName, contactName, contactNumber string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	doc, ok := f.byID[partnerID][documentID]
	if !ok {
		return domain.ErrDocumentNotFound
	}
	doc.InsurerName, doc.InsuranceContactName, doc.InsuranceContactNumber = insurerName, contactName, contactNumber
	f.byID[partnerID][documentID] = doc
	return nil
}

func (f *fakeDocRepo) UpsertCertificate(_ context.Context, partnerID string, cert domain.ProjectedCertificate) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.byID[partnerID] == nil {
		f.byID[partnerID] = map[string]domain.ComplianceDocument{}
	}
	doc := f.byID[partnerID][cert.ID]
	doc.ID, doc.Type = cert.ID, domain.DocumentTypeGoodsInTransit
	doc.Status, doc.DocumentName = cert.Status, cert.DocumentName
	doc.GoodsTypes, doc.CoverageCents, doc.ExpiresAt = cert.GoodsTypes, cert.CoverageCents, cert.ExpiresAt
	doc.InsurerName, doc.File = cert.InsurerName, cert.File
	f.byID[partnerID][cert.ID] = doc
	return nil
}

func (f *fakeDocRepo) AttachFile(_ context.Context, partnerID, documentID string, file domain.DocumentFile) (domain.ComplianceDocument, error) {
	return f.transition(partnerID, documentID, func(doc domain.ComplianceDocument) (domain.ComplianceDocument, error) {
		return doc.AttachFile(file)
	})
}

// fakeCertificateAppender stands in for the tenant-resolved aggregate write
// path plus the projector, collapsed into one synchronous object. It holds a
// real TransporterProfile per organization and applies the real events, then
// writes the resulting certificates through UpsertCertificate exactly as
// orchestration.Projector does — so a roundtrip spec exercises the genuine
// register -> event -> projection path rather than a shortcut that would pass
// whatever the command wrote.
type fakeCertificateAppender struct {
	mu      sync.Mutex
	docs    *fakeDocRepo
	profile map[string]*profiledomain.TransporterProfile
}

func newFakeCertificateAppender(docs *fakeDocRepo) *fakeCertificateAppender {
	return &fakeCertificateAppender{docs: docs, profile: map[string]*profiledomain.TransporterProfile{}}
}

func (f *fakeCertificateAppender) CertificateCommands(string) (commands.CertificateAppender, error) {
	return f, nil
}

// aggregate lazily creates the profile, since these specs register documents
// against organizations that never went through the vetting saga.
func (f *fakeCertificateAppender) aggregate(contextKey, organizationID string) *profiledomain.TransporterProfile {
	agg, ok := f.profile[organizationID]
	if !ok {
		agg = &profiledomain.TransporterProfile{}
		created, err := agg.Create(contextKey, organizationID)
		if err == nil {
			agg.Apply(created)
		}
		f.profile[organizationID] = agg
	}
	return agg
}

func (f *fakeCertificateAppender) project(organizationID string, agg *profiledomain.TransporterProfile) {
	for _, certificate := range agg.State().Certificates {
		_ = f.docs.UpsertCertificate(context.Background(), organizationID, domain.ProjectedCertificate{
			ID: certificate.ID, Status: certificate.Status, DocumentName: certificate.DocumentName,
			GoodsTypes: certificate.GoodsTypes, CoverageCents: certificate.CoverageCents,
			ExpiresAt: certificate.ExpiresAt, InsurerName: certificate.InsurerName, File: certificate.File,
		})
	}
}

func (f *fakeCertificateAppender) RegisterCertificate(_ context.Context, contextKey, organizationID string, doc domain.ComplianceDocument, actorName, sourceIP string) (profiledomain.State, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	agg := f.aggregate(contextKey, organizationID)
	event, err := agg.RegisterCertificate(doc, actorName, sourceIP)
	if err != nil {
		return profiledomain.State{}, err
	}
	agg.Apply(event)
	f.project(organizationID, agg)
	return agg.State(), nil
}

func (f *fakeCertificateAppender) AttachCertificateFile(_ context.Context, contextKey, organizationID, documentID string, file domain.DocumentFile, actorName, sourceIP string) (profiledomain.State, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	agg := f.aggregate(contextKey, organizationID)
	event, err := agg.AttachCertificateFile(documentID, file, actorName, sourceIP)
	if err != nil {
		return profiledomain.State{}, err
	}
	agg.Apply(event)
	f.project(organizationID, agg)
	return agg.State(), nil
}

func (f *fakeCertificateAppender) SetCertificateExpiry(_ context.Context, contextKey, organizationID, documentID string, expiresAt *int64, now time.Time, actorName, sourceIP string) (profiledomain.State, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	agg := f.aggregate(contextKey, organizationID)
	event, err := agg.SetCertificateExpiry(documentID, expiresAt, now, actorName, sourceIP)
	if err != nil {
		return profiledomain.State{}, err
	}
	agg.Apply(event)
	f.project(organizationID, agg)
	return agg.State(), nil
}

func (f *fakeCertificateAppender) ApproveCertificate(_ context.Context, contextKey, organizationID, documentID, insurerName string, now time.Time, actorName, sourceIP string) (profiledomain.State, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	agg := f.aggregate(contextKey, organizationID)
	events, err := agg.ApproveCertificate(documentID, insurerName, now, actorName, sourceIP)
	if err != nil {
		return profiledomain.State{}, err
	}
	for _, event := range events {
		agg.Apply(event)
	}
	f.project(organizationID, agg)
	return agg.State(), nil
}

func (f *fakeCertificateAppender) RejectCertificate(_ context.Context, contextKey, organizationID, documentID, actorName, sourceIP string) (profiledomain.State, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	agg := f.aggregate(contextKey, organizationID)
	event, err := agg.RejectCertificate(documentID, actorName, sourceIP)
	if err != nil {
		return profiledomain.State{}, err
	}
	agg.Apply(event)
	f.project(organizationID, agg)
	return agg.State(), nil
}

func (f *fakeCertificateAppender) UpdateCertificateDetails(_ context.Context, contextKey, organizationID, documentID string, goodsTypes []string, coverageCents *int64, insurerName string, contactsChanged bool, actorName, sourceIP string) (profiledomain.State, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	agg := f.aggregate(contextKey, organizationID)
	event, err := agg.UpdateCertificateDetails(documentID, goodsTypes, coverageCents, insurerName, contactsChanged, actorName, sourceIP)
	if err != nil {
		return profiledomain.State{}, err
	}
	agg.Apply(event)
	f.project(organizationID, agg)
	return agg.State(), nil
}

// acceptingGoodsTypes accepts any code. BR-TP64's vocabulary check has its own
// specs against the command; these roundtrip specs are about the api.* surface.
type acceptingGoodsTypes struct{}

func (acceptingGoodsTypes) GoodsTypeExists(context.Context, string, string, string) (bool, error) {
	return true, nil
}

type fakeFleetRepo struct {
	mu     sync.Mutex
	byName map[string][]domain.FleetAsset
}

func newFakeFleetRepo() *fakeFleetRepo {
	return &fakeFleetRepo{byName: map[string][]domain.FleetAsset{}}
}

func (f *fakeFleetRepo) AddFleetAsset(_ context.Context, partnerID string, a domain.FleetAsset) (domain.FleetAsset, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.byName[partnerID] = append(f.byName[partnerID], a)
	return a, nil
}

func (f *fakeFleetRepo) ListFleetAssets(_ context.Context, partnerID string) ([]domain.FleetAsset, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.byName[partnerID], nil
}

// recordingValidator captures which tenant BR-TP14's lookup was performed as —
// the assertion target for claim (2) above.
type recordingValidator struct {
	mu           sync.Mutex
	seenTenants  []string
	seenContexts []string
	result       bool
}

func (v *recordingValidator) Exists(_ context.Context, tenant, contextKey, _ string) (bool, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.seenTenants = append(v.seenTenants, tenant)
	v.seenContexts = append(v.seenContexts, contextKey)
	return v.result, nil
}

func (v *recordingValidator) tenants() []string {
	v.mu.Lock()
	defer v.mu.Unlock()
	return append([]string(nil), v.seenTenants...)
}

type fakeAudit struct {
	mu      sync.Mutex
	entries []domain.AuditEntry
}

func (f *fakeAudit) Record(_ context.Context, e domain.AuditEntry) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.entries = append(f.entries, e)
	return nil
}

func (f *fakeAudit) ListByPartner(_ context.Context, partnerID string) ([]domain.AuditEntry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []domain.AuditEntry
	for _, e := range f.entries {
		if e.OrganizationID == partnerID {
			out = append(out, e)
		}
	}
	return out, nil
}

func (f *fakeAudit) all() []domain.AuditEntry {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]domain.AuditEntry(nil), f.entries...)
}

var _ = Describe("api.* round trips (Phase 26h)", func() {
	const requestTimeout = 2 * time.Second

	var (
		nc        *nats.Conn
		partners  *fakePartnerRepo
		docs      *fakeDocRepo
		fleet     *fakeFleetRepo
		validator *recordingValidator
		audit     *fakeAudit
		vetting   *recordingVetting
	)

	// request sends data on subject and decodes the reply into out.
	request := func(subject string, in any, out any) {
		GinkgoHelper()
		body, err := json.Marshal(in)
		Expect(err).NotTo(HaveOccurred())
		reply, err := nc.Request(subject, body, requestTimeout)
		Expect(err).NotTo(HaveOccurred())
		if out != nil {
			Expect(json.Unmarshal(reply.Data, out)).To(Succeed())
		}
	}

	BeforeEach(func() {
		nc = newTestNATSConn()
		partners = newFakePartnerRepo()
		docs = newFakeDocRepo()
		fleet = newFakeFleetRepo()
		validator = &recordingValidator{result: true}
		audit = &fakeAudit{}

		vetting = &recordingVetting{}

		adapter, err := browserrpc.New(nc, browserrpc.Deps{
			Organizations: commands.NewOrganizationHandler(partners, audit),
			Documents: commands.NewComplianceDocumentHandler(partners, docs).
				WithGoodsTypeValidator(acceptingGoodsTypes{}).
				WithCertificateAppender(newFakeCertificateAppender(docs)),
			FleetAssets: commands.NewFleetAssetHandler(partners, fleet, validator),
			Audit:       audit,
			Vetting:     vetting,
			// The tenant this adapter's connection authenticated into.
			Tenant: "acme",
		})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = adapter.Stop() })
	})

	Context("context scoping", func() {
		It("takes {context} from the subject, not the request body", func() {
			// The security property: a body field must not be able to redirect a
			// write into a context the subject didn't name.
			var registered struct {
				ID      string `json:"id"`
				Context string `json:"context"`
			}
			request("api.acme-north.organizations.organization.register.v1",
				map[string]any{"name": "Initech", "type": "SHIPPER", "context": "acme-south"},
				&registered)

			Expect(registered.Context).To(Equal("acme-north"))
			Expect(registered.Context).NotTo(Equal("acme-south"))
		})

		It("lists only partners in the subject's context", func() {
			request("api.acme-north.organizations.organization.register.v1",
				map[string]any{"name": "North Co", "type": "SHIPPER"}, nil)
			request("api.acme-south.organizations.organization.register.v1",
				map[string]any{"name": "South Co", "type": "SHIPPER"}, nil)

			var listed struct {
				Organizations []struct {
					Name string `json:"name"`
				} `json:"organizations"`
			}
			request("api.acme-north.organizations.organization.list.v1", map[string]any{}, &listed)

			Expect(listed.Organizations).To(HaveLen(1))
			Expect(listed.Organizations[0].Name).To(Equal("North Co"))
		})
	})

	Context("BR-TP14 tenant resolution", func() {
		It("validates vehicleTypeCode as the connection's tenant, ignoring any body-supplied tenant", func() {
			// This is the improvement over REST, and the reason the api.*
			// fleetAssetRequest has no `tenant` field: HTTP had to trust the body.
			var registered struct {
				ID string `json:"id"`
			}
			request("api.acme-north.organizations.organization.register.v1",
				map[string]any{"name": "Acme Trucking", "type": "TRANSPORTER"}, &registered)
			Expect(registered.ID).NotTo(BeEmpty())

			request("api.acme-north.organizations.fleet-asset.add.v1", map[string]any{
				"id":              registered.ID,
				"registrationNo":  "ABC123",
				"vin":             "VIN1",
				"make":            "Volvo",
				"model":           "FH16",
				"vehicleTypeCode": "TAUTLINER",
				// A caller trying to borrow another tenant's refdata corpus.
				"tenant": "globex",
			}, nil)

			Expect(validator.tenants()).To(ConsistOf("acme"))
			Expect(validator.tenants()).NotTo(ContainElement("globex"))
		})
	})

	Context("lifecycle + audit (BR-TP03/BR-TP04/BR-TP06)", func() {
		It("activates and suspends over api.*, recording audit rows with the actor", func() {
			var registered struct {
				ID string `json:"id"`
			}
			request("api.acme.organizations.organization.register.v1",
				map[string]any{"name": "Globex", "type": "SHIPPER"}, &registered)

			request("api.acme.organizations.organization.activate.v1",
				map[string]any{"id": registered.ID}, nil)

			var suspended struct {
				Status string `json:"status"`
			}
			request("api.acme.organizations.organization.suspend.v1",
				map[string]any{"id": registered.ID, "reason": "docs expired"}, &suspended)

			Expect(suspended.Status).To(Equal("suspended"))

			actions := make([]string, 0)
			for _, e := range audit.all() {
				actions = append(actions, e.Action)
				Expect(e.Actor).NotTo(BeEmpty(), "BR-TP06 requires an actor on every row")
			}
			Expect(actions).To(ContainElements(
				domain.AuditActionRegistered, domain.AuditActionActivated, domain.AuditActionSuspended,
			))
		})

		It("returns a 404-coded error for an unknown partner rather than hanging", func() {
			reply, err := nc.Request("api.acme.organizations.organization.get.v1",
				[]byte(`{"id":"does-not-exist"}`), requestTimeout)
			Expect(err).NotTo(HaveOccurred())

			Expect(reply.Header.Get("Nats-Service-Error-Code")).To(Equal("404"))
			var out struct {
				Error    string `json:"error"`
				NotFound bool   `json:"notFound"`
			}
			Expect(json.Unmarshal(reply.Data, &out)).To(Succeed())
			Expect(out.NotFound).To(BeTrue())
			Expect(out.Error).To(ContainSubstring("not found"))
		})

		Context("BR-TP37/BR-TP38 vetting state over api.*", func() {
			It("answers hasProfile=false for a Shipper rather than erroring", func() {
				var registered struct {
					ID string `json:"id"`
				}
				request("api.acme.organizations.organization.register.v1",
					map[string]any{"name": "Initech", "type": "SHIPPER"}, &registered)

				var out struct {
					HasProfile bool   `json:"hasProfile"`
					GitStatus  string `json:"gitStatus"`
				}
				request("api.acme.organizations.organization.profile.v1",
					map[string]any{"id": registered.ID}, &out)

				Expect(out.HasProfile).To(BeFalse(), "a Shipper legitimately has no TransporterProfile")
				Expect(out.GitStatus).To(Equal("None"))
			})

			It("derives GIT status from the partner's documents (BR-TP38)", func() {
				var registered struct {
					ID string `json:"id"`
				}
				request("api.acme.organizations.organization.register.v1",
					map[string]any{"name": "Acme Trucking", "type": "TRANSPORTER"}, &registered)

				var out struct {
					GitStatus string `json:"gitStatus"`
				}
				request("api.acme.organizations.organization.profile.v1",
					map[string]any{"id": registered.ID}, &out)
				Expect(out.GitStatus).To(Equal("None"), "no GIT document yet")

				var added struct {
					ID string `json:"id"`
				}
				request("api.acme.organizations.document.add.v1", map[string]any{
					"id": registered.ID, "type": "GOODS_IN_TRANSIT", "goodsTypes": []any{"FOOD"}, "documentName": "git.pdf",
				}, &added)

				request("api.acme.organizations.organization.profile.v1",
					map[string]any{"id": registered.ID}, &out)
				Expect(out.GitStatus).To(Equal("Pending"), "a pending GIT document")

				// BR-TP66: insurer and the contact pair are approval-time
				// requirements on a GIT certificate, so an approval without
				// them is refused rather than silently approving.
				request("api.acme.organizations.document.approve.v1", map[string]any{
					"id": registered.ID, "documentId": added.ID,
					"insurerName": "Acme Insurance", "insuranceContactName": "Dana Reyes",
					"insuranceContactNumber": "+27 11 555 0100",
				}, nil)

				request("api.acme.organizations.organization.profile.v1",
					map[string]any{"id": registered.ID}, &out)
				Expect(out.GitStatus).To(Equal("Active"), "approved and unexpired")
			})

			It("accepts an expiry on add and on set-expiry, and rejects a past one (BR-TP59)", func() {
				var registered struct {
					ID string `json:"id"`
				}
				request("api.acme.organizations.organization.register.v1",
					map[string]any{"name": "Expiry Trucking", "type": "TRANSPORTER"}, &registered)

				future := time.Now().Add(30 * 24 * time.Hour).Unix()
				var added struct {
					ID        string `json:"id"`
					ExpiresAt *int64 `json:"expiresAt"`
				}
				request("api.acme.organizations.document.add.v1", map[string]any{
					"id": registered.ID, "type": "GOODS_IN_TRANSIT", "goodsTypes": []any{"FOOD"},
					"documentName": "git.pdf", "expiresAt": future,
				}, &added)
				Expect(added.ExpiresAt).NotTo(BeNil(), "the expiry must survive the api.* round trip")
				Expect(*added.ExpiresAt).To(Equal(future))

				later := time.Now().Add(60 * 24 * time.Hour).Unix()
				var updated struct {
					ExpiresAt *int64 `json:"expiresAt"`
				}
				request("api.acme.organizations.document.set-expiry.v1", map[string]any{
					"id": registered.ID, "documentId": added.ID, "expiresAt": later,
				}, &updated)
				Expect(*updated.ExpiresAt).To(Equal(later))

				past := time.Now().Add(-time.Hour).Unix()
				reply, err := nc.Request("api.acme.organizations.document.set-expiry.v1",
					[]byte(fmt.Sprintf(`{"id":%q,"documentId":%q,"expiresAt":%d}`, registered.ID, added.ID, past)),
					requestTimeout)
				Expect(err).NotTo(HaveOccurred())
				Expect(reply.Header.Get("Nats-Service-Error")).NotTo(BeEmpty(),
					"a past expiry is a data-entry error and must not reach the row")

				// BR-TP61: each accepted expiry write re-arms the cover timer,
				// and the refused one must not — signalling on a write that
				// never landed would re-arm against an expiry the row does not
				// have.
				//
				// One signal, not two: 39a moved supersede from registration to
				// approval (BR-TP69), so registering a certificate no longer
				// changes any cover and no longer re-arms anything. The accepted
				// set-expiry is the only write here that moves an expiry.
				Expect(vetting.coverSignals).To(HaveLen(1))
				Expect(vetting.coverSignals[0]).To(Equal("acme|" + registered.ID))
			})

			It("returns 404 for an unknown partner, not an empty profile", func() {
				reply, err := nc.Request("api.acme.organizations.organization.profile.v1",
					[]byte(`{"id":"does-not-exist"}`), requestTimeout)
				Expect(err).NotTo(HaveOccurred())
				Expect(reply.Header.Get("Nats-Service-Error-Code")).To(Equal("404"),
					"an unknown ID must not masquerade as a partner with no profile")
			})
		})

		Context("BR-TP32/BR-TP34/BR-TP35 editable Company Information over api.*", func() {
			It("registers with Company Information in one call (BR-TP35)", func() {
				var registered struct {
					ID                string `json:"id"`
					CompanyName       string `json:"companyName"`
					RegistrationNo    string `json:"registrationNo"`
					VatRegistrationNo string `json:"vatRegistrationNo"`
					TradingAs         string `json:"tradingAs"`
					Version           int    `json:"version"`
				}
				request("api.acme.organizations.organization.register.v1", map[string]any{
					"name":              "Globex",
					"type":              "SHIPPER",
					"companyName":       "Globex (Pty) Ltd",
					"registrationNo":    "2019/123456/07",
					"vatRegistrationNo": "4123456789",
					"tradingAs":         "Globex Freight",
				}, &registered)

				Expect(registered.CompanyName).To(Equal("Globex (Pty) Ltd"))
				Expect(registered.RegistrationNo).To(Equal("2019/123456/07"))
				Expect(registered.VatRegistrationNo).To(Equal("4123456789"))
				Expect(registered.TradingAs).To(Equal("Globex Freight"))
				Expect(registered.Version).To(Equal(1), "BR-TP33: a new row starts at version 1")
			})

			It("updates Company Information and bumps the version (BR-TP32/BR-TP33)", func() {
				var registered struct {
					ID      string `json:"id"`
					Version int    `json:"version"`
				}
				request("api.acme.organizations.organization.register.v1",
					map[string]any{"name": "Globex", "type": "SHIPPER"}, &registered)

				var updated struct {
					Name        string `json:"name"`
					CompanyName string `json:"companyName"`
					Type        string `json:"type"`
					Context     string `json:"context"`
					Status      string `json:"status"`
					Version     int    `json:"version"`
				}
				request("api.acme.organizations.organization.update.v1", map[string]any{
					"id":          registered.ID,
					"version":     registered.Version,
					"name":        "Globex International",
					"companyName": "Globex International (Pty) Ltd",
				}, &updated)

				Expect(updated.Name).To(Equal("Globex International"))
				Expect(updated.CompanyName).To(Equal("Globex International (Pty) Ltd"))
				Expect(updated.Version).To(Equal(registered.Version + 1))
				// BR-TP32: these three are not reachable through this endpoint,
				// even though a caller could put them in the body.
				Expect(updated.Type).To(Equal("SHIPPER"))
				Expect(updated.Context).To(Equal("acme"))
				Expect(updated.Status).To(Equal("registered"))
			})

			It("ignores type/context/status supplied in an update body (BR-TP32)", func() {
				var registered struct {
					ID      string `json:"id"`
					Version int    `json:"version"`
				}
				request("api.acme.organizations.organization.register.v1",
					map[string]any{"name": "Globex", "type": "SHIPPER"}, &registered)

				var updated struct {
					Type    string `json:"type"`
					Context string `json:"context"`
					Status  string `json:"status"`
				}
				request("api.acme.organizations.organization.update.v1", map[string]any{
					"id":      registered.ID,
					"version": registered.Version,
					"name":    "Globex",
					"type":    "TRANSPORTER",
					"context": "someone-elses-context",
					"status":  "active",
				}, &updated)

				Expect(updated.Type).To(Equal("SHIPPER"), "type is immutable — a Shipper must not become a Transporter via an edit")
				Expect(updated.Context).To(Equal("acme"), "context comes from the subject, never the body")
				Expect(updated.Status).To(Equal("registered"), "status has its own lifecycle endpoints")
			})

			// BR-TP34 over the wire: the rule says 409, and before 38c-i the
			// shared api.* reply path had no 409 at all (every conflict was a 500),
			// so this spec pins the code as well as the rejection.
			It("rejects a stale version with a 409, not a 500 (BR-TP34)", func() {
				var registered struct {
					ID      string `json:"id"`
					Version int    `json:"version"`
				}
				request("api.acme.organizations.organization.register.v1",
					map[string]any{"name": "Globex", "type": "SHIPPER"}, &registered)

				// Alice saves first and wins.
				request("api.acme.organizations.organization.update.v1", map[string]any{
					"id": registered.ID, "version": registered.Version,
					"name": "Globex", "companyName": "Set by Alice",
				}, nil)

				// Bob is still holding the version he read before Alice saved.
				body, err := json.Marshal(map[string]any{
					"id": registered.ID, "version": registered.Version,
					"name": "Globex", "companyName": "Set by Bob",
				})
				Expect(err).NotTo(HaveOccurred())
				reply, err := nc.Request("api.acme.organizations.organization.update.v1", body, requestTimeout)
				Expect(err).NotTo(HaveOccurred())

				Expect(reply.Header.Get("Nats-Service-Error-Code")).To(Equal("409"))
				var out struct {
					Error    string `json:"error"`
					Conflict bool   `json:"conflict"`
					NotFound bool   `json:"notFound"`
				}
				Expect(json.Unmarshal(reply.Data, &out)).To(Succeed())
				Expect(out.Conflict).To(BeTrue())
				Expect(out.NotFound).To(BeFalse(), "the partner exists — this is a conflict, not a 404")
				Expect(out.Error).To(ContainSubstring("modified by someone else"))

				// Alice's write survived intact.
				var current struct {
					CompanyName string `json:"companyName"`
				}
				request("api.acme.organizations.organization.get.v1",
					map[string]any{"id": registered.ID}, &current)
				Expect(current.CompanyName).To(Equal("Set by Alice"))
			})

			It("records a details-updated audit row with the actor (BR-TP06)", func() {
				var registered struct {
					ID      string `json:"id"`
					Version int    `json:"version"`
				}
				request("api.acme.organizations.organization.register.v1",
					map[string]any{"name": "Globex", "type": "SHIPPER"}, &registered)
				request("api.acme.organizations.organization.update.v1", map[string]any{
					"id": registered.ID, "version": registered.Version, "name": "Renamed",
				}, nil)

				actions := make([]string, 0)
				for _, e := range audit.all() {
					actions = append(actions, e.Action)
				}
				Expect(actions).To(ContainElement(domain.AuditActionDetailsUpdated))
			})
		})

		It("identifies itself in the Nats-Responder header (Phase 18 invariant)", func() {
			reply, err := nc.Request("api.acme.organizations.organization.list.v1", []byte(`{}`), requestTimeout)
			Expect(err).NotTo(HaveOccurred())
			Expect(reply.Header.Get("Nats-Responder")).To(HavePrefix(browserrpc.ServiceName + "/"))
		})
	})

	Context("obs.trace.* side-channel (BR-036/BR-037/BR-TP15)", func() {
		// traceSpan is a strict superset of the pre-Phase-28 obsEnvelope: this
		// decodes it into the old shape (ignoring every new field) to assert
		// backward compatibility, then again into the full shape for the new
		// tracing fields.
		type legacyEnvelope struct {
			Direction     string              `json:"direction"`
			CorrelationID string              `json:"correlationId"`
			Subject       string              `json:"subject"`
			Payload       json.RawMessage     `json:"payload,omitempty"`
			Error         string              `json:"error,omitempty"`
			Headers       map[string][]string `json:"headers,omitempty"`
			Timestamp     time.Time           `json:"timestamp"`
			PayloadBytes  int                 `json:"payloadBytes"`
		}
		type traceSpan struct {
			legacyEnvelope
			TraceID       string            `json:"traceId,omitempty"`
			SpanID        string            `json:"spanId,omitempty"`
			ParentSpanID  string            `json:"parentSpanId,omitempty"`
			Service       string            `json:"service,omitempty"`
			Entity        string            `json:"entity,omitempty"`
			Action        string            `json:"action,omitempty"`
			StatusCode    string            `json:"statusCode,omitempty"`
			StatusMessage string            `json:"statusMessage,omitempty"`
			Attributes    map[string]string `json:"attributes,omitempty"`
			Redacted      []string          `json:"redacted,omitempty"`
			Truncated     bool              `json:"truncated,omitempty"`
		}

		It("publishes one span per call to obs.trace.{context}.organizations.{entity}.{action}, decodable both as the old envelope shape and the new one", func() {
			spans := make(chan *nats.Msg, 8)
			sub, err := nc.Subscribe("obs.trace.>", func(m *nats.Msg) { spans <- m })
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { _ = sub.Unsubscribe() })
			Expect(nc.Flush()).To(Succeed())

			request("api.acme.organizations.organization.list.v1", map[string]any{}, nil)

			var msg *nats.Msg
			Eventually(spans).Should(Receive(&msg))
			Expect(msg.Subject).To(Equal("obs.trace.acme.organizations.organization.list"))

			var legacy legacyEnvelope
			Expect(json.Unmarshal(msg.Data, &legacy)).To(Succeed())
			Expect(legacy.Subject).To(Equal("api.acme.organizations.organization.list.v1"))
			Expect(legacy.PayloadBytes).To(BeNumerically(">", 0))

			var span traceSpan
			Expect(json.Unmarshal(msg.Data, &span)).To(Succeed())
			Expect(span.TraceID).NotTo(BeEmpty())
			Expect(span.SpanID).NotTo(BeEmpty())
			Expect(span.ParentSpanID).To(BeEmpty(), "a call with no inbound traceparent is a root span")
			Expect(span.Service).To(Equal("organizations"))
			Expect(span.Entity).To(Equal("organization"))
			Expect(span.Action).To(Equal("list"))
			Expect(span.StatusCode).To(Equal("OK"))
		})

		It("marks a failed call with statusCode ERROR and the error message", func() {
			spans := make(chan *nats.Msg, 8)
			sub, err := nc.Subscribe("obs.trace.>", func(m *nats.Msg) { spans <- m })
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { _ = sub.Unsubscribe() })
			Expect(nc.Flush()).To(Succeed())

			request("api.acme.organizations.organization.get.v1", map[string]any{"id": "does-not-exist"}, nil)

			var msg *nats.Msg
			Eventually(spans).Should(Receive(&msg))

			var span traceSpan
			Expect(json.Unmarshal(msg.Data, &span)).To(Succeed())
			Expect(span.StatusCode).To(Equal("ERROR"))
			Expect(span.StatusMessage).NotTo(BeEmpty())
		})
	})
})

// recordingVetting stands in for the Temporal-backed gateway. BR-TP57 is
// about *when* the adapter signals, so the transport itself is irrelevant
// here — what matters is that a signal is sent for a review that happened and
// withheld for one that did not.
type recordingVetting struct {
	signals   []string
	signalErr error
	submits   []string
	submitErr error
	// coverSignals records BR-TP61's re-arms, so a spec can prove an expiry
	// write reaches the parked workflow rather than only the row.
	coverSignals []string
}

func (v *recordingVetting) SignalCoverChanged(_ context.Context, contextKey, organizationID string) error {
	v.coverSignals = append(v.coverSignals, contextKey+"|"+organizationID)
	return v.signalErr
}

func (v *recordingVetting) Submit(_ context.Context, tenant, contextKey, organizationID string) error {
	v.submits = append(v.submits, tenant+"|"+contextKey+"|"+organizationID)
	return v.submitErr
}

func (v *recordingVetting) SignalDocumentReview(_ context.Context, contextKey, organizationID, reference string, approved bool) error {
	verdict := "rejected"
	if approved {
		verdict = "approved"
	}
	v.signals = append(v.signals, contextKey+"|"+organizationID+"|"+reference+"|"+verdict)
	return v.signalErr
}

var _ = Describe("BR-TP57 the api.* boundary signals document reviews", func() {
	// These run against the same harness as the roundtrip specs above; see its
	// BeforeEach for the wiring.
	var (
		nc       *nats.Conn
		partners *fakePartnerRepo
		docs     *fakeDocRepo
		audit    *fakeAudit
		vetting  *recordingVetting
	)

	const requestTimeout = 2 * time.Second

	request := func(subject string, in any, out any) *nats.Msg {
		body, err := json.Marshal(in)
		Expect(err).NotTo(HaveOccurred())
		reply, err := nc.Request(subject, body, requestTimeout)
		Expect(err).NotTo(HaveOccurred())
		if out != nil {
			Expect(json.Unmarshal(reply.Data, out)).To(Succeed())
		}
		return reply
	}

	// addTransporterDocument registers a TRANSPORTER and attaches one document,
	// returning both IDs — the starting point every spec below needs.
	addTransporterDocument := func() (string, string) {
		var registered struct {
			ID string `json:"id"`
		}
		request("api.acme.organizations.organization.register.v1",
			map[string]any{"name": "Haulage Co", "type": "TRANSPORTER"}, &registered)

		// documentResponse embeds ComplianceDocument, so the document's fields
		// are top-level in the reply rather than nested under a key.
		var added struct {
			ID string `json:"id"`
		}
		request("api.acme.organizations.document.add.v1",
			map[string]any{"id": registered.ID, "type": "GOODS_IN_TRANSIT", "goodsTypes": []any{"FOOD"}, "documentName": "git-1.pdf"}, &added)
		Expect(added.ID).NotTo(BeEmpty(), "BR-TP29 mints the document ID this signal uses as its reference")
		return registered.ID, added.ID
	}

	BeforeEach(func() {
		nc = newTestNATSConn()
		partners = newFakePartnerRepo()
		docs = newFakeDocRepo()
		audit = &fakeAudit{}
		vetting = &recordingVetting{}

		adapter, err := browserrpc.New(nc, browserrpc.Deps{
			Organizations: commands.NewOrganizationHandler(partners, audit),
			Documents: commands.NewComplianceDocumentHandler(partners, docs).
				WithGoodsTypeValidator(acceptingGoodsTypes{}).
				WithCertificateAppender(newFakeCertificateAppender(docs)),
			Audit:   audit,
			Vetting: vetting,
			Tenant:  "acme",
		})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = adapter.Stop() })
	})

	// BR-TP66: a GIT approval carries the insurer and the contact pair. These
	// specs are about BR-TP57's signal, not about the approval rules, so they
	// supply valid details once here rather than restating them per spec.
	approveGit := func(organizationID, documentID string) map[string]any {
		return map[string]any{
			"id": organizationID, "documentId": documentID,
			"insurerName": "Acme Insurance", "insuranceContactName": "Dana Reyes",
			"insuranceContactNumber": "+27 11 555 0100",
		}
	}

	It("signals the approval with the document ID as the reference", func() {
		organizationID, documentID := addTransporterDocument()

		request("api.acme.organizations.document.approve.v1", approveGit(organizationID, documentID), nil)

		Expect(vetting.signals).To(HaveExactElements("acme|" + organizationID + "|" + documentID + "|approved"))
	})

	It("signals a rejection with the same reference and the opposite verdict", func() {
		organizationID, documentID := addTransporterDocument()

		request("api.acme.organizations.document.reject.v1",
			map[string]any{"id": organizationID, "documentId": documentID}, nil)

		Expect(vetting.signals).To(HaveExactElements("acme|" + organizationID + "|" + documentID + "|rejected"))
	})

	// The workflow advances its required set on each signal, so signalling a
	// transition that the domain refused would let an attempt progress on a
	// document that never moved.
	It("sends nothing when the transition itself was refused", func() {
		organizationID, documentID := addTransporterDocument()
		request("api.acme.organizations.document.approve.v1", approveGit(organizationID, documentID), nil)
		vetting.signals = nil

		// BR-TP09: Approve is legal only from Pending, and this one is already
		// Approved.
		reply := request("api.acme.organizations.document.approve.v1", approveGit(organizationID, documentID), nil)

		Expect(reply.Header.Get("Nats-Service-Error-Code")).To(Equal("409"))
		Expect(vetting.signals).To(BeEmpty())
	})

	// Best-effort in BR-TP06's sense: the review is the authoritative act.
	It("still completes the review when the signal fails", func() {
		organizationID, documentID := addTransporterDocument()
		vetting.signalErr = errors.New("temporal unavailable")

		var out struct {
			Status string `json:"status"`
		}
		request("api.acme.organizations.document.approve.v1", approveGit(organizationID, documentID), &out)

		Expect(out.Status).To(Equal("APPROVED"),
			"a failed signal must never roll back or block the review it describes")
	})
})
