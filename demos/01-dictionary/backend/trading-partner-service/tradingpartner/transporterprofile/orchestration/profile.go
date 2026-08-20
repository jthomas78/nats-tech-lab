// Package orchestration contains TransporterProfile command handlers and the
// cross-aggregate TradingPartner activation gate.
package orchestration

import (
	"context"
	"errors"

	profiledomain "github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/trading-partner-service/tradingpartner/transporterprofile/domain"
)

var ErrSequenceConflict = errors.New("transporter profile sequence conflict")

type EventStore interface {
	Hydrate(ctx context.Context, contextKey, tradingPartnerID string) (*profiledomain.TransporterProfile, uint64, error)
	Append(ctx context.Context, contextKey, tradingPartnerID string, event profiledomain.Event, expectedSequence uint64) (uint64, error)
}

type ProfileHandler struct{ store EventStore }

func NewProfileHandler(store EventStore) *ProfileHandler { return &ProfileHandler{store: store} }

func (h *ProfileHandler) CreateTransporterProfile(ctx context.Context, contextKey, tradingPartnerID string) (profiledomain.State, error) {
	return h.createOrEnsure(ctx, contextKey, tradingPartnerID)
}

func (h *ProfileHandler) EnsureTransporterProfile(ctx context.Context, contextKey, tradingPartnerID string) (profiledomain.State, error) {
	return h.createOrEnsure(ctx, contextKey, tradingPartnerID)
}

func (h *ProfileHandler) Resubmit(ctx context.Context, contextKey, tradingPartnerID string) (profiledomain.State, error) {
	agg, sequence, err := h.store.Hydrate(ctx, contextKey, tradingPartnerID)
	if err != nil {
		return profiledomain.State{}, err
	}
	event, err := agg.Resubmit()
	if err != nil {
		return profiledomain.State{}, err
	}
	if _, err = h.store.Append(ctx, contextKey, tradingPartnerID, event, sequence); err != nil {
		return profiledomain.State{}, err
	}
	agg.Apply(event)
	return agg.State(), nil
}

func (h *ProfileHandler) RecordVetted(ctx context.Context, contextKey, tradingPartnerID string, attemptNumber, step int) error {
	agg, sequence, err := h.store.Hydrate(ctx, contextKey, tradingPartnerID)
	if err != nil {
		return err
	}
	event, err := agg.RecordVetted(attemptNumber, step)
	if err != nil {
		return err
	}
	_, err = h.store.Append(ctx, contextKey, tradingPartnerID, event, sequence)
	return err
}

func (h *ProfileHandler) createOrEnsure(ctx context.Context, contextKey, tradingPartnerID string) (profiledomain.State, error) {
	agg, sequence, err := h.store.Hydrate(ctx, contextKey, tradingPartnerID)
	if err != nil {
		return profiledomain.State{}, err
	}
	if agg.Exists() {
		return agg.State(), nil
	}
	event, err := agg.Create(contextKey, tradingPartnerID)
	if err != nil {
		return profiledomain.State{}, err
	}
	if _, err = h.store.Append(ctx, contextKey, tradingPartnerID, event, sequence); err != nil {
		if !errors.Is(err, ErrSequenceConflict) {
			return profiledomain.State{}, err
		}
		// One bounded re-hydration converts a concurrent winning create into
		// the same idempotent result without appending or resetting anything.
		agg, _, err = h.store.Hydrate(ctx, contextKey, tradingPartnerID)
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
