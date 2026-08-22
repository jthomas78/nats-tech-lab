// Package orchestration contains TransporterProfile command handlers and the
// cross-aggregate Organization activation gate.
package orchestration

import (
	"context"
	"errors"
	"time"

	organizationdomain "github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/internal/domain"
	profiledomain "github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/transporterprofile/domain"
)

var ErrSequenceConflict = errors.New("transporter profile sequence conflict")

type EventStore interface {
	Hydrate(ctx context.Context, contextKey, organizationID string) (*profiledomain.TransporterProfile, uint64, error)
	Append(ctx context.Context, contextKey, organizationID string, event profiledomain.Event, expectedSequence uint64) (uint64, error)
}

type ProfileHandler struct{ store EventStore }

func NewProfileHandler(store EventStore) *ProfileHandler { return &ProfileHandler{store: store} }

func (h *ProfileHandler) CreateTransporterProfile(ctx context.Context, contextKey, organizationID string) (profiledomain.State, error) {
	return h.createOrEnsure(ctx, contextKey, organizationID)
}

func (h *ProfileHandler) EnsureTransporterProfile(ctx context.Context, contextKey, organizationID string) (profiledomain.State, error) {
	return h.createOrEnsure(ctx, contextKey, organizationID)
}

func (h *ProfileHandler) Resubmit(ctx context.Context, contextKey, organizationID string) (profiledomain.State, error) {
	agg, sequence, err := h.store.Hydrate(ctx, contextKey, organizationID)
	if err != nil {
		return profiledomain.State{}, err
	}
	event, err := agg.Resubmit()
	if err != nil {
		return profiledomain.State{}, err
	}
	if _, err = h.store.Append(ctx, contextKey, organizationID, event, sequence); err != nil {
		return profiledomain.State{}, err
	}
	agg.Apply(event)
	return agg.State(), nil
}

func (h *ProfileHandler) RecordVetted(ctx context.Context, contextKey, organizationID string, attemptNumber, step int) error {
	agg, sequence, err := h.store.Hydrate(ctx, contextKey, organizationID)
	if err != nil {
		return err
	}
	event, err := agg.RecordVetted(attemptNumber, step)
	if err != nil {
		return err
	}
	_, err = h.store.Append(ctx, contextKey, organizationID, event, sequence)
	return err
}

func (h *ProfileHandler) createOrEnsure(ctx context.Context, contextKey, organizationID string) (profiledomain.State, error) {
	agg, sequence, err := h.store.Hydrate(ctx, contextKey, organizationID)
	if err != nil {
		return profiledomain.State{}, err
	}
	if agg.Exists() {
		return agg.State(), nil
	}
	event, err := agg.Create(contextKey, organizationID)
	if err != nil {
		return profiledomain.State{}, err
	}
	if _, err = h.store.Append(ctx, contextKey, organizationID, event, sequence); err != nil {
		if !errors.Is(err, ErrSequenceConflict) {
			return profiledomain.State{}, err
		}
		// One bounded re-hydration converts a concurrent winning create into
		// the same idempotent result without appending or resetting anything.
		agg, _, err = h.store.Hydrate(ctx, contextKey, organizationID)
		if err != nil {
			return profiledomain.State{}, err
		}
		if !agg.Exists() {
			return profiledomain.State{}, ErrSequenceConflict
		}
		return agg.State(), nil
	}
	agg.Apply(event)
	return agg.State(), nil
}

// ConfigureTrackingCredential appends BR-TP55's event under BR-TP20's
// sequence guard, like every other write on this aggregate.
//
// The payload is not a parameter and never reaches this package: BR-TP52
// confines it to the sealed KV bucket, and the caller stores it before
// calling here (BR-TP53's ordering).
func (h *ProfileHandler) ConfigureTrackingCredential(ctx context.Context, contextKey, organizationID, provider, credentialType string) (profiledomain.State, error) {
	agg, sequence, err := h.store.Hydrate(ctx, contextKey, organizationID)
	if err != nil {
		return profiledomain.State{}, err
	}
	event, err := agg.ConfigureTrackingCredential(provider, credentialType)
	if err != nil {
		return profiledomain.State{}, err
	}
	if _, err = h.store.Append(ctx, contextKey, organizationID, event, sequence); err != nil {
		return profiledomain.State{}, err
	}
	agg.Apply(event)
	return agg.State(), nil
}

