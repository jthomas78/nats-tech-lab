package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/domain"
)

// The trusted-publishers table, written through the same shape as the plugin
// document: one transaction against a locked revision row, a server-assigned
// revision, a read-back from inside that transaction, and an audit row for
// every write whether it applied or not (BR-AS38).
//
// The shape is deliberately reproduced rather than shared. Apply and
// ApplyPublisher key on different counters and mutate different tables; the
// only thing they have in common is the discipline, and a generic writer
// abstracting three statements would make that discipline harder to read
// rather than easier to keep.

// auditScopePublisher marks the audit rows whose revision belongs to the
// trust table's counter rather than the plugin document's.
const auditScopePublisher = "publisher"

// Publishers returns the whole trust table at its own revision.
func (s *Store) Publishers(ctx context.Context) (domain.PublisherDocument, error) {
	return currentPublishers(ctx, s.db)
}

func currentPublishers(ctx context.Context, q querier) (domain.PublisherDocument, error) {
	var revision int64
	if err := q.QueryRowContext(ctx, `SELECT revision FROM registry.publisher_revision WHERE only_row`).Scan(&revision); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			revision = domain.NoRevision
		} else {
			return domain.PublisherDocument{}, err
		}
	}
	doc := domain.PublisherDocument{Revision: revision, Publishers: []domain.Publisher{}}

	byID := map[string]*domain.Publisher{}
	rows, err := q.QueryContext(ctx, `SELECT id, name FROM registry.publishers ORDER BY id`)
	if err != nil {
		return domain.PublisherDocument{}, err
	}
	defer rows.Close()
	order := []string{}
	for rows.Next() {
		var p domain.Publisher
		if err := rows.Scan(&p.ID, &p.Name); err != nil {
			return domain.PublisherDocument{}, err
		}
		p.Keys, p.Plugins = []domain.PublisherKey{}, []string{}
		byID[p.ID] = &p
		order = append(order, p.ID)
	}
	if err := rows.Err(); err != nil {
		return domain.PublisherDocument{}, err
	}

	keyRows, err := q.QueryContext(ctx,
		`SELECT publisher_id, public_key, state, added_at, changed_at FROM registry.publisher_keys ORDER BY public_key`)
	if err != nil {
		return domain.PublisherDocument{}, err
	}
	defer keyRows.Close()
	for keyRows.Next() {
		var owner string
		var k domain.PublisherKey
		var added, changed sql.NullTime
		if err := keyRows.Scan(&owner, &k.PublicKey, &k.State, &added, &changed); err != nil {
			return domain.PublisherDocument{}, err
		}
		k.AddedAt, k.ChangedAt = stamp(added), stamp(changed)
		if p, ok := byID[owner]; ok {
			p.Keys = append(p.Keys, k)
		}
	}
	if err := keyRows.Err(); err != nil {
		return domain.PublisherDocument{}, err
	}

	ownerRows, err := q.QueryContext(ctx, `SELECT publisher_id, plugin_id FROM registry.plugin_owners ORDER BY plugin_id`)
	if err != nil {
		return domain.PublisherDocument{}, err
	}
	defer ownerRows.Close()
	for ownerRows.Next() {
		var owner, pluginID string
		if err := ownerRows.Scan(&owner, &pluginID); err != nil {
			return domain.PublisherDocument{}, err
		}
		if p, ok := byID[owner]; ok {
			p.Plugins = append(p.Plugins, pluginID)
		}
	}
	if err := ownerRows.Err(); err != nil {
		return domain.PublisherDocument{}, err
	}

	for _, id := range order {
		doc.Publishers = append(doc.Publishers, *byID[id])
	}
	domain.SortPublishers(doc.Publishers)
	return doc, nil
}

func stamp(t sql.NullTime) string {
	if !t.Valid {
		return ""
	}
	return t.Time.UTC().Format("2006-01-02T15:04:05Z")
}

// ApplyPublisher performs one curated change to the trust table.
func (s *Store) ApplyPublisher(ctx context.Context, w domain.PublisherWrite) (domain.PublisherDocument, error) {
	if err := w.Validate(); err != nil {
		s.auditPublisherRefusal(ctx, w, err)
		return domain.PublisherDocument{}, err
	}
	doc, err := s.applyPublisher(ctx, w)
	if err != nil {
		s.auditPublisherRefusal(ctx, w, err)
		return domain.PublisherDocument{}, err
	}
	return doc, nil
}

