// Package browserrpc translates the registry's api.* payloads. Business
// decisions remain in domain and Apply; this adapter holds no registry state.
package browserrpc

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/domain"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/postgres"
	"github.com/jthomas78/nats-tech-lab/shared/mferegistry"
)

// The subjects this adapter serves are the shared contract, not local
// literals: accounts-service grants them when it mints a browser credential
// (BR-AS25/AS27) and now does so from another module, so the list lives in
// shared/mferegistry where both sides read one copy.
const (
	ShellReadSubject  = mferegistry.ShellRead
	CuratedSubject    = mferegistry.Curated
	UpsertSubject     = mferegistry.Upsert
	SetEnabledSubject = mferegistry.SetEnabled
	AuditSubject      = mferegistry.Audit

	// The trust table: one read, one write carrying its op (BR-AS38).
	PublishersSubject     = mferegistry.Publishers
	PublisherWriteSubject = mferegistry.PublisherWrite
)

var ErrRevisionRequired = domain.ErrRevisionRequired

type Service interface {
	Read(context.Context) domain.Document
	Curated(context.Context) (domain.Document, error)
	Apply(context.Context, domain.Write) (domain.Document, error)
	Publishers(context.Context) (domain.PublisherDocument, error)
	ApplyPublisher(context.Context, domain.PublisherWrite) (domain.PublisherDocument, error)
	Allowlist() domain.Allowlist
}

type Auditor interface {
	Audit(context.Context, int) ([]postgres.AuditPage, error)
}

type Endpoints struct {
	svc   Service
	audit Auditor
}

func New(svc Service, audit Auditor) *Endpoints { return &Endpoints{svc: svc, audit: audit} }

type ReadRequest struct {
	HeldRevision int64 `json:"heldRevision"`
}

type ReadResponse struct {
	OK            bool           `json:"ok"`
	Unchanged     bool           `json:"unchanged"`
	SchemaVersion int            `json:"schemaVersion"`
	Revision      int64          `json:"revision"`
	Degraded      bool           `json:"degraded"`
	Plugins       []domain.Entry `json:"entries"`
}

func (e *Endpoints) Read(ctx context.Context, req ReadRequest) (ReadResponse, error) {
	doc := e.svc.Read(ctx).Readable(e.svc.Allowlist())
	if doc.Degraded {
		doc = domain.Degraded()
	}
	out := ReadResponse{OK: true, SchemaVersion: doc.SchemaVersion, Revision: doc.Revision, Degraded: doc.Degraded}
	out.Unchanged = !doc.Degraded && req.HeldRevision != 0 && req.HeldRevision == doc.Revision
	if !out.Unchanged {
		out.Plugins = doc.Entries
	}
	return out, nil
}

type EntryView struct {
	domain.Entry
	Conforming bool `json:"conforming"`
}

type CuratedResponse struct {
	SchemaVersion  int         `json:"schemaVersion"`
	Revision       int64       `json:"revision"`
	AllowedOrigins []string    `json:"allowedOrigins"`
	Plugins        []EntryView `json:"plugins"`
}

func (e *Endpoints) curate(doc domain.Document) CuratedResponse {
	allowed := e.svc.Allowlist()
	out := CuratedResponse{SchemaVersion: doc.SchemaVersion, Revision: doc.Revision, AllowedOrigins: allowed.Origins(), Plugins: []EntryView{}}
	for _, entry := range doc.Entries {
		out.Plugins = append(out.Plugins, EntryView{Entry: entry, Conforming: allowed.Check(entry) == nil})
	}
	return out
}

func (e *Endpoints) Curated(ctx context.Context) (CuratedResponse, error) {
	doc, err := e.svc.Curated(ctx)
	if err != nil {
		return CuratedResponse{}, errors.New("the registry could not be read")
	}
	return e.curate(doc), nil
}

type UpsertRequest struct {
	IfRevision      int64         `json:"ifRevision"`
	EntryID         string        `json:"entryId"`
	Entry           *domain.Entry `json:"entry"`
	revisionPresent bool
}

type SetEnabledRequest struct {
	IfRevision      int64  `json:"ifRevision"`
	EntryID         string `json:"entryId"`
	Enabled         bool   `json:"enabled"`
	revisionPresent bool
}

// Preserve explicit JSON zero without confusing an omitted/null precondition
// with the first write's legitimate revision. Direct Go literals in the
// contract use nonzero revisions; their zero value denotes omission.
func (r *UpsertRequest) UnmarshalJSON(data []byte) error {
	type plain UpsertRequest
	var wire struct {
		*plain
		IfRevision *int64 `json:"ifRevision"`
	}
	*r = UpsertRequest{}
	wire.plain = (*plain)(r)
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	r.revisionPresent = wire.IfRevision != nil
	if wire.IfRevision != nil {
		r.IfRevision = *wire.IfRevision
	}
	return nil
}

func (r *SetEnabledRequest) UnmarshalJSON(data []byte) error {
	type plain SetEnabledRequest
	var wire struct {
		*plain
		IfRevision *int64 `json:"ifRevision"`
	}
	*r = SetEnabledRequest{}
	wire.plain = (*plain)(r)
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	r.revisionPresent = wire.IfRevision != nil
	if wire.IfRevision != nil {
		r.IfRevision = *wire.IfRevision
	}
	return nil
}

func (e *Endpoints) Upsert(ctx context.Context, req UpsertRequest) (CuratedResponse, error) {
	if err := domain.RequireRevision(req.revisionPresent || req.IfRevision != 0); err != nil {
		return CuratedResponse{}, err
	}
	return e.apply(ctx, domain.Write{Op: domain.OpUpsert, EntryID: req.EntryID, Entry: req.Entry, IfRevision: req.IfRevision, Actor: domain.SharedAdminActor})
}

