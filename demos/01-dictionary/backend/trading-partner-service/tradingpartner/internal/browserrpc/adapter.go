// Package browserrpc is trading-partner-service's api.* frontend-to-service
// NATS adapter (Phase 26g's micro registration + Phase 26h's endpoints),
// modeled on pricing-service's internal/browserrpc.
//
// Three things about the design, all deliberate:
//
//   - Like pricing-service's package of the same name, the wire subject family
//     is api.*, not rpc.*. Per CLAUDE.md's subject taxonomy (and
//     ARCHITECTURE-COMMUNICATIONS.md § 2), api.* is frontend-to-service and
//     rpc.* is service-to-service, and "a browser credential is never granted
//     rpc.>". The Admin UI is the only caller of this service today, so its
//     endpoints live under api.{context}.trading-partner.{entity}.{action}.v1 —
//     6 tokens, fixed arity, since parsers read {context} by position.
//     rpc.* endpoints get added when a *backend* caller actually exists — the
//     marketplace/tender phase that finally gives BR-TP04's Suspend an
//     enforcement consumer — not speculatively now.
//
//   - REST (internal/rest) used to stay wired and serve the same operations
//     as a dual transport, matching pricing-service — but Phase 33.5 deleted
//     that business REST outright (no admin/operator routes existed to
//     reclassify), so api.* is now the only business transport. REST is
//     /healthz only.
//
//   - Two things a handler here must never take from the request body, because
//     the transport already carries them authoritatively:
//     {context} comes from the subject (contextFromSubject), and the *tenant*
//     comes from this adapter's own connection — one Adapter per tenant NATS
//     connection (see internal/tenants), so deps.Tenant is the account the
//     caller authenticated into. That second point is why the api.* fleet-asset
//     endpoint has no `tenant` field even though REST's fleetAssetRequest does:
//     HTTP had no tenant identity to work from and had to trust the body
//     (see internal/domain/vehicle_type_validator.go), while NATS does. The
//     api.* path is strictly the safer of the two here.
package browserrpc

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/micro"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/trading-partner-service/tradingpartner/internal/application/commands"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/trading-partner-service/tradingpartner/internal/domain"
	profiledomain "github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/trading-partner-service/tradingpartner/transporterprofile/domain"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/trading-partner-service/tradingpartner/transporterprofile/orchestration"
	sharedbrowserrpc "github.com/jthomas78/nats-tech-lab/shared/browserrpc"
	"github.com/jthomas78/nats-tech-lab/shared/natstrace"
)

// Subject constants. The {context} token is a wildcard here and resolved
// per-request by contextFromSubject — one registration serves every context in
// the account, exactly as pricing-service's do.
const (
	PartnerRegisterSubject   = "api.*.trading-partner.partner.register.v1"
	PartnerListSubject       = "api.*.trading-partner.partner.list.v1"
	PartnerGetSubject        = "api.*.trading-partner.partner.get.v1"
	PartnerUpdateSubject     = "api.*.trading-partner.partner.update.v1"
	PartnerActivateSubject   = "api.*.trading-partner.partner.activate.v1"
	PartnerSuspendSubject    = "api.*.trading-partner.partner.suspend.v1"
	PartnerReactivateSubject = "api.*.trading-partner.partner.reactivate.v1"
	PartnerAuditSubject      = "api.*.trading-partner.partner.audit.v1"
	PartnerProfileSubject    = "api.*.trading-partner.partner.profile.v1"

	DocumentAddSubject      = "api.*.trading-partner.document.add.v1"
	DocumentListSubject     = "api.*.trading-partner.document.list.v1"
	DocumentApproveSubject  = "api.*.trading-partner.document.approve.v1"
	DocumentRejectSubject   = "api.*.trading-partner.document.reject.v1"
	DocumentResubmitSubject = "api.*.trading-partner.document.resubmit.v1"

	// Phase 38c-ii (BR-TP41). These endpoints mint capability tickets; the
	// bytes themselves never travel over NATS — see internal/rest's
	// document_files.go for why, and internal/filetickets for why the grant is
	// decided here, on an authenticated connection, rather than at the HTTP
	// ingress that spends it.
	DocumentUploadTicketSubject   = "api.*.trading-partner.document.upload-ticket.v1"
	DocumentDownloadTicketSubject = "api.*.trading-partner.document.download-ticket.v1"

	FleetAssetAddSubject  = "api.*.trading-partner.fleet-asset.add.v1"
	FleetAssetListSubject = "api.*.trading-partner.fleet-asset.list.v1"
)

