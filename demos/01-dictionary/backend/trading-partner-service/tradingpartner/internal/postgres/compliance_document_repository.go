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

// AddDocument implements BR-TP08's repository-level "one per (partner,
// type)" invariant: adding a document for a type that already exists
// upserts — replaces the row and resets it to Pending, since new content
// always needs fresh review (the Phase 26 plan section's BR-TP08 note).
func (r *ComplianceDocumentRepository) AddDocument(ctx context.Context, partnerID string, doc domain.ComplianceDocument) (domain.ComplianceDocument, error) {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO trading_partner.compliance_documents
			(trading_partner_id, type, status, reference, expires_at, coverage_cents, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, now())
		ON CONFLICT (trading_partner_id, type) DO UPDATE SET
			status = $3, reference = $4, expires_at = $5, coverage_cents = $6, updated_at = now()`,
		partnerID, doc.Type, doc.Status, doc.Reference, doc.ExpiresAt, doc.CoverageCents)
	if err != nil {
		return domain.ComplianceDocument{}, err
	}
	return doc, nil
}

func (r *ComplianceDocumentRepository) ListDocuments(ctx context.Context, partnerID string) ([]domain.ComplianceDocument, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT type, status, reference, expires_at, coverage_cents
		FROM trading_partner.compliance_documents WHERE trading_partner_id = $1 ORDER BY type`, partnerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	all := []domain.ComplianceDocument{}
	for rows.Next() {
		var doc domain.ComplianceDocument
		if err := rows.Scan(&doc.Type, &doc.Status, &doc.Reference, &doc.ExpiresAt, &doc.CoverageCents); err != nil {
			return nil, err
		}
		all = append(all, doc)
	}
	return all, rows.Err()
}

// ApproveDocument implements BR-TP09 — legal only Pending -> Approved.
func (r *ComplianceDocumentRepository) ApproveDocument(ctx context.Context, partnerID string, docType domain.DocumentType) (domain.ComplianceDocument, error) {
	return r.transition(ctx, partnerID, docType, domain.ComplianceDocument.Approve)
}

// RejectDocument implements BR-TP10 — legal only Pending -> Rejected.
func (r *ComplianceDocumentRepository) RejectDocument(ctx context.Context, partnerID string, docType domain.DocumentType) (domain.ComplianceDocument, error) {
	return r.transition(ctx, partnerID, docType, domain.ComplianceDocument.Reject)
}

// ResubmitDocument implements BR-TP11 — legal only Rejected -> Pending.
func (r *ComplianceDocumentRepository) ResubmitDocument(ctx context.Context, partnerID string, docType domain.DocumentType) (domain.ComplianceDocument, error) {
	return r.transition(ctx, partnerID, docType, domain.ComplianceDocument.Resubmit)
}

func (r *ComplianceDocumentRepository) transition(ctx context.Context, partnerID string, docType domain.DocumentType, apply func(domain.ComplianceDocument) (domain.ComplianceDocument, error)) (domain.ComplianceDocument, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.ComplianceDocument{}, err
	}
	defer tx.Rollback() //nolint:errcheck

	var doc domain.ComplianceDocument
	err = tx.QueryRowContext(ctx, `
		SELECT type, status, reference, expires_at, coverage_cents
		FROM trading_partner.compliance_documents WHERE trading_partner_id = $1 AND type = $2 FOR UPDATE`,
		partnerID, docType).Scan(&doc.Type, &doc.Status, &doc.Reference, &doc.ExpiresAt, &doc.CoverageCents)
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
		WHERE trading_partner_id = $1 AND type = $2`, partnerID, docType, updated.Status); err != nil {
		return domain.ComplianceDocument{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.ComplianceDocument{}, err
	}
	return updated, nil
}
