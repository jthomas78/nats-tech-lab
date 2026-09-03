package postgres

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/domain"
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
	rows, err := q.QueryContext(ctx, `SELECT id, enabled, lifecycle, withheld, withdrawn, release, approved_backend_services, entry, manifest, signature, signing_key FROM registry.entries ORDER BY id`)
	if err != nil {
		return domain.Document{}, err
	}
	defer rows.Close()

	doc := domain.Document{SchemaVersion: domain.SchemaVersion, Revision: revision, Entries: []domain.Entry{}}
	for rows.Next() {
		var id, lifecycle, manifest, signature, signingKey string
		var enabled, withheld, withdrawn bool
		var release int64
		var raw, approved []byte
		if err := rows.Scan(&id, &enabled, &lifecycle, &withheld, &withdrawn, &release, &approved, &raw, &manifest, &signature, &signingKey); err != nil {
			return domain.Document{}, err
		}
		e, err := entryOf(id, raw, manifest, signature, signingKey)
		if err != nil {
			return domain.Document{}, err
		}
		// The columns win over the document body for the facts the
		// columns own, so a hand-edited row can't disagree with itself.
		e.ID = id
		e.Enabled = enabled
		e.Lifecycle = lifecycle
		e.Withheld = withheld
		e.Withdrawn = withdrawn
		// Zero is "this row predates the column", and then the projection or
		// the manifest is all there is to go on.
		if release > 0 {
			e.Release = release
		}
		// SQL NULL is "no operator has answered", which is the same nil the
		// domain reads as not configured — so a row predating the column and
		// a row nobody has curated for health say the same true thing. An
		// empty array is a real answer and survives as one.
		e.ApprovedBackendServices = nil
		if approved != nil {
			var list []string
			if err := json.Unmarshal(approved, &list); err != nil {
				return domain.Document{}, fmt.Errorf("registry: entry %q has an unreadable backend approval: %w", id, err)
			}
			e.ApprovedBackendServices = list
		}
		doc.Entries = append(doc.Entries, e)
	}
	return doc, rows.Err()
}

