package postgres

import (
	"context"
	"database/sql"
	"errors"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/trading-partner-service/tradingpartner/internal/domain"
)

// TradingPartnerRepository is the Postgres adapter for
// domain.TradingPartnerRepository.
type TradingPartnerRepository struct{ db *sql.DB }

func NewTradingPartnerRepository(db *sql.DB) *TradingPartnerRepository {
	return &TradingPartnerRepository{db: db}
}

// partnerColumns is the shared SELECT list, so the scan order in scanPartner
// can't drift from it. version (BR-TP33) is part of every read: a caller
// cannot honour BR-TP34's guard without the version it read.
const partnerColumns = `id, context, name, type, status, trading_as, company_name, registration_no, vat_registration_no, version`

func scanPartner(row interface {
	Scan(dest ...any) error
}) (domain.TradingPartner, error) {
	var tp domain.TradingPartner
	err := row.Scan(&tp.ID, &tp.Context, &tp.Name, &tp.Type, &tp.Status,
		&tp.TradingAs, &tp.CompanyName, &tp.RegistrationNo, &tp.VatRegistrationNo, &tp.Version)
	return tp, err
}

func (r *TradingPartnerRepository) Register(ctx context.Context, tp domain.TradingPartner) (domain.TradingPartner, error) {
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO trading_partner.trading_partners
			(context, name, type, status, trading_as, company_name, registration_no, vat_registration_no)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, version`,
		tp.Context, tp.Name, tp.Type, tp.Status, tp.TradingAs, tp.CompanyName, tp.RegistrationNo, tp.VatRegistrationNo,
	).Scan(&tp.ID, &tp.Version)
	if err != nil {
		return domain.TradingPartner{}, err
	}
	return tp, nil
}

func (r *TradingPartnerRepository) Get(ctx context.Context, id string) (domain.TradingPartner, error) {
	tp, err := scanPartner(r.db.QueryRowContext(ctx, `
		SELECT `+partnerColumns+`
		FROM trading_partner.trading_partners WHERE id = $1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.TradingPartner{}, domain.ErrTradingPartnerNotFound
	}
	return tp, err
}

func (r *TradingPartnerRepository) List(ctx context.Context, contextKey string) ([]domain.TradingPartner, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT `+partnerColumns+`
		FROM trading_partner.trading_partners WHERE context = $1 ORDER BY name`, contextKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	all := []domain.TradingPartner{}
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
func (r *TradingPartnerRepository) Activate(ctx context.Context, id string) (domain.TradingPartner, error) {
	return r.transition(ctx, id, domain.TradingPartner.Activate)
}

// Suspend implements BR-TP04 — legal only Active -> Suspended, with a
// required reason (checked by the domain layer before the status guard).
func (r *TradingPartnerRepository) Suspend(ctx context.Context, id string, reason string) (domain.TradingPartner, error) {
	return r.transition(ctx, id, func(tp domain.TradingPartner) (domain.TradingPartner, error) {
		return tp.Suspend(reason)
	})
}

// Reactivate implements BR-TP05 — legal only Suspended -> Active.
func (r *TradingPartnerRepository) Reactivate(ctx context.Context, id string) (domain.TradingPartner, error) {
	return r.transition(ctx, id, domain.TradingPartner.Reactivate)
}

// transition loads the current row, applies the domain-layer guard, and
// persists the resulting status if the guard allowed it — so BR-TP03/04/05's
// legality checks always run against the actually-persisted status, never a
// stale in-memory copy.
func (r *TradingPartnerRepository) transition(ctx context.Context, id string, apply func(domain.TradingPartner) (domain.TradingPartner, error)) (domain.TradingPartner, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.TradingPartner{}, err
	}
	defer tx.Rollback() //nolint:errcheck

	tp, err := scanPartner(tx.QueryRowContext(ctx, `
		SELECT `+partnerColumns+`
		FROM trading_partner.trading_partners WHERE id = $1 FOR UPDATE`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.TradingPartner{}, domain.ErrTradingPartnerNotFound
	}
	if err != nil {
		return domain.TradingPartner{}, err
	}

	updated, err := apply(tp)
	if err != nil {
		return domain.TradingPartner{}, err
	}

	// BR-TP33: a lifecycle transition bumps version like any other write, so
	// an edit form left open across someone else's Suspend goes stale. It
	// deliberately does NOT check version (BR-TP34) — the status guard above
	// already rejects an illegal repeat, so requiring a version here would
	// only make a correct transition fail.
	if err := tx.QueryRowContext(ctx, `
		UPDATE trading_partner.trading_partners
		SET status = $2, version = version + 1
		WHERE id = $1
		RETURNING version`, id, updated.Status).Scan(&updated.Version); err != nil {
		return domain.TradingPartner{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.TradingPartner{}, err
	}
	return updated, nil
}

// UpdateDetails implements BR-TP32/BR-TP33/BR-TP34 — edits Company
// Information under an optimistic version guard.
//
// The version appears in two places on purpose. The domain method
// (TradingPartner.UpdateDetails) is where the business rule lives, per
// CLAUDE.md's "business rules live in the domain layer"; the `AND version =
// $2` predicate below is what makes the check atomic, since two callers could
// otherwise both pass the domain check against the same loaded row and both
// write. Neither alone is sufficient: the domain check without the predicate
// is racy, and the predicate without the domain check would put a business
// rule in an adapter.
func (r *TradingPartnerRepository) UpdateDetails(ctx context.Context, id string, expectedVersion int, details domain.Details) (domain.TradingPartner, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.TradingPartner{}, err
	}
	defer tx.Rollback() //nolint:errcheck

	current, err := scanPartner(tx.QueryRowContext(ctx, `
		SELECT `+partnerColumns+`
		FROM trading_partner.trading_partners WHERE id = $1 FOR UPDATE`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.TradingPartner{}, domain.ErrTradingPartnerNotFound
	}
	if err != nil {
		return domain.TradingPartner{}, err
	}

	updated, err := current.UpdateDetails(expectedVersion, details)
	if err != nil {
		return domain.TradingPartner{}, err
	}

	// The predicate repeats the guard the domain method just applied. If the
	// row moved between the SELECT ... FOR UPDATE and here it matches nothing,
	// and that is reported as a conflict rather than a silent no-op.
	err = tx.QueryRowContext(ctx, `
		UPDATE trading_partner.trading_partners
		SET name = $3, trading_as = $4, company_name = $5,
		    registration_no = $6, vat_registration_no = $7, version = version + 1
		WHERE id = $1 AND version = $2
		RETURNING version`,
		id, expectedVersion, updated.Name, updated.TradingAs, updated.CompanyName,
		updated.RegistrationNo, updated.VatRegistrationNo).Scan(&updated.Version)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.TradingPartner{}, domain.ErrVersionConflict
	}
	if err != nil {
		return domain.TradingPartner{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.TradingPartner{}, err
	}
	return updated, nil
}
