package postgres

import (
	"context"
	"database/sql"
	"errors"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/trading-partner-service/tradingpartner/internal/domain"
)

// ComplianceDocumentRepository is the Postgres adapter for
// domain.ComplianceDocumentRepository.
type ComplianceDocumentRepository struct{ db *sql.DB }

func NewComplianceDocumentRepository(db *sql.DB) *ComplianceDocumentRepository {
	return &ComplianceDocumentRepository{db: db}
}

// documentColumns is the shared SELECT list, so the scan order in
// scanDocument can't drift from it.
const documentColumns = `id, type, status, reference, expires_at, coverage_cents,
	file_name, file_content_type, file_size_bytes, file_object_name, file_uploaded_at`

func scanDocument(row interface {
	Scan(dest ...any) error
}) (domain.ComplianceDocument, error) {
	var doc domain.ComplianceDocument
	// BR-TP45: the five file columns are nullable as a group, so they are
	// scanned into nullables and folded into a single *DocumentFile — a
	// half-populated File would be a shape the domain has no rule for.
	var (
		fileName    sql.NullString
		contentType sql.NullString
		sizeBytes   sql.NullInt64
		objectName  sql.NullString
		uploadedAt  sql.NullTime
	)
	err := row.Scan(&doc.ID, &doc.Type, &doc.Status, &doc.Reference, &doc.ExpiresAt, &doc.CoverageCents,
		&fileName, &contentType, &sizeBytes, &objectName, &uploadedAt)
	if err != nil {
		return doc, err
	}
	if objectName.Valid {
		doc.File = &domain.DocumentFile{
			FileName:    fileName.String,
			ContentType: contentType.String,
			SizeBytes:   sizeBytes.Int64,
			ObjectName:  objectName.String,
			UploadedAt:  uploadedAt.Time.Unix(),
		}
	}
	return doc, nil
}

// AddDocument implements BR-TP29/BR-TP30 — always an insert, never an
// upsert. Keeping one *current* document per (partner, type) is done by
// superseding the incumbent first, in the same transaction, so a reader can
// never observe two current documents of one type nor zero.
//
// This replaces BR-TP08's ON CONFLICT DO UPDATE, which reached the same end
// state by overwriting the previous row — destroying exactly the history an
// audit of "what was approved, and when" needs, and leaving 38c-ii no stable
// per-document ID to name an Object Store blob with.
func (r *ComplianceDocumentRepository) AddDocument(ctx context.Context, partnerID string, doc domain.ComplianceDocument) (domain.ComplianceDocument, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.ComplianceDocument{}, err
	}
	defer tx.Rollback() //nolint:errcheck

	// Supersede the incumbent, if any. Unconditional on its status: BR-TP30
	// is legal from Pending, Approved and Rejected alike.
	if _, err := tx.ExecContext(ctx, `
		UPDATE trading_partner.compliance_documents
		SET status = $3, updated_at = now()
		WHERE trading_partner_id = $1 AND type = $2 AND status <> $3`,
		partnerID, doc.Type, domain.DocumentStatusSuperseded); err != nil {
		return domain.ComplianceDocument{}, err
	}

	// BR-TP29: the ID is minted here, by the column default, and returned —
	// the caller never supplies one.
	inserted, err := scanDocument(tx.QueryRowContext(ctx, `
		INSERT INTO trading_partner.compliance_documents
			(trading_partner_id, type, status, reference, expires_at, coverage_cents, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, now())
		RETURNING `+documentColumns,
		partnerID, doc.Type, doc.Status, doc.Reference, doc.ExpiresAt, doc.CoverageCents))
	if err != nil {
		return domain.ComplianceDocument{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.ComplianceDocument{}, err
	}
	return inserted, nil
}

