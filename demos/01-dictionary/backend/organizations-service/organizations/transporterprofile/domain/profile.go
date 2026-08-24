// Package domain holds the event-sourced TransporterProfile aggregate.
package domain

import (
	"errors"
	"slices"
	"sort"
	"strings"
	"time"

	organizationdomain "github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/internal/domain"
)

const (
	StreamName                    = "TRANSPORTER"
	ServiceName                   = "organizations"
	AggregateName                 = "transporter"
	CreatedEvent                  = "created"
	VettingStartedEvent           = "vetting-started"
	DocumentApprovedEvent         = "document-approved"
	DocumentRejectedEvent         = "document-rejected"
	DocumentApprovalRevertedEvent = "document-approval-reverted"
	DocumentRegisteredEvent       = "document-registered"
	DocumentDetailsUpdatedEvent   = "document-details-updated"
	DocumentFileAttachedEvent     = "document-file-attached"
	DocumentSupersededEvent       = "document-superseded"
	DocumentReviewCancelledEvent  = "document-review-cancelled"
	GitVerifiedEvent              = "git-verified"
	VettedEvent                   = "vetted"
	RejectedEvent                 = "rejected"
	VettingResubmittedEvent       = "vetting-resubmitted"
	FleetAvailabilityRevokedEvent = "fleet-availability-revoked"
	// TrackingCredentialConfiguredEvent (BR-TP55) records that a telematics
	// integration was configured. It carries the provider and credential
	// type only — never the payload, which BR-TP52 confines to an
	// at-rest-encrypted KV bucket. An event log is replayed and audited and
	// cannot be redacted the way a row can be updated, so a secret written
	// here would be permanently worse than V2's plaintext columns.
	TrackingCredentialConfiguredEvent = "tracking-credential-configured"
	SubjectWildcard                   = "evt.*." + ServiceName + "." + AggregateName + ".>"
)

type Status string
type DocumentReviewStatus string

const (
	StatusAwaiting Status = "Awaiting"
	StatusInReview Status = "InReview"
	StatusVetted   Status = "Vetted"
	StatusRejected Status = "Rejected"
	// StatusCoverLapsed (BR-TP63, 38h-ii) is entered from Vetted when
	// goods-in-transit cover expires or is revoked. It exists so that a
	// lapse is visible in Status rather than only in FleetAvailabilityGate:
	// before it, a lapsed transporter read `Vetted` with the gate false, and
	// "is this transporter vetted" had two answers depending on which field
	// you read. 38h-ii's cover timer is armed if and only if the profile is
	// Vetted, an invariant that is unstatable while a lapsed profile still
	// claims that status.
	//
	// There is no direct un-lapse: the way back to Vetted is the normal
	// resubmit-and-re-vet path (BR-TP26), because renewed cover is a new
	// document (BR-TP30) and needs reviewing like any other.
	StatusCoverLapsed Status = "CoverLapsed"
)

const (
	DocumentPendingReview   DocumentReviewStatus = "PendingReview"
	DocumentApproved        DocumentReviewStatus = "Approved"
	DocumentRejected        DocumentReviewStatus = "Rejected"
	DocumentReviewCancelled DocumentReviewStatus = "Cancelled"
)

var ErrNotFound = errors.New("transporter profile not found")

// State is the Shape-B projection written to Postgres and the KV cache.
type State struct {
	Context               string                          `json:"context"`
	ID                    string                          `json:"id"`
	Status                Status                          `json:"status"`
	AttemptNumber         int                             `json:"attemptNumber"`
	FleetAvailabilityGate bool                            `json:"fleetAvailabilityGate"`
	GitVerified           bool                            `json:"gitVerified"`
	DocumentReviews       map[string]DocumentReviewStatus `json:"documentReviews,omitempty"`
	// Certificates is the replayed GIT write model. Contact values are
	// intentionally absent: their projection-only storage is BR-TP72's named
	// redaction exception.
	Certificates map[string]Certificate `json:"certificates,omitempty"`
	// TrackingCredentials maps a configured provider to its credential type
	// (BR-TP55). Values only — never a secret (BR-TP52).
	TrackingCredentials map[string]string `json:"trackingCredentials,omitempty"`
	UpdatedAt           time.Time         `json:"updatedAt"`
}

