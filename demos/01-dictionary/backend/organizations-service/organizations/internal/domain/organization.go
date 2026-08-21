// Package domain holds the organizations-service domain model.
// Organization registration is plain Postgres CRUD (see
// obsidian/V3-Platform/Architecture/Dictionary-POC/ARCHITECTURE.md §
// "Event Sourcing vs Plain CRUD") — only current state is ever queried,
// nothing reconstructs a partner from a log. Whether the lifecycle itself
// should later become event-sourced or temporal is a deliberately deferred,
// named open item (see BUSINESS_RULES-ORGANIZATIONS.md's intro), not
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
	ErrNameRequired = errors.New("organization name is required")

	// ErrContextRequired — BR-TP02: context is required at Register.
	ErrContextRequired = errors.New("organization context is required")

	// ErrInvalidPartnerType — BR-TP01: type must be SHIPPER or TRANSPORTER.
	ErrInvalidPartnerType = errors.New("organization type must be SHIPPER or TRANSPORTER")

	// ErrNotRegistered — BR-TP03: Activate is only legal from Registered.
	ErrNotRegistered = errors.New("organization is not in Registered status")

	// ErrNotActive — BR-TP04: Suspend is only legal from Active.
	ErrNotActive = errors.New("organization is not in Active status")

	// ErrNotSuspended — BR-TP05: Reactivate is only legal from Suspended.
	ErrNotSuspended = errors.New("organization is not in Suspended status")

	// ErrSuspendReasonRequired — BR-TP04: reason is required regardless of
	// status, checked before the status guard.
	ErrSuspendReasonRequired = errors.New("a reason is required to suspend an organization")

	// ErrOrganizationNotFound — no Organization exists for the given ID.
	ErrOrganizationNotFound = errors.New("organization not found")

	// ErrVersionConflict — BR-TP34: the version the caller read is not the
	// version on the row, so someone else has written since. Surfaced as
	// 409 Conflict. Distinct from the lifecycle guards above: nothing about
	// the requested change is illegal, it is simply based on stale data.
	ErrVersionConflict = errors.New("organization has been modified by someone else")
)

// Organization is a single Shipper or Transporter business record
// (BR-TP01), identity fields plain Postgres CRUD, no platform/tenant
// membership split in v1 (see the Phase 26 plan section).
type Organization struct {
	ID                string        `json:"id,omitempty"`
	Name              string        `json:"name"`
	Type              PartnerType   `json:"type"`
	Context           string        `json:"context"`
	Status            PartnerStatus `json:"status"`
	TradingAs         string        `json:"tradingAs,omitempty"`
	CompanyName       string        `json:"companyName,omitempty"`
	RegistrationNo    string        `json:"registrationNo,omitempty"`
	VatRegistrationNo string        `json:"vatRegistrationNo,omitempty"`

	// Version is BR-TP33's row version, starting at 1 and incremented by
	// exactly 1 on every successful write — lifecycle transitions included,
	// so an edit form left open across someone else's Suspend goes stale.
	Version int `json:"version,omitempty"`
}

// Details is the editable field set (BR-TP32) — the "Company Information"
// section of the UI, plus Name. Deliberately does NOT carry Type, Context or
// Status: Type gates document validity (BR-TP07) and fleet-asset attachment
// (BR-TP12) so editing it could retroactively invalidate rows that were
// legal when created; Context is the business-unit scope, and moving a
// partner between contexts is a migration rather than an edit; Status has
// its own lifecycle rules (BR-TP03-BR-TP05). Making them absent from this
// struct is the enforcement — there is no field to set, so no handler can
// pass one through by accident.
type Details struct {
	Name              string `json:"name"`
	TradingAs         string `json:"tradingAs,omitempty"`
	CompanyName       string `json:"companyName,omitempty"`
	RegistrationNo    string `json:"registrationNo,omitempty"`
	VatRegistrationNo string `json:"vatRegistrationNo,omitempty"`
}

// Register implements BR-TP01/BR-TP02 — name, type, and context are the
// only required fields; every other field is fillable incrementally as
// KYC/vetting proceeds. Always lands in Registered status.
func Register(name string, partnerType PartnerType, context string) (Organization, error) {
	return RegisterWithDetails(partnerType, context, Details{Name: name})
}

// RegisterWithDetails implements BR-TP35 — Register widened to accept the
// Company Information fields at registration time. The required set is
// unchanged (name, type, context); every other field stays optional and
// omitting it is identical to calling Register.
//
// This exists so 38d's registration wizard can commit a partner and its
// details in one call. Having the wizard register-then-update instead would
// introduce exactly the half-commit shape ADR-049 finding 6 warns about: a
// failed second call leaving a partner registered with none of its details,
// and no aggregate boundary to explain why.
func RegisterWithDetails(partnerType PartnerType, context string, d Details) (Organization, error) {
	if d.Name == "" {
		return Organization{}, ErrNameRequired
	}
	if context == "" {
		return Organization{}, ErrContextRequired
	}
	switch partnerType {
	case PartnerTypeShipper, PartnerTypeTransporter:
	default:
		return Organization{}, ErrInvalidPartnerType
	}
	return Organization{
		Name:              d.Name,
		Type:              partnerType,
		Context:           context,
		Status:            StatusRegistered,
		TradingAs:         d.TradingAs,
		CompanyName:       d.CompanyName,
		RegistrationNo:    d.RegistrationNo,
		VatRegistrationNo: d.VatRegistrationNo,
	}, nil
}

// UpdateDetails implements BR-TP32/BR-TP33/BR-TP34 — edits the Company
// Information fields, guarded by the version the caller read.
//
// expectedVersion must match the version currently on the aggregate. A
// mismatch returns ErrVersionConflict and mutates nothing, which is the
// lost-update-across-think-time case (ADR-049 finding 5a): two operators open
// the same form, both read version 1, and the second save must fail rather
// than silently overwrite the first. A `SELECT ... FOR UPDATE` structurally
// cannot catch that — it protects the span of a transaction, not the minutes
// a form sits open between two separate reads.
//
// Note this guard is necessary but not sufficient on its own: the repository
// also carries `AND version = ?` in the UPDATE, which is what makes the
// check atomic against a genuinely simultaneous write. The domain check is
// the business rule; the SQL predicate is its race-free enforcement.
func (p Organization) UpdateDetails(expectedVersion int, d Details) (Organization, error) {
	if d.Name == "" {
		return p, ErrNameRequired
	}
	if expectedVersion != p.Version {
		return p, ErrVersionConflict
	}
	p.Name = d.Name
	p.TradingAs = d.TradingAs
	p.CompanyName = d.CompanyName
	p.RegistrationNo = d.RegistrationNo
	p.VatRegistrationNo = d.VatRegistrationNo
	p.Version = expectedVersion + 1
	return p, nil
}

// Activate implements BR-TP03 — legal only Registered -> Active.
func (p Organization) Activate() (Organization, error) {
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
func (p Organization) Suspend(reason string) (Organization, error) {
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
func (p Organization) Reactivate() (Organization, error) {
	if p.Status != StatusSuspended {
		return p, ErrNotSuspended
	}
	p.Status = StatusActive
	return p, nil
}
