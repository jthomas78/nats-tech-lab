package commands

import (
	"context"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata/internal/domain"
)

// CorpusHandler's notifier is optional (nil in tests / no-NATS mode), same
// convention as ItemHandler/ReferenceHandler's ChangeNotifier — a nil check
// guards every call site below.
type CorpusHandler struct {
	corpus   domain.CorpusRepository
	notifier domain.CorpusNotifier
}

func NewCorpusHandler(corpus domain.CorpusRepository, notifier domain.CorpusNotifier) *CorpusHandler {
	return &CorpusHandler{corpus: corpus, notifier: notifier}
}
func (h *CorpusHandler) CreateDraft(ctx context.Context, key, notes string) (domain.CorpusVersion, error) {
	return h.corpus.CreateDraft(ctx, key, notes)
}
func (h *CorpusHandler) PutDraftItem(ctx context.Context, key string, item domain.CorpusItem) error {
	return h.corpus.PutDraftItem(ctx, key, item)
}
func (h *CorpusHandler) PutDraftLocalization(ctx context.Context, key string, loc domain.CorpusLocalization) error {
	return h.corpus.PutDraftLocalization(ctx, key, loc)
}
func (h *CorpusHandler) Publish(ctx context.Context, key string) (domain.CorpusVersion, error) {
	version, err := h.corpus.Publish(ctx, key)
	if err != nil {
		return version, err
	}
	if h.notifier != nil {
		if err := h.notifier.NotifyPublished(ctx, key, version.Version); err != nil {
			return version, err
		}
	}
	return version, nil
}
func (h *CorpusHandler) Rollback(ctx context.Context, key string, version int, notes string) (domain.CorpusVersion, error) {
	result, err := h.corpus.Rollback(ctx, key, version, notes)
	if err != nil {
		return result, err
	}
	if h.notifier != nil {
		if err := h.notifier.NotifyRolledBack(ctx, key, result.Version); err != nil {
			return result, err
		}
	}
	return result, nil
}
func (h *CorpusHandler) Versions(ctx context.Context, key string) ([]domain.CorpusVersion, error) {
	return h.corpus.Versions(ctx, key)
}
func (h *CorpusHandler) GetVersion(ctx context.Context, key string, version int) (domain.CorpusVersion, error) {
	return h.corpus.GetVersion(ctx, key, version)
}
func (h *CorpusHandler) ItemsAtVersion(ctx context.Context, key string, version int) ([]domain.CorpusItem, error) {
	return h.corpus.ItemsAtVersion(ctx, key, version)
}
func (h *CorpusHandler) LocalizationsAtVersion(ctx context.Context, key string, version int) ([]domain.CorpusLocalization, error) {
	return h.corpus.LocalizationsAtVersion(ctx, key, version)
}
func (h *CorpusHandler) Diff(ctx context.Context, key string, from, to int) ([]domain.CorpusDiffEntry, error) {
	return h.corpus.Diff(ctx, key, from, to)
}
