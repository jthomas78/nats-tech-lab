package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/refdata-service/refdata/internal/domain"
)

// CorpusRepository persists drafts and published immutable snapshots. Every
// publish-relevant write is transaction-bound so a partial corpus cannot be
// observed (BR-V03).
type CorpusRepository struct{ db *sql.DB }

func NewCorpusRepository(db *sql.DB) *CorpusRepository { return &CorpusRepository{db: db} }

// CreateDraft re-flattens the context's full inheritance chain on every new
// draft (resolved Q1: the working tables, not the previous corpus_items row
// set, are the always-current source for this context's own local edits).
// For each ancestor, only its own locally-authored rows (source_context ==
// that ancestor) are read from its latest published corpus — never an
// already-flattened row — so FlattenCorpus can correctly re-apply
// child-overrides-parent precedence without double-counting anything an
// ancestor itself inherited further up the chain.
func (r *CorpusRepository) CreateDraft(ctx context.Context, contextKey, notes string) (domain.CorpusVersion, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.CorpusVersion{}, err
	}
	defer tx.Rollback()

	chain, err := ancestorChainTx(ctx, tx, contextKey)
	if err != nil {
		return domain.CorpusVersion{}, err
	}

	var ownParent sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT version FROM refdata.corpus_versions WHERE context = $1 AND status = 'published' ORDER BY version DESC LIMIT 1`, contextKey).Scan(&ownParent); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return domain.CorpusVersion{}, err
	}
	var version int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) + 1 FROM refdata.corpus_versions WHERE context = $1`, contextKey).Scan(&version); err != nil {
		return domain.CorpusVersion{}, err
	}

	chainKeys := make([]string, len(chain))
	localItems := map[string][]domain.DictionaryItem{}
	localLocs := map[string][]domain.Localization{}
	var baseContextVersion sql.NullInt64

	for i, node := range chain {
		chainKeys[i] = node.Context
		if node.Context == contextKey {
			// The working tables are the sole, full-replace source for
			// content this context plainly authors itself (resolved Q1) — a
			// row deleted from the working table must disappear from the
			// next draft, not resurrect. The one thing the working tables
			// structurally cannot represent is an override of an item this
			// context did not itself author (their FK requires the item to
			// exist in this context's own dictionary_items) — those only
			// ever land via PutDraftItem/PutDraftLocalization, so they're
			// carried forward from what was locally published last time.
			var ownOverrideItems []domain.DictionaryItem
			var ownOverrideLocs []domain.Localization
			if ownParent.Valid {
				ownOverrideItems, err = fetchLocalCorpusOverrideItemsTx(ctx, tx, contextKey, int(ownParent.Int64))
				if err != nil {
					return domain.CorpusVersion{}, err
				}
				ownOverrideLocs, err = fetchLocalCorpusLocalizationOverridesTx(ctx, tx, contextKey, int(ownParent.Int64))
				if err != nil {
					return domain.CorpusVersion{}, err
				}
			}
			working, err := fetchWorkingItemsTx(ctx, tx, contextKey)
			if err != nil {
				return domain.CorpusVersion{}, err
			}
			workingLocs, err := fetchWorkingLocalizationsTx(ctx, tx, contextKey)
			if err != nil {
				return domain.CorpusVersion{}, err
			}
			localItems[contextKey] = mergeItems(ownOverrideItems, working)
			localLocs[contextKey] = mergeLocalizations(ownOverrideLocs, workingLocs)
			continue
		}
		var ancestorVersion sql.NullInt64
		if err := tx.QueryRowContext(ctx, `SELECT version FROM refdata.corpus_versions WHERE context = $1 AND status = 'published' ORDER BY version DESC LIMIT 1`, node.Context).Scan(&ancestorVersion); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return domain.CorpusVersion{}, err
		}
		if !ancestorVersion.Valid {
			continue // ancestor has never published a corpus; nothing to inherit from it yet
		}
		if i == 1 {
			baseContextVersion = ancestorVersion
		}
		items, err := fetchLocalCorpusItemsTx(ctx, tx, node.Context, int(ancestorVersion.Int64))
		if err != nil {
			return domain.CorpusVersion{}, err
		}
		locs, err := fetchLocalCorpusLocalizationsTx(ctx, tx, node.Context, int(ancestorVersion.Int64))
		if err != nil {
			return domain.CorpusVersion{}, err
		}
		localItems[node.Context], localLocs[node.Context] = items, locs
	}

	flattenedItems := domain.FlattenCorpus(chainKeys, localItems)
	flattenedLocs := domain.FlattenLocalizations(chainKeys, localLocs)

	if _, err := tx.ExecContext(ctx, `INSERT INTO refdata.corpus_versions (context, version, status, parent_version, base_context_version, notes) VALUES ($1, $2, 'draft', NULLIF($3, 0), NULLIF($4, 0), $5)`,
		contextKey, version, ownParent.Int64, baseContextVersion.Int64, notes); err != nil {
		// The partial unique index is the authoritative concurrent guard.
		return domain.CorpusVersion{}, mapDraftError(err)
	}

	for _, item := range flattenedItems {
		attrs, err := json.Marshal(item.Attrs)
		if err != nil {
			return domain.CorpusVersion{}, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO refdata.corpus_items (context, version, type_key, code, status, attrs, source_context, is_override) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
			contextKey, version, item.TypeKey, item.Code, item.Status, attrs, item.SourceContext, item.IsOverride); err != nil {
			return domain.CorpusVersion{}, err
		}
	}
	for _, loc := range flattenedLocs {
		if _, err := tx.ExecContext(ctx, `INSERT INTO refdata.corpus_localizations (context, version, type_key, code, locale, label, description, source, source_context) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
			contextKey, version, loc.TypeKey, loc.Code, loc.Locale, loc.Label, loc.Description, loc.Source, loc.SourceContext); err != nil {
			return domain.CorpusVersion{}, err
		}
	}

	if err := tx.Commit(); err != nil {
		return domain.CorpusVersion{}, err
	}
	result := domain.CorpusVersion{Context: contextKey, Version: version, Status: domain.CorpusDraft, Notes: notes}
	if ownParent.Valid {
		value := int(ownParent.Int64)
		result.ParentVersion = &value
	}
	if baseContextVersion.Valid {
		value := int(baseContextVersion.Int64)
		result.BaseContextVersion = &value
	}
	return result, nil
}

// mergeItems overlays working-table edits onto the context's own last
// locally-published rows; the working table wins on a shared key.
func mergeItems(published, working []domain.DictionaryItem) []domain.DictionaryItem {
	merged := map[string]domain.DictionaryItem{}
	for _, item := range published {
		merged[item.TypeKey+"\x00"+item.Code] = item
	}
	for _, item := range working {
		merged[item.TypeKey+"\x00"+item.Code] = item
	}
	out := make([]domain.DictionaryItem, 0, len(merged))
	for _, item := range merged {
		out = append(out, item)
	}
	return out
}

func mergeLocalizations(published, working []domain.Localization) []domain.Localization {
	merged := map[string]domain.Localization{}
	for _, loc := range published {
		merged[loc.TypeKey+"\x00"+loc.Code+"\x00"+loc.Locale] = loc
	}
	for _, loc := range working {
		merged[loc.TypeKey+"\x00"+loc.Code+"\x00"+loc.Locale] = loc
	}
	out := make([]domain.Localization, 0, len(merged))
	for _, loc := range merged {
		out = append(out, loc)
	}
	return out
}

// ancestorChainTx returns contextKey followed by each ancestor up to the
// root, ordered child-first — the precedence order FlattenCorpus expects.
// Duplicated from ContextRepository.Ancestors (rather than shared) because
// it must run inside CreateDraft's transaction for a consistent snapshot.
func ancestorChainTx(ctx context.Context, tx *sql.Tx, key string) ([]domain.Context, error) {
	rows, err := tx.QueryContext(ctx, `
		WITH RECURSIVE tree AS (
			SELECT context, parent, name, description, 0 AS depth FROM refdata.contexts WHERE context = $1
			UNION ALL
			SELECT c.context, c.parent, c.name, c.description, tree.depth + 1
			FROM refdata.contexts c JOIN tree ON c.context = tree.parent
			WHERE tree.depth < 100
		) SELECT context, COALESCE(parent, ''), name, description FROM tree ORDER BY depth`, key)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := []domain.Context{}
	for rows.Next() {
		var value domain.Context
		if err := rows.Scan(&value.Context, &value.Parent, &value.Name, &value.Description); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	if len(values) == 0 {
		return nil, domain.ErrContextNotFound
	}
	return values, rows.Err()
}

func fetchWorkingItemsTx(ctx context.Context, tx *sql.Tx, contextKey string) ([]domain.DictionaryItem, error) {
	rows, err := tx.QueryContext(ctx, `SELECT type_key, code, status, attrs FROM refdata.dictionary_items WHERE context = $1`, contextKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []domain.DictionaryItem{}
	for rows.Next() {
		var item domain.DictionaryItem
		var attrs []byte
		if err := rows.Scan(&item.TypeKey, &item.Code, &item.Status, &attrs); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(attrs, &item.Attrs); err != nil {
			return nil, err
		}
		item.Context = contextKey
		items = append(items, item)
	}
	return items, rows.Err()
}

func fetchWorkingLocalizationsTx(ctx context.Context, tx *sql.Tx, contextKey string) ([]domain.Localization, error) {
	rows, err := tx.QueryContext(ctx, `SELECT type_key, code, locale, label, description, source FROM refdata.dictionary_localizations WHERE context = $1`, contextKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	locs := []domain.Localization{}
	for rows.Next() {
		var loc domain.Localization
		if err := rows.Scan(&loc.TypeKey, &loc.Code, &loc.Locale, &loc.Label, &loc.Description, &loc.Source); err != nil {
			return nil, err
		}
		loc.Context = contextKey
		locs = append(locs, loc)
	}
	return locs, rows.Err()
}

// fetchLocalCorpusOverrideItemsTx returns contextKey's own is_override rows
// from its own latest local corpus — the override-of-an-inherited-item edits
// that only ever land via PutDraftItem, never via the working table.
func fetchLocalCorpusOverrideItemsTx(ctx context.Context, tx *sql.Tx, contextKey string, version int) ([]domain.DictionaryItem, error) {
	rows, err := tx.QueryContext(ctx, `SELECT type_key, code, status, attrs FROM refdata.corpus_items WHERE context = $1 AND version = $2 AND source_context = $1 AND is_override`, contextKey, version)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []domain.DictionaryItem{}
	for rows.Next() {
		var item domain.DictionaryItem
		var attrs []byte
		if err := rows.Scan(&item.TypeKey, &item.Code, &item.Status, &attrs); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(attrs, &item.Attrs); err != nil {
			return nil, err
		}
		item.Context = contextKey
		items = append(items, item)
	}
	return items, rows.Err()
}

// fetchLocalCorpusLocalizationOverridesTx returns locale overrides contextKey
// authored for an item it does NOT itself own (the item's own source_context
// differs from contextKey) — these only ever land via PutDraftLocalization,
// since the working table's FK requires the item to exist in the same
// context's own dictionary_items.
func fetchLocalCorpusLocalizationOverridesTx(ctx context.Context, tx *sql.Tx, contextKey string, version int) ([]domain.Localization, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT l.type_key, l.code, l.locale, l.label, l.description, l.source
		FROM refdata.corpus_localizations l
		JOIN refdata.corpus_items i ON i.context = l.context AND i.version = l.version AND i.type_key = l.type_key AND i.code = l.code
		WHERE l.context = $1 AND l.version = $2 AND l.source_context = $1 AND i.source_context <> $1`, contextKey, version)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	locs := []domain.Localization{}
	for rows.Next() {
		var loc domain.Localization
		if err := rows.Scan(&loc.TypeKey, &loc.Code, &loc.Locale, &loc.Label, &loc.Description, &loc.Source); err != nil {
			return nil, err
		}
		loc.Context = contextKey
		locs = append(locs, loc)
	}
	return locs, rows.Err()
}

