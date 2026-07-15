package commands

import (
	"context"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/refdata-service/refdata/internal/domain"
)

// LocalizationInput is the payload for setting an item's label/description
// in one locale.
type LocalizationInput struct {
	TypeKey     string
	Code        string
	Context     string
	Locale      string
	Label       string
	Description string
}

// ResolvedItem is an item paired with its BR-D03-resolved localization.
type ResolvedItem struct {
	Item         domain.DictionaryItem
	Localization domain.Localization
}

// LocalizationHandler executes localization and locale-management commands.
type LocalizationHandler struct {
	items    domain.ItemRepository
	locs     domain.LocalizationRepository
	locales  domain.LocaleRepository
	notifier domain.ChangeNotifier // optional — nil skips cache/change-event notification
}

func NewLocalizationHandler(items domain.ItemRepository, locs domain.LocalizationRepository, locales domain.LocaleRepository, notifier domain.ChangeNotifier) *LocalizationHandler {
	return &LocalizationHandler{items: items, locs: locs, locales: locales, notifier: notifier}
}

// SetLocalization upserts a label/description for an item in one locale.
// The item must already exist.
func (h *LocalizationHandler) SetLocalization(ctx context.Context, in LocalizationInput) error {
	if _, err := h.items.Get(ctx, in.TypeKey, in.Context, in.Code); err != nil {
		return err
	}
	if err := h.locs.Upsert(ctx, domain.Localization{
		TypeKey: in.TypeKey, Code: in.Code, Context: in.Context,
		Locale: in.Locale, Label: in.Label, Description: in.Description,
		Source: "manual",
	}); err != nil {
		return err
	}
	if h.notifier == nil {
		return nil
	}
	return h.notifier.NotifyItemChanged(ctx, in.Context, in.TypeKey, in.Code)
}

// ResolveItem returns an item together with its BR-D03-resolved label for
// requestedLocale. Works for deprecated items too — BR-D06's "still resolves
// on read" applies regardless of locale.
func (h *LocalizationHandler) ResolveItem(ctx context.Context, typeKey, itemContext, code, requestedLocale string) (ResolvedItem, error) {
	item, err := h.items.Get(ctx, typeKey, itemContext, code)
	if err != nil {
		return ResolvedItem{}, err
	}
	locs, err := h.locs.ListForItem(ctx, typeKey, itemContext, code)
	if err != nil {
		return ResolvedItem{}, err
	}
	defaultLocale, err := h.DefaultLocale(ctx, itemContext)
	if err != nil {
		return ResolvedItem{}, err
	}
	resolved := domain.ResolveLabel(typeKey, code, itemContext, requestedLocale, defaultLocale, locs)
	return ResolvedItem{Item: item, Localization: resolved}, nil
}

// ListForItem returns every locale's localization recorded for one item —
// the editor's "Localizations" tab.
func (h *LocalizationHandler) ListForItem(ctx context.Context, typeKey, itemContext, code string) ([]domain.Localization, error) {
	return h.locs.ListForItem(ctx, typeKey, itemContext, code)
}

// AddLocale registers a locale as known for a context, optionally marking it
// the default (there is at most one default per context).
func (h *LocalizationHandler) AddLocale(ctx context.Context, itemContext, locale string, isDefault bool) error {
	return h.locales.Add(ctx, itemContext, locale, isDefault)
}

func (h *LocalizationHandler) ListLocales(ctx context.Context, itemContext string) ([]string, error) {
	return h.locales.List(ctx, itemContext)
}

// DefaultLocale reports the context's effective default locale: the one
// marked default, or en when none is marked (BR-D15).
func (h *LocalizationHandler) DefaultLocale(ctx context.Context, itemContext string) (string, error) {
	marked, err := h.locales.Default(ctx, itemContext)
	if err != nil {
		return "", err
	}
	return domain.EffectiveDefaultLocale(marked), nil
}

// Completeness reports how many of a type's items have a localization for
// locale, out of the total.
func (h *LocalizationHandler) Completeness(ctx context.Context, typeKey, itemContext, locale string) (total, localized int, err error) {
	items, err := h.items.List(ctx, typeKey, itemContext)
	if err != nil {
		return 0, 0, err
	}
	count, err := h.locs.CountLocalized(ctx, typeKey, itemContext, locale)
	if err != nil {
		return 0, 0, err
	}
	return len(items), count, nil
}
