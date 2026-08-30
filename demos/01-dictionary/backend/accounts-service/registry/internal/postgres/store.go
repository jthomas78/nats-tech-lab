package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/accounts-service/registry/internal/domain"
)

// Store is the registry's source of truth.
//
// Its whole interface is Current and Apply (decision 35). Revision
// assignment, the origin check, the audit append and the stale-write refusal
// all happen behind those two calls, in one transaction, because every one of
// them is only correct relative to the revision the write is keyed on.
type Store struct {
	db        *sql.DB
	allowlist domain.Allowlist
}

func NewStore(db *sql.DB, allowlist domain.Allowlist) *Store {
	return &Store{db: db, allowlist: allowlist}
}

// Current returns the whole stored document, entries and revision together.
// Filtering for a reader is the caller's step (domain.Document.Readable):
// the admin surface has to see a disabled or newly non-conforming entry in
// order to fix it, and the shell must not.
func (s *Store) Current(ctx context.Context) (domain.Document, error) {
	return currentDoc(ctx, s.db)
}

// querier is the intersection of *sql.DB and *sql.Tx that current needs.
//
// It exists so a write can read back the document it installed from INSIDE
// its own transaction (decision 49). Reading it afterwards on the request
// context looked equivalent and was not: the commit had already happened, so
// a client that hung up in that window made Apply return an error for a
// durably applied write — audited as a refusal, answered 500, and the cache
// refresh and the change notification skipped, leaving every shell holding a
// revision the database no longer had.
type querier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

func currentDoc(ctx context.Context, q querier) (domain.Document, error) {
	var revision int64
	if err := q.QueryRowContext(ctx, `SELECT revision FROM registry.revision WHERE only_row`).Scan(&revision); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			revision = domain.NoRevision
		} else {
			return domain.Document{}, err
		}
	}
	rows, err := q.QueryContext(ctx, `SELECT id, enabled, entry FROM registry.entries ORDER BY id`)
	if err != nil {
		return domain.Document{}, err
	}
	defer rows.Close()

	doc := domain.Document{SchemaVersion: domain.SchemaVersion, Revision: revision, Entries: []domain.Entry{}}
	for rows.Next() {
		var id string
		var enabled bool
		var raw []byte
		if err := rows.Scan(&id, &enabled, &raw); err != nil {
			return domain.Document{}, err
		}
		var e domain.Entry
		if err := json.Unmarshal(raw, &e); err != nil {
			return domain.Document{}, fmt.Errorf("registry: entry %q is not readable: %w", id, err)
		}
		// The columns win over the document body for the two facts the
		// columns own, so a hand-edited row can't disagree with itself.
		e.ID = id
		e.Enabled = enabled
		doc.Entries = append(doc.Entries, e)
	}
	return doc, rows.Err()
}

// Apply performs one curated write and returns the document it installed.
//
// Everything happens in one transaction against a locked revision row, so a
// second writer either waits or is refused — never merged (BR-AS18). A
// refusal is audited on its own connection, after the rollback, because a
// rolled-back audit row is no audit at all — and every error path below is a
// path that did NOT commit, which is the property that makes auditing them
// all as refusals true (decision 49).
func (s *Store) Apply(ctx context.Context, w domain.Write) (domain.Document, error) {
	if err := w.Validate(); err != nil {
		s.auditRefusal(ctx, w, err)
		return domain.Document{}, err
	}
	// The origin check is a rule about the entry, so it runs before any
	// database work and refuses without naming the URL (BR-AS04, BR-AS20).
	if w.Op == domain.OpUpsert {
		if err := s.allowlist.Check(*w.Entry); err != nil {
			s.auditRefusal(ctx, w, err)
			return domain.Document{}, err
		}
	}

	doc, err := s.apply(ctx, w)
	if err != nil {
		s.auditRefusal(ctx, w, err)
		return domain.Document{}, err
	}
	return doc, nil
}

