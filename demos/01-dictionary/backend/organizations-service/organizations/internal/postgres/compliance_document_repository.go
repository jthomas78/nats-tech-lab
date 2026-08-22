package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/internal/domain"
)

// ComplianceDocumentRepository is the Postgres adapter for
// domain.ComplianceDocumentRepository.
type ComplianceDocumentRepository struct{ db *sql.DB }

func NewComplianceDocumentRepository(db *sql.DB) *ComplianceDocumentRepository {
	return &ComplianceDocumentRepository{db: db}
}

// documentColumns is the shared SELECT list, so the scan order in
// scanDocument can't drift from it.
const documentColumns = `id, type, status, reference, expires_at, coverage_cents, goods_types,
	insurer_name, insurance_contact_name, insurance_contact_number, created_at,
	file_name, file_content_type, file_size_bytes, file_object_name, file_uploaded_at`

func scanDocument(row interface {
	Scan(dest ...any) error
}) (domain.ComplianceDocument, error) {
	var doc domain.ComplianceDocument
	// BR-TP45: the five file columns are nullable as a group, so they are
	// scanned into nullables and folded into a single *DocumentFile — a
	// half-populated File would be a shape the domain has no rule for.
	var (
		goodsTypes  []byte
		fileName    sql.NullString
		contentType sql.NullString
		sizeBytes   sql.NullInt64
		objectName  sql.NullString
		uploadedAt  sql.NullTime
	)
	// expires_at is TIMESTAMPTZ in Postgres but *int64 (Unix seconds) on the
	// domain type, so it cannot be scanned directly — doing so fails with
	// "converting driver.Value type time.Time ... to a int64". The conversion
	// lives here rather than the column being changed because the whole
	// domain treats instants as Unix seconds (DocumentFile.UploadedAt does the
	// same just below), while the column wants to stay a real timestamp so
	// SQL-level expiry reporting keeps working.
	var expiresAt sql.NullTime
	err := row.Scan(&doc.ID, &doc.Type, &doc.Status, &doc.Reference, &expiresAt, &doc.CoverageCents, &goodsTypes,
		&doc.InsurerName, &doc.InsuranceContactName, &doc.InsuranceContactNumber, &doc.CreatedAt,
		&fileName, &contentType, &sizeBytes, &objectName, &uploadedAt)
	if err != nil {
		return doc, err
	}
	if expiresAt.Valid {
		seconds := expiresAt.Time.Unix()
		doc.ExpiresAt = &seconds
	}
	if len(goodsTypes) > 0 {
		if err := json.Unmarshal(goodsTypes, &doc.GoodsTypes); err != nil {
			return doc, err
		}
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

// expiryParam is scanDocument's inverse: the domain's Unix seconds become a
// *time.Time so the driver writes a real TIMESTAMPTZ. Without it an insert
// carrying an expiry writes an integer into a timestamp column.
func expiryParam(seconds *int64) *time.Time {
	if seconds == nil {
		return nil
	}
	at := time.Unix(*seconds, 0).UTC()
	return &at
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
// SetInsuranceContact is decision 25's direct write — the one path by which
// the two contact columns are ever populated, since they are deliberately
// absent from the stream (BR-TP72). Deliberately not folded into
// UpsertCertificate: keeping the replayed write and the un-replayed write in
// separate methods is what makes it impossible for the projector to touch
// these columns by accident.
// goodsTypesParam coerces a nil slice to an empty array. goods_types is NOT
// NULL with a '{}' default, but a default only applies when the column is
// omitted — passing an explicit nil sends NULL and violates the constraint.
// Every non-GIT document has no goods types at all, so this is the common
// path, not an edge case.
func goodsTypesParam(codes []string) []byte {
	if codes == nil {
		codes = []string{}
	}
	encoded, err := json.Marshal(codes)
	if err != nil {
		// A []string cannot fail to marshal; an empty array is still the only
		// value that keeps the NOT NULL column satisfiable.
		return []byte("[]")
	}
	return encoded
}

func (r *ComplianceDocumentRepository) SetInsuranceContact(ctx context.Context, partnerID, documentID, insurerName, contactName, contactNumber string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE organizations.compliance_documents
		SET insurer_name = $3, insurance_contact_name = $4, insurance_contact_number = $5, updated_at = now()
		WHERE organization_id = $1 AND id = $2`,
		partnerID, documentID, insurerName, contactName, contactNumber)
	return err
}

// UpsertCertificate is the projection write for ADR-050 Option A. Every
// column it sets comes off the stream; the two contact columns are absent
// from both the INSERT list and the DO UPDATE list, so replaying the whole
// stream over an existing table leaves them exactly as the command wrote them
// and replaying into an empty one leaves them NULL — which is the documented,
// deliberate cost of keeping them off the log.
func (r *ComplianceDocumentRepository) UpsertCertificate(ctx context.Context, partnerID string, cert domain.ProjectedCertificate) error {
	var (
		fileName    any
		contentType any
		sizeBytes   any
		objectName  any
		uploadedAt  any
	)
	if cert.File != nil {
		fileName, contentType, sizeBytes = cert.File.FileName, cert.File.ContentType, cert.File.SizeBytes
		objectName, uploadedAt = cert.File.ObjectName, time.Unix(cert.File.UploadedAt, 0).UTC()
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO organizations.compliance_documents
			(organization_id, id, type, status, reference, expires_at, coverage_cents, goods_types, insurer_name,
			 file_name, file_content_type, file_size_bytes, file_object_name, file_uploaded_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, now())
		ON CONFLICT (organization_id, id) DO UPDATE SET
			status = EXCLUDED.status,
			reference = EXCLUDED.reference,
			expires_at = EXCLUDED.expires_at,
			coverage_cents = EXCLUDED.coverage_cents,
			goods_types = EXCLUDED.goods_types,
			insurer_name = EXCLUDED.insurer_name,
			file_name = EXCLUDED.file_name,
			file_content_type = EXCLUDED.file_content_type,
			file_size_bytes = EXCLUDED.file_size_bytes,
			file_object_name = EXCLUDED.file_object_name,
			file_uploaded_at = EXCLUDED.file_uploaded_at,
			updated_at = now()`,
		partnerID, cert.ID, domain.DocumentTypeGoodsInTransit, cert.Status, cert.Reference,
		expiryParam(cert.ExpiresAt), cert.CoverageCents, goodsTypesParam(cert.GoodsTypes), cert.InsurerName,
		fileName, contentType, sizeBytes, objectName, uploadedAt)
	return err
}

func (r *ComplianceDocumentRepository) AddDocument(ctx context.Context, partnerID string, doc domain.ComplianceDocument) (domain.ComplianceDocument, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.ComplianceDocument{}, err
	}
	defer tx.Rollback() //nolint:errcheck

	// The four legacy document types retain BR-TP30's replace-on-register
	// behaviour. GIT deliberately does not: early renewals coexist until the
	// new certificate is approved (BR-TP69).
	if doc.Type != domain.DocumentTypeGoodsInTransit {
		if _, err := tx.ExecContext(ctx, `
			UPDATE organizations.compliance_documents
			SET status = $3, updated_at = now()
			WHERE organization_id = $1 AND type = $2 AND status <> $3`,
			partnerID, doc.Type, domain.DocumentStatusSuperseded); err != nil {
			return domain.ComplianceDocument{}, err
		}
	}

	// BR-TP29: the ID is minted here, by the column default, and returned —
	// the caller never supplies one.
	inserted, err := scanDocument(tx.QueryRowContext(ctx, `
		INSERT INTO organizations.compliance_documents
			(organization_id, type, status, reference, expires_at, coverage_cents, goods_types, insurer_name, insurance_contact_name, insurance_contact_number, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, now())
		RETURNING `+documentColumns,
		partnerID, doc.Type, doc.Status, doc.Reference, expiryParam(doc.ExpiresAt), doc.CoverageCents, goodsTypesParam(doc.GoodsTypes), doc.InsurerName, doc.InsuranceContactName, doc.InsuranceContactNumber))
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
		FROM organizations.compliance_documents
		WHERE organization_id = $1 AND status <> $2
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
		FROM organizations.compliance_documents
		WHERE organization_id = $1 AND id = $2`, partnerID, documentID))
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
		FROM organizations.compliance_documents
		WHERE organization_id = $1 AND id = $2 FOR UPDATE`, partnerID, documentID))
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
		UPDATE organizations.compliance_documents
		SET file_name = $3, file_content_type = $4, file_size_bytes = $5,
		    file_object_name = $6, file_uploaded_at = to_timestamp($7), updated_at = now()
		WHERE organization_id = $1 AND id = $2`,
		partnerID, documentID, updated.File.FileName, updated.File.ContentType,
		updated.File.SizeBytes, updated.File.ObjectName, updated.File.UploadedAt); err != nil {
		return domain.ComplianceDocument{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.ComplianceDocument{}, err
	}
	return updated, nil
}

// SetDocumentExpiry implements BR-TP59. It reuses transition's
// lock-then-apply shape but writes expires_at instead of status, because the
// domain guard it applies (ErrDocumentExpiryInPast, ErrDocumentSuperseded)
// has to run against the row as it stands, not against whatever the caller
// last read.
func (r *ComplianceDocumentRepository) SetDocumentExpiry(ctx context.Context, partnerID, documentID string, expiresAt *int64) (domain.ComplianceDocument, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.ComplianceDocument{}, err
	}
	defer tx.Rollback() //nolint:errcheck

	doc, err := scanDocument(tx.QueryRowContext(ctx, `
		SELECT `+documentColumns+`
		FROM organizations.compliance_documents
		WHERE organization_id = $1 AND id = $2 FOR UPDATE`, partnerID, documentID))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ComplianceDocument{}, domain.ErrDocumentNotFound
	}
	if err != nil {
		return domain.ComplianceDocument{}, err
	}

	updated, err := doc.SetExpiry(expiresAt, time.Now().UTC())
	if err != nil {
		return domain.ComplianceDocument{}, err
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE organizations.compliance_documents SET expires_at = $3, updated_at = now()
		WHERE organization_id = $1 AND id = $2`,
		partnerID, documentID, expiryParam(updated.ExpiresAt)); err != nil {
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
		FROM organizations.compliance_documents
		WHERE organization_id = $1 AND id = $2 FOR UPDATE`, partnerID, documentID))
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
		UPDATE organizations.compliance_documents SET status = $3, updated_at = now()
		WHERE organization_id = $1 AND id = $2`, partnerID, documentID, updated.Status); err != nil {
		return domain.ComplianceDocument{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.ComplianceDocument{}, err
	}
	return updated, nil
}
