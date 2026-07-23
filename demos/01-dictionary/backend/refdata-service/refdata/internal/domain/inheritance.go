package domain

// CorpusItem is the flattened, versioned representation of an item. The
// source fields retain how it became visible without making reads traverse a
// context chain.
type CorpusItem struct {
	DictionaryItem
	SourceContext string `json:"sourceContext"`
	IsOverride    bool   `json:"isOverride"`
}

func corpusKey(item DictionaryItem) string { return item.TypeKey + "\x00" + item.Code }

// FlattenCorpus overlays local items from root through child. localByContext
// contains only locally-authored rows: inherited rows must never be copied
// into a child's overlay, otherwise a later parent change could not propagate.
func FlattenCorpus(chainChildToRoot []string, localByContext map[string][]DictionaryItem) []CorpusItem {
	flattened := map[string]CorpusItem{}
	for i := len(chainChildToRoot) - 1; i >= 0; i-- {
		context := chainChildToRoot[i]
		for _, item := range localByContext[context] {
			key := corpusKey(item)
			_, replacesAncestor := flattened[key]
			item.Context = context
			flattened[key] = CorpusItem{DictionaryItem: item, SourceContext: context, IsOverride: replacesAncestor}
		}
	}
	out := make([]CorpusItem, 0, len(flattened))
	for _, item := range flattened {
		out = append(out, item)
	}
	return out
}

// CanDeleteLocalItem protects inherited rows. A child has no local row for an
// inherited item, so deletion is rejected; it may create an override instead.
func CanDeleteLocalItem(item CorpusItem, deletingContext string) error {
	if item.SourceContext != deletingContext {
		return ErrCannotDeleteInheritedItem
	}
	return nil
}

// CorpusLocalization is the flattened, versioned representation of a single
// item-locale pair. Localizations flow with their item down the inheritance
// chain (resolved open question 3): a child may override one locale for an
// item it did not itself author, without overriding the item.
type CorpusLocalization struct {
	Localization
	SourceContext string `json:"sourceContext"`
	IsOverride    bool   `json:"isOverride"`
}

func localizationKey(loc Localization) string {
	return loc.TypeKey + "\x00" + loc.Code + "\x00" + loc.Locale
}

// FlattenLocalizations overlays local localizations from root through child,
// keyed per (item, locale) rather than per item — so a child overriding one
// locale of an inherited item leaves every other locale of that item
// (whether inherited or locally authored elsewhere) untouched.
func FlattenLocalizations(chainChildToRoot []string, localByContext map[string][]Localization) []CorpusLocalization {
	flattened := map[string]CorpusLocalization{}
	for i := len(chainChildToRoot) - 1; i >= 0; i-- {
		context := chainChildToRoot[i]
		for _, loc := range localByContext[context] {
			key := localizationKey(loc)
			_, replacesAncestor := flattened[key]
			loc.Context = context
			flattened[key] = CorpusLocalization{Localization: loc, SourceContext: context, IsOverride: replacesAncestor}
		}
	}
	out := make([]CorpusLocalization, 0, len(flattened))
	for _, loc := range flattened {
		out = append(out, loc)
	}
	return out
}
