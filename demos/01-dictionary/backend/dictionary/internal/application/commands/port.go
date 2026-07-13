package commands

import (
	"context"
	"fmt"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/dictionary/internal/domain"
)

// PortHandler manages the ports reference table. Ports are plain Postgres-
// backed master data, not an event-sourced aggregate — registering one is a
// direct write, not a JetStream publish, because a port has no lifecycle
// worth replaying; it exists only to be looked up by Ship/Container commands
// enforcing BR-017/BR-018.
type PortHandler struct {
	repo domain.PortRepository
}

func NewPortHandler(repo domain.PortRepository) *PortHandler {
	return &PortHandler{repo: repo}
}

func (h *PortHandler) Register(ctx context.Context, kvContext, name string) error {
	if kvContext == "" {
		return fmt.Errorf("context is required")
	}
	if name == "" {
		return fmt.Errorf("name is required")
	}
	return h.repo.Register(ctx, kvContext, name)
}

func (h *PortHandler) List(ctx context.Context, kvContext string) ([]string, error) {
	return h.repo.List(ctx, kvContext)
}
