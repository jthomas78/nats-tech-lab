package commands

import (
	"context"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata/internal/domain"
)

// TypeHandler executes type-registry commands.
type TypeHandler struct {
	types domain.TypeRepository
}

func NewTypeHandler(types domain.TypeRepository) *TypeHandler {
	return &TypeHandler{types: types}
}

// RegisterType creates or updates a type-registry entry. BR-D09: the
// category must be one of the controlled vocabulary.
func (h *TypeHandler) RegisterType(ctx context.Context, t domain.DictionaryType) error {
	if err := domain.ValidateCategory(t.Category); err != nil {
		return err
	}
	return h.types.Register(ctx, t)
}

func (h *TypeHandler) ListTypes(ctx context.Context) ([]domain.DictionaryType, error) {
	return h.types.List(ctx)
}