// fetchLocalCorpusItemsTx returns only the rows an ancestor authored itself
// (source_context == the ancestor) from its latest published corpus —
// excluding anything that ancestor in turn inherited, which is picked up
// separately when that further ancestor is processed in the same chain walk.
func fetchLocalCorpusItemsTx(ctx context.Context, tx *sql.Tx, contextKey string, version int) ([]domain.DictionaryItem, error) {
	rows, err := tx.QueryContext(ctx, `SELECT type_key, code, status, attrs FROM refdata.corpus_items WHERE context = $1 AND version = $2 AND source_context = $1`, contextKey, version)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []domain.DictionaryItem{}
	for rows.Next() {
		var item domain.DictionaryItem
		var attrs []byte
		if err := rows.Scan(&item.TypeKey, &item.Code, &item.Status, &attrs); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(attrs, &item.Attrs); err != nil {
			return nil, err
		}
		item.Context = contextKey
		items = append(items, item)
	}
	return items, rows.Err()
}

func fetchLocalCorpusLocalizationsTx(ctx context.Context, tx *sql.Tx, contextKey string, version int) ([]domain.Localization, error) {
	rows, err := tx.QueryContext(ctx, `SELECT type_key, code, locale, label, description, source FROM refdata.corpus_localizations WHERE context = $1 AND version = $2 AND source_context = $1`, contextKey, version)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	locs := []domain.Localization{}
	for rows.Next() {
		var loc domain.Localization
		if err := rows.Scan(&loc.TypeKey, &loc.Code, &loc.Locale, &loc.Label, &loc.Description, &loc.Source); err != nil {
			return nil, err
		}
		loc.Context = contextKey
		locs = append(locs, loc)
	}
	return locs, rows.Err()
}

