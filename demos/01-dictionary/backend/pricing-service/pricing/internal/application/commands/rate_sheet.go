package commands

import (
	"context"
	"time"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/pricing-service/pricing/internal/domain"
)

// RateSheetHandler is a thin pass-through onto domain.RateSheetRepository.
type RateSheetHandler struct {
	repo domain.RateSheetRepository
}

func NewRateSheetHandler(repo domain.RateSheetRepository) *RateSheetHandler {
	return &RateSheetHandler{repo: repo}
}

func (h *RateSheetHandler) Register(ctx context.Context, rs domain.RateSheet) error {
	return h.repo.Register(ctx, rs)
}

func (h *RateSheetHandler) Get(ctx context.Context, contextKey, name string) (domain.RateSheet, error) {
	return h.repo.Get(ctx, contextKey, name)
}

func (h *RateSheetHandler) List(ctx context.Context, contextKey string) ([]domain.RateSheet, error) {
	return h.repo.List(ctx, contextKey)
}

func (h *RateSheetHandler) CreateDraft(ctx context.Context, contextKey, name string) (domain.RateSheetVersion, error) {
	return h.repo.CreateDraft(ctx, contextKey, name)
}

func (h *RateSheetHandler) AddEntry(ctx context.Context, contextKey, name string, version int, e domain.RateSheetEntry) error {
	return h.repo.AddEntry(ctx, contextKey, name, version, e)
}

func (h *RateSheetHandler) SetFeeScaleOverride(ctx context.Context, contextKey, name string, version int, feeScaleName string) error {
	return h.repo.SetFeeScaleOverride(ctx, contextKey, name, version, feeScaleName)
}

func (h *RateSheetHandler) Publish(ctx context.Context, contextKey, name string) (domain.RateSheetVersion, error) {
	return h.repo.Publish(ctx, contextKey, name)
}

func (h *RateSheetHandler) Rollback(ctx context.Context, contextKey, name string, targetVersion int) (domain.RateSheetVersion, error) {
	return h.repo.Rollback(ctx, contextKey, name, targetVersion)
}

func (h *RateSheetHandler) Versions(ctx context.Context, contextKey, name string) ([]domain.RateSheetVersion, error) {
	return h.repo.Versions(ctx, contextKey, name)
}

func (h *RateSheetHandler) ActiveVersion(ctx context.Context, contextKey, name string) (domain.RateSheetVersion, error) {
	return h.repo.ActiveVersion(ctx, contextKey, name)
}

// AdditionalDropsCharge resolves the active version's entry for
// routeKey/vehicleType and applies BR-P12.
func (h *RateSheetHandler) AdditionalDropsCharge(ctx context.Context, contextKey, name, routeKey, vehicleType string, addressCount int) (int64, error) {
	v, err := h.repo.ActiveVersion(ctx, contextKey, name)
	if err != nil {
		return 0, err
	}
	for _, e := range v.Entries {
		if e.RouteKey == routeKey && e.VehicleType == vehicleType {
			return e.AdditionalDropsCharge(addressCount), nil
		}
	}
	return 0, domain.ErrRateSheetEntryNotFound
}

// IndexDieselPrice upserts a diesel price row for the context (BR-P18).
func (h *RateSheetHandler) IndexDieselPrice(ctx context.Context, contextKey string, price domain.DieselPrice) error {
	return h.repo.IndexDieselPrice(ctx, contextKey, price)
}

// ListDieselPrices returns every diesel price for the context (BR-P18).
func (h *RateSheetHandler) ListDieselPrices(ctx context.Context, contextKey string) ([]domain.DieselPrice, error) {
	return h.repo.ListDieselPrices(ctx, contextKey)
}

// ApplyDieselOverlay resolves the diesel price in effect on newActiveDate,
// appends an overlay to the active rate-sheet version, and persists the
// result (BR-P20/BR-P21/BR-P22). Returns ErrNoDieselPrice when no price
// covers newActiveDate (BR-P21 fail-closed).
func (h *RateSheetHandler) ApplyDieselOverlay(ctx context.Context, contextKey, name string, newActiveDate time.Time) (domain.RateSheetVersion, error) {
	prices, err := h.repo.ListDieselPrices(ctx, contextKey)
	if err != nil {
		return domain.RateSheetVersion{}, err
	}
	v, err := h.repo.ActiveVersion(ctx, contextKey, name)
	if err != nil {
		return domain.RateSheetVersion{}, err
	}
	newV, err := domain.AppendDieselOverlayFromIndex(v, prices, newActiveDate)
	if err != nil {
		return domain.RateSheetVersion{}, err
	}
	if err := h.repo.PersistDieselOverlay(ctx, contextKey, name, newV); err != nil {
		return domain.RateSheetVersion{}, err
	}
	return newV, nil
}
