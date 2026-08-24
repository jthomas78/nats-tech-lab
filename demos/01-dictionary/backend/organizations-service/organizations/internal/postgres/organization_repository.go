package postgres

import (
	"context"
	"database/sql"
	"errors"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/internal/domain"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/internal/identity"
)

// OrganizationRepository is the Postgres adapter for
// domain.OrganizationRepository.
type OrganizationRepository struct{ db *sql.DB }

func NewOrganizationRepository(db *sql.DB) *OrganizationRepository {
	return &OrganizationRepository{db: db}
}

// partnerColumns is the shared SELECT list, so the scan order in scanPartner
// can't drift from it. version (BR-TP33) is part of every read: a caller
// cannot honour BR-TP34's guard without the version it read.
const partnerColumns = `id, context, name, type, status, trading_as, company_name, registration_no, vat_registration_no, version`

func scanPartner(row interface {
	Scan(dest ...any) error
}) (domain.Organization, error) {
	var tp domain.Organization
	err := row.Scan(&tp.ID, &tp.Context, &tp.Name, &tp.Type, &tp.Status,
		&tp.TradingAs, &tp.CompanyName, &tp.RegistrationNo, &tp.VatRegistrationNo, &tp.Version)
	return tp, err
}

// Register mints the organization's identity here rather than reading it back
// from a column default. BR-TP73 (ADR-051) is why: the id is a ULID, and
// Postgres has no function that produces one, so the choice is between the
// service minting it and the database not having one to give.
//
// It stays in this adapter rather than moving up into the domain's Register
// because nothing in the domain branches on an ID's value or shape — hoisting
// it would add a port with one implementation and no second caller. What the
// caller sees is unchanged: it passes an Organization with no ID and gets one
// back with an ID, exactly as when the column default supplied it.
func (r *OrganizationRepository) Register(ctx context.Context, tp domain.Organization) (domain.Organization, error) {
	tp.ID = identity.New()
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO organizations.organizations
			(id, context, name, type, status, trading_as, company_name, registration_no, vat_registration_no)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING version`,
		tp.ID, tp.Context, tp.Name, tp.Type, tp.Status, tp.TradingAs, tp.CompanyName, tp.RegistrationNo, tp.VatRegistrationNo,
	).Scan(&tp.Version)
	if err != nil {
		return domain.Organization{}, err
	}
	return tp, nil
}

func (r *OrganizationRepository) Get(ctx context.Context, id string) (domain.Organization, error) {
	tp, err := scanPartner(r.db.QueryRowContext(ctx, `
		SELECT `+partnerColumns+`
		FROM organizations.organizations WHERE id = $1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Organization{}, domain.ErrOrganizationNotFound
	}
	return tp, err
}

func (r *OrganizationRepository) List(ctx context.Context, contextKey string) ([]domain.Organization, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT `+partnerColumns+`
		FROM organizations.organizations WHERE context = $1 ORDER BY name`, contextKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	all := []domain.Organization{}
	for rows.Next() {
		tp, err := scanPartner(rows)
		if err != nil {
			return nil, err
		}
		all = append(all, tp)
	}
	return all, rows.Err()
}

// Activate implements BR-TP03 — legal only Registered -> Active, checked
// against the currently-persisted status before writing.
func (r *OrganizationRepository) Activate(ctx context.Context, id string) (domain.Organization, error) {
	return r.transition(ctx, id, domain.Organization.Activate)
}

// Suspend implements BR-TP04 — legal only Active -> Suspended, with a
// required reason (checked by the domain layer before the status guard).
func (r *OrganizationRepository) Suspend(ctx context.Context, id string, reason string) (domain.Organization, error) {
	return r.transition(ctx, id, func(tp domain.Organization) (domain.Organization, error) {
		return tp.Suspend(reason)
	})
}

// Reactivate implements BR-TP05 — legal only Suspended -> Active.
func (r *OrganizationRepository) Reactivate(ctx context.Context, id string) (domain.Organization, error) {
	return r.transition(ctx, id, domain.Organization.Reactivate)
}

// transition loads the current row, applies the domain-layer guard, and
// persists the resulting status if the guard allowed it — so BR-TP03/04/05's
// legality checks always run against the actually-persisted status, never a
// stale in-memory copy.
func (r *OrganizationRepository) transition(ctx context.Context, id string, apply func(domain.Organization) (domain.Organization, error)) (domain.Organization, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Organization{}, err
	}
	defer tx.Rollback() //nolint:errcheck

	tp, err := scanPartner(tx.QueryRowContext(ctx, `
		SELECT `+partnerColumns+`
		FROM organizations.organizations WHERE id = $1 FOR UPDATE`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Organization{}, domain.ErrOrganizationNotFound
	}
	if err != nil {
		return domain.Organization{}, err
	}

	updated, err := apply(tp)
	if err != nil {
		return domain.Organization{}, err
	}

	// BR-TP33: a lifecycle transition bumps version like any other write, so
	// an edit form left open across someone else's Suspend goes stale. It
	// deliberately does NOT check version (BR-TP34) — the status guard above
	// already rejects an illegal repeat, so requiring a version here would
	// only make a correct transition fail.
	if err := tx.QueryRowContext(ctx, `
		UPDATE organizations.organizations
		SET status = $2, version = version + 1
		WHERE id = $1
		RETURNING version`, id, updated.Status).Scan(&updated.Version); err != nil {
		return domain.Organization{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Organization{}, err
	}
	return updated, nil
}

// UpdateDetails implements BR-TP32/BR-TP33/BR-TP34 — edits Company
// Information under an optimistic version guard.
//
// The version appears in two places on purpose. The domain method
// (Organization.UpdateDetails) is where the business rule lives, per
// CLAUDE.md's "business rules live in the domain layer"; the `AND version =
// $2` predicate below is what makes the check atomic, since two callers could
// otherwise both pass the domain check against the same loaded row and both
// write. Neither alone is sufficient: the domain check without the predicate
// is racy, and the predicate without the domain check would put a business
// rule in an adapter.
func (r *OrganizationRepository) UpdateDetails(ctx context.Context, id string, expectedVersion int, details domain.Details) (domain.Organization, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Organization{}, err
	}
	defer tx.Rollback() //nolint:errcheck

	current, err := scanPartner(tx.QueryRowContext(ctx, `
		SELECT `+partnerColumns+`
		FROM organizations.organizations WHERE id = $1 FOR UPDATE`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Organization{}, domain.ErrOrganizationNotFound
	}
	if err != nil {
		return domain.Organization{}, err
	}

	updated, err := current.UpdateDetails(expectedVersion, details)
	if err != nil {
		return domain.Organization{}, err
	}

	// The predicate repeats the guard the domain method just applied. If the
	// row moved between the SELECT ... FOR UPDATE and here it matches nothing,
	// and that is reported as a conflict rather than a silent no-op.
	err = tx.QueryRowContext(ctx, `
		UPDATE organizations.organizations
		SET name = $3, trading_as = $4, company_name = $5,
		    registration_no = $6, vat_registration_no = $7, version = version + 1
		WHERE id = $1 AND version = $2
		RETURNING version`,
		id, expectedVersion, updated.Name, updated.TradingAs, updated.CompanyName,
		updated.RegistrationNo, updated.VatRegistrationNo).Scan(&updated.Version)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Organization{}, domain.ErrVersionConflict
	}
	if err != nil {
		return domain.Organization{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Organization{}, err
	}
	return updated, nil
}
