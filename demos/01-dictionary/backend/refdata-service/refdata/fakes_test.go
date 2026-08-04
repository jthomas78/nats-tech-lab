package refdata_test

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata/internal/domain"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata/internal/kvcache"
)

// newTestNamespaces builds a BR-D31 key-namespace resolver over a type
// registry seeded with the given types. A type absent from the registry
// resolves unnamespaced, so a suite that only exercises standards-category
// types can pass none at all.
func newTestNamespaces(types ...domain.DictionaryType) *kvcache.TypeNamespaces {
	repo := newFakeTypeRepo()
	for _, t := range types {
		_ = repo.Register(context.Background(), t)
	}
	return kvcache.NewTypeNamespaces(repo)
}

type fakeItemRepo struct {
	mu    sync.Mutex
	items map[string]domain.DictionaryItem
}

type fakeContextRepo struct{ contexts map[string]domain.Context }

func newFakeContextRepo() *fakeContextRepo {
	return &fakeContextRepo{contexts: map[string]domain.Context{}}
}
func (r *fakeContextRepo) Register(_ context.Context, value domain.Context) error {
	r.contexts[value.Context] = value
	return nil
}
func (r *fakeContextRepo) Get(_ context.Context, key string) (domain.Context, error) {
	value, ok := r.contexts[key]
	if !ok {
		return domain.Context{}, domain.ErrContextNotFound
	}
	return value, nil
}
func (r *fakeContextRepo) List(_ context.Context) ([]domain.Context, error) {
	out := make([]domain.Context, 0, len(r.contexts))
	for _, value := range r.contexts {
		out = append(out, value)
	}
	return out, nil
}
func (r *fakeContextRepo) ListByTenant(_ context.Context, tenant string) ([]domain.Context, error) {
	out := make([]domain.Context, 0, len(r.contexts))
	for _, value := range r.contexts {
		if value.Tenant == tenant || value.Tenant == "" {
			out = append(out, value)
		}
	}
	return out, nil
}
func (r *fakeContextRepo) Ancestors(_ context.Context, key string) ([]domain.Context, error) {
	value, err := r.Get(context.Background(), key)
	if err != nil {
		return nil, err
	}
	out := []domain.Context{value}
	for value.Parent != "" {
		value, err = r.Get(context.Background(), value.Parent)
		if err != nil {
			return nil, err
		}
		out = append(out, value)
	}
	return out, nil
}
func (r *fakeContextRepo) Descendants(_ context.Context, key string) ([]domain.Context, error) {
	_, err := r.Get(context.Background(), key)
	return nil, err
}
func (r *fakeContextRepo) SetVisible(_ context.Context, key string, _ bool) error {
	if _, ok := r.contexts[key]; !ok {
		return domain.ErrContextNotFound
	}
	return nil
}

func newFakeItemRepo() *fakeItemRepo {
	return &fakeItemRepo{items: make(map[string]domain.DictionaryItem)}
}

func (r *fakeItemRepo) key(typeKey, itemContext, code string) string {
	return itemContext + "|" + typeKey + "|" + code
}

func (r *fakeItemRepo) Exists(_ context.Context, typeKey, itemContext, code string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.items[r.key(typeKey, itemContext, code)]
	return ok, nil
}

func (r *fakeItemRepo) Create(_ context.Context, item domain.DictionaryItem) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.items[r.key(item.TypeKey, item.Context, item.Code)] = item
	return nil
}

func (r *fakeItemRepo) Get(_ context.Context, typeKey, itemContext, code string) (domain.DictionaryItem, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.items[r.key(typeKey, itemContext, code)]
	if !ok {
		return domain.DictionaryItem{}, domain.ErrItemNotFound
	}
	return item, nil
}

func (r *fakeItemRepo) List(_ context.Context, typeKey, itemContext string) ([]domain.DictionaryItem, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []domain.DictionaryItem
	for _, item := range r.items {
		if item.TypeKey == typeKey && item.Context == itemContext {
			out = append(out, item)
		}
	}
	return out, nil
}

func (r *fakeItemRepo) Deprecate(_ context.Context, typeKey, itemContext, code string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	k := r.key(typeKey, itemContext, code)
	item, ok := r.items[k]
	if !ok {
		return domain.ErrItemNotFound
	}
	item.Status = domain.StatusDeprecated
	r.items[k] = item
	return nil
}

func (r *fakeItemRepo) Reactivate(_ context.Context, typeKey, itemContext, code string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	k := r.key(typeKey, itemContext, code)
	item, ok := r.items[k]
	if !ok {
		return domain.ErrItemNotFound
	}
	item.Status = domain.StatusActive
	r.items[k] = item
	return nil
}

