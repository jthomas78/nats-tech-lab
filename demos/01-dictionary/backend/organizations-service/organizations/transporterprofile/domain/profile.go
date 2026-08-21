// Package domain holds the event-sourced TransporterProfile aggregate.
package domain

import (
	"errors"
	"strings"
	"time"
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
	StatusAwaitingDocumentation Status = "AwaitingDocumentation"
	StatusDocumentsInReview     Status = "DocumentsInReview"
	StatusVetted                Status = "Vetted"
	StatusRejected              Status = "Rejected"
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
	DocumentPendingReview DocumentReviewStatus = "PendingReview"
	DocumentApproved      DocumentReviewStatus = "Approved"
	DocumentRejected      DocumentReviewStatus = "Rejected"
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
	// TrackingCredentials maps a configured provider to its credential type
	// (BR-TP55). Values only — never a secret (BR-TP52).
	TrackingCredentials map[string]string `json:"trackingCredentials,omitempty"`
	UpdatedAt           time.Time         `json:"updatedAt"`
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
	OccurredAt            time.Time                       `json:"occurredAt"`
}

func NewCreatedEvent(contextKey, organizationID string) Event {
	return Event{
		Type:           CreatedEvent,
		Context:        contextKey,
		OrganizationID: organizationID,
		Status:         StatusAwaitingDocumentation,
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
		p.state = State{Context: event.Context, ID: event.OrganizationID, Status: StatusAwaitingDocumentation, UpdatedAt: event.OccurredAt}
		p.exists = true
	case VettingStartedEvent, VettingResubmittedEvent:
		if !p.exists {
			return
		}
		p.state.Status = StatusDocumentsInReview
		p.state.AttemptNumber = event.AttemptNumber
		p.state.FleetAvailabilityGate = false
		p.state.GitVerified = false
		p.state.UpdatedAt = event.OccurredAt
	case DocumentApprovedEvent:
		if !p.exists {
			return
		}
		p.setDocumentReview(event.DocumentReference, DocumentApproved)
		p.state.UpdatedAt = event.OccurredAt
	case DocumentRejectedEvent:
		if !p.exists {
			return
		}
		p.setDocumentReview(event.DocumentReference, DocumentRejected)
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
	// StatusDocumentsInReview, not p.state.Status. The old value here was
	// Rejected — the status being resubmitted *away from* — which left a
	// profile with a live attempt running still reading as Rejected, because
	// the workflow skips its own VettingStarted append when Resubmitted is
	// true and so nothing else ever moved it. The workflow's appendEvent
	// already maps VettingResubmittedEvent to StatusDocumentsInReview; this
	// makes the aggregate agree with it.
	//
	// Never observable before 38b's completion wired VettingService to a live
	// caller: until then this path only ran in tests, which do not read the
	// projection back.
	return p.event(VettingResubmittedEvent, p.state.AttemptNumber+1, StatusDocumentsInReview), nil
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
	return state
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
}

// ProjectsState reports whether eventType transitions projected profile state.
// Unknown event types return false, so the projector skips them rather than
// treating them as a transition.
func ProjectsState(eventType string) bool {
	_, ok := stateProjectingEvents[eventType]
	return ok
}