// Certificate is the event-safe portion of a GIT document. It is deliberately
// not a ComplianceDocument: adding contact fields to it later would silently
// put redactable data in every replay, so the absence is structural.
type Certificate struct {
	ID            string                            `json:"id"`
	Status        organizationdomain.DocumentStatus `json:"status"`
	DocumentName  string                            `json:"documentName"`
	GoodsTypes    []string                          `json:"goodsTypes,omitempty"`
	CoverageCents *int64                            `json:"coverageCents,omitempty"`
	ExpiresAt     *int64                            `json:"expiresAt,omitempty"`
	InsurerName   string                            `json:"insurerName,omitempty"`
	File          *organizationdomain.DocumentFile  `json:"file,omitempty"`
}

// FieldChange is the explicit before/after form required for all certificate
// mutations. Event consumers never infer a previous value from a snapshot.
type FieldChange struct {
	Field string `json:"field"`
	From  any    `json:"from"`
	To    any    `json:"to"`
	// Withheld marks a field whose *values* are deliberately absent from the
	// log (BR-TP72). Without it, "this changed, we are not recording to what"
	// is indistinguishable from "this changed to null", and Phase 46's export
	// would render a redaction as a cleared value — the one reading a
	// compliance log must not produce.
	Withheld bool `json:"withheld,omitempty"`
}

// WithheldChange is the only way a redacted field enters the log. A helper
// rather than a struct literal at each call site so no caller can half-build
// one and leave a real value in From or To.
func WithheldChange(field string) FieldChange {
	return FieldChange{Field: field, Withheld: true}
}

// AvailableForAssignment is BR-TP55's computed fleet-availability value.
//
// Two conditions, not V2's three. V2's isOwner() requires ownership=OWNED
// AND a linked tracking credential AND trackingStatus=LIVE; this returns
// the vetting gate AND at least one configured credential.
//
// The missing condition is ownership, and its absence is a real gap, not a
// simplification chosen here: BR-TP13's FleetAsset carries only
// registrationNo/vin/make/model/vehicleTypeCode — the `ownership` field the
// design intended was never built. Adding it is a change to 38a's model and
// belongs to whoever approves that, not to this rule.
//
// Note also that credentials are profile-level here, where V2 links them
// per-asset. FleetAsset has no credential link, so "this transporter can
// be assigned loads" is the honest granularity; per-asset availability
// would need that link first.
func (s State) AvailableForAssignment() bool {
	return s.FleetAvailabilityGate && len(s.TrackingCredentials) > 0
}

// Event is the stable envelope for TransporterProfile events. Event Type is
// selected by the final subject token and retained here for domain-only fakes.
type Event struct {
	Type                  string                          `json:"type,omitempty"`
	Context               string                          `json:"context"`
	OrganizationID        string                          `json:"organizationID"`
	Status                Status                          `json:"status,omitempty"`
	AttemptNumber         int                             `json:"attemptNumber,omitempty"`
	Step                  int                             `json:"step,omitempty"`
	DocumentReference     string                          `json:"documentReference,omitempty"`
	Provider              string                          `json:"provider,omitempty"`
	CredentialType        string                          `json:"credentialType,omitempty"`
	FleetAvailabilityGate bool                            `json:"fleetAvailabilityGate,omitempty"`
	GitVerified           bool                            `json:"gitVerified,omitempty"`
	DocumentReviews       map[string]DocumentReviewStatus `json:"documentReviews,omitempty"`
	Certificate           *Certificate                    `json:"certificate,omitempty"`
	Changes               []FieldChange                   `json:"changes,omitempty"`
	ActorName             string                          `json:"actorName,omitempty"`
	ActorSourceIP         string                          `json:"actorSourceIP,omitempty"`
	OccurredAt            time.Time                       `json:"occurredAt"`
}

func NewCreatedEvent(contextKey, organizationID string) Event {
	return Event{
		Type:           CreatedEvent,
		Context:        contextKey,
		OrganizationID: organizationID,
		Status:         StatusAwaiting,
		OccurredAt:     time.Now().UTC(),
	}
}

// TransporterProfile reconstructs its state by applying its event history.
// Its identity is the associated Organization ID; there is no surrogate.
type TransporterProfile struct {
	state  State
	exists bool
}

