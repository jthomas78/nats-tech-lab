package commands

import (
	"context"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata/internal/domain"
)

// TranslationHandler drafts AI-assisted translations for missing locales
// (BR-D07). Drafting never persists anything — saving a draft is a separate,
// explicit SetLocalization call made by the caller once a human accepts it.
type TranslationHandler struct {
	items   domain.ItemRepository
	locs    domain.LocalizationRepository
	locales domain.LocaleRepository
	drafter domain.TranslationDrafter
}

func NewTranslationHandler(items domain.ItemRepository, locs domain.LocalizationRepository, locales domain.LocaleRepository, drafter domain.TranslationDrafter) *TranslationHandler {
	return &TranslationHandler{items: items, locs: locs, locales: locales, drafter: drafter}
}

// DraftInput requests AI-drafted translations for one item across one or
// more target locales.
type DraftInput struct {
	TypeKey       string
	Code          string
	Context       string
	TargetLocales []string
}

// Draft is one target locale's outcome: either a candidate translation, or
// an Error message if the model call failed for that locale — a single
// locale's failure never aborts the others.
type Draft struct {
	Locale      string
	Label       string
	Description string
	Error       string
}

// DraftTranslations drafts a candidate label/description for each requested
// target locale, calling the model once per locale, strictly sequentially
// (BR-D24). Nothing is persisted here — saving is a separate SetLocalization
// call once a human accepts a draft (BR-D07).
func (h *TranslationHandler) DraftTranslations(ctx context.Context, in DraftInput) ([]Draft, error) {
	if _, err := h.items.Get(ctx, in.TypeKey, in.Context, in.Code); err != nil {
		return nil, err
	}
	defaultLocale, err := h.DefaultLocale(ctx, in.Context)
	if err != nil {
		return nil, err
	}
	sourceLocs, err := h.locs.ListForItem(ctx, in.TypeKey, in.Context, in.Code)
	if err != nil {
		return nil, err
	}
	source := domain.ResolveLabel(in.TypeKey, in.Code, in.Context, defaultLocale, defaultLocale, sourceLocs)

	drafts := make([]Draft, 0, len(in.TargetLocales))
	for _, target := range in.TargetLocales {
		draft, err := h.drafter.Draft(ctx, domain.TranslationDraftInput{
			TypeKey:           in.TypeKey,
			Code:              in.Code,
			Context:           in.Context,
			SourceLocale:      defaultLocale,
			SourceLabel:       source.Label,
			SourceDescription: source.Description,
			TargetLocale:      target,
		})
		if err != nil {
			drafts = append(drafts, Draft{Locale: target, Error: err.Error()})
			continue
		}
		drafts = append(drafts, Draft{Locale: target, Label: draft.Label, Description: draft.Description})
	}
	return drafts, nil
}

// DefaultLocale mirrors LocalizationHandler.DefaultLocale (BR-D15) — kept
// local so TranslationHandler doesn't need a LocalizationHandler reference.
func (h *TranslationHandler) DefaultLocale(ctx context.Context, itemContext string) (string, error) {
	marked, err := h.locales.Default(ctx, itemContext)
	if err != nil {
		return "", err
	}
	return domain.EffectiveDefaultLocale(marked), nil
}