// entryOf builds one entry from its row.
//
// A signed row is assembled from the manifest bytes, never from the `entry`
// column: the column is a projection this service derived, and re-serving a
// derived shape is the failure BR-AS37 names. The projection column stays for
// query and display, and for the rows nobody signed it is all there is.
func entryOf(id string, raw []byte, manifest, signature, signingKey string) (domain.Entry, error) {
	if manifest != "" {
		blob, err := base64.StdEncoding.DecodeString(manifest)
		if err != nil {
			return domain.Entry{}, fmt.Errorf("registry: entry %q has an unreadable manifest: %w", id, err)
		}
		e, err := domain.EntryFromManifest(blob, signature, signingKey)
		if err != nil {
			return domain.Entry{}, fmt.Errorf("registry: entry %q has an unreadable manifest: %w", id, err)
		}
		// The announcement stamps are the store's own record of when it saw
		// this publisher, not something the publisher signed — signedContent
		// excludes them for exactly that reason. They live in the projection
		// column, so they are carried across rather than lost with the rest
		// of it.
		var projected domain.Entry
		if err := json.Unmarshal(raw, &projected); err == nil {
			e.AnnouncedAt = projected.AnnouncedAt
			e.LastAnnouncedAt = projected.LastAnnouncedAt
		}
		return e, nil
	}
	var e domain.Entry
	if err := json.Unmarshal(raw, &e); err != nil {
		return domain.Entry{}, fmt.Errorf("registry: entry %q is not readable: %w", id, err)
	}
	return e, nil
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
	// BR-AS48 — trust is re-read here, under the revision lock, not back
	// where the signature was checked. Between those two points a key can be
	// revoked, and the announcement it signed must not land after that.
	if err := requireKeyEnabled(ctx, tx, w.RequireKeyEnabled); err != nil {
		return domain.Document{}, err
	}

	switch w.Op {
	case domain.OpUpsert:
		// Lifecycle is lifted for Admin filtering/sorting, not duplicated in
		// JSON. currentDoc overlays the column even on legacy JSON bodies.
		entry := *w.Entry
		entry.Lifecycle = ""
		// Lifted to its own column for the same reason as lifecycle, and
		// stripped from the projection so the two cannot disagree.
		var approved []byte
		if w.Entry.ApprovedBackendServices != nil {
			approved, err = json.Marshal(w.Entry.ApprovedBackendServices)
			if err != nil {
				return domain.Document{}, err
			}
		}
		entry.ApprovedBackendServices = nil
		// The manifest goes to its own column, so it is not also embedded in
		// the projection where JSONB would quietly rewrite it.
		var manifest, signature, signingKey string
		if entry.Signed() {
			manifest = base64.StdEncoding.EncodeToString(entry.Manifest.Bytes)
			signature = entry.Manifest.Signature
			signingKey = entry.Manifest.SigningKey
		}
		entry.Manifest = nil
		body, err := json.Marshal(entry)
		if err != nil {
			return domain.Document{}, err
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO registry.entries (id, enabled, entry, lifecycle, manifest, signature, signing_key, withdrawn, release, approved_backend_services) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
			 ON CONFLICT (id) DO UPDATE SET enabled = EXCLUDED.enabled, entry = EXCLUDED.entry, lifecycle = EXCLUDED.lifecycle, manifest = EXCLUDED.manifest, signature = EXCLUDED.signature, signing_key = EXCLUDED.signing_key, withdrawn = EXCLUDED.withdrawn, release = EXCLUDED.release, approved_backend_services = EXCLUDED.approved_backend_services, updated_at = now()`,
			w.EntryID, w.Entry.Enabled, body, w.Entry.Lifecycle, manifest, signature, signingKey, w.Entry.Withdrawn, w.Entry.Release, approved); err != nil {
			return domain.Document{}, err
		}
	case domain.OpSetEnabled:
		// No insert: enabling something that was never curated would be a
		// plugin announcing itself by the back door (BR-AS21).
		//
		// Enabling clears the withheld mark, and only enabling does. That is
		// the deliberate half of BR-AS38: an operator putting a withheld entry
		// back is looking at that entry, which is exactly what re-enabling the
		// key was not.
		//
		// It clears the publisher's withdrawal for the same reason (BR-AS55):
		// approval outranks availability, and an operator re-enabling a
		// withdrawn entry is looking at that entry and saying run it.
		//
		// Disabling still says nothing about the publisher, but for a DYNAMIC
		// entry it withdraws the plugin itself (BR-AS54): a running shell has
		// to be told to take the UI away, and a marker is the only way to say
		// that (absence is what an outage looks like). Three conditions, each
		// load-bearing: NOT $2 because enabling is the opposite move; enabled
		// because an entry no operator ever approved was never running and
		// needs nothing said about it; lifecycle = 'dynamic' because BR-AS53
		// says a static plugin's contributions keep running and are offered a
		// reload instead — that one leaves the document by the Enabled check.
		res, err := tx.ExecContext(ctx,
			`UPDATE registry.entries SET enabled = $2, withheld = withheld AND NOT $2, withdrawn = (withdrawn OR (NOT $2 AND enabled AND lifecycle = 'dynamic')) AND NOT $2, updated_at = now() WHERE id = $1`, w.EntryID, w.Enabled)
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
// withholdKey takes every entry a key signed out of service and reports which
// ids it touched, in id order. The selection rule lives in the domain; this
// runs it against the rows inside the caller's transaction.
func withholdKey(ctx context.Context, tx *sql.Tx, publicKey string) ([]string, error) {
	doc, err := currentDoc(ctx, tx)
	if err != nil {
		return nil, err
	}
	ids := domain.RevocationEffect(doc.Entries, publicKey)
	if len(ids) == 0 {
		return ids, nil
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE registry.entries SET enabled = false, withheld = true, updated_at = now() WHERE signing_key = $1`,
		publicKey); err != nil {
		return nil, err
	}
	return ids, nil
}

// requireKeyEnabled re-reads one publisher key's state inside the caller's
// transaction. A key that has vanished is refused as untrusted, not as
// missing: to a publisher the two are the same answer.
func requireKeyEnabled(ctx context.Context, tx *sql.Tx, publicKey string) error {
	if publicKey == "" {
		return nil
	}
	var state string
	err := tx.QueryRowContext(ctx, `SELECT state FROM registry.publisher_keys WHERE public_key = $1`, publicKey).Scan(&state)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ErrKeyNotTrusted
	}
	if err != nil {
		return err
	}
	switch state {
	case domain.KeyEnabled:
		return nil
	case domain.KeyRetired:
		return domain.ErrKeyRetired
	case domain.KeyRevoked:
		return domain.ErrKeyRevoked
	}
	return domain.ErrKeyNotTrusted
}

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

