package commands

import (
	"context"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata/internal/domain"
)

type ContextHandler struct{ contexts domain.ContextRepository }

func NewContextHandler(contexts domain.ContextRepository) *ContextHandler {
	return &ContextHandler{contexts: contexts}
}
func (h *ContextHandler) Register(ctx context.Context, value domain.Context) error {
	if err := domain.ValidateContextName(value.Context); err != nil {
		return err
	}
	return h.contexts.Register(ctx, value)
}
func (h *ContextHandler) Get(ctx context.Context, key string) (domain.Context, error) {
	return h.contexts.Get(ctx, key)
}
func (h *ContextHandler) List(ctx context.Context) ([]domain.Context, error) {
	return h.contexts.List(ctx)
}
func (h *ContextHandler) Ancestors(ctx context.Context, key string) ([]domain.Context, error) {
	return h.contexts.Ancestors(ctx, key)
}
func (h *ContextHandler) Descendants(ctx context.Context, key string) ([]domain.Context, error) {
	return h.contexts.Descendants(ctx, key)
}