// ServiceName must match the nats.Name(...) on the connection this adapter is
// registered against, so Nats-Responder/Nats-Requestor agree on one identity
// string per service — the same invariant pricing-service's and
// shipping-service's adapters hold (Phase 18).
const ServiceName = "trading-partner-service"

// ServiceVersion tracks the other services' 1.0.0 rather than starting at
// 0.x: this is the same service identity the REST API already serves, not a
// new pre-release thing.
const ServiceVersion = "1.0.0"

// Deps is what the adapter needs from composition — the same command handlers
// the REST layer calls (rest.Deps), plus the tenant identity of this
// adapter's connection.
type Deps struct {
	TradingPartners *commands.TradingPartnerHandler
	Documents       *commands.ComplianceDocumentHandler
	DocumentFiles   *commands.DocumentFileHandler
	FleetAssets     *commands.FleetAssetHandler
	Audit           domain.AuditReader
	// ProfileProjection is the canonical Postgres reader used by BR-TP19.
	// Deliberately no KV/cache interface is accepted at this boundary.
	ProfileProjection orchestration.CanonicalProjectionReader
	ProfileCommands   *orchestration.ProfileHandler

	// Tenant is the NATS account this adapter's connection authenticated into.
	// It labels the registration in $SRV metadata (trading-partner-service holds
	// one connection per tenant, so without it the Services panel would show
	// several indistinguishable rows for one service name) *and* supplies
	// BR-TP14's tenant for fleet-asset validation — see the package doc on why
	// that must not come from the request body.
	Tenant string
	Log    *slog.Logger
}

// Adapter owns one tenant connection's micro service registration + endpoints.
type Adapter struct {
	nc     *nats.Conn
	svc    micro.Service
	log    *slog.Logger
	tracer *natstrace.Tracer

	tradingPartners   *commands.TradingPartnerHandler
	activation        *orchestration.ActivationHandler
	profileProjection orchestration.CanonicalProjectionReader
	profiles          *orchestration.ProfileHandler
	documents         *commands.ComplianceDocumentHandler
	documentFiles     *commands.DocumentFileHandler
	fleetAssets       *commands.FleetAssetHandler
	audit             domain.AuditReader
	tenant            string
}

// New registers the micro service and its api.* endpoints on nc. nc is
// expected to be a single tenant's NATS connection (see internal/tenants) —
// both $SRV and api.* subjects resolve only within that connection's own
// account. Callers should Stop() the returned Adapter when that tenant
// connection is torn down.
func New(nc *nats.Conn, deps Deps) (*Adapter, error) {
	a := &Adapter{
		nc:              nc,
		log:             deps.Log,
		tracer:          natstrace.New(nc),
		tradingPartners: deps.TradingPartners,
		documents:       deps.Documents,
		documentFiles:   deps.DocumentFiles,
		fleetAssets:     deps.FleetAssets,
		audit:           deps.Audit,
		tenant:          deps.Tenant,
		profiles:        deps.ProfileCommands,

		// BR-TP37 reads the canonical projection directly — the same reader the
		// activation guard is given, deliberately not the KV cache.
		profileProjection: deps.ProfileProjection,
	}
	a.activation = orchestration.NewActivationHandler(a.tradingPartners, deps.ProfileProjection)

	svc, err := micro.AddService(nc, micro.Config{
		Name:        ServiceName,
		Version:     ServiceVersion,
		Description: "trading-partner-service api.* frontend-to-service endpoints (Phase 26h)",
		Metadata:    map[string]string{"tenant": deps.Tenant},
	})
	if err != nil {
		return nil, err
	}
	a.svc = svc

	endpoints := []struct {
		name    string
		handler micro.HandlerFunc
		subject string
	}{
		{"partner-register", a.handlePartnerRegister, PartnerRegisterSubject},
		{"partner-list", a.handlePartnerList, PartnerListSubject},
		{"partner-get", a.handlePartnerGet, PartnerGetSubject},
		{"partner-update", a.handlePartnerUpdate, PartnerUpdateSubject},
		{"partner-activate", a.handlePartnerActivate, PartnerActivateSubject},
		{"partner-suspend", a.handlePartnerSuspend, PartnerSuspendSubject},
		{"partner-reactivate", a.handlePartnerReactivate, PartnerReactivateSubject},
		{"partner-audit", a.handlePartnerAudit, PartnerAuditSubject},
		{"partner-profile", a.handlePartnerProfile, PartnerProfileSubject},

		{"document-add", a.handleDocumentAdd, DocumentAddSubject},
		{"document-list", a.handleDocumentList, DocumentListSubject},
		{"document-approve", a.handleDocumentApprove, DocumentApproveSubject},
		{"document-reject", a.handleDocumentReject, DocumentRejectSubject},
		{"document-resubmit", a.handleDocumentResubmit, DocumentResubmitSubject},
		{"document-upload-ticket", a.handleDocumentUploadTicket, DocumentUploadTicketSubject},
		{"document-download-ticket", a.handleDocumentDownloadTicket, DocumentDownloadTicketSubject},

		{"fleet-asset-add", a.handleFleetAssetAdd, FleetAssetAddSubject},
		{"fleet-asset-list", a.handleFleetAssetList, FleetAssetListSubject},
	}
	for _, ep := range endpoints {
		if err := svc.AddEndpoint(ep.name, a.tracer.Middleware(ep.handler), micro.WithEndpointSubject(ep.subject)); err != nil {
			_ = svc.Stop()
			return nil, err
		}
	}
	return a, nil
}

