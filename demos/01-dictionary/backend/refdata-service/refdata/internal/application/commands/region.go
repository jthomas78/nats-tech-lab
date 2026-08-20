package commands

import (
	"context"
	"errors"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata/internal/domain"
)

// RegionInput is the payload for registering a region (BR-D46). CountryCode
// is not optional — BR-D47 makes the parent country part of what a region
// *is*, so it is a field on the create payload rather than a follow-up call.
type RegionInput struct {
	Context     string
	Code        string
	CountryCode string
	Name        string
}

// RegionHandler executes region-corpus commands (BR-D46-BR-D48).
//
// A region is an ordinary DictionaryItem plus a `country` reference, so this
// handler composes ItemHandler and ReferenceHandler rather than reaching
// past them — the generic rules (BR-D01 uniqueness, BR-D05 reference
// integrity, BR-D22 charset) stay enforced in exactly one place.
type RegionHandler struct {
	items domain.ItemRepository
	refs  domain.ReferenceRepository

	itemH *ItemHandler
	refH  *ReferenceHandler
}

func NewRegionHandler(items domain.ItemRepository, refs domain.ReferenceRepository, notifier domain.ChangeNotifier) *RegionHandler {
	return &RegionHandler{
		items: items,
		refs:  refs,
		itemH: NewItemHandler(items, refs, notifier),
		refH:  NewReferenceHandler(items, refs, notifier),
	}
}

// RegisterRegion creates a region item and its country relation together.
//
// The country is validated *before* the item is written. Nothing here spans
// two repositories transactionally, and the failure modes are not
// symmetric: an item created ahead of a refused reference is a region with
// no country, which is precisely the state BR-D47 exists to forbid and is
// indistinguishable from one after the fact. Checking first turns the
// common failure into a clean rejection that writes nothing.
func (h *RegionHandler) RegisterRegion(ctx context.Context, in RegionInput) (domain.DictionaryItem, error) {
	if err := domain.ValidateCode(in.Code); err != nil {
		return domain.DictionaryItem{}, err
	}
	if err := domain.ValidateRegionCountry(in.Code, in.CountryCode); err != nil {
		return domain.DictionaryItem{}, err
	}

	// BR-D05's target checks, applied up front for the orphan reason above.
	// CreateReference re-applies them below; that repetition is deliberate,
	// since this pre-check is about write ordering and the one inside
	// ReferenceHandler is the rule's actual enforcement point.
	country, err := h.items.Get(ctx, domain.CountryTypeKey, in.Context, in.CountryCode)
	if errors.Is(err, domain.ErrItemNotFound) {
		return domain.DictionaryItem{}, domain.ErrReferenceTargetNotFound
	}
	if err != nil {
		return domain.DictionaryItem{}, err
	}
	if country.Status != domain.StatusActive {
		return domain.DictionaryItem{}, domain.ErrReferenceTargetNotActive
	}

	attrs := map[string]any{}
	if in.Name != "" {
		attrs["name"] = in.Name
	}

	item, err := h.itemH.RegisterItem(ctx, ItemInput{
		TypeKey: domain.RegionTypeKey,
		Code:    in.Code,
		Context: in.Context,
		Attrs:   attrs,
	})
	if err != nil {
		return domain.DictionaryItem{}, err
	}

	if err := h.refH.CreateReference(ctx, ReferenceInput{
		Context:            in.Context,
		FromTypeKey:        domain.RegionTypeKey,
		FromCode:           in.Code,
		Relation:           domain.RegionCountryRelation,
		DeclaredTargetType: domain.CountryTypeKey,
		ToTypeKey:          domain.CountryTypeKey,
		ToCode:             in.CountryCode,
	}); err != nil {
		// The pre-check above makes this path narrow (a concurrent
		// deprecation of the country, or a repository fault), but a region
		// left without its country would violate BR-D47 silently. Undo the
		// item rather than leave one behind; the delete is best-effort and
		// the original error is what the caller needs to see.
		_ = h.itemH.DeleteItem(ctx, domain.RegionTypeKey, in.Context, in.Code)
		return domain.DictionaryItem{}, err
	}

	return item, nil
}
