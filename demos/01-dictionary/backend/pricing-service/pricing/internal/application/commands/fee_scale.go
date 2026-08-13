package commands

import (
	"context"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/pricing-service/pricing/internal/domain"
)

// FeeScaleHandler is a thin pass-through onto domain.FeeScaleRepository —
// no notifier, since pricing-service has no NATS wiring yet (Phase 25e is
// still undecided).
type FeeScaleHandler struct {
	repo domain.FeeScaleRepository
}

func NewFeeScaleHandler(repo domain.FeeScaleRepository) *FeeScaleHandler {
	return &FeeScaleHandler{repo: repo}
}

func (h *FeeScaleHandler) Register(ctx context.Context, fs domain.FeeScale) error {
	return h.repo.Register(ctx, fs)
}

func (h *FeeScaleHandler) Get(ctx context.Context, contextKey, name string) (domain.FeeScale, error) {
	return h.repo.Get(ctx, contextKey, name)
}

// List resolves BR-P16 — soft-deleted fee scales never appear in a listing.
func (h *FeeScaleHandler) List(ctx context.Context, contextKey string) ([]domain.FeeScale, error) {
	all, err := h.repo.List(ctx, contextKey)
	if err != nil {
		return nil, err
	}
	return domain.VisibleFeeScales(all), nil
}

func (h *FeeScaleHandler) CreateDraft(ctx context.Context, contextKey, name string) (domain.FeeScaleVersion, error) {
	return h.repo.CreateDraft(ctx, contextKey, name)
}

func (h *FeeScaleHandler) AddRange(ctx context.Context, contextKey, name string, version int, r domain.FeeScaleRange) error {
	return h.repo.AddRange(ctx, contextKey, name, version, r)
}

func (h *FeeScaleHandler) Publish(ctx context.Context, contextKey, name string) (domain.FeeScaleVersion, error) {
	return h.repo.Publish(ctx, contextKey, name)
}

func (h *FeeScaleHandler) Rollback(ctx context.Context, contextKey, name string, targetVersion int) (domain.FeeScaleVersion, error) {
	return h.repo.Rollback(ctx, contextKey, name, targetVersion)
}

func (h *FeeScaleHandler) Versions(ctx context.Context, contextKey, name string) ([]domain.FeeScaleVersion, error) {
	return h.repo.Versions(ctx, contextKey, name)
}

func (h *FeeScaleHandler) ActiveVersion(ctx context.Context, contextKey, name string) (domain.FeeScaleVersion, error) {
	return h.repo.ActiveVersion(ctx, contextKey, name)
}

// CalculateFee resolves the active version and applies BR-P03/BR-P04/BR-P05.
func (h *FeeScaleHandler) CalculateFee(ctx context.Context, contextKey, name string, centBid int64) (int64, error) {
	v, err := h.repo.ActiveVersion(ctx, contextKey, name)
	if err != nil {
		return 0, err
	}
	return v.CalculateFee(centBid)
}