func (s *Store) applyPublisher(ctx context.Context, w domain.PublisherWrite) (domain.PublisherDocument, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.PublisherDocument{}, err
	}
	defer tx.Rollback()

	var current int64
	if err := tx.QueryRowContext(ctx, `SELECT revision FROM registry.publisher_revision WHERE only_row FOR UPDATE`).Scan(&current); err != nil {
		return domain.PublisherDocument{}, err
	}
	if err := domain.CheckRevision(current, w.IfRevision); err != nil {
		return domain.PublisherDocument{}, err
	}

	switch w.Op {
	case domain.OpPublisherUpsert:
		name := ""
		if w.Publisher != nil {
			name = w.Publisher.Name
		}
		// A name-less upsert creates the row and leaves an existing name
		// alone, so "make sure this publisher exists" is a safe write.
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO registry.publishers (id, name) VALUES ($1, $2)
			 ON CONFLICT (id) DO UPDATE SET name = COALESCE(NULLIF(EXCLUDED.name, ''), registry.publishers.name), updated_at = now()`,
			w.PublisherID, name); err != nil {
			return domain.PublisherDocument{}, err
		}
	case domain.OpPublisherAddKey:
		if err := requirePublisher(ctx, tx, w.PublisherID); err != nil {
			return domain.PublisherDocument{}, err
		}
		// A key belongs to exactly one publisher. Re-adding a publisher's own
		// key is a no-op; claiming somebody else's is refused, because that
		// would silently move trust between identities.
		res, err := tx.ExecContext(ctx,
			`INSERT INTO registry.publisher_keys (public_key, publisher_id, state) VALUES ($1, $2, $3)
			 ON CONFLICT (public_key) DO UPDATE SET public_key = registry.publisher_keys.public_key
			 WHERE registry.publisher_keys.publisher_id = $2`,
			w.PublicKey, w.PublisherID, domain.KeyEnabled)
		if err != nil {
			return domain.PublisherDocument{}, err
		}
		if n, err := res.RowsAffected(); err == nil && n == 0 {
			return domain.PublisherDocument{}, fmt.Errorf("registry: that key is already held by another publisher")
		}
	case domain.OpPublisherSetKeyState:
		res, err := tx.ExecContext(ctx,
			`UPDATE registry.publisher_keys SET state = $3, changed_at = now() WHERE public_key = $1 AND publisher_id = $2`,
			w.PublicKey, w.PublisherID, w.KeyState)
		if err != nil {
			return domain.PublisherDocument{}, err
		}
		if n, err := res.RowsAffected(); err == nil && n == 0 {
			return domain.PublisherDocument{}, fmt.Errorf("%w: %q", domain.ErrNoPublisherKey, w.PublisherID)
		}
	case domain.OpPublisherTransfer:
		if err := requirePublisher(ctx, tx, w.PublisherID); err != nil {
			return domain.PublisherDocument{}, err
		}
		// Ownership only. A transfer never touches a key: the two are
		// separate tables precisely so this write cannot reach one of them.
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO registry.plugin_owners (plugin_id, publisher_id) VALUES ($1, $2)
			 ON CONFLICT (plugin_id) DO UPDATE SET publisher_id = EXCLUDED.publisher_id, updated_at = now()`,
			w.PluginID, w.PublisherID); err != nil {
			return domain.PublisherDocument{}, err
		}
	}

	// A revocation reaches the plugin document, in this same transaction
	// (decision 104, BR-AS38). Two transactions would leave a window where
	// trust is withdrawn but the code it signed is still being served, and
	// that window is the whole thing a revocation exists to close.
	if w.Op == domain.OpPublisherSetKeyState && w.KeyState == domain.KeyRevoked {
		if err := withholdInTx(ctx, tx, w); err != nil {
			return domain.PublisherDocument{}, err
		}
	}

	next := domain.NextRevision(current)
	if _, err := tx.ExecContext(ctx, `UPDATE registry.publisher_revision SET revision = $1, updated_at = now() WHERE only_row`, next); err != nil {
		return domain.PublisherDocument{}, err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO registry.audit (revision, op, entry_id, actor, outcome, scope) VALUES ($1, $2, $3, $4, $5, $6)`,
		next, w.Op, w.Subject(), w.Actor, domain.AuditAccepted, auditScopePublisher); err != nil {
		return domain.PublisherDocument{}, err
	}
	doc, err := currentPublishers(ctx, tx)
	if err != nil {
		return domain.PublisherDocument{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.PublisherDocument{}, err
	}
	return doc, nil
}

// withholdInTx takes every entry the revoked key signed out of service, under
// the plugin document's own revision lock, and leaves exactly one audit row
// naming the key and the entries that followed from it.
//
// A revocation that touched nothing moves no revision. Bumping it would make
// every connected shell re-read a document that did not change, and a
// publisher's first key is routinely revoked before it has signed anything.
//
// Lock order is publisher_revision then registry.revision, always — the
// announce path takes only the second, so nothing can hold them the other way
// round and deadlock.
func withholdInTx(ctx context.Context, tx *sql.Tx, w domain.PublisherWrite) error {
	var current int64
	if err := tx.QueryRowContext(ctx, `SELECT revision FROM registry.revision WHERE only_row FOR UPDATE`).Scan(&current); err != nil {
		return err
	}
	ids, err := withholdKey(ctx, tx, w.PublicKey)
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}
	next := domain.NextRevision(current)
	if _, err := tx.ExecContext(ctx, `UPDATE registry.revision SET revision = $1, updated_at = now() WHERE only_row`, next); err != nil {
		return err
	}
	detail := "key revoked; withheld " + strings.Join(ids, ", ")
	_, err = tx.ExecContext(ctx,
		`INSERT INTO registry.audit (revision, op, entry_id, actor, outcome, detail, scope) VALUES ($1, $2, $3, $4, $5, $6, 'registry')`,
		next, domain.OpWithholdKey, w.PublicKey, w.Actor, domain.AuditAccepted, detail)
	return err
}

func requirePublisher(ctx context.Context, q querier, id string) error {
	var found string
	err := q.QueryRowContext(ctx, `SELECT id FROM registry.publishers WHERE id = $1`, id).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: %q", domain.ErrNoPublisherRow, id)
	}
	return err
}

// auditPublisherRefusal records a trust-table write that was not applied.
// Best effort and detached from the caller's context, for the reasons
// auditRefusal gives.
func (s *Store) auditPublisherRefusal(ctx context.Context, w domain.PublisherWrite, cause error) {
	actor, op := w.Actor, w.Op
	if actor == "" {
		actor = "unknown"
	}
	if op == "" {
		op = "unknown"
	}
	_, _ = s.db.ExecContext(context.WithoutCancel(ctx),
		`INSERT INTO registry.audit (revision, op, entry_id, actor, outcome, detail, scope) VALUES (NULL, $1, $2, $3, $4, $5, $6)`,
		op, w.Subject(), actor, domain.AuditRefused, cause.Error(), auditScopePublisher)
}
