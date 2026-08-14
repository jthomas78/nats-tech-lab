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

func (r *TradingPartnerRepository) Register(ctx context.Context, tp domain.TradingPartner) (domain.TradingPartner, error) {
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO trading_partner.trading_partners
			(context, name, type, status, trading_as, company_name, registration_no, vat_registration_no)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id`,
		tp.Context, tp.Name, tp.Type, tp.Status, tp.TradingAs, tp.CompanyName, tp.RegistrationNo, tp.VatRegistrationNo,
	).Scan(&tp.ID)
	if err != nil {
		return domain.TradingPartner{}, err
	}
	return tp, nil
}

func (r *TradingPartnerRepository) Get(ctx context.Context, id string) (domain.TradingPartner, error) {
	var tp domain.TradingPartner
	err := r.db.QueryRowContext(ctx, `
		SELECT id, context, name, type, status, trading_as, company_name, registration_no, vat_registration_no
		FROM trading_partner.trading_partners WHERE id = $1`, id,
	).Scan(&tp.ID, &tp.Context, &tp.Name, &tp.Type, &tp.Status, &tp.TradingAs, &tp.CompanyName, &tp.RegistrationNo, &tp.VatRegistrationNo)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.TradingPartner{}, domain.ErrTradingPartnerNotFound
	}
	return tp, err
}

func (r *TradingPartnerRepository) List(ctx context.Context, contextKey string) ([]domain.TradingPartner, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, context, name, type, status, trading_as, company_name, registration_no, vat_registration_no
		FROM trading_partner.trading_partners WHERE context = $1 ORDER BY name`, contextKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	all := []domain.TradingPartner{}
	for rows.Next() {
		var tp domain.TradingPartner
		if err := rows.Scan(&tp.ID, &tp.Context, &tp.Name, &tp.Type, &tp.Status, &tp.TradingAs, &tp.CompanyName, &tp.RegistrationNo, &tp.VatRegistrationNo); err != nil {
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

	var tp domain.TradingPartner
	err = tx.QueryRowContext(ctx, `
		SELECT id, context, name, type, status, trading_as, company_name, registration_no, vat_registration_no
		FROM trading_partner.trading_partners WHERE id = $1 FOR UPDATE`, id,
	).Scan(&tp.ID, &tp.Context, &tp.Name, &tp.Type, &tp.Status, &tp.TradingAs, &tp.CompanyName, &tp.RegistrationNo, &tp.VatRegistrationNo)
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

	if _, err := tx.ExecContext(ctx, `
		UPDATE trading_partner.trading_partners SET status = $2 WHERE id = $1`, id, updated.Status); err != nil {
		return domain.TradingPartner{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.TradingPartner{}, err
	}
	return updated, nil
}
