package commands

import (
	"context"
	"errors"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/refdata-service/refdata/internal/domain"
)

// ReferenceInput is the payload for linking two dictionary items. Relation
// declares its own target type (DeclaredTargetType) independently of the
// concrete ToTypeKey being linked, so a mismatch is itself a rule violation
// rather than an ambiguous lookup miss.
type ReferenceInput struct {
	Context            string
	FromTypeKey        string
	FromCode           string
	Relation           string
	DeclaredTargetType string
	ToTypeKey          string
	ToCode             string
}

// ReferenceHandler executes reference commands.
type ReferenceHandler struct {
	items    domain.ItemRepository
	refs     domain.ReferenceRepository
	notifier domain.ChangeNotifier // optional — nil skips cache/change-event notification
}

func NewReferenceHandler(items domain.ItemRepository, refs domain.ReferenceRepository, notifier domain.ChangeNotifier) *ReferenceHandler {
	return &ReferenceHandler{items: items, refs: refs, notifier: notifier}
}

// CreateReference links two items. BR-D05: the target must be of the
// relation's declared type, must exist, and must be active.
func (h *ReferenceHandler) CreateReference(ctx context.Context, in ReferenceInput) error {
	if in.ToTypeKey != in.DeclaredTargetType {
		return domain.ErrReferenceTargetWrongType
	}

	target, err := h.items.Get(ctx, in.ToTypeKey, in.Context, in.ToCode)
	if errors.Is(err, domain.ErrItemNotFound) {
		return domain.ErrReferenceTargetNotFound
	}
	if err != nil {
		return err
	}
	if target.Status != domain.StatusActive {
		return domain.ErrReferenceTargetNotActive
	}

	ref := domain.DictionaryReference{
		Context:     in.Context,
		FromTypeKey: in.FromTypeKey,
		FromCode:    in.FromCode,
		Relation:    in.Relation,
		ToTypeKey:   in.ToTypeKey,
		ToCode:      in.ToCode,
	}
	if err := h.refs.Create(ctx, ref); err != nil {
		return err
	}
	if h.notifier == nil {
		return nil
	}
	// The FROM item's cache entry carries its outbound references, so a new
	// reference is a change to the FROM item's type set (not the target's).
	return h.notifier.NotifyItemChanged(ctx, in.Context, in.FromTypeKey, in.FromCode)
}

// Expand resolves the item a named relation points to — e.g. a country's
// "defaultCurrency" reference expanded to the currency item itself.
func (h *ReferenceHandler) Expand(ctx context.Context, itemContext, fromTypeKey, fromCode, relation string) (domain.DictionaryItem, error) {
	ref, err := h.refs.Get(ctx, itemContext, fromTypeKey, fromCode, relation)
	if err != nil {
		return domain.DictionaryItem{}, err
	}
	return h.items.Get(ctx, ref.ToTypeKey, itemContext, ref.ToCode)
}

// ListFrom returns every outbound reference recorded from one item — the
// editor's "References" tab.
func (h *ReferenceHandler) ListFrom(ctx context.Context, itemContext, fromTypeKey, fromCode string) ([]domain.DictionaryReference, error) {
	return h.refs.ListFrom(ctx, itemContext, fromTypeKey, fromCode)
}