// RegisterCertificate appends the GIT registration fact. The caller has
// already validated goods-type membership through its tenant-scoped refdata
// port; this aggregate enforces the replayed certificate shape.
func (h *ProfileHandler) RegisterCertificate(ctx context.Context, contextKey, organizationID string, doc organizationdomain.ComplianceDocument, actorName, sourceIP string) (profiledomain.State, error) {
	agg, sequence, err := h.store.Hydrate(ctx, contextKey, organizationID)
	if err != nil {
		return profiledomain.State{}, err
	}
	event, err := agg.RegisterCertificate(doc, actorName, sourceIP)
	if err != nil {
		return profiledomain.State{}, err
	}
	if _, err = h.store.Append(ctx, contextKey, organizationID, event, sequence); err != nil {
		return profiledomain.State{}, err
	}
	agg.Apply(event)
	return agg.State(), nil
}

// AttachCertificateFile appends BR-TP68's file fact, which is also what moves
// a cheap registration into the reviewer's queue.
func (h *ProfileHandler) AttachCertificateFile(ctx context.Context, contextKey, organizationID, documentID string, file organizationdomain.DocumentFile, actorName, sourceIP string) (profiledomain.State, error) {
	agg, sequence, err := h.store.Hydrate(ctx, contextKey, organizationID)
	if err != nil {
		return profiledomain.State{}, err
	}
	event, err := agg.AttachCertificateFile(documentID, file, actorName, sourceIP)
	if err != nil {
		return profiledomain.State{}, err
	}
	if _, err = h.store.Append(ctx, contextKey, organizationID, event, sequence); err != nil {
		return profiledomain.State{}, err
	}
	agg.Apply(event)
	return agg.State(), nil
}

// SetCertificateExpiry appends BR-TP59's correction.
func (h *ProfileHandler) SetCertificateExpiry(ctx context.Context, contextKey, organizationID, documentID string, expiresAt *int64, now time.Time, actorName, sourceIP string) (profiledomain.State, error) {
	agg, sequence, err := h.store.Hydrate(ctx, contextKey, organizationID)
	if err != nil {
		return profiledomain.State{}, err
	}
	event, err := agg.SetCertificateExpiry(documentID, expiresAt, now, actorName, sourceIP)
	if err != nil {
		return profiledomain.State{}, err
	}
	if _, err = h.store.Append(ctx, contextKey, organizationID, event, sequence); err != nil {
		return profiledomain.State{}, err
	}
	agg.Apply(event)
	return agg.State(), nil
}

// ApproveCertificate appends BR-TP69's approval and every compensating lock
// fact in order. Each append advances the expected sequence, preserving the
// aggregate-wide guard even for the multi-event command.
func (h *ProfileHandler) ApproveCertificate(ctx context.Context, contextKey, organizationID, documentID, insurerName string, now time.Time, actorName, sourceIP string) (profiledomain.State, error) {
	agg, sequence, err := h.store.Hydrate(ctx, contextKey, organizationID)
	if err != nil {
		return profiledomain.State{}, err
	}
	events, err := agg.ApproveCertificate(documentID, insurerName, now, actorName, sourceIP)
	if err != nil {
		return profiledomain.State{}, err
	}
	for _, event := range events {
		sequence, err = h.store.Append(ctx, contextKey, organizationID, event, sequence)
		if err != nil {
			return profiledomain.State{}, err
		}
		agg.Apply(event)
	}
	return agg.State(), nil
}

// RejectCertificate appends the GIT review verdict to the owning aggregate.
func (h *ProfileHandler) RejectCertificate(ctx context.Context, contextKey, organizationID, documentID, actorName, sourceIP string) (profiledomain.State, error) {
	agg, sequence, err := h.store.Hydrate(ctx, contextKey, organizationID)
	if err != nil {
		return profiledomain.State{}, err
	}
	event, err := agg.RejectCertificate(documentID, actorName, sourceIP)
	if err != nil {
		return profiledomain.State{}, err
	}
	if _, err = h.store.Append(ctx, contextKey, organizationID, event, sequence); err != nil {
		return profiledomain.State{}, err
	}
	agg.Apply(event)
	return agg.State(), nil
}

func (h *ProfileHandler) UpdateCertificateDetails(ctx context.Context, contextKey, organizationID, documentID, reference string, goodsTypes []string, coverageCents *int64, insurerName string, contactsChanged bool, actorName, sourceIP string) (profiledomain.State, error) {
	agg, sequence, err := h.store.Hydrate(ctx, contextKey, organizationID)
	if err != nil {
		return profiledomain.State{}, err
	}
	event, err := agg.UpdateCertificateDetails(documentID, reference, goodsTypes, coverageCents, insurerName, contactsChanged, actorName, sourceIP)
	if err != nil {
		return profiledomain.State{}, err
	}
	if _, err = h.store.Append(ctx, contextKey, organizationID, event, sequence); err != nil {
		return profiledomain.State{}, err
	}
	agg.Apply(event)
	return agg.State(), nil
}
