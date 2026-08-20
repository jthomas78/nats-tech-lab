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
	SubjectWildcard               = "evt.*." + ServiceName + "." + AggregateName + ".>"
)

type Status string
type DocumentReviewStatus string

const (
	StatusAwaitingDocumentation Status = "AwaitingDocumentation"
	StatusDocumentsInReview     Status = "DocumentsInReview"
	StatusVetted                Status = "Vetted"
	StatusRejected              Status = "Rejected"
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
	UpdatedAt             time.Time                       `json:"updatedAt"`
}

// Event is the stable envelope for TransporterProfile events. Event Type is
// selected by the final subject token and retained here for domain-only fakes.
type Event struct {
	Type                  string                          `json:"type,omitempty"`
	Context               string                          `json:"context"`
	TradingPartnerID      string                          `json:"tradingPartnerID"`
	Status                Status                          `json:"status,omitempty"`
	AttemptNumber         int                             `json:"attemptNumber,omitempty"`
	Step                  int                             `json:"step,omitempty"`
	DocumentReference     string                          `json:"documentReference,omitempty"`
	FleetAvailabilityGate bool                            `json:"fleetAvailabilityGate,omitempty"`
	GitVerified           bool                            `json:"gitVerified,omitempty"`
	DocumentReviews       map[string]DocumentReviewStatus `json:"documentReviews,omitempty"`
	OccurredAt            time.Time                       `json:"occurredAt"`
}

func NewCreatedEvent(contextKey, tradingPartnerID string) Event {
	return Event{
		Type:             CreatedEvent,
		Context:          contextKey,
		TradingPartnerID: tradingPartnerID,
		Status:           StatusAwaitingDocumentation,
		OccurredAt:       time.Now().UTC(),
	}
}

// TransporterProfile reconstructs its state by applying its event history.
// Its identity is the associated TradingPartner ID; there is no surrogate.
type TransporterProfile struct {
	state  State
	exists bool
}

func (p *TransporterProfile) Apply(event Event) {
	switch event.Type {
	case CreatedEvent:
		p.state = State{Context: event.Context, ID: event.TradingPartnerID, Status: StatusAwaitingDocumentation, UpdatedAt: event.OccurredAt}
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
		p.state.FleetAvailabilityGate = false
		p.state.GitVerified = false
		p.state.UpdatedAt = event.OccurredAt
	}
}

func (p *TransporterProfile) Resubmit() (Event, error) {
	if !p.exists {
		return Event{}, ErrNotFound
	}
	if p.state.Status != StatusRejected {
		return Event{}, errors.New("only a rejected transporter profile can be resubmitted")
	}
	return p.event(VettingResubmittedEvent, p.state.AttemptNumber+1, p.state.Status), nil
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
	event := p.event(FleetAvailabilityRevokedEvent, p.state.AttemptNumber, p.state.Status)
	event.FleetAvailabilityGate = false
	event.GitVerified = false
	return event, nil
}

func (p *TransporterProfile) event(eventType string, attemptNumber int, status Status) Event {
	return Event{Type: eventType, Context: p.state.Context, TradingPartnerID: p.state.ID, Status: status, AttemptNumber: attemptNumber, FleetAvailabilityGate: p.state.FleetAvailabilityGate, GitVerified: p.state.GitVerified, OccurredAt: time.Now().UTC()}
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

func (p *TransporterProfile) Create(contextKey, tradingPartnerID string) (Event, error) {
	if p.exists {
		return Event{}, errors.New("transporter profile already exists")
	}
	return NewCreatedEvent(contextKey, tradingPartnerID), nil
}

func Subject(contextKey, tradingPartnerID, eventType string) string {
	return strings.Join([]string{"evt", contextKey, ServiceName, AggregateName, tradingPartnerID, eventType}, ".")
}

// InstanceSubject is both the hydration filter and BR-TP20 guard filter.
func InstanceSubject(contextKey, tradingPartnerID string) string {
	return strings.Join([]string{"evt", contextKey, ServiceName, AggregateName, tradingPartnerID, ">"}, ".")
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
}

// ProjectsState reports whether eventType transitions projected profile state.
// Unknown event types return false, so the projector skips them rather than
// treating them as a transition.
func ProjectsState(eventType string) bool {
	_, ok := stateProjectingEvents[eventType]
	return ok
}