func (r *CorpusRepository) PutDraftItem(ctx context.Context, contextKey string, item domain.CorpusItem) error {
	var version int
	err := r.db.QueryRowContext(ctx, `SELECT version FROM refdata.corpus_versions WHERE context = $1 AND status = 'draft'`, contextKey).Scan(&version)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ErrDraftNotFound
	}
	if err != nil {
		return err
	}
	if item.SourceContext == "" {
		item.SourceContext = contextKey
	}
	attrs, err := json.Marshal(item.Attrs)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `INSERT INTO refdata.corpus_items (context, version, type_key, code, status, attrs, source_context, is_override)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (context, version, type_key, code) DO UPDATE SET status = EXCLUDED.status, attrs = EXCLUDED.attrs, source_context = EXCLUDED.source_context, is_override = EXCLUDED.is_override`,
		contextKey, version, item.TypeKey, item.Code, item.Status, attrs, item.SourceContext, item.IsOverride)
	return err
}

// PutDraftLocalization overrides a single item-locale pair on the current
// draft directly, without touching the item's own corpus_items row. This is
// the escape valve for overriding one locale of an item the context did not
// itself author (resolved Q3) — the working-table SetLocalization path
// cannot do this, since its FK requires the item to exist in the same
// context's own dictionary_items.
func (r *CorpusRepository) PutDraftLocalization(ctx context.Context, contextKey string, loc domain.CorpusLocalization) error {
	var version int
	err := r.db.QueryRowContext(ctx, `SELECT version FROM refdata.corpus_versions WHERE context = $1 AND status = 'draft'`, contextKey).Scan(&version)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ErrDraftNotFound
	}
	if err != nil {
		return err
	}
	if loc.SourceContext == "" {
		loc.SourceContext = contextKey
	}
	_, err = r.db.ExecContext(ctx, `INSERT INTO refdata.corpus_localizations (context, version, type_key, code, locale, label, description, source, source_context)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (context, version, type_key, code, locale) DO UPDATE SET label = EXCLUDED.label, description = EXCLUDED.description, source = EXCLUDED.source, source_context = EXCLUDED.source_context`,
		contextKey, version, loc.TypeKey, loc.Code, loc.Locale, loc.Label, loc.Description, loc.Source, loc.SourceContext)
	return err
}

