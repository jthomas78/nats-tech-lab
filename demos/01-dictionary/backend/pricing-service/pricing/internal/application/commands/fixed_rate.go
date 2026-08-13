package commands

import (
	"context"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/pricing-service/pricing/internal/domain"
)

// FixedRateHandler is a thin pass-through onto domain.FixedRateRepository.
type FixedRateHandler struct {
	repo domain.FixedRateRepository
}

func NewFixedRateHandler(repo domain.FixedRateRepository) *FixedRateHandler {
	return &FixedRateHandler{repo: repo}
}

func (h *FixedRateHandler) Register(ctx context.Context, fr domain.FixedRate) error {
	return h.repo.Register(ctx, fr)
}

func (h *FixedRateHandler) Get(ctx context.Context, contextKey, name string) (domain.FixedRate, error) {
	return h.repo.Get(ctx, contextKey, name)
}

func (h *FixedRateHandler) List(ctx context.Context, contextKey string) ([]domain.FixedRate, error) {
	return h.repo.List(ctx, contextKey)
}

func (h *FixedRateHandler) CreateDraft(ctx context.Context, contextKey, name string, centRate int64, pointCount int, centAdditionalDropRate int64) (domain.FixedRateVersion, error) {
	return h.repo.CreateDraft(ctx, contextKey, name, centRate, pointCount, centAdditionalDropRate)
}

func (h *FixedRateHandler) Publish(ctx context.Context, contextKey, name string) (domain.FixedRateVersion, error) {
	return h.repo.Publish(ctx, contextKey, name)
}

func (h *FixedRateHandler) Rollback(ctx context.Context, contextKey, name string, targetVersion int) (domain.FixedRateVersion, error) {
	return h.repo.Rollback(ctx, contextKey, name, targetVersion)
}

func (h *FixedRateHandler) Versions(ctx context.Context, contextKey, name string) ([]domain.FixedRateVersion, error) {
	return h.repo.Versions(ctx, contextKey, name)
}

func (h *FixedRateHandler) ActiveVersion(ctx context.Context, contextKey, name string) (domain.FixedRateVersion, error) {
	return h.repo.ActiveVersion(ctx, contextKey, name)
}

// AdditionalDropsCharge resolves the active version and applies BR-P15.
func (h *FixedRateHandler) AdditionalDropsCharge(ctx context.Context, contextKey, name string, addressCount int) (int64, error) {
	v, err := h.repo.ActiveVersion(ctx, contextKey, name)
	if err != nil {
		return 0, err
	}
	return v.AdditionalDropsCharge(addressCount), nil
}