func (p *TransporterProfile) Apply(event Event) {
	switch event.Type {
	case CreatedEvent:
		p.state = State{Context: event.Context, ID: event.OrganizationID, Status: StatusAwaiting, UpdatedAt: event.OccurredAt}
		p.exists = true
	case VettingStartedEvent, VettingResubmittedEvent:
		if !p.exists {
			return
		}
		p.state.Status = StatusInReview
		p.state.AttemptNumber = event.AttemptNumber
		p.state.FleetAvailabilityGate = false
		p.state.GitVerified = false
		p.state.UpdatedAt = event.OccurredAt
	case DocumentApprovedEvent:
		if !p.exists {
			return
		}
		p.setDocumentReview(event.DocumentReference, DocumentApproved)
		if event.Certificate != nil {
			p.setCertificate(*event.Certificate)
		}
		p.state.UpdatedAt = event.OccurredAt
	case DocumentRegisteredEvent, DocumentDetailsUpdatedEvent, DocumentFileAttachedEvent:
		if !p.exists || event.Certificate == nil || event.Certificate.ID == "" {
			return
		}
		p.setCertificate(*event.Certificate)
		p.state.UpdatedAt = event.OccurredAt
	case DocumentSupersededEvent:
		if !p.exists || event.Certificate == nil || event.Certificate.ID == "" {
			return
		}
		p.setCertificate(*event.Certificate)
		p.state.UpdatedAt = event.OccurredAt
	case DocumentReviewCancelledEvent:
		if !p.exists {
			return
		}
		p.setDocumentReview(event.DocumentReference, DocumentReviewCancelled)
		p.state.UpdatedAt = event.OccurredAt
	case DocumentRejectedEvent:
		if !p.exists {
			return
		}
		p.setDocumentReview(event.DocumentReference, DocumentRejected)
		if event.Certificate != nil {
			p.setCertificate(*event.Certificate)
		}
		p.state.UpdatedAt = event.OccurredAt
	case DocumentApprovalRevertedEvent:
		if !p.exists {
			return
		}
		p.setDocumentReview(event.DocumentReference, DocumentPendingReview)
		p.state.UpdatedAt = event.OccurredAt
	case GitVerifiedEvent:
		if !p.exists {
			return
		}
		p.state.GitVerified = true
		p.state.UpdatedAt = event.OccurredAt
	case VettedEvent:
		if !p.exists {
			return
		}
		p.state.Status = StatusVetted
		p.state.AttemptNumber = event.AttemptNumber
		p.state.FleetAvailabilityGate = true
		p.state.DocumentReviews = cloneDocumentReviews(event.DocumentReviews)
		p.state.UpdatedAt = event.OccurredAt
	case RejectedEvent:
		if !p.exists {
			return
		}
		p.state.Status = StatusRejected
		p.state.AttemptNumber = event.AttemptNumber
		p.state.FleetAvailabilityGate = false
		p.state.GitVerified = false
		p.state.DocumentReviews = cloneDocumentReviews(event.DocumentReviews)
		p.state.UpdatedAt = event.OccurredAt
	case FleetAvailabilityRevokedEvent:
		if !p.exists {
			return
		}
		// BR-TP63: Status moves with the gate. Taken from the event rather
		// than assigned as a constant here so a replay of a pre-38h-ii event
		// (which carries the incumbent status) reconstructs what actually
		// happened, instead of retro-fitting CoverLapsed onto history.
		if event.Status != "" {
			p.state.Status = event.Status
		}
		p.state.FleetAvailabilityGate = false
		p.state.GitVerified = false
		p.state.UpdatedAt = event.OccurredAt
	case TrackingCredentialConfiguredEvent:
		if !p.exists {
			return
		}
		if p.state.TrackingCredentials == nil {
			p.state.TrackingCredentials = map[string]string{}
		}
		// Overwrite, not append (BR-TP54): re-configuring a provider
		// replaces it. Unlike a document, a credential is current state,
		// nothing in the log references a payload, and retaining a
		// superseded secret would keep compromised material alive for no
		// reader.
		p.state.TrackingCredentials[event.Provider] = event.CredentialType
		p.state.UpdatedAt = event.OccurredAt
	}
}