func (r *fakeItemRepo) UpdateAttrs(_ context.Context, typeKey, itemContext, code string, attrs map[string]any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	k := r.key(typeKey, itemContext, code)
	item, ok := r.items[k]
	if !ok {
		return domain.ErrItemNotFound
	}
	item.Attrs = attrs
	r.items[k] = item
	return nil
}

func (r *fakeItemRepo) Delete(_ context.Context, typeKey, itemContext, code string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.items, r.key(typeKey, itemContext, code))
	return nil
}

type fakeTypeRepo struct {
	mu    sync.Mutex
	types map[string]domain.DictionaryType
}

func newFakeTypeRepo() *fakeTypeRepo {
	return &fakeTypeRepo{types: make(map[string]domain.DictionaryType)}
}

func (r *fakeTypeRepo) Register(_ context.Context, t domain.DictionaryType) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.types[t.TypeKey] = t
	return nil
}

func (r *fakeTypeRepo) Get(_ context.Context, typeKey string) (domain.DictionaryType, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	t, ok := r.types[typeKey]
	if !ok {
		return domain.DictionaryType{}, domain.ErrTypeNotFound
	}
	return t, nil
}

func (r *fakeTypeRepo) List(_ context.Context) ([]domain.DictionaryType, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]domain.DictionaryType, 0, len(r.types))
	for _, t := range r.types {
		out = append(out, t)
	}
	return out, nil
}

type fakeReferenceRepo struct {
	mu   sync.Mutex
	refs map[string]domain.DictionaryReference
}

func newFakeReferenceRepo() *fakeReferenceRepo {
	return &fakeReferenceRepo{refs: make(map[string]domain.DictionaryReference)}
}

func (r *fakeReferenceRepo) Create(_ context.Context, ref domain.DictionaryReference) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := ref.Context + "|" + ref.FromTypeKey + "|" + ref.FromCode + "|" + ref.Relation
	r.refs[key] = ref
	return nil
}

func (r *fakeReferenceRepo) IsReferenced(_ context.Context, typeKey, itemContext, code string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, ref := range r.refs {
		if ref.Context == itemContext && ref.ToTypeKey == typeKey && ref.ToCode == code {
			return true, nil
		}
	}
	return false, nil
}

func (r *fakeReferenceRepo) Get(_ context.Context, itemContext, fromTypeKey, fromCode, relation string) (domain.DictionaryReference, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := itemContext + "|" + fromTypeKey + "|" + fromCode + "|" + relation
	ref, ok := r.refs[key]
	if !ok {
		return domain.DictionaryReference{}, domain.ErrReferenceNotFound
	}
	return ref, nil
}

func (r *fakeReferenceRepo) ListFrom(_ context.Context, itemContext, fromTypeKey, fromCode string) ([]domain.DictionaryReference, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []domain.DictionaryReference
	for _, ref := range r.refs {
		if ref.Context == itemContext && ref.FromTypeKey == fromTypeKey && ref.FromCode == fromCode {
			out = append(out, ref)
		}
	}
	return out, nil
}

type fakeLocalizationRepo struct {
	mu   sync.Mutex
	locs map[string][]domain.Localization
}

func newFakeLocalizationRepo() *fakeLocalizationRepo {
	return &fakeLocalizationRepo{locs: make(map[string][]domain.Localization)}
}

func (r *fakeLocalizationRepo) key(typeKey, itemContext, code string) string {
	return itemContext + "|" + typeKey + "|" + code
}

func (r *fakeLocalizationRepo) Upsert(_ context.Context, loc domain.Localization) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	k := r.key(loc.TypeKey, loc.Context, loc.Code)
	existing := r.locs[k]
	for i, l := range existing {
		if l.Locale == loc.Locale {
			existing[i] = loc
			r.locs[k] = existing
			return nil
		}
	}
	r.locs[k] = append(existing, loc)
	return nil
}

func (r *fakeLocalizationRepo) Get(_ context.Context, typeKey, itemContext, code, locale string) (domain.Localization, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, l := range r.locs[r.key(typeKey, itemContext, code)] {
		if l.Locale == locale {
			return l, nil
		}
	}
	return domain.Localization{}, domain.ErrLocalizationNotFound
}

func (r *fakeLocalizationRepo) ListForItem(_ context.Context, typeKey, itemContext, code string) ([]domain.Localization, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]domain.Localization(nil), r.locs[r.key(typeKey, itemContext, code)]...), nil
}

