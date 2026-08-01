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

// RegisterPlatformRoot registers the reserved "_"-prefixed platform-root
// context (Phase 16d) — the one legitimate exception to Register's BR-D33
// rejection of a leading "_", which exists specifically to keep the
// reserved namespace out of reach of the public
// POST /api/refdata/admin/contexts endpoint (Register, above always calls
// the full ValidateContextName check; nothing REST-facing calls this
// method). Applies ValidateSubjectToken alone — the charset check still
// holds, only the reserved-prefix rejection is skipped. Only seed.go calls
// this, and only for the literal "_platform" value.
func (h *ContextHandler) RegisterPlatformRoot(ctx context.Context, value domain.Context) error {
	if err := domain.ValidateSubjectToken(value.Context); err != nil {
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

// ListByTenant returns tenant's own contexts plus the shared platform roots
// (Phase 16f) — see domain.ContextRepository.ListByTenant.
func (h *ContextHandler) ListByTenant(ctx context.Context, tenant string) ([]domain.Context, error) {
	return h.contexts.ListByTenant(ctx, tenant)
}
func (h *ContextHandler) Ancestors(ctx context.Context, key string) ([]domain.Context, error) {
	return h.contexts.Ancestors(ctx, key)
}
func (h *ContextHandler) Descendants(ctx context.Context, key string) ([]domain.Context, error) {
	return h.contexts.Descendants(ctx, key)
}