// RegisterCertificate creates the event-safe GIT registration fact. The
// caller supplies a document with a service-minted ID; non-GIT documents stay
// on their existing CRUD path during Phase 39a.
func (p *TransporterProfile) RegisterCertificate(doc organizationdomain.ComplianceDocument, actorName, sourceIP string) (Event, error) {
	if !p.exists {
		return Event{}, ErrNotFound
	}
	if doc.Type != organizationdomain.DocumentTypeGoodsInTransit {
		return Event{}, errors.New("only goods-in-transit documents belong to the transporter profile")
	}
	if err := doc.ValidateGitCertificate(); err != nil {
		return Event{}, err
	}
	event := p.event(DocumentRegisteredEvent, p.state.AttemptNumber, p.state.Status)
	event.Certificate = certificateFromDocument(doc)
	event.DocumentReference = doc.ID
	// Registration's from side is empty by definition; the fields are still
	// listed one by one so an exported row reads the same shape as every
	// later edit (decision 16).
	event.Changes = []FieldChange{
		{Field: "documentName", From: nil, To: doc.DocumentName},
		{Field: "goodsTypes", From: nil, To: append([]string(nil), doc.GoodsTypes...)},
		{Field: "coverageCents", From: nil, To: doc.CoverageCents},
		{Field: "expiresAt", From: nil, To: doc.ExpiresAt},
		{Field: "status", From: nil, To: doc.Status},
	}
	event.ActorName, event.ActorSourceIP = actorName, sourceIP
	return event, nil
}

// ApproveCertificate is BR-TP69's write-side lock: once a new certificate is
// approved, every earlier unsuperseded certificate is superseded and any open
// review is explicitly cancelled. The caller appends the returned events in
// order under the aggregate sequence guard.
// insurerName and the two withheld contact-field markers travel with the
// approval because BR-TP66 makes them approval-time requirements: they are
// what the reviewer supplied in order to approve.
func (p *TransporterProfile) ApproveCertificate(documentID, insurerName string, now time.Time, actorName, sourceIP string) ([]Event, error) {
	if !p.exists {
		return nil, ErrNotFound
	}
	certificate, ok := p.state.Certificates[documentID]
	if !ok {
		return nil, organizationdomain.ErrDocumentNotFound
	}
	if certificate.Status == organizationdomain.DocumentStatusSuperseded {
		return nil, organizationdomain.ErrDocumentSuperseded
	}
	// Replay must be sufficient to enforce BR-TP67. The projection-read
	// command applies the same domain guard so it can fetch the deliberately
	// un-replayed contact pair, but it is not the write-side authority.
	probe := organizationdomain.ComplianceDocument{
		ID: certificate.ID, Type: organizationdomain.DocumentTypeGoodsInTransit,
		Status: certificate.Status, ExpiresAt: certificate.ExpiresAt,
	}
	if _, err := probe.ApproveWithInsuranceDetails(insurerName, "withheld", "withheld", now); err != nil {
		return nil, err
	}
	approved := certificate
	from := approved.Status
	approved.Status = organizationdomain.DocumentStatusApproved
	approval := p.event(DocumentApprovedEvent, p.state.AttemptNumber, p.state.Status)
	approval.DocumentReference = documentID
	approval.Certificate = &approved
	approval.Changes = []FieldChange{{Field: "status", From: from, To: approved.Status}}
	if insurerName != certificate.InsurerName {
		approval.Changes = append(approval.Changes,
			FieldChange{Field: "insurerName", From: certificate.InsurerName, To: insurerName})
		approved.InsurerName = insurerName
		approval.Certificate = &approved
	}
	// BR-TP72: the contact pair is required to approve and so certainly
	// changed, but its values never reach this log. Recorded as withheld
	// rather than omitted — an auditor asking "was a contact recorded?" gets
	// an answer, just not the number.
	approval.Changes = append(approval.Changes,
		WithheldChange("insuranceContactName"), WithheldChange("insuranceContactNumber"))
	approval.ActorName, approval.ActorSourceIP = actorName, sourceIP
	events := []Event{approval}

	// Sorted, not map order: these events go onto an immutable log in the
	// order they are appended, and ranging a map would give two replays of the
	// same command two different histories.
	ids := make([]string, 0, len(p.state.Certificates))
	for id := range p.state.Certificates {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		earlier := p.state.Certificates[id]
		if id == documentID || earlier.Status == organizationdomain.DocumentStatusSuperseded {
			continue
		}
		superseded := earlier
		superseded.Status = organizationdomain.DocumentStatusSuperseded
		event := p.event(DocumentSupersededEvent, p.state.AttemptNumber, p.state.Status)
		event.DocumentReference = id
		event.Certificate = &superseded
		event.Changes = []FieldChange{{Field: "status", From: earlier.Status, To: superseded.Status}}
		event.ActorName, event.ActorSourceIP = actorName, sourceIP
		events = append(events, event)
		if p.state.DocumentReviews[id] == DocumentPendingReview {
			cancelled := p.event(DocumentReviewCancelledEvent, p.state.AttemptNumber, p.state.Status)
			cancelled.DocumentReference = id
			cancelled.Changes = []FieldChange{{Field: "reviewStatus", From: DocumentPendingReview, To: DocumentReviewCancelled}}
			cancelled.ActorName, cancelled.ActorSourceIP = actorName, sourceIP
			events = append(events, cancelled)
		}
	}
	return events, nil
}

