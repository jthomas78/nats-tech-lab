// Package domain holds the trading-partner-service domain model.
// TradingPartner registration is plain Postgres CRUD (see
// obsidian/V3-Platform/Architecture/Dictionary-POC/ARCHITECTURE.md §
// "Event Sourcing vs Plain CRUD") — only current state is ever queried,
// nothing reconstructs a partner from a log. Whether the lifecycle itself
// should later become event-sourced or temporal is a deliberately deferred,
// named open item (see BUSINESS_RULES-TRADING-PARTNER.md's intro), not
// decided by this package's shape.
package domain

import "errors"

// PartnerType is the Shipper/Transporter discriminator (BR-TP01) —
// V3's replacement for V2's BusinessType.CUSTOMER (see
// linebooker_shipper_vs_customer_naming.md).
type PartnerType string

const (
	PartnerTypeShipper     PartnerType = "SHIPPER"
	PartnerTypeTransporter PartnerType = "TRANSPORTER"
)

// PartnerStatus is the Register -> Activate -> Suspend -> Reactivate
// lifecycle (BR-TP02-BR-TP05), mirroring accounts-service's
// create/suspend/reactivate triple (BR-AC08-AC10).
type PartnerStatus string

const (
	StatusRegistered PartnerStatus = "REGISTERED"
	StatusActive     PartnerStatus = "ACTIVE"
	StatusSuspended  PartnerStatus = "SUSPENDED"
)

var (
	// ErrNameRequired — BR-TP02: name is required at Register.
	ErrNameRequired = errors.New("trading partner name is required")

	// ErrContextRequired — BR-TP02: context is required at Register.
	ErrContextRequired = errors.New("trading partner context is required")

	// ErrInvalidPartnerType — BR-TP01: type must be SHIPPER or TRANSPORTER.
	ErrInvalidPartnerType = errors.New("trading partner type must be SHIPPER or TRANSPORTER")

	// ErrNotRegistered — BR-TP03: Activate is only legal from Registered.
	ErrNotRegistered = errors.New("trading partner is not in Registered status")

	// ErrNotActive — BR-TP04: Suspend is only legal from Active.
	ErrNotActive = errors.New("trading partner is not in Active status")

	// ErrNotSuspended — BR-TP05: Reactivate is only legal from Suspended.
	ErrNotSuspended = errors.New("trading partner is not in Suspended status")

	// ErrSuspendReasonRequired — BR-TP04: reason is required regardless of
	// status, checked before the status guard.
	ErrSuspendReasonRequired = errors.New("a reason is required to suspend a trading partner")

	// ErrTradingPartnerNotFound — no TradingPartner exists for the given ID.
	ErrTradingPartnerNotFound = errors.New("trading partner not found")
)

// TradingPartner is a single Shipper or Transporter business record
// (BR-TP01), identity fields plain Postgres CRUD, no platform/tenant
// membership split in v1 (see the Phase 26 plan section).
type TradingPartner struct {
	ID                string        `json:"id,omitempty"`
	Name              string        `json:"name"`
	Type              PartnerType   `json:"type"`
	Context           string        `json:"context"`
	Status            PartnerStatus `json:"status"`
	TradingAs         string        `json:"tradingAs,omitempty"`
	CompanyName       string        `json:"companyName,omitempty"`
	RegistrationNo    string        `json:"registrationNo,omitempty"`
	VatRegistrationNo string        `json:"vatRegistrationNo,omitempty"`
}

// Register implements BR-TP01/BR-TP02 — name, type, and context are the
// only required fields; every other field is fillable incrementally as
// KYC/vetting proceeds. Always lands in Registered status.
func Register(name string, partnerType PartnerType, context string) (TradingPartner, error) {
	if name == "" {
		return TradingPartner{}, ErrNameRequired
	}
	if context == "" {
		return TradingPartner{}, ErrContextRequired
	}
	switch partnerType {
	case PartnerTypeShipper, PartnerTypeTransporter:
	default:
		return TradingPartner{}, ErrInvalidPartnerType
	}
	return TradingPartner{
		Name:    name,
		Type:    partnerType,
		Context: context,
		Status:  StatusRegistered,
	}, nil
}

// Activate implements BR-TP03 — legal only Registered -> Active.
func (p TradingPartner) Activate() (TradingPartner, error) {
	if p.Status != StatusRegistered {
		return p, ErrNotRegistered
	}
	p.Status = StatusActive
	return p, nil
}

// Suspend implements BR-TP04 — legal only Active -> Suspended, and requires
// a non-empty reason. The reason check runs before the status check: it is
// an input-validation gate at the domain boundary, not a state-dependent
// rule, so it rejects the same way regardless of the partner's status.
func (p TradingPartner) Suspend(reason string) (TradingPartner, error) {
	if reason == "" {
		return p, ErrSuspendReasonRequired
	}
	if p.Status != StatusActive {
		return p, ErrNotActive
	}
	p.Status = StatusSuspended
	return p, nil
}

// Reactivate implements BR-TP05 — legal only Suspended -> Active, completing
// the lifecycle triple with Activate/Suspend. There is no further terminal
// state in v1 (explicit non-goal, mirroring BR-AC03's retention rationale).
func (p TradingPartner) Reactivate() (TradingPartner, error) {
	if p.Status != StatusSuspended {
		return p, ErrNotSuspended
	}
	p.Status = StatusActive
	return p, nil
}