// Stop drains the adapter's subscriptions.
func (a *Adapter) Stop() error {
	if a.svc == nil {
		return nil
	}
	return a.svc.Stop()
}

// --- request/response wire shapes ---
// Deliberately the same JSON field names the REST layer uses, so the two
// transports are interchangeable from a client's point of view — with the two
// documented exceptions: no `context` field (subject-derived) and no `tenant`
// field on fleet-asset add (connection-derived).

type partnerIDRequest struct {
	ID string `json:"id"`
}

// registerRequest carries BR-TP02's required set plus BR-TP35's optional
// Company Information fields. `type` and `context` are absent from
// updateRequest below on purpose (BR-TP32) — they are settable only here, at
// registration.
type registerRequest struct {
	Name string             `json:"name"`
	Type domain.PartnerType `json:"type"`

	TradingAs         string `json:"tradingAs,omitempty"`
	CompanyName       string `json:"companyName,omitempty"`
	RegistrationNo    string `json:"registrationNo,omitempty"`
	VatRegistrationNo string `json:"vatRegistrationNo,omitempty"`
}

// updateRequest is BR-TP32's editable field set plus BR-TP34's expected
// version. There is deliberately no `type`, `context` or `status` field: the
// absence is the enforcement, so no future edit to this handler can pass one
// through by accident.
type updateRequest struct {
	ID      string `json:"id"`
	Version int    `json:"version"`

	Name              string `json:"name"`
	TradingAs         string `json:"tradingAs,omitempty"`
	CompanyName       string `json:"companyName,omitempty"`
	RegistrationNo    string `json:"registrationNo,omitempty"`
	VatRegistrationNo string `json:"vatRegistrationNo,omitempty"`
}

type suspendRequest struct {
	ID     string `json:"id"`
	Reason string `json:"reason"`
}

// documentRequest serves both add and the three review transitions. Add uses
// Type (which document is being supplied); the transitions use DocumentID
// (BR-TP31 — after BR-TP29 a type no longer identifies one row).
type documentRequest struct {
	ID         string              `json:"id"`
	Type       domain.DocumentType `json:"type,omitempty"`
	DocumentID string              `json:"documentId,omitempty"`
	Reference  string              `json:"reference,omitempty"`
}

type fleetAssetRequest struct {
	ID              string `json:"id"`
	RegistrationNo  string `json:"registrationNo"`
	VIN             string `json:"vin"`
	Make            string `json:"make"`
	Model           string `json:"model"`
	VehicleTypeCode string `json:"vehicleTypeCode"`
}

type tradingPartnerResponse struct {
	domain.TradingPartner
}

type tradingPartnersResponse struct {
	TradingPartners []domain.TradingPartner `json:"tradingPartners"`
}