// ListDocuments implements BR-TP31 — current documents only. Superseded rows
// stay in Postgres for audit but are never returned, so this response shape
// is unchanged from before 38c-i and 38b's workflow and 38d's Documents tab
// see exactly what they saw before.
func (r *ComplianceDocumentRepository) ListDocuments(ctx context.Context, partnerID string) ([]domain.ComplianceDocument, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT `+documentColumns+`
		FROM trading_partner.compliance_documents
		WHERE trading_partner_id = $1 AND status <> $2
		ORDER BY type`, partnerID, domain.DocumentStatusSuperseded)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	all := []domain.ComplianceDocument{}
	for rows.Next() {
		doc, err := scanDocument(rows)
		if err != nil {
			return nil, err
		}
		all = append(all, doc)
	}
	return all, rows.Err()
}

// GetDocument reads one document by ID, superseded rows included — unlike
// ListDocuments (BR-TP31). A download of a superseded document's bytes is
// legitimate and expected: BR-TP43 keeps objects forever precisely so the
// history BR-TP30 preserves stays retrievable, and the supersede-and-replace
// path is the only way to correct a file.
func (r *ComplianceDocumentRepository) GetDocument(ctx context.Context, partnerID, documentID string) (domain.ComplianceDocument, error) {
	doc, err := scanDocument(r.db.QueryRowContext(ctx, `
		SELECT `+documentColumns+`
		FROM trading_partner.compliance_documents
		WHERE trading_partner_id = $1 AND id = $2`, partnerID, documentID))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ComplianceDocument{}, domain.ErrDocumentNotFound
	}
	return doc, err
}

// AttachFile implements BR-TP43/BR-TP45 — records uploaded bytes against a
// document.
//
// The row is locked FOR UPDATE and the domain guard re-applied inside the
// transaction, so two uploads racing for the same document cannot both win:
// the second reads the first's File and gets ErrDocumentFileAlreadyAttached.
// That matters more here than for a status transition, because the loser has
// already written its bytes — see the handler for why an orphan object is the
// acceptable outcome and a lost one would not be.
func (r *ComplianceDocumentRepository) AttachFile(ctx context.Context, partnerID, documentID string, file domain.DocumentFile) (domain.ComplianceDocument, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.ComplianceDocument{}, err
	}
	defer tx.Rollback() //nolint:errcheck

	doc, err := scanDocument(tx.QueryRowContext(ctx, `
		SELECT `+documentColumns+`
		FROM trading_partner.compliance_documents
		WHERE trading_partner_id = $1 AND id = $2 FOR UPDATE`, partnerID, documentID))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ComplianceDocument{}, domain.ErrDocumentNotFound
	}
	if err != nil {
		return domain.ComplianceDocument{}, err
	}

	updated, err := doc.AttachFile(file)
	if err != nil {
		return domain.ComplianceDocument{}, err
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE trading_partner.compliance_documents
		SET file_name = $3, file_content_type = $4, file_size_bytes = $5,
		    file_object_name = $6, file_uploaded_at = to_timestamp($7), updated_at = now()
		WHERE trading_partner_id = $1 AND id = $2`,
		partnerID, documentID, updated.File.FileName, updated.File.ContentType,
		updated.File.SizeBytes, updated.File.ObjectName, updated.File.UploadedAt); err != nil {
		return domain.ComplianceDocument{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.ComplianceDocument{}, err
	}
	return updated, nil
}

// ApproveDocument implements BR-TP09 — legal only Pending -> Approved.
func (r *ComplianceDocumentRepository) ApproveDocument(ctx context.Context, partnerID, documentID string) (domain.ComplianceDocument, error) {
	return r.transition(ctx, partnerID, documentID, domain.ComplianceDocument.Approve)
}

// RejectDocument implements BR-TP10 — legal only Pending -> Rejected.
func (r *ComplianceDocumentRepository) RejectDocument(ctx context.Context, partnerID, documentID string) (domain.ComplianceDocument, error) {
	return r.transition(ctx, partnerID, documentID, domain.ComplianceDocument.Reject)
}

// ResubmitDocument implements BR-TP11 — legal only Rejected -> Pending.
func (r *ComplianceDocumentRepository) ResubmitDocument(ctx context.Context, partnerID, documentID string) (domain.ComplianceDocument, error) {
	return r.transition(ctx, partnerID, documentID, domain.ComplianceDocument.Resubmit)
}

// transition addresses a document by ID (BR-TP31), not by type — after
// BR-TP29 a type no longer identifies a single row. The domain method
// supplies the legality guard, including rejecting a superseded document.
func (r *ComplianceDocumentRepository) transition(ctx context.Context, partnerID, documentID string, apply func(domain.ComplianceDocument) (domain.ComplianceDocument, error)) (domain.ComplianceDocument, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.ComplianceDocument{}, err
	}
	defer tx.Rollback() //nolint:errcheck

	doc, err := scanDocument(tx.QueryRowContext(ctx, `
		SELECT `+documentColumns+`
		FROM trading_partner.compliance_documents
		WHERE trading_partner_id = $1 AND id = $2 FOR UPDATE`, partnerID, documentID))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ComplianceDocument{}, domain.ErrDocumentNotFound
	}
	if err != nil {
		return domain.ComplianceDocument{}, err
	}

	updated, err := apply(doc)
	if err != nil {
		return domain.ComplianceDocument{}, err
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE trading_partner.compliance_documents SET status = $3, updated_at = now()
		WHERE trading_partner_id = $1 AND id = $2`, partnerID, documentID, updated.Status); err != nil {
		return domain.ComplianceDocument{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.ComplianceDocument{}, err
	}
	return updated, nil
}