// RejectCertificate records BR-TP10's Pending -> Rejected transition on the
// aggregate that owns GIT certificate state. A direct projection-row update
// would be replayed away by the next certificate event.
func (p *TransporterProfile) RejectCertificate(documentID, actorName, sourceIP string) (Event, error) {
	if !p.exists {
		return Event{}, ErrNotFound
	}
	certificate, ok := p.state.Certificates[documentID]
	if !ok {
		return Event{}, organizationdomain.ErrDocumentNotFound
	}
	if certificate.Status == organizationdomain.DocumentStatusSuperseded {
		return Event{}, organizationdomain.ErrDocumentSuperseded
	}
	// The one status ComplianceDocument.Reject admits (BR-TP10 as amended by
	// Phase 40): a registered certificate sits in FOR_REVIEW, and that is the
	// queue a reviewer rejects from.
	if certificate.Status != organizationdomain.DocumentStatusForReview {
		return Event{}, organizationdomain.ErrDocumentNotForReview
	}
	rejected := certificate
	rejected.Status = organizationdomain.DocumentStatusRejected
	event := p.event(DocumentRejectedEvent, p.state.AttemptNumber, p.state.Status)
	event.DocumentReference = documentID
	event.Certificate = &rejected
	event.Changes = []FieldChange{{Field: "status", From: certificate.Status, To: rejected.Status}}
	event.ActorName, event.ActorSourceIP = actorName, sourceIP
	return event, nil
}

// UpdateCertificateDetails is the edit view's replay-safe write. Expiry is a
// separate command because BR-TP70 permits that one correction even after a
// certificate is superseded; every field here remains locked then.
func (p *TransporterProfile) UpdateCertificateDetails(documentID string, goodsTypes []string, coverageCents *int64, insurerName string, contactsChanged bool, actorName, sourceIP string) (Event, error) {
	if !p.exists {
		return Event{}, ErrNotFound
	}
	certificate, ok := p.state.Certificates[documentID]
	if !ok {
		return Event{}, organizationdomain.ErrDocumentNotFound
	}
	if certificate.Status == organizationdomain.DocumentStatusSuperseded {
		return Event{}, organizationdomain.ErrDocumentSuperseded
	}
	// The probe re-runs registration validation over the edited values. It is
	// handed the certificate's existing name rather than one from the caller:
	// Phase 40 made the name read-only, so an edit can never change it.
	probe, err := organizationdomain.AddDocument(organizationdomain.PartnerTypeTransporter, organizationdomain.DocumentTypeGoodsInTransit, certificate.DocumentName)
	if err != nil {
		return Event{}, err
	}
	probe.GoodsTypes, probe.CoverageCents = append([]string(nil), goodsTypes...), cloneInt64(coverageCents)
	if err := probe.ValidateGitCertificate(); err != nil {
		return Event{}, err
	}

	updated := certificate
	updated.GoodsTypes = append([]string(nil), goodsTypes...)
	updated.CoverageCents = cloneInt64(coverageCents)
	updated.InsurerName = insurerName
	event := p.event(DocumentDetailsUpdatedEvent, p.state.AttemptNumber, p.state.Status)
	event.DocumentReference = documentID
	event.Certificate = &updated
	if !slices.Equal(goodsTypes, certificate.GoodsTypes) {
		event.Changes = append(event.Changes, FieldChange{Field: "goodsTypes", From: certificate.GoodsTypes, To: goodsTypes})
	}
	if !equalInt64(coverageCents, certificate.CoverageCents) {
		event.Changes = append(event.Changes, FieldChange{Field: "coverageCents", From: certificate.CoverageCents, To: coverageCents})
	}
	if insurerName != certificate.InsurerName {
		event.Changes = append(event.Changes, FieldChange{Field: "insurerName", From: certificate.InsurerName, To: insurerName})
	}
	if contactsChanged {
		event.Changes = append(event.Changes, WithheldChange("insuranceContactName"), WithheldChange("insuranceContactNumber"))
	}
	event.ActorName, event.ActorSourceIP = actorName, sourceIP
	return event, nil
}

