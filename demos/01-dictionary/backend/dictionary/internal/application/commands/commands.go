// Package commands holds the write-side use cases. Commands validate input
// and publish events to JetStream; all state is derived downstream by the
// event handlers (Shape A: KV, Shape B: Postgres + KV).
package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/dictionary/internal/domain"
)

// Publisher is the outbound port to the event backbone.
type Publisher interface {
	Publish(ctx context.Context, subject string, data []byte) error
}

// EntryInput carries the caller-supplied fields shared by create and update.
type EntryInput struct {
	Context    string         `json:"context"`
	EntityType string         `json:"entityType"`
	ID         string         `json:"id"`
	Label      string         `json:"label"`
	Attributes map[string]any `json:"attributes,omitempty"`
}

func (in EntryInput) toEntry(now time.Time) domain.DictionaryEntry {
	return domain.DictionaryEntry{
		Context:    in.Context,
		EntityType: in.EntityType,
		ID:         in.ID,
		Label:      in.Label,
		Attributes: in.Attributes,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
}

// Handler executes CreateEntry and UpdateEntry commands.
type Handler struct {
	pub Publisher
}

func NewHandler(pub Publisher) *Handler {
	return &Handler{pub: pub}
}

func (h *Handler) CreateEntry(ctx context.Context, in EntryInput) (domain.DictionaryEntry, error) {
	return h.publish(ctx, in, domain.SubjectEntryCreated)
}

func (h *Handler) UpdateEntry(ctx context.Context, in EntryInput) (domain.DictionaryEntry, error) {
	return h.publish(ctx, in, domain.SubjectEntryUpdated)
}

func (h *Handler) publish(ctx context.Context, in EntryInput, subject string) (domain.DictionaryEntry, error) {
	now := time.Now().UTC()
	entry := in.toEntry(now)
	if err := entry.Validate(); err != nil {
		return domain.DictionaryEntry{}, err
	}
	payload, err := json.Marshal(domain.EntryEvent{Entry: entry, OccurredAt: now})
	if err != nil {
		return domain.DictionaryEntry{}, fmt.Errorf("marshal event: %w", err)
	}
	if err := h.pub.Publish(ctx, subject, payload); err != nil {
		return domain.DictionaryEntry{}, err
	}
	return entry, nil
}
