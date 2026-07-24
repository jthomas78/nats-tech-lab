package domain

import "context"

// TranslationDraftInput is one item/locale's AI-translation request context
// (BR-D07). SourceLabel/SourceDescription are the item's existing text in
// SourceLocale, given to the model as the thing to translate.
type TranslationDraftInput struct {
	TypeKey           string
	Code              string
	Context           string
	SourceLocale      string
	SourceLabel       string
	SourceDescription string
	TargetLocale      string
}

// TranslationDraft is a candidate label/description for TargetLocale. It is
// never persisted by the drafter itself — only returned for a human to
// review and explicitly save (BR-D07).
type TranslationDraft struct {
	Locale      string
	Label       string
	Description string
}

// TranslationDrafter produces a candidate translation for one item/locale.
// Implementations must not mutate any state — drafting is read-only; saving
// a draft is a separate, explicit step (BR-D07). Kept as a port so the
// application layer stays decoupled from any concrete model API.
type TranslationDrafter interface {
	Draft(ctx context.Context, in TranslationDraftInput) (TranslationDraft, error)
}