// AttachCertificateFile records that bytes landed against a certificate
// (BR-TP68 as amended by Phase 40). It carries no transition: a certificate
// is registered in FOR_REVIEW, so the file has no cheaper state to promote it
// out of. Re-uploading against an approved certificate still does not send it
// back for review, and a superseded one is not revived.
func (p *TransporterProfile) AttachCertificateFile(documentID string, file organizationdomain.DocumentFile, actorName, sourceIP string) (Event, error) {
	if !p.exists {
		return Event{}, ErrNotFound
	}
	certificate, ok := p.state.Certificates[documentID]
	if !ok {
		return Event{}, organizationdomain.ErrDocumentNotFound
	}
	if certificate.Status == organizationdomain.DocumentStatusSuperseded {
		return Event{}, organizationdomain.ErrDocumentSuperseded
	}
	if certificate.File != nil {
		return Event{}, organizationdomain.ErrDocumentFileAlreadyAttached
	}
	updated := certificate
	updated.File = &file
	// No status change accompanies the bytes any more: Phase 40 registers
	// every certificate in FOR_REVIEW, so there is nothing for the file to
	// promote it out of.
	changes := []FieldChange{{Field: "file", From: nil, To: file.ObjectName}}
	event := p.event(DocumentFileAttachedEvent, p.state.AttemptNumber, p.state.Status)
	event.DocumentReference = documentID
	event.Certificate = &updated
	event.Changes = changes
	event.ActorName, event.ActorSourceIP = actorName, sourceIP
	return event, nil
}

// SetCertificateExpiry is BR-TP59's correction on the event-sourced path.
//
// It is admissible on a superseded certificate (BR-TP70, decision 23): a
// historical correction is not a review decision, and it cannot restore
// cover, because cover is derived from approved documents alone. Without this
// path the expiry would be written to the projection directly and the next
// certificate event would replay the old value straight back over it.
func (p *TransporterProfile) SetCertificateExpiry(documentID string, expiresAt *int64, now time.Time, actorName, sourceIP string) (Event, error) {
	if !p.exists {
		return Event{}, ErrNotFound
	}
	certificate, ok := p.state.Certificates[documentID]
	if !ok {
		return Event{}, organizationdomain.ErrDocumentNotFound
	}
	// The guard is ComplianceDocument's, not a second copy of it: SetExpiry is
	// deliberately the single point BR-TP59 is enforced at.
	probe := organizationdomain.ComplianceDocument{
		ID: certificate.ID, Type: organizationdomain.DocumentTypeGoodsInTransit,
		Status: certificate.Status, ExpiresAt: certificate.ExpiresAt,
	}
	updatedDoc, err := probe.SetExpiry(expiresAt, now)
	if err != nil {
		return Event{}, err
	}
	updated := certificate
	updated.ExpiresAt = updatedDoc.ExpiresAt
	event := p.event(DocumentDetailsUpdatedEvent, p.state.AttemptNumber, p.state.Status)
	event.DocumentReference = documentID
	event.Certificate = &updated
	event.Changes = []FieldChange{{Field: "expiresAt", From: certificate.ExpiresAt, To: updated.ExpiresAt}}
	event.ActorName, event.ActorSourceIP = actorName, sourceIP
	return event, nil
}