func (s *Store) apply(ctx context.Context, w domain.Write) (domain.Document, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Document{}, err
	}
	defer tx.Rollback()

	var current int64
	if err := tx.QueryRowContext(ctx, `SELECT revision FROM registry.revision WHERE only_row FOR UPDATE`).Scan(&current); err != nil {
		return domain.Document{}, err
	}
	if err := domain.CheckRevision(current, w.IfRevision); err != nil {
		return domain.Document{}, err
	}

	switch w.Op {
	case domain.OpUpsert:
		body, err := json.Marshal(w.Entry)
		if err != nil {
			return domain.Document{}, err
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO registry.entries (id, enabled, entry) VALUES ($1, $2, $3)
			 ON CONFLICT (id) DO UPDATE SET enabled = EXCLUDED.enabled, entry = EXCLUDED.entry, updated_at = now()`,
			w.EntryID, w.Entry.Enabled, body); err != nil {
			return domain.Document{}, err
		}
	case domain.OpSetEnabled:
		// No insert: enabling something that was never curated would be a
		// plugin announcing itself by the back door (BR-AS21).
		res, err := tx.ExecContext(ctx,
			`UPDATE registry.entries SET enabled = $2, updated_at = now() WHERE id = $1`, w.EntryID, w.Enabled)
		if err != nil {
			return domain.Document{}, err
		}
		if n, err := res.RowsAffected(); err == nil && n == 0 {
			return domain.Document{}, fmt.Errorf("registry: no curated entry %q", w.EntryID)
		}
	}

	next := domain.NextRevision(current)
	if _, err := tx.ExecContext(ctx, `UPDATE registry.revision SET revision = $1, updated_at = now() WHERE only_row`, next); err != nil {
		return domain.Document{}, err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO registry.audit (revision, op, entry_id, actor, outcome) VALUES ($1, $2, $3, $4, $5)`,
		next, w.Op, w.EntryID, w.Actor, domain.AuditAccepted); err != nil {
		return domain.Document{}, err
	}
	// Read back before the commit, not after. Inside the transaction this is
	// the document the write installs, under the same revision lock that
	// decided it — so it cannot race a later writer, and it cannot be lost to
	// a cancellation that arrives once the write is already durable.
	doc, err := currentDoc(ctx, tx)
	if err != nil {
		return domain.Document{}, err
	}
	// The last statement that can fail. Every error path above this line
	// rolled back, which is what lets Apply audit them all as refusals.
	if err := tx.Commit(); err != nil {
		return domain.Document{}, err
	}
	return doc, nil
}

// auditRefusal records a write that was not applied. Best effort by design:
// a failure to audit a refusal must not turn into a second, different error
// on the caller's path, where it would say less than the real one.
func (s *Store) auditRefusal(ctx context.Context, w domain.Write, cause error) {
	actor := w.Actor
	if actor == "" {
		actor = "unknown"
	}
	op := w.Op
	if op == "" {
		op = "unknown"
	}
	// Detached from the caller's context: the audit row is the record that a
	// write was refused, and a client hanging up is one of the ways a write
	// gets refused. Cancelling the audit with the request would lose exactly
	// the entries an operator most needs.
	_, _ = s.db.ExecContext(context.WithoutCancel(ctx),
		`INSERT INTO registry.audit (revision, op, entry_id, actor, outcome, detail) VALUES (NULL, $1, $2, $3, $4, $5)`,
		op, w.EntryID, actor, domain.AuditRefused, cause.Error())
}

// AuditPage is one recorded write, for the admin surface.
type AuditPage struct {
	Revision *int64 `json:"revision"`
	Op       string `json:"op"`
	EntryID  string `json:"entryId"`
	Actor    string `json:"actor"`
	Outcome  string `json:"outcome"`
	Detail   string `json:"detail,omitempty"`
	At       string `json:"at"`
}

// Audit returns the most recent writes, newest first.
func (s *Store) Audit(ctx context.Context, limit int) ([]AuditPage, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT revision, op, entry_id, actor, outcome, detail, created_at
		 FROM registry.audit ORDER BY id DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []AuditPage{}
	for rows.Next() {
		var a AuditPage
		var rev sql.NullInt64
		var at sql.NullTime
		if err := rows.Scan(&rev, &a.Op, &a.EntryID, &a.Actor, &a.Outcome, &a.Detail, &at); err != nil {
			return nil, err
		}
		if rev.Valid {
			v := rev.Int64
			a.Revision = &v
		}
		if at.Valid {
			a.At = at.Time.UTC().Format("2006-01-02T15:04:05Z")
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