func (r *fakeLocalizationRepo) CountLocalized(_ context.Context, typeKey, itemContext, locale string) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	prefix := itemContext + "|" + typeKey + "|"
	count := 0
	for k, locs := range r.locs {
		if !strings.HasPrefix(k, prefix) {
			continue
		}
		for _, l := range locs {
			if l.Locale == locale {
				count++
				break
			}
		}
	}
	return count, nil
}

type fakeLocaleRepo struct {
	mu       sync.Mutex
	locales  map[string]map[string]bool
	defaults map[string]string
}

func newFakeLocaleRepo() *fakeLocaleRepo {
	return &fakeLocaleRepo{locales: make(map[string]map[string]bool), defaults: make(map[string]string)}
}

func (r *fakeLocaleRepo) Add(_ context.Context, itemContext, locale string, isDefault bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.locales[itemContext] == nil {
		r.locales[itemContext] = make(map[string]bool)
	}
	r.locales[itemContext][locale] = true
	if isDefault {
		r.defaults[itemContext] = locale
	}
	return nil
}

func (r *fakeLocaleRepo) List(_ context.Context, itemContext string) ([]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.locales[itemContext]))
	for l := range r.locales[itemContext] {
		out = append(out, l)
	}
	return out, nil
}

func (r *fakeLocaleRepo) Default(_ context.Context, itemContext string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.defaults[itemContext], nil
}

type fakeVersionRepo struct {
	mu       sync.Mutex
	versions map[string]int
}

func newFakeVersionRepo() *fakeVersionRepo {
	return &fakeVersionRepo{versions: make(map[string]int)}
}

func (r *fakeVersionRepo) key(itemContext, typeKey string) string { return itemContext + "|" + typeKey }

func (r *fakeVersionRepo) Bump(_ context.Context, itemContext, typeKey string) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	k := r.key(itemContext, typeKey)
	r.versions[k]++
	return r.versions[k], nil
}

func (r *fakeVersionRepo) Current(_ context.Context, itemContext, typeKey string) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.versions[r.key(itemContext, typeKey)], nil
}

// fakeNotifier is a domain.ChangeNotifier spy — counts calls so a test can
// assert a no-op write skipped the notification.
type fakeNotifier struct {
	mu    sync.Mutex
	calls int
}

func newFakeNotifier() *fakeNotifier { return &fakeNotifier{} }

func (n *fakeNotifier) NotifyItemChanged(_ context.Context, _, _, _ string) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.calls++
	return nil
}

func (n *fakeNotifier) callCount() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.calls
}

// fakeTranslationDrafter is a domain.TranslationDrafter spy — records each
// call's input in order and tracks the highest number of overlapping calls,
// so a test can assert BR-D24's "sequential, never concurrent" guarantee. An
// optional per-call latency makes an accidental concurrent refactor
// detectable (with zero latency, calls never overlap even if the caller did
// fan out, since goroutine scheduling wouldn't reliably interleave them).
type fakeTranslationDrafter struct {
	mu          sync.Mutex
	calls       []domain.TranslationDraftInput
	inFlight    int
	maxInFlight int
	latency     time.Duration
	errByLocale map[string]error
}

func newFakeTranslationDrafter() *fakeTranslationDrafter {
	return &fakeTranslationDrafter{errByLocale: make(map[string]error)}
}

// failLocale makes the next (and every subsequent) Draft call for locale
// return err instead of a successful draft.
func (d *fakeTranslationDrafter) failLocale(locale string, err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.errByLocale[locale] = err
}

func (d *fakeTranslationDrafter) Draft(_ context.Context, in domain.TranslationDraftInput) (domain.TranslationDraft, error) {
	d.mu.Lock()
	d.calls = append(d.calls, in)
	d.inFlight++
	if d.inFlight > d.maxInFlight {
		d.maxInFlight = d.inFlight
	}
	latency, callErr := d.latency, d.errByLocale[in.TargetLocale]
	d.mu.Unlock()

	if latency > 0 {
		time.Sleep(latency)
	}

	d.mu.Lock()
	d.inFlight--
	d.mu.Unlock()

	if callErr != nil {
		return domain.TranslationDraft{}, callErr
	}
	return domain.TranslationDraft{
		Locale:      in.TargetLocale,
		Label:       in.SourceLabel + " (" + in.TargetLocale + ")",
		Description: in.SourceDescription,
	}, nil
}

func (d *fakeTranslationDrafter) callLocales() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]string, len(d.calls))
	for i, c := range d.calls {
		out[i] = c.TargetLocale
	}
	return out
}

func (d *fakeTranslationDrafter) maxConcurrent() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.maxInFlight
}