func (e *Endpoints) SetEnabled(ctx context.Context, req SetEnabledRequest) (CuratedResponse, error) {
	if err := domain.RequireRevision(req.revisionPresent || req.IfRevision != 0); err != nil {
		return CuratedResponse{}, err
	}
	return e.apply(ctx, domain.Write{Op: domain.OpSetEnabled, EntryID: req.EntryID, Enabled: req.Enabled, IfRevision: req.IfRevision, Actor: domain.SharedAdminActor})
}

type StaleRefusal struct {
	Current  int64
	Supplied int64
	Merged   bool
}

func (e StaleRefusal) Error() string                      { return "the registry moved on while you were editing" }
func (e StaleRefusal) Unwrap() error                      { return domain.ErrStaleRevision }
func AsStaleRefusal(err error, target *StaleRefusal) bool { return errors.As(err, target) }

func (e *Endpoints) apply(ctx context.Context, w domain.Write) (CuratedResponse, error) {
	doc, err := e.svc.Apply(ctx, w)
	if err == nil {
		return e.curate(doc), nil
	}
	var stale domain.StaleRevisionError
	if errors.As(err, &stale) {
		return CuratedResponse{}, StaleRefusal{Current: stale.Current, Supplied: stale.Supplied}
	}
	// Pass only known, address-free refusals through to the browser.
	for _, safe := range []error{domain.ErrOriginNotAllowed, domain.ErrNoEntry, domain.ErrNoEntryID, domain.ErrEntryIDMismatch, domain.ErrNoActor} {
		if errors.Is(err, safe) {
			return CuratedResponse{}, safe
		}
	}
	return CuratedResponse{}, errors.New("the write could not be applied")
}

type AuditRequest struct {
	Limit int `json:"limit"`
}

func (e *Endpoints) Audit(ctx context.Context, req AuditRequest) ([]postgres.AuditPage, error) {
	if e.audit == nil {
		return []postgres.AuditPage{}, nil
	}
	rows, err := e.audit.Audit(ctx, req.Limit)
	if err != nil {
		return nil, errors.New("the audit trail could not be read")
	}
	if rows == nil {
		rows = []postgres.AuditPage{}
	}
	return rows, nil
}

// PublishersResponse is the trust table as the operator surface sees it.
// Whole, never filtered: a revoked key is exactly what an operator has to be
// able to see in order to act on it.
type PublishersResponse struct {
	Revision   int64              `json:"revision"`
	Publishers []domain.Publisher `json:"publishers"`
}

func (e *Endpoints) Publishers(ctx context.Context) (PublishersResponse, error) {
	doc, err := e.svc.Publishers(ctx)
	if err != nil {
		return PublishersResponse{}, errors.New("the trust table could not be read")
	}
	if doc.Publishers == nil {
		doc.Publishers = []domain.Publisher{}
	}
	return PublishersResponse{Revision: doc.Revision, Publishers: doc.Publishers}, nil
}

// PublisherWriteRequest carries its op, unlike the entry surface where the op
// is the subject. See shared/mferegistry for why the trust table is one
// subject rather than four.
type PublisherWriteRequest struct {
	IfRevision      int64             `json:"ifRevision"`
	Op              string            `json:"op"`
	PublisherID     string            `json:"publisherId"`
	Publisher       *domain.Publisher `json:"publisher,omitempty"`
	PublicKey       string            `json:"publicKey,omitempty"`
	KeyState        string            `json:"keyState,omitempty"`
	PluginID        string            `json:"pluginId,omitempty"`
	revisionPresent bool
}

func (r *PublisherWriteRequest) UnmarshalJSON(data []byte) error {
	type plain PublisherWriteRequest
	var wire struct {
		*plain
		IfRevision *int64 `json:"ifRevision"`
	}
	*r = PublisherWriteRequest{}
	wire.plain = (*plain)(r)
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	r.revisionPresent = wire.IfRevision != nil
	if wire.IfRevision != nil {
		r.IfRevision = *wire.IfRevision
	}
	return nil
}

func (e *Endpoints) PublisherWrite(ctx context.Context, req PublisherWriteRequest) (PublishersResponse, error) {
	if err := domain.RequireRevision(req.revisionPresent || req.IfRevision != 0); err != nil {
		return PublishersResponse{}, err
	}
	doc, err := e.svc.ApplyPublisher(ctx, domain.PublisherWrite{
		Op:          req.Op,
		PublisherID: req.PublisherID,
		Actor:       domain.SharedAdminActor,
		Publisher:   req.Publisher,
		PublicKey:   req.PublicKey,
		KeyState:    req.KeyState,
		PluginID:    req.PluginID,
		IfRevision:  req.IfRevision,
	})
	if err == nil {
		if doc.Publishers == nil {
			doc.Publishers = []domain.Publisher{}
		}
		return PublishersResponse{Revision: doc.Revision, Publishers: doc.Publishers}, nil
	}
	var stale domain.StaleRevisionError
	if errors.As(err, &stale) {
		return PublishersResponse{}, StaleRefusal{Current: stale.Current, Supplied: stale.Supplied}
	}
	// Same rule as the entry surface: only known, address-free refusals reach
	// the browser, so a database error cannot leak its message (BR-AS04).
	for _, safe := range []error{
		domain.ErrNoActor, domain.ErrNoPublisher, domain.ErrNoPluginID, domain.ErrPublisherIDMismatch,
		domain.ErrBadPublisherKey, domain.ErrBadKeyState, domain.ErrNoPublisherRow, domain.ErrNoPublisherKey,
		domain.ErrUnknownOp,
	} {
		if errors.Is(err, safe) {
			return PublishersResponse{}, safe
		}
	}
	return PublishersResponse{}, errors.New("the write could not be applied")
}