// ConfigureTrackingCredential appends BR-TP55's event. The payload is not a
// parameter: it never reaches this aggregate, so it cannot reach the log.
func (p *TransporterProfile) ConfigureTrackingCredential(provider, credentialType string) (Event, error) {
	if !p.exists {
		return Event{}, ErrNotFound
	}
	event := p.event(TrackingCredentialConfiguredEvent, p.state.AttemptNumber, p.state.Status)
	event.Provider = provider
	event.CredentialType = credentialType
	return event, nil
}

func (p *TransporterProfile) Resubmit() (Event, error) {
	if !p.exists {
		return Event{}, ErrNotFound
	}
	if p.state.Status != StatusRejected {
		return Event{}, errors.New("only a rejected transporter profile can be resubmitted")
	}
	// StatusInReview, not p.state.Status. The old value here was
	// Rejected — the status being resubmitted *away from* — which left a
	// profile with a live attempt running still reading as Rejected, because
	// the workflow skips its own VettingStarted append when Resubmitted is
	// true and so nothing else ever moved it. The workflow's appendEvent
	// already maps VettingResubmittedEvent to StatusInReview; this
	// makes the aggregate agree with it.
	//
	// Never observable before 38b's completion wired VettingService to a live
	// caller: until then this path only ran in tests, which do not read the
	// projection back.
	return p.event(VettingResubmittedEvent, p.state.AttemptNumber+1, StatusInReview), nil
}

func (p *TransporterProfile) RecordVetted(attemptNumber, step int) (Event, error) {
	if !p.exists {
		return Event{}, ErrNotFound
	}
	if attemptNumber < p.state.AttemptNumber {
		return Event{}, errors.New("vetting attempt number cannot go backwards")
	}
	event := p.event(VettedEvent, attemptNumber, StatusVetted)
	event.Step = step
	event.FleetAvailabilityGate = true
	event.GitVerified = true
	return event, nil
}

func (p *TransporterProfile) RevokeFleetAvailability() (Event, error) {
	if !p.exists {
		return Event{}, ErrNotFound
	}
	if !p.state.FleetAvailabilityGate {
		return Event{}, errors.New("fleet availability is already revoked")
	}
	// BR-TP63: the lapse moves Status, rather than preserving the incumbent
	// one as this did before 38h-ii.
	event := p.event(FleetAvailabilityRevokedEvent, p.state.AttemptNumber, StatusCoverLapsed)
	event.FleetAvailabilityGate = false
	event.GitVerified = false
	return event, nil
}

func (p *TransporterProfile) event(eventType string, attemptNumber int, status Status) Event {
	return Event{Type: eventType, Context: p.state.Context, OrganizationID: p.state.ID, Status: status, AttemptNumber: attemptNumber, FleetAvailabilityGate: p.state.FleetAvailabilityGate, GitVerified: p.state.GitVerified, OccurredAt: time.Now().UTC()}
}

func (p *TransporterProfile) Exists() bool { return p.exists }

func (p *TransporterProfile) State() State {
	state := p.state
	if p.state.DocumentReviews != nil {
		state.DocumentReviews = make(map[string]DocumentReviewStatus, len(p.state.DocumentReviews))
		for reference, status := range p.state.DocumentReviews {
			state.DocumentReviews[reference] = status
		}
	}
	if p.state.Certificates != nil {
		state.Certificates = make(map[string]Certificate, len(p.state.Certificates))
		for id, certificate := range p.state.Certificates {
			state.Certificates[id] = cloneCertificate(certificate)
		}
	}
	return state
}

func (p *TransporterProfile) setCertificate(certificate Certificate) {
	if p.state.Certificates == nil {
		p.state.Certificates = map[string]Certificate{}
	}
	p.state.Certificates[certificate.ID] = cloneCertificate(certificate)
}

func certificateFromDocument(doc organizationdomain.ComplianceDocument) *Certificate {
	return &Certificate{ID: doc.ID, Status: doc.Status, DocumentName: doc.DocumentName,
		GoodsTypes: append([]string(nil), doc.GoodsTypes...), CoverageCents: cloneInt64(doc.CoverageCents),
		ExpiresAt: cloneInt64(doc.ExpiresAt), InsurerName: doc.InsurerName, File: cloneFile(doc.File)}
}

