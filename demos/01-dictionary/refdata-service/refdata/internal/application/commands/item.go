package commands

import (
	"context"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/refdata-service/refdata/internal/domain"
)

// ItemInput is the payload for registering a new dictionary item.
type ItemInput struct {
	TypeKey string
	Code    string
	Context string
	Attrs   map[string]any
}

// ItemHandler executes item lifecycle commands.
type ItemHandler struct {
	items    domain.ItemRepository
	refs     domain.ReferenceRepository
	notifier domain.ChangeNotifier // optional — nil skips cache/change-event notification
}

func NewItemHandler(items domain.ItemRepository, refs domain.ReferenceRepository, notifier domain.ChangeNotifier) *ItemHandler {
	return &ItemHandler{items: items, refs: refs, notifier: notifier}
}

func (h *ItemHandler) notify(ctx context.Context, typeKey, itemContext, code string) error {
	if h.notifier == nil {
		return nil
	}
	return h.notifier.NotifyItemChanged(ctx, itemContext, typeKey, code)
}

// RegisterItem creates a new item. BR-D01: the code must be free within its
// {type, context}.
func (h *ItemHandler) RegisterItem(ctx context.Context, in ItemInput) (domain.DictionaryItem, error) {
	exists, err := h.items.Exists(ctx, in.TypeKey, in.Context, in.Code)
	if err != nil {
		return domain.DictionaryItem{}, err
	}
	if exists {
		return domain.DictionaryItem{}, domain.ErrDuplicateItemCode
	}

	item := domain.DictionaryItem{
		TypeKey: in.TypeKey,
		Code:    in.Code,
		Context: in.Context,
		Status:  domain.StatusActive,
		Attrs:   in.Attrs,
	}
	if err := h.items.Create(ctx, item); err != nil {
		return domain.DictionaryItem{}, err
	}
	if err := h.notify(ctx, in.TypeKey, in.Context, in.Code); err != nil {
		return domain.DictionaryItem{}, err
	}
	return item, nil
}

// Get resolves an item regardless of status — BR-D06: deprecated items still
// resolve on direct read.
func (h *ItemHandler) Get(ctx context.Context, typeKey, itemContext, code string) (domain.DictionaryItem, error) {
	return h.items.Get(ctx, typeKey, itemContext, code)
}

// ListAssignable returns only active items — BR-D06: deprecated items are
// excluded from assignable-value listings by default.
func (h *ItemHandler) ListAssignable(ctx context.Context, typeKey, itemContext string) ([]domain.DictionaryItem, error) {
	items, err := h.items.List(ctx, typeKey, itemContext)
	if err != nil {
		return nil, err
	}
	return domain.FilterAssignable(items), nil
}

// ListAll returns every item regardless of status — for admin views that
// need to show deprecated items too (opt-in, unlike ListAssignable).
func (h *ItemHandler) ListAll(ctx context.Context, typeKey, itemContext string) ([]domain.DictionaryItem, error) {
	return h.items.List(ctx, typeKey, itemContext)
}

// DeprecateItem marks an item deprecated — the fallback for BR-D02 when an
// item is referenced and so cannot be hard-deleted.
func (h *ItemHandler) DeprecateItem(ctx context.Context, typeKey, itemContext, code string) error {
	if _, err := h.items.Get(ctx, typeKey, itemContext, code); err != nil {
		return err
	}
	if err := h.items.Deprecate(ctx, typeKey, itemContext, code); err != nil {
		return err
	}
	return h.notify(ctx, typeKey, itemContext, code)
}

// ReactivateItem flips a deprecated item back to active — BR-D12: reactivation
// is a plain status reversal, symmetric with DeprecateItem, with no
// restrictions on when or how many times it can be applied.
func (h *ItemHandler) ReactivateItem(ctx context.Context, typeKey, itemContext, code string) error {
	if _, err := h.items.Get(ctx, typeKey, itemContext, code); err != nil {
		return err
	}
	if err := h.items.Reactivate(ctx, typeKey, itemContext, code); err != nil {
		return err
	}
	return h.notify(ctx, typeKey, itemContext, code)
}

// DeleteItem hard-deletes an item. BR-D02: only unreferenced items may be
// hard-deleted; a referenced item must be deprecated instead.
func (h *ItemHandler) DeleteItem(ctx context.Context, typeKey, itemContext, code string) error {
	if _, err := h.items.Get(ctx, typeKey, itemContext, code); err != nil {
		return err
	}
	referenced, err := h.refs.IsReferenced(ctx, typeKey, itemContext, code)
	if err != nil {
		return err
	}
	if referenced {
		return domain.ErrItemReferenced
	}
	if err := h.items.Delete(ctx, typeKey, itemContext, code); err != nil {
		return err
	}
	return h.notify(ctx, typeKey, itemContext, code)
}