// profileResponse is BR-TP37's wire shape. HasProfile distinguishes "this
// partner has no TransporterProfile" from "it has one in a zero-ish state" —
// a Shipper legitimately has none, and without an explicit flag a client
// cannot tell the two apart from the State fields alone.
type profileResponse struct {
	HasProfile bool                `json:"hasProfile"`
	Profile    profiledomain.State `json:"profile"`
	GitStatus  domain.GitStatus    `json:"gitStatus"`
}

type documentResponse struct {
	domain.ComplianceDocument
}

type documentsResponse struct {
	Documents []domain.ComplianceDocument `json:"documents"`
}

// documentTicketResponse is BR-TP41's wire shape. MaxBytes rides along so the
// browser can refuse an oversized file before spending a ticket and a minute
// uploading it — the server still enforces BR-TP44 regardless, since a client
// -side check is a courtesy, not a control.
type documentTicketResponse struct {
	Ticket   string `json:"ticket"`
	MaxBytes int64  `json:"maxBytes"`
}

type fleetAssetResponse struct {
	domain.FleetAsset
}

type fleetAssetsResponse struct {
	FleetAssets []domain.FleetAsset `json:"fleetAssets"`
}

type auditEntryResponse struct {
	ID        string         `json:"id"`
	Action    string         `json:"action"`
	Actor     string         `json:"actor"`
	SourceIP  string         `json:"sourceIp"`
	Outcome   string         `json:"outcome"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	CreatedAt string         `json:"createdAt"`
}

type auditResponse struct {
	Events []auditEntryResponse `json:"events"`
}

func isNotFoundErr(err error) bool {
	return errors.Is(err, domain.ErrTradingPartnerNotFound) ||
		errors.Is(err, domain.ErrDocumentNotFound) ||
		// BR-TP41: asking for a download ticket for a document that has no
		// bytes is a 404, not a failure — the document is real, the file is
		// simply not there.
		errors.Is(err, domain.ErrDocumentFileMissing)
}

// isConflictErr classifies the errors BUSINESS_RULES-TRADING-PARTNER.md
// describes as "rejected with 409 Conflict" — the illegal state transitions
// (BR-TP03-BR-TP05, BR-TP09-BR-TP11, BR-TP30) and BR-TP34's stale version.
//
// Before 38c-i every one of these came back as 500 over api.*: the 409s were
// written for the REST layer Phase 33.5 retired, and the shared api.* reply
// path only knew 404 and 500. A client could not tell "you cannot do that
// from this state" from "the service broke", which for an optimistic-
// concurrency conflict is the difference between "reload and retry" and
// "report a bug".
func isConflictErr(err error) bool {
	return errors.Is(err, domain.ErrVersionConflict) ||
		errors.Is(err, domain.ErrNotRegistered) ||
		errors.Is(err, domain.ErrNotActive) ||
		errors.Is(err, domain.ErrNotSuspended) ||
		errors.Is(err, domain.ErrDocumentNotPending) ||
		errors.Is(err, domain.ErrDocumentNotRejected) ||
		errors.Is(err, domain.ErrDocumentSuperseded) ||
		// BR-TP43: bytes are write-once, so "this already has a file" is the
		// same class of answer as an illegal transition — supersede and
		// re-upload, not retry.
		errors.Is(err, domain.ErrDocumentFileAlreadyAttached)
}

// --- TradingPartner handlers (BR-TP01-BR-TP06) ---

func (a *Adapter) handlePartnerRegister(req micro.Request) {
	subject := req.Subject()
	var in registerRequest
	if err := json.Unmarshal(req.Data(), &in); err != nil {
		a.reply(req, nil, err)
		return
	}
	contextKey := sharedbrowserrpc.ContextFromSubject(subject)
	tp, err := a.tradingPartners.Register(context.Background(), a.actor(req), in.Type, contextKey, domain.Details{
		Name:              in.Name,
		TradingAs:         in.TradingAs,
		CompanyName:       in.CompanyName,
		RegistrationNo:    in.RegistrationNo,
		VatRegistrationNo: in.VatRegistrationNo,
	})
	if err == nil && tp.Type == domain.PartnerTypeTransporter && a.profiles != nil {
		_, err = a.profiles.CreateTransporterProfile(context.Background(), contextKey, tp.ID)
	}
	a.reply(req, tradingPartnerResponse{tp}, err)
}

// handlePartnerUpdate is BR-TP32/BR-TP34's editable Company Information. A
// version mismatch surfaces as domain.ErrVersionConflict, which isConflictErr
// maps to 409 — the operator is told someone else changed the record, not
// that their input was invalid or that the service failed.
func (a *Adapter) handlePartnerUpdate(req micro.Request) {
	var in updateRequest
	if err := json.Unmarshal(req.Data(), &in); err != nil {
		a.reply(req, nil, err)
		return
	}
	tp, err := a.tradingPartners.UpdateDetails(context.Background(), a.actor(req), in.ID, in.Version, domain.Details{
		Name:              in.Name,
		TradingAs:         in.TradingAs,
		CompanyName:       in.CompanyName,
		RegistrationNo:    in.RegistrationNo,
		VatRegistrationNo: in.VatRegistrationNo,
	})
	a.reply(req, tradingPartnerResponse{tp}, err)
}

// handlePartnerProfile implements BR-TP37 — the browser's only route to
// vetting state. It reads the canonical Postgres projection (the same reader
// BR-TP19's activation guard uses), never Temporal's Query API: status must
// not acquire a hard runtime dependency on Temporal being reachable
// (ADR-047/ADR-049 finding 3).
//
// GIT status (BR-TP38) is derived here from the partner's current documents
// rather than from the profile, because it is a function of the documents.
// GitVerified on the profile is the saga's own branch flag — a different
// question ("did the workflow's verification step complete") from "does this
// Transporter have cover today", which is what an operator reads the badge
// for.
func (a *Adapter) handlePartnerProfile(req micro.Request) {
	var in partnerIDRequest
	if err := json.Unmarshal(req.Data(), &in); err != nil {
		a.reply(req, nil, err)
		return
	}
	ctx := context.Background()

	// Confirms the partner exists, so an unknown ID is a 404 rather than an
	// empty "no profile" answer that looks like a legitimate Shipper.
	tp, err := a.tradingPartners.Get(ctx, in.ID)
	if err != nil {
		a.reply(req, nil, err)
		return
	}

	docs, err := a.documents.ListDocuments(ctx, in.ID)
	if err != nil {
		a.reply(req, nil, err)
		return
	}
	out := profileResponse{GitStatus: domain.DeriveGitStatus(docs, time.Now().UTC())}

	// A Shipper has no profile by design, and a Transporter registered before
	// its profile was created has none yet. Neither is an error.
	if tp.Type == domain.PartnerTypeTransporter && a.profileProjection != nil {
		state, err := a.profileProjection.Get(ctx, in.ID)
		switch {
		case err == nil:
			out.HasProfile = true
			out.Profile = state
		case errors.Is(err, profiledomain.ErrNotFound):
			// Leave HasProfile false — nothing to report yet.
		default:
			a.reply(req, nil, err)
			return
		}
	}
	a.reply(req, out, nil)
}

func (a *Adapter) handlePartnerList(req micro.Request) {
	subject := req.Subject()
	partners, err := a.tradingPartners.List(context.Background(), sharedbrowserrpc.ContextFromSubject(subject))
	a.reply(req, tradingPartnersResponse{TradingPartners: partners}, err)
}

func (a *Adapter) handlePartnerGet(req micro.Request) {
	var in partnerIDRequest
	if err := json.Unmarshal(req.Data(), &in); err != nil {
		a.reply(req, nil, err)
		return
	}
	tp, err := a.tradingPartners.Get(context.Background(), in.ID)
	a.reply(req, tradingPartnerResponse{tp}, err)
}

func (a *Adapter) handlePartnerActivate(req micro.Request) {
	var in partnerIDRequest
	if err := json.Unmarshal(req.Data(), &in); err != nil {
		a.reply(req, nil, err)
		return
	}
	tp, err := a.activation.Activate(context.Background(), a.actor(req), in.ID)
	a.reply(req, tradingPartnerResponse{tp}, err)
}

func (a *Adapter) handlePartnerSuspend(req micro.Request) {
	var in suspendRequest
	if err := json.Unmarshal(req.Data(), &in); err != nil {
		a.reply(req, nil, err)
		return
	}
	tp, err := a.tradingPartners.Suspend(context.Background(), a.actor(req), in.ID, in.Reason)
	a.reply(req, tradingPartnerResponse{tp}, err)
}

func (a *Adapter) handlePartnerReactivate(req micro.Request) {
	var in partnerIDRequest
	if err := json.Unmarshal(req.Data(), &in); err != nil {
		a.reply(req, nil, err)
		return
	}
	tp, err := a.tradingPartners.Reactivate(context.Background(), a.actor(req), in.ID)
	a.reply(req, tradingPartnerResponse{tp}, err)
}

func (a *Adapter) handlePartnerAudit(req micro.Request) {
	var in partnerIDRequest
	if err := json.Unmarshal(req.Data(), &in); err != nil {
		a.reply(req, nil, err)
		return
	}
	events, err := a.audit.ListByPartner(context.Background(), in.ID)
	if err != nil {
		a.reply(req, nil, err)
		return
	}
	out := make([]auditEntryResponse, 0, len(events))
	for _, e := range events {
		out = append(out, auditEntryResponse{
			ID: e.ID, Action: e.Action, Actor: e.Actor, SourceIP: e.SourceIP,
			Outcome: e.Outcome, Metadata: e.Metadata,
			CreatedAt: e.CreatedAt.UTC().Format(time.RFC3339),
		})
	}
	a.reply(req, auditResponse{Events: out}, nil)
}

// --- ComplianceDocument handlers (BR-TP07-BR-TP11) ---

func (a *Adapter) handleDocumentAdd(req micro.Request) {
	var in documentRequest
	if err := json.Unmarshal(req.Data(), &in); err != nil {
		a.reply(req, nil, err)
		return
	}
	doc, err := a.documents.AddDocument(context.Background(), in.ID, in.Type, in.Reference)
	a.reply(req, documentResponse{doc}, err)
}

func (a *Adapter) handleDocumentList(req micro.Request) {
	var in partnerIDRequest
	if err := json.Unmarshal(req.Data(), &in); err != nil {
		a.reply(req, nil, err)
		return
	}
	docs, err := a.documents.ListDocuments(context.Background(), in.ID)
	a.reply(req, documentsResponse{Documents: docs}, err)
}

func (a *Adapter) handleDocumentApprove(req micro.Request) {
	a.documentTransition(req, a.documents.ApproveDocument)
}

func (a *Adapter) handleDocumentReject(req micro.Request) {
	a.documentTransition(req, a.documents.RejectDocument)
}

func (a *Adapter) handleDocumentResubmit(req micro.Request) {
	a.documentTransition(req, a.documents.ResubmitDocument)
}

// documentTransition is the shared body of approve/reject/resubmit — all three
// take (partnerID, documentID) and return the updated document, so the only
// thing that varies is which command handler runs.
func (a *Adapter) documentTransition(
	req micro.Request,
	fn func(context.Context, string, string) (domain.ComplianceDocument, error),
) {
	var in documentRequest
	if err := json.Unmarshal(req.Data(), &in); err != nil {
		a.reply(req, nil, err)
		return
	}
	doc, err := fn(context.Background(), in.ID, in.DocumentID)
	a.reply(req, documentResponse{doc}, err)
}

// --- Document file tickets (BR-TP41, Phase 38c-ii) ---

// handleDocumentUploadTicket and handleDocumentDownloadTicket are the only
// api.* endpoints whose result is a credential rather than data.
//
// Both take the tenant from a.tenant — this adapter's own authenticated
// connection — and {context} from the subject, exactly as every other endpoint
// here does, and neither reads either from the request body. That is the whole
// mechanism: by the time the HTTP ingress sees a ticket, the tenancy decision
// has already been made server-side by NATS, so the ingress never has to make
// one. See the package doc's first rule, and internal/filetickets.
func (a *Adapter) handleDocumentUploadTicket(req micro.Request) {
	a.documentTicket(req, func(ctx context.Context, contextKey, partnerID, documentID string) (string, error) {
		return a.documentFiles.MintUploadTicket(ctx, a.tenant, contextKey, partnerID, documentID)
	})
}

func (a *Adapter) handleDocumentDownloadTicket(req micro.Request) {
	a.documentTicket(req, func(ctx context.Context, contextKey, partnerID, documentID string) (string, error) {
		return a.documentFiles.MintDownloadTicket(ctx, a.tenant, contextKey, partnerID, documentID)
	})
}

func (a *Adapter) documentTicket(
	req micro.Request,
	mint func(ctx context.Context, contextKey, partnerID, documentID string) (string, error),
) {
	var in documentRequest
	if err := json.Unmarshal(req.Data(), &in); err != nil {
		a.reply(req, nil, err)
		return
	}
	if a.documentFiles == nil {
		a.reply(req, nil, domain.ErrDocumentFileMissing)
		return
	}
	token, err := mint(context.Background(), sharedbrowserrpc.ContextFromSubject(req.Subject()), in.ID, in.DocumentID)
	a.reply(req, documentTicketResponse{Ticket: token, MaxBytes: domain.MaxDocumentFileBytes}, err)
}

// --- FleetAsset handlers (BR-TP12-BR-TP14) ---

func (a *Adapter) handleFleetAssetAdd(req micro.Request) {
	var in fleetAssetRequest
	if err := json.Unmarshal(req.Data(), &in); err != nil {
		a.reply(req, nil, err)
		return
	}
	// a.tenant, not a body field: BR-TP14's refdata lookup rides the tenant's
	// own account import, and this adapter *is* that tenant's connection.
	// natstrace.ContextWithSpan (BR-037) carries this request's span down to
	// FleetAssetHandler -> Manager.Exists -> refdataclient.Client's outbound
	// rpc.* call, so that call's span continues this trace instead of
	// starting a disconnected root — the only place in this codebase ctx
	// carries a value beyond cancellation, and it costs the command/domain
	// layer nothing (ctx is already threaded everywhere unchanged).
	ctx := natstrace.ContextWithSpan(context.Background(), natstrace.SpanFrom(req))
	asset, err := a.fleetAssets.AddFleetAsset(
		ctx, in.ID, a.tenant,
		in.RegistrationNo, in.VIN, in.Make, in.Model, in.VehicleTypeCode,
	)
	a.reply(req, fleetAssetResponse{asset}, err)
}

func (a *Adapter) handleFleetAssetList(req micro.Request) {
	var in partnerIDRequest
	if err := json.Unmarshal(req.Data(), &in); err != nil {
		a.reply(req, nil, err)
		return
	}
	assets, err := a.fleetAssets.ListFleetAssets(context.Background(), in.ID)
	a.reply(req, fleetAssetsResponse{FleetAssets: assets}, err)
}

// --- shared plumbing (Phase 35: shared/browserrpc) ---

const (
	// actorHeader is the api.* equivalent of REST's X-Actor header.
	actorHeader = "X-Actor"
	// defaultActor is a placeholder for the not-yet-existent human identity
	// this service will eventually carry once WorkOS-backed human auth lands
	// for the whole POC. Pre-Phase-33.5 this mirrored REST's BasicAuthUser
	// value ("admin"); that REST layer is gone, but the placeholder name is
	// kept unchanged since nothing about the identity it stands in for has
	// changed.
	defaultActor = "admin"
	// requestorHeader is the caller-identity header shared/browserrpc's own
	// Nats-Responder mirrors on the reply side — this service is the only one
	// of the four that reads its own inbound copy (for BR-TP06's audit actor
	// SourceIP), so it stays local rather than moving to the shared package.
	requestorHeader = "Nats-Requestor"
)

// actor builds BR-TP06's audit actor: an optional client-supplied header
// over a fixed placeholder, neither authenticated — a standing placeholder
// until WorkOS-backed human auth lands for the whole POC.
//
// SourceIP has no NATS equivalent — micro.Request exposes no client address —
// so it carries the caller's Nats-Requestor identity instead, prefixed to make
// clear it is not an IP. Better than writing an empty column or, worse, a
// value a reader would mistake for a real address.
func (a *Adapter) actor(req micro.Request) commands.Actor {
	name := defaultActor
	if h := req.Headers().Get(actorHeader); h != "" {
		name = h
	}
	source := "nats"
	if r := req.Headers().Get(requestorHeader); r != "" {
		source = "nats:" + r
	}
	return commands.Actor{Name: name, SourceIP: source}
}

// reply is the shared tail end of every handler above: nil error replies with
// result, any error replies with the mapped error response. Delegates to
// shared/browserrpc.Reply, which derives subject/correlationID from req
// itself, marshals, finishes the natstrace span, and stamps Nats-Responder.
func (a *Adapter) reply(req micro.Request, result any, err error) {
	sharedbrowserrpc.ReplyWithConflicts(req, a.svc, a.log, isNotFoundErr, isConflictErr, result, err)
}