func cloneCertificate(c Certificate) Certificate {
	c.GoodsTypes = append([]string(nil), c.GoodsTypes...)
	c.CoverageCents, c.ExpiresAt, c.File = cloneInt64(c.CoverageCents), cloneInt64(c.ExpiresAt), cloneFile(c.File)
	return c
}

func cloneInt64(value *int64) *int64 {
	if value == nil {
		return nil
	}
	clone := *value
	return &clone
}

func equalInt64(a, b *int64) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}

func cloneFile(file *organizationdomain.DocumentFile) *organizationdomain.DocumentFile {
	if file == nil {
		return nil
	}
	clone := *file
	return &clone
}

func (p *TransporterProfile) setDocumentReview(reference string, status DocumentReviewStatus) {
	if reference == "" {
		return
	}
	if p.state.DocumentReviews == nil {
		p.state.DocumentReviews = make(map[string]DocumentReviewStatus)
	}
	p.state.DocumentReviews[reference] = status
}

func cloneDocumentReviews(reviews map[string]DocumentReviewStatus) map[string]DocumentReviewStatus {
	if reviews == nil {
		return nil
	}
	clone := make(map[string]DocumentReviewStatus, len(reviews))
	for reference, status := range reviews {
		clone[reference] = status
	}
	return clone
}

func (p *TransporterProfile) Create(contextKey, organizationID string) (Event, error) {
	if p.exists {
		return Event{}, errors.New("transporter profile already exists")
	}
	return NewCreatedEvent(contextKey, organizationID), nil
}

func Subject(contextKey, organizationID, eventType string) string {
	return strings.Join([]string{"evt", contextKey, ServiceName, AggregateName, organizationID, eventType}, ".")
}

// InstanceSubject is both the hydration filter and BR-TP20 guard filter.
func InstanceSubject(contextKey, organizationID string) string {
	return strings.Join([]string{"evt", contextKey, ServiceName, AggregateName, organizationID, ">"}, ".")
}

func EventType(subject string) (string, bool) {
	parts := strings.Split(subject, ".")
	if len(parts) != 6 || parts[0] != "evt" || parts[2] != ServiceName || parts[3] != AggregateName || parts[4] == "" {
		return "", false
	}
	return parts[5], true
}

// stateProjectingEvents is the closed allowlist of event types that carry a
// profile-state transition. Document/GIT branch events are deliberately absent:
// they are audit transitions, and the surrounding started/rejected/vetted
// events project the resulting state.
//
// This is an allowlist, not a denylist, on purpose. A subject's final token is
// free-form, so any unrecognised event landing on the aggregate's wildcard —
// a future event type, another service's stray publish, or a sequence-bumping
// event like BR-TP20's guard test uses — must not be mistaken for a state
// transition and overwrite the projection with a zero-valued State.
var stateProjectingEvents = map[string]struct{}{
	CreatedEvent:                  {},
	VettingStartedEvent:           {},
	VettedEvent:                   {},
	RejectedEvent:                 {},
	VettingResubmittedEvent:       {},
	FleetAvailabilityRevokedEvent: {},
	// BR-TP55: this event does transition projected state — it is what makes
	// AvailableForAssignment true — so unlike the document/GIT branch events
	// it belongs in the allowlist.
	TrackingCredentialConfiguredEvent: {},
	// Certificate facts are also projected state. The projector rehydrates the
	// aggregate before writing, so these sparse from/to events never need to
	// carry a forbidden full-state snapshot.
	DocumentRegisteredEvent:      {},
	DocumentDetailsUpdatedEvent:  {},
	DocumentFileAttachedEvent:    {},
	DocumentApprovedEvent:        {},
	DocumentRejectedEvent:        {},
	DocumentSupersededEvent:      {},
	DocumentReviewCancelledEvent: {},
}

// ProjectsState reports whether eventType transitions projected profile state.
// Unknown event types return false, so the projector skips them rather than
// treating them as a transition.
func ProjectsState(eventType string) bool {
	_, ok := stateProjectingEvents[eventType]
	return ok
}