func (r *CorpusRepository) Publish(ctx context.Context, contextKey string) (domain.CorpusVersion, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.CorpusVersion{}, err
	}
	defer tx.Rollback()
	var result domain.CorpusVersion
	err = tx.QueryRowContext(ctx, `UPDATE refdata.corpus_versions SET status = 'published', published_at = now() WHERE context = $1 AND status = 'draft' RETURNING version, notes`, contextKey).Scan(&result.Version, &result.Notes)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.CorpusVersion{}, domain.ErrDraftNotFound
	}
	if err != nil {
		return domain.CorpusVersion{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.CorpusVersion{}, err
	}
	result.Context, result.Status = contextKey, domain.CorpusPublished
	return result, nil
}

func (r *CorpusRepository) Rollback(ctx context.Context, contextKey string, targetVersion int, notes string) (domain.CorpusVersion, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.CorpusVersion{}, err
	}
	defer tx.Rollback()
	var targetStatus domain.CorpusStatus
	if err := tx.QueryRowContext(ctx, `SELECT status FROM refdata.corpus_versions WHERE context = $1 AND version = $2`, contextKey, targetVersion).Scan(&targetStatus); errors.Is(err, sql.ErrNoRows) {
		return domain.CorpusVersion{}, domain.ErrRollbackTargetNotPublic
	} else if err != nil {
		return domain.CorpusVersion{}, err
	}
	if err := domain.CanRollbackTo(domain.CorpusVersion{Status: targetStatus}); err != nil {
		return domain.CorpusVersion{}, err
	}
	var current int
	if err := tx.QueryRowContext(ctx, `SELECT version FROM refdata.corpus_versions WHERE context = $1 AND status = 'published' ORDER BY version DESC LIMIT 1`, contextKey).Scan(&current); err != nil {
		return domain.CorpusVersion{}, err
	}
	var next int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) + 1 FROM refdata.corpus_versions WHERE context = $1`, contextKey).Scan(&next); err != nil {
		return domain.CorpusVersion{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO refdata.corpus_versions (context, version, status, parent_version, notes, published_at) VALUES ($1, $2, 'published', $3, $4, now())`, contextKey, next, targetVersion, notes); err != nil {
		return domain.CorpusVersion{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO refdata.corpus_items (context, version, type_key, code, status, attrs, source_context, is_override) SELECT context, $3, type_key, code, status, attrs, source_context, is_override FROM refdata.corpus_items WHERE context = $1 AND version = $2`, contextKey, targetVersion, next); err != nil {
		return domain.CorpusVersion{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO refdata.corpus_localizations (context, version, type_key, code, locale, label, description, source, source_context) SELECT context, $3, type_key, code, locale, label, description, source, source_context FROM refdata.corpus_localizations WHERE context = $1 AND version = $2`, contextKey, targetVersion, next); err != nil {
		return domain.CorpusVersion{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE refdata.corpus_versions SET status = 'rolled-back', rolled_back_at = now(), rolled_back_by = $3 WHERE context = $1 AND version = $2`, contextKey, current, next); err != nil {
		return domain.CorpusVersion{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.CorpusVersion{}, err
	}
	return domain.CorpusVersion{Context: contextKey, Version: next, Status: domain.CorpusPublished, Notes: notes}, nil
}

func (r *CorpusRepository) Versions(ctx context.Context, contextKey string) ([]domain.CorpusVersion, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT context, version, status, parent_version, base_context_version, notes, rolled_back_by FROM refdata.corpus_versions WHERE context = $1 ORDER BY version DESC`, contextKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	versions := []domain.CorpusVersion{}
	for rows.Next() {
		version, err := scanCorpusVersion(rows)
		if err != nil {
			return nil, err
		}
		versions = append(versions, version)
	}
	return versions, rows.Err()
}

func (r *CorpusRepository) GetVersion(ctx context.Context, contextKey string, number int) (domain.CorpusVersion, error) {
	row := r.db.QueryRowContext(ctx, `SELECT context, version, status, parent_version, base_context_version, notes, rolled_back_by FROM refdata.corpus_versions WHERE context = $1 AND version = $2`, contextKey, number)
	version, err := scanCorpusVersion(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.CorpusVersion{}, domain.ErrContextNotFound
	}
	return version, err
}

func (r *CorpusRepository) ItemsAtVersion(ctx context.Context, contextKey string, version int) ([]domain.CorpusItem, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT type_key, code, status, attrs, source_context, is_override FROM refdata.corpus_items WHERE context = $1 AND version = $2 ORDER BY type_key, code`, contextKey, version)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []domain.CorpusItem{}
	for rows.Next() {
		var item domain.CorpusItem
		var attrs []byte
		if err := rows.Scan(&item.TypeKey, &item.Code, &item.Status, &attrs, &item.SourceContext, &item.IsOverride); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(attrs, &item.Attrs); err != nil {
			return nil, err
		}
		item.Context = contextKey
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *CorpusRepository) LocalizationsAtVersion(ctx context.Context, contextKey string, version int) ([]domain.CorpusLocalization, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT type_key, code, locale, label, description, source, source_context FROM refdata.corpus_localizations WHERE context = $1 AND version = $2 ORDER BY type_key, code, locale`, contextKey, version)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	locs := []domain.CorpusLocalization{}
	for rows.Next() {
		var loc domain.CorpusLocalization
		if err := rows.Scan(&loc.TypeKey, &loc.Code, &loc.Locale, &loc.Label, &loc.Description, &loc.Source, &loc.SourceContext); err != nil {
			return nil, err
		}
		loc.Context = contextKey
		loc.IsOverride = loc.SourceContext == contextKey
		locs = append(locs, loc)
	}
	return locs, rows.Err()
}

// Diff reports which (type_key, code) keys were added, removed, or changed
// between two versions of the same context's corpus — a plain list of
// changed keys, per the current scope for audit/diff visibility (§6.2).
func (r *CorpusRepository) Diff(ctx context.Context, contextKey string, fromVersion, toVersion int) ([]domain.CorpusDiffEntry, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT type_key, code, 'added' FROM refdata.corpus_items a
			WHERE a.context = $1 AND a.version = $3
			AND NOT EXISTS (SELECT 1 FROM refdata.corpus_items b WHERE b.context = $1 AND b.version = $2 AND b.type_key = a.type_key AND b.code = a.code)
		UNION
		SELECT type_key, code, 'removed' FROM refdata.corpus_items a
			WHERE a.context = $1 AND a.version = $2
			AND NOT EXISTS (SELECT 1 FROM refdata.corpus_items b WHERE b.context = $1 AND b.version = $3 AND b.type_key = a.type_key AND b.code = a.code)
		UNION
		SELECT a.type_key, a.code, 'changed' FROM refdata.corpus_items a
			JOIN refdata.corpus_items b ON b.context = a.context AND b.type_key = a.type_key AND b.code = a.code
			WHERE a.context = $1 AND a.version = $3 AND b.version = $2
			AND (a.attrs IS DISTINCT FROM b.attrs OR a.status IS DISTINCT FROM b.status)
		ORDER BY 1, 2`, contextKey, fromVersion, toVersion)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	entries := []domain.CorpusDiffEntry{}
	for rows.Next() {
		var entry domain.CorpusDiffEntry
		if err := rows.Scan(&entry.TypeKey, &entry.Code, &entry.Change); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

type corpusScanner interface{ Scan(...any) error }

func scanCorpusVersion(row corpusScanner) (domain.CorpusVersion, error) {
	var version domain.CorpusVersion
	var parent, base, rolled sql.NullInt64
	err := row.Scan(&version.Context, &version.Version, &version.Status, &parent, &base, &version.Notes, &rolled)
	if parent.Valid {
		value := int(parent.Int64)
		version.ParentVersion = &value
	}
	if base.Valid {
		value := int(base.Int64)
		version.BaseContextVersion = &value
	}
	if rolled.Valid {
		value := int(rolled.Int64)
		version.RolledBackBy = &value
	}
	return version, err
}

func mapDraftError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "one_refdata_draft_per_context" {
		return domain.ErrDraftAlreadyExists
	}
	return err
}