// RecordIgnored durably records an announcement that domain decided must not
// change a static entry. The revision lock ensures that decision still refers
// to the entry read by the handler. No registry revision is consumed.
func (s *Store) RecordIgnored(ctx context.Context, w domain.Write) error {
	if err := w.Validate(); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var revision int64
	if err := tx.QueryRowContext(ctx, `SELECT revision FROM registry.revision WHERE only_row FOR UPDATE`).Scan(&revision); err != nil {
		return err
	}
	if err := domain.CheckRevision(revision, w.IfRevision); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO registry.audit (revision, op, entry_id, actor, outcome, detail) VALUES (NULL, 'announce', $1, $2, $3, $4)`,
		w.EntryID, w.Actor, domain.AnnounceIgnored, domain.IgnoredAnnouncementDetail); err != nil {
		return err
	}
	return tx.Commit()
}

/*
AdvanceRelease moves one entry's release watermark and touches nothing
else (BR-AS73, decision 10). It reports whether the row actually moved.

This is the write convergence costs, and its safety rests on three
deliberate choices about concurrency — the review asked for exactly this
to be checked against the entry row's concurrency control.

FIRST, it takes the same revision lock every other write takes, so it
serialises against a real announce rather than interleaving with one.

SECOND, it does NOT check IfRevision, and it takes none. Optimistic
concurrency asks "is the document still what I read?", and this write
asserts nothing about the document — it ratchets one integer. During a
reset storm the revision moves constantly for unrelated plugins, so a
revision check here would refuse hundreds of correct watermark advances
for a change that had nothing to do with them.

THIRD, and this is what makes it unable to lose a concurrent real
announce: the UPDATE is guarded by `release < $2` and sets `release`
ALONE. A real announce that won the lock first has already stored a
higher release along with its new content; this statement then matches no
row and does nothing. It can never move content backwards, because it
never writes content — the columns are not in the statement.

No revision is consumed and no audit row is written. That is the whole
point: the expensive halves of a write are what convergence skips.
*/
func (s *Store) AdvanceRelease(ctx context.Context, entryID string, release int64, signingKey string) (bool, error) {
	if entryID == "" {
		return false, domain.ErrNoEntryID
	}
	if release <= 0 {
		return false, domain.ErrNoRelease
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()

	var revision int64
	if err := tx.QueryRowContext(ctx, `SELECT revision FROM registry.revision WHERE only_row FOR UPDATE`).Scan(&revision); err != nil {
		return false, err
	}
	// BR-AS48 applies here as much as to any other announcement-driven
	// write: a key revoked between the signature check and this moment must
	// not get its release number onto the record.
	if err := requireKeyEnabled(ctx, tx, signingKey); err != nil {
		return false, err
	}
	res, err := tx.ExecContext(ctx,
		`UPDATE registry.entries SET release = $2, updated_at = now() WHERE id = $1 AND release < $2`, entryID, release)
	if err != nil {
		return false, err
	}
	moved, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return moved > 0, nil
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
