// Package browserrpc is organizations-service's api.* frontend-to-service
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
//     endpoints live under api.{context}.organizations.{entity}.{action}.v1 —
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

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/internal/application/commands"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/internal/domain"
	profiledomain "github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/transporterprofile/domain"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/transporterprofile/orchestration"
	profileworker "github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/organizations-service/organizations/transporterprofile/worker"
	sharedbrowserrpc "github.com/jthomas78/nats-tech-lab/shared/browserrpc"
	"github.com/jthomas78/nats-tech-lab/shared/natstrace"
)

// Subject constants. The {context} token is a wildcard here and resolved
// per-request by contextFromSubject — one registration serves every context in
// the account, exactly as pricing-service's do.
const (
	OrganizationRegisterSubject   = "api.*.organizations.organization.register.v1"
	OrganizationListSubject       = "api.*.organizations.organization.list.v1"
	OrganizationGetSubject        = "api.*.organizations.organization.get.v1"
	OrganizationUpdateSubject     = "api.*.organizations.organization.update.v1"
	OrganizationActivateSubject   = "api.*.organizations.organization.activate.v1"
	OrganizationSuspendSubject    = "api.*.organizations.organization.suspend.v1"
	OrganizationReactivateSubject = "api.*.organizations.organization.reactivate.v1"
	OrganizationAuditSubject      = "api.*.organizations.organization.audit.v1"
	OrganizationProfileSubject    = "api.*.organizations.organization.profile.v1"

	// BR-TP56 — the first-submission entry point the vetting saga never had.
	// Symmetric with BR-TP26's resubmit, which this command routes to when the
	// profile is already Rejected.
	OrganizationSubmitVettingSubject = "api.*.organizations.organization.submit-vetting.v1"

	DocumentAddSubject      = "api.*.organizations.document.add.v1"
	DocumentListSubject     = "api.*.organizations.document.list.v1"
	DocumentApproveSubject  = "api.*.organizations.document.approve.v1"
	DocumentRejectSubject   = "api.*.organizations.document.reject.v1"
	DocumentResubmitSubject = "api.*.organizations.document.resubmit.v1"

	// DocumentSetExpirySubject is BR-TP59. Its own verb rather than a field
	// on approve: an expiry is a fact about the document, supplied when it
	// is registered or corrected, and folding it into the review would
	// conflate "this cover runs to date X" with "a human accepted it".
	DocumentSetExpirySubject = "api.*.organizations.document.set-expiry.v1"

	// Phase 38c-ii (BR-TP41). These endpoints mint capability tickets; the
	// bytes themselves never travel over NATS — see internal/rest's
	// document_files.go for why, and internal/filetickets for why the grant is
	// decided here, on an authenticated connection, rather than at the HTTP
	// ingress that spends it.
	DocumentUploadTicketSubject   = "api.*.organizations.document.upload-ticket.v1"
	// DocumentGitRegisterSubject is decision 28's one-call registration: it
	// registers the certificate and returns the ticket its bytes will be
	// spent against, so a drag-and-drop produces row and file from a single
	// gesture (decision 3). A separate endpoint rather than a flag on
	// document-add because it returns a credential as well as data, which is
	// a different response shape and a different security story.
	DocumentGitRegisterSubject = "api.*.organizations.document.git-register.v1"
	DocumentDownloadTicketSubject = "api.*.organizations.document.download-ticket.v1"

	FleetAssetAddSubject  = "api.*.organizations.fleet-asset.add.v1"
	FleetAssetListSubject = "api.*.organizations.fleet-asset.list.v1"

	// Operating areas (BR-TP46-BR-TP50). Like fleet-asset add, the add
	// endpoint takes no `tenant` field — BR-TP47's refdata lookup rides this
	// adapter's own tenant connection.
	// Tracking credentials (BR-TP51-BR-TP55). There is deliberately NO
	// get/read-payload subject: BR-TP52 means no api.* endpoint can return
	// credential material, and the absence is the enforcement.
	TrackingCredentialConfigureSubject = "api.*.organizations.tracking-credential.configure.v1"
	TrackingCredentialListSubject      = "api.*.organizations.tracking-credential.list.v1"

	OperatingAreaAddSubject    = "api.*.organizations.operating-area.add.v1"
	OperatingAreaListSubject   = "api.*.organizations.operating-area.list.v1"
	OperatingAreaRemoveSubject = "api.*.organizations.operating-area.remove.v1"
)

// ServiceName must match the nats.Name(...) on the connection this adapter is
// registered against, so Nats-Responder/Nats-Requestor agree on one identity
// string per service — the same invariant pricing-service's and
// shipping-service's adapters hold (Phase 18).
const ServiceName = "organizations-service"

// ServiceVersion tracks the other services' 1.0.0 rather than starting at
// 0.x: this is the same service identity the REST API already serves, not a
// new pre-release thing.
const ServiceVersion = "1.0.0"

// Deps is what the adapter needs from composition — the same command handlers
// the REST layer calls (rest.Deps), plus the tenant identity of this
// adapter's connection.
type Deps struct {
	Organizations  *commands.OrganizationHandler
	Documents      *commands.ComplianceDocumentHandler
	DocumentFiles  *commands.DocumentFileHandler
	FleetAssets    *commands.FleetAssetHandler
	OperatingAreas *commands.OperatingAreaHandler
	TrackingCreds  *commands.TrackingCredentialHandler
	Audit          domain.AuditReader
	// ProfileProjection is the canonical Postgres reader used by BR-TP19.
	// Deliberately no KV/cache interface is accepted at this boundary.
	ProfileProjection orchestration.CanonicalProjectionReader
	ProfileCommands   *orchestration.ProfileHandler

	// VettingFactory builds Vetting for one tenant. A factory rather than a
	// ready-made gateway because BR-TP26's resubmit appends to that tenant's
	// own JetStream stream, so the ProfileHandler inside it is per-connection
	// — the same reason ProfileCommands is filled in per tenant rather than
	// passed once. Nil leaves Vetting nil; see below.
	VettingFactory VettingFactory

	// Vetting starts BR-TP56's workflow and carries BR-TP57's review signals.
	// Nil where no Temporal worker is composed (tests, and any deployment
	// without one): submit then answers with an explicit error rather than
	// accepting a command it cannot honour, and a review still writes its row
	// and simply signals nothing.
	Vetting VettingGateway

	// Tenant is the NATS account this adapter's connection authenticated into.
	// It labels the registration in $SRV metadata (organizations-service holds
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

	organizations     *commands.OrganizationHandler
	activation        *orchestration.ActivationHandler
	profileProjection orchestration.CanonicalProjectionReader
	profiles          *orchestration.ProfileHandler
	documents         *commands.ComplianceDocumentHandler
	documentFiles     *commands.DocumentFileHandler
	fleetAssets       *commands.FleetAssetHandler
	operatingAreas    *commands.OperatingAreaHandler
	trackingCreds     *commands.TrackingCredentialHandler
	audit             domain.AuditReader
	vetting           VettingGateway
	tenant            string
}

// New registers the micro service and its api.* endpoints on nc. nc is
// expected to be a single tenant's NATS connection (see internal/tenants) —
// both $SRV and api.* subjects resolve only within that connection's own
// account. Callers should Stop() the returned Adapter when that tenant
// connection is torn down.
func New(nc *nats.Conn, deps Deps) (*Adapter, error) {
	a := &Adapter{
		nc:             nc,
		log:            deps.Log,
		tracer:         natstrace.New(nc),
		organizations:  deps.Organizations,
		documents:      deps.Documents,
		documentFiles:  deps.DocumentFiles,
		fleetAssets:    deps.FleetAssets,
		operatingAreas: deps.OperatingAreas,
		trackingCreds:  deps.TrackingCreds,
		audit:          deps.Audit,
		vetting:        deps.Vetting,
		tenant:         deps.Tenant,
		profiles:       deps.ProfileCommands,

		// BR-TP37 reads the canonical projection directly — the same reader the
		// activation guard is given, deliberately not the KV cache.
		profileProjection: deps.ProfileProjection,
	}
	a.activation = orchestration.NewActivationHandler(a.organizations, deps.ProfileProjection)

	svc, err := micro.AddService(nc, micro.Config{
		Name:        ServiceName,
		Version:     ServiceVersion,
		Description: "organizations-service api.* frontend-to-service endpoints (Phase 26h)",
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
		{"partner-register", a.handlePartnerRegister, OrganizationRegisterSubject},
		{"partner-list", a.handlePartnerList, OrganizationListSubject},
		{"partner-get", a.handlePartnerGet, OrganizationGetSubject},
		{"partner-update", a.handlePartnerUpdate, OrganizationUpdateSubject},
		{"partner-activate", a.handlePartnerActivate, OrganizationActivateSubject},
		{"partner-suspend", a.handlePartnerSuspend, OrganizationSuspendSubject},
		{"partner-reactivate", a.handlePartnerReactivate, OrganizationReactivateSubject},
		{"partner-audit", a.handlePartnerAudit, OrganizationAuditSubject},
		{"partner-profile", a.handlePartnerProfile, OrganizationProfileSubject},
		{"organization-submit-vetting", a.handleSubmitVetting, OrganizationSubmitVettingSubject},

		{"document-add", a.handleDocumentAdd, DocumentAddSubject},
		{"document-set-expiry", a.handleDocumentSetExpiry, DocumentSetExpirySubject},
		{"document-list", a.handleDocumentList, DocumentListSubject},
		{"document-approve", a.handleDocumentApprove, DocumentApproveSubject},
		{"document-reject", a.handleDocumentReject, DocumentRejectSubject},
		{"document-resubmit", a.handleDocumentResubmit, DocumentResubmitSubject},
		{"document-git-register", a.handleDocumentGitRegister, DocumentGitRegisterSubject},
		{"document-upload-ticket", a.handleDocumentUploadTicket, DocumentUploadTicketSubject},
		{"document-download-ticket", a.handleDocumentDownloadTicket, DocumentDownloadTicketSubject},

		{"fleet-asset-add", a.handleFleetAssetAdd, FleetAssetAddSubject},
		{"fleet-asset-list", a.handleFleetAssetList, FleetAssetListSubject},
		{"operating-area-add", a.handleOperatingAreaAdd, OperatingAreaAddSubject},
		{"operating-area-list", a.handleOperatingAreaList, OperatingAreaListSubject},
		{"operating-area-remove", a.handleOperatingAreaRemove, OperatingAreaRemoveSubject},
		{"tracking-credential-configure", a.handleTrackingCredentialConfigure, TrackingCredentialConfigureSubject},
		{"tracking-credential-list", a.handleTrackingCredentialList, TrackingCredentialListSubject},
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
	// ExpiresAt is BR-TP59's optional expiry, Unix seconds like every other
	// instant on this surface. A pointer so "not supplied" stays distinct
	// from "cleared" — set-expiry uses null to clear, and a plain int64
	// would make an omitted field indistinguishable from the epoch.
	ExpiresAt *int64 `json:"expiresAt,omitempty"`

	// --- GIT certificate fields (39a) ---------------------------------
	// GoodsTypes and CoverageCents are registration-time (BR-TP64/BR-TP65);
	// the three insurance fields are approval-time (BR-TP66) and are read
	// only by the approve endpoint. The contact pair is the one input on
	// this surface that never reaches the event log (BR-TP72).
	GoodsTypes             []string `json:"goodsTypes,omitempty"`
	CoverageCents          *int64   `json:"coverageCents,omitempty"`
	InsurerName            string   `json:"insurerName,omitempty"`
	InsuranceContactName   string   `json:"insuranceContactName,omitempty"`
	InsuranceContactNumber string   `json:"insuranceContactNumber,omitempty"`
}

type fleetAssetRequest struct {
	ID              string `json:"id"`
	RegistrationNo  string `json:"registrationNo"`
	VIN             string `json:"vin"`
	Make            string `json:"make"`
	Model           string `json:"model"`
	VehicleTypeCode string `json:"vehicleTypeCode"`
}

type organizationResponse struct {
	domain.Organization
}

type organizationsResponse struct {
	Organizations []domain.Organization `json:"organizations"`
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

// documentRegistrationResponse is decision 28's combined reply: the row that
// now exists, and the ticket to spend on its bytes.
type documentRegistrationResponse struct {
	Document domain.ComplianceDocument `json:"document"`
	Ticket   string                    `json:"ticket"`
	MaxBytes int64                     `json:"maxBytes"`
}

// operatingAreaRequest carries no tenant and no countryCode. The tenant is
// the adapter's connection (same reasoning as fleetAssetRequest); the
// country is resolved from refdata's `country` relation (BR-D47), never
// taken from the caller — a client-supplied parent would let a caller
// misfile a region and defeat BR-TP48's overlap check.
type operatingAreaRequest struct {
	ID    string           `json:"id"`
	Level domain.AreaLevel `json:"level"`
	Code  string           `json:"code"`
}

type operatingAreaResponse struct {
	OperatingArea domain.OperatingArea `json:"operatingArea"`
}

type operatingAreasResponse struct {
	OperatingAreas []domain.OperatingArea `json:"operatingAreas"`
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
	return errors.Is(err, domain.ErrOrganizationNotFound) ||
		errors.Is(err, domain.ErrDocumentNotFound) ||
		// BR-TP41: asking for a download ticket for a document that has no
		// bytes is a 404, not a failure — the document is real, the file is
		// simply not there.
		errors.Is(err, domain.ErrDocumentFileMissing)
}

// isConflictErr classifies the errors BUSINESS_RULES-ORGANIZATIONS.md
// describes as "rejected with 409 Conflict" — the illegal state transitions
// (BR-TP03-BR-TP05, BR-TP09-BR-TP11, BR-TP30) and BR-TP34's stale version.
//
// Before 38c-i every one of these came back as 500 over api.*: the 409s were
// written for the REST layer Phase 33.5 retired, and the shared api.* reply
// path only knew 404 and 500. A client could not tell "you cannot do that
// from this state" from "the service broke", which for an optimistic-
// concurrency conflict is the difference between "reload and retry" and
// "report a bug".
// VettingGateway is the transporterprofile worker facade, narrowed to what
// this boundary uses. An interface rather than the concrete *VettingService
// so browserrpc keeps depending on behaviour, not on the Temporal SDK.
// VettingFactory builds a tenant's VettingGateway around that tenant's own
// profile command handler.
type VettingFactory func(tenant string, commands *orchestration.ProfileHandler) VettingGateway

type VettingGateway interface {
	Submit(ctx context.Context, tenant, contextKey, organizationID string) error
	SignalDocumentReview(ctx context.Context, contextKey, organizationID, reference string, approved bool) error
	// SignalCoverChanged re-arms BR-TP61's cover timer.
	SignalCoverChanged(ctx context.Context, contextKey, organizationID string) error
}

// ErrVettingUnavailable is the answer when no worker is composed. Deliberately
// not a silent success: accepting a submit that nothing will ever run would
// leave the transporter looking submitted forever.
var ErrVettingUnavailable = errors.New("vetting worker is not available")

// handleSubmitVetting is BR-TP56. The tenant comes from the adapter's own
// connection, never the request body — same reason BR-TP14's fleet-asset
// validation takes it from there (see the package doc).
func (a *Adapter) handleSubmitVetting(req micro.Request) {
	var in partnerIDRequest
	if err := json.Unmarshal(req.Data(), &in); err != nil {
		a.reply(req, nil, err)
		return
	}
	if a.vetting == nil {
		a.reply(req, nil, ErrVettingUnavailable)
		return
	}
	contextKey := sharedbrowserrpc.ContextFromSubject(req.Subject())
	if err := a.vetting.Submit(context.Background(), a.tenant, contextKey, in.ID); err != nil {
		a.reply(req, nil, err)
		return
	}
	tp, err := a.organizations.Get(context.Background(), in.ID)
	a.reply(req, organizationResponse{tp}, err)
}

func isConflictErr(err error) bool {
	return errors.Is(err, profileworker.ErrNotTransporter) ||
		errors.Is(err, profileworker.ErrProfileNotSubmittable) ||
		errors.Is(err, profileworker.ErrNoPendingDocuments) ||
		errors.Is(err, domain.ErrVersionConflict) ||
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

// --- Organization handlers (BR-TP01-BR-TP06) ---

func (a *Adapter) handlePartnerRegister(req micro.Request) {
	subject := req.Subject()
	var in registerRequest
	if err := json.Unmarshal(req.Data(), &in); err != nil {
		a.reply(req, nil, err)
		return
	}
	contextKey := sharedbrowserrpc.ContextFromSubject(subject)
	tp, err := a.organizations.Register(context.Background(), a.actor(req), in.Type, contextKey, domain.Details{
		Name:              in.Name,
		TradingAs:         in.TradingAs,
		CompanyName:       in.CompanyName,
		RegistrationNo:    in.RegistrationNo,
		VatRegistrationNo: in.VatRegistrationNo,
	})
	if err == nil && tp.Type == domain.PartnerTypeTransporter && a.profiles != nil {
		_, err = a.profiles.CreateTransporterProfile(context.Background(), contextKey, tp.ID)
	}
	a.reply(req, organizationResponse{tp}, err)
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
	tp, err := a.organizations.UpdateDetails(context.Background(), a.actor(req), in.ID, in.Version, domain.Details{
		Name:              in.Name,
		TradingAs:         in.TradingAs,
		CompanyName:       in.CompanyName,
		RegistrationNo:    in.RegistrationNo,
		VatRegistrationNo: in.VatRegistrationNo,
	})
	a.reply(req, organizationResponse{tp}, err)
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
	tp, err := a.organizations.Get(ctx, in.ID)
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
	partners, err := a.organizations.List(context.Background(), sharedbrowserrpc.ContextFromSubject(subject))
	a.reply(req, organizationsResponse{Organizations: partners}, err)
}

func (a *Adapter) handlePartnerGet(req micro.Request) {
	var in partnerIDRequest
	if err := json.Unmarshal(req.Data(), &in); err != nil {
		a.reply(req, nil, err)
		return
	}
	tp, err := a.organizations.Get(context.Background(), in.ID)
	a.reply(req, organizationResponse{tp}, err)
}

func (a *Adapter) handlePartnerActivate(req micro.Request) {
	var in partnerIDRequest
	if err := json.Unmarshal(req.Data(), &in); err != nil {
		a.reply(req, nil, err)
		return
	}
	tp, err := a.activation.Activate(context.Background(), a.actor(req), in.ID)
	a.reply(req, organizationResponse{tp}, err)
}

func (a *Adapter) handlePartnerSuspend(req micro.Request) {
	var in suspendRequest
	if err := json.Unmarshal(req.Data(), &in); err != nil {
		a.reply(req, nil, err)
		return
	}
	tp, err := a.organizations.Suspend(context.Background(), a.actor(req), in.ID, in.Reason)
	a.reply(req, organizationResponse{tp}, err)
}

func (a *Adapter) handlePartnerReactivate(req micro.Request) {
	var in partnerIDRequest
	if err := json.Unmarshal(req.Data(), &in); err != nil {
		a.reply(req, nil, err)
		return
	}
	tp, err := a.organizations.Reactivate(context.Background(), a.actor(req), in.ID)
	a.reply(req, organizationResponse{tp}, err)
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
	contextKey := sharedbrowserrpc.ContextFromSubject(req.Subject())
	if in.Type == domain.DocumentTypeGoodsInTransit {
		// 39a: GIT registration is event-sourced (ADR-050 Option A) and never
		// gated — an early renewal may be registered while current cover is
		// live (BR-TP68). No cover-changed signal is sent: unlike the old
		// supersede-on-register path, registering changes no cover at all.
		// Approval does (BR-TP69), and that is where the signal now belongs.
		doc, err := a.documents.AddGitDocument(context.Background(), a.tenant, contextKey,
			in.ID, in.Reference, in.ExpiresAt, in.CoverageCents, in.GoodsTypes, a.actor(req))
		a.reply(req, documentResponse{doc}, err)
		return
	}
	doc, err := a.documents.AddDocument(context.Background(), in.ID, in.Type, in.Reference, in.ExpiresAt)
	a.reply(req, documentResponse{doc}, err)
}

// coverChangedSignal is BR-TP61's re-arm, and follows BR-TP57's shape exactly:
// after the row is written, never in front of it, and logged rather than
// fatal. The failure it leaves open is a timer still armed against the old
// expiry — which is why it is logged: nothing reconciles the two
// automatically, and the next document write is the next chance to correct it.
func (a *Adapter) coverChangedSignal(req micro.Request, organizationID string) {
	if a.vetting == nil {
		return
	}
	contextKey := sharedbrowserrpc.ContextFromSubject(req.Subject())
	if err := a.vetting.SignalCoverChanged(context.Background(), contextKey, organizationID); err != nil && a.log != nil {
		a.log.Warn("cover-changed signal failed; a vetted transporter may still be armed against its previous expiry",
			"context", contextKey, "organization", organizationID, "err", err)
	}
}

// handleDocumentSetExpiry implements BR-TP59. It sends no review signal
// (unlike approve/reject): an expiry change is not a review decision, and the
// vetting workflow is not waiting on one.
func (a *Adapter) handleDocumentSetExpiry(req micro.Request) {
	if in, ok := a.gitRequest(req); ok {
		doc, err := a.documents.SetGitDocumentExpiry(context.Background(), a.tenant,
			sharedbrowserrpc.ContextFromSubject(req.Subject()), in.ID, in.DocumentID, in.ExpiresAt, a.actor(req))
		if err == nil {
			a.coverChangedSignal(req, in.ID)
		}
		a.reply(req, documentResponse{doc}, err)
		return
	}
	a.handleDocumentSetExpiryCRUD(req)
}

// gitRequest decodes the request and reports whether it names a GIT document.
// Decoding twice is the cost of routing on the stored type rather than on a
// caller-supplied one, and it is the right trade: a client must not be able to
// pick which write path its document takes.
func (a *Adapter) gitRequest(req micro.Request) (documentRequest, bool) {
	var in documentRequest
	if err := json.Unmarshal(req.Data(), &in); err != nil {
		return in, false
	}
	return in, a.isGitDocument(in.ID, in.DocumentID)
}

func (a *Adapter) handleDocumentSetExpiryCRUD(req micro.Request) {
	var in documentRequest
	if err := json.Unmarshal(req.Data(), &in); err != nil {
		a.reply(req, nil, err)
		return
	}
	doc, err := a.documents.SetDocumentExpiry(context.Background(), in.ID, in.DocumentID, in.ExpiresAt)
	if err == nil && doc.Type == domain.DocumentTypeGoodsInTransit {
		a.coverChangedSignal(req, in.ID)
	}
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

// reviewSignal is BR-TP57. It runs after the row write and never in front of
// it: the review is the authoritative act, and a workflow that has already
// finished (or was never started) must not turn a legitimate approval into an
// error. Best-effort in the same sense as BR-TP06's audit writes — logged,
// never fatal.
//
// The failure mode this deliberately leaves open, and the reason it is logged
// rather than swallowed: a review that writes its row and then fails to signal
// reads as approved while the workflow still waits on it. Nothing reconciles
// the two automatically; the operator re-reviews, or the attempt times out.
func (a *Adapter) reviewSignal(req micro.Request, organizationID, documentID string, approved bool) {
	if a.vetting == nil || documentID == "" {
		return
	}
	contextKey := sharedbrowserrpc.ContextFromSubject(req.Subject())
	err := a.vetting.SignalDocumentReview(context.Background(), contextKey, organizationID, documentID, approved)
	// a.log is nil when Deps.Log was not supplied — the only other use of it
	// goes through ReplyWithConflicts, which does its own nil check, so this
	// path is the first that could dereference it. Swallowing the log is the
	// right trade against panicking inside a review that already succeeded.
	if err != nil && a.log != nil {
		a.log.Warn("document review signal failed; the row is written but the vetting workflow was not told",
			"context", contextKey, "organization", organizationID, "document", documentID,
			"approved", approved, "err", err)
	}
}

func (a *Adapter) handleDocumentApprove(req micro.Request) {
	var in documentRequest
	if err := json.Unmarshal(req.Data(), &in); err != nil {
		a.reply(req, nil, err)
		return
	}
	// A GIT approval is a different command, not the same one with extra
	// fields: it carries BR-TP66's insurance requirements, supersedes and
	// locks every earlier certificate (BR-TP69), and is the sole producer of
	// document-approved on the stream (decision 14).
	if a.isGitDocument(in.ID, in.DocumentID) {
		contextKey := sharedbrowserrpc.ContextFromSubject(req.Subject())
		doc, err := a.documents.ApproveGitDocument(context.Background(), a.tenant, contextKey,
			in.ID, in.DocumentID, in.InsurerName, in.InsuranceContactName, in.InsuranceContactNumber, a.actor(req))
		if err == nil {
			// Approval is what moves cover now, so this is where BR-TP61's
			// re-arm belongs — registration no longer changes any expiry.
			a.coverChangedSignal(req, in.ID)
			a.reviewSignal(req, in.ID, doc.ID, true)
		}
		a.reply(req, documentResponse{doc}, err)
		return
	}
	a.documentTransition(req, a.documents.ApproveDocument, reviewApproved)
}

// isGitDocument reads the document's type before choosing a command. A lookup
// rather than a flag on the request: the caller does not get to decide which
// write path its document takes, and a client that sent the wrong type would
// otherwise pick the wrong one.
func (a *Adapter) isGitDocument(partnerID, documentID string) bool {
	if partnerID == "" || documentID == "" {
		return false
	}
	doc, err := a.documents.GetDocument(context.Background(), partnerID, documentID)
	return err == nil && doc.Type == domain.DocumentTypeGoodsInTransit
}

func (a *Adapter) handleDocumentReject(req micro.Request) {
	a.documentTransition(req, a.documents.RejectDocument, reviewRejected)
}

func (a *Adapter) handleDocumentResubmit(req micro.Request) {
	// Resubmit deliberately sends no signal: a rejection already ended the
	// attempt (BR-TP23), and the workflow's required set was fixed at submit
	// time. Putting the document back to Pending is what makes it eligible for
	// the *next* attempt, which BR-TP56 starts.
	a.documentTransition(req, a.documents.ResubmitDocument, reviewNoSignal)
}

// documentTransition is the shared body of approve/reject/resubmit — all three
// take (partnerID, documentID) and return the updated document, so the only
// thing that varies is which command handler runs.
// reviewOutcome says whether a document transition carries a BR-TP57 signal,
// and with what verdict. A three-valued type rather than a bool so resubmit's
// "no signal at all" is stated at the call site instead of inferred.
type reviewOutcome int

const (
	reviewNoSignal reviewOutcome = iota
	reviewApproved
	reviewRejected
)

func (a *Adapter) documentTransition(
	req micro.Request,
	fn func(context.Context, string, string) (domain.ComplianceDocument, error),
	outcome reviewOutcome,
) {
	var in documentRequest
	if err := json.Unmarshal(req.Data(), &in); err != nil {
		a.reply(req, nil, err)
		return
	}
	doc, err := fn(context.Background(), in.ID, in.DocumentID)
	// Only a transition that actually happened is signalled — a rejected
	// approval (BR-TP09's not-Pending guard) changed nothing, so telling the
	// workflow about it would advance an attempt on a document that never
	// moved.
	if err == nil && outcome != reviewNoSignal {
		a.reviewSignal(req, in.ID, doc.ID, outcome == reviewApproved)
	}
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
		return a.documentFiles.MintUploadTicket(ctx, a.tenant, contextKey, partnerID, documentID, a.actor(req))
	})
}

func (a *Adapter) handleDocumentDownloadTicket(req micro.Request) {
	a.documentTicket(req, func(ctx context.Context, contextKey, partnerID, documentID string) (string, error) {
		return a.documentFiles.MintDownloadTicket(ctx, a.tenant, contextKey, partnerID, documentID)
	})
}

// handleDocumentGitRegister is the drop zone's endpoint (decision 28).
//
// It registers first and mints second, and the failure that leaves open is a
// PENDING certificate with no file — not a broken state but the documented
// one BR-TP68 already names ("row minted, no file yet"). The reverse order
// has no meaning: a ticket names a document, so there is nothing to mint
// against until the row exists.
func (a *Adapter) handleDocumentGitRegister(req micro.Request) {
	var in documentRequest
	if err := json.Unmarshal(req.Data(), &in); err != nil {
		a.reply(req, nil, err)
		return
	}
	if a.documentFiles == nil {
		a.reply(req, nil, domain.ErrDocumentFileMissing)
		return
	}
	contextKey := sharedbrowserrpc.ContextFromSubject(req.Subject())
	actor := a.actor(req)
	doc, err := a.documents.AddGitDocument(context.Background(), a.tenant, contextKey,
		in.ID, in.Reference, in.ExpiresAt, in.CoverageCents, in.GoodsTypes, actor)
	if err != nil {
		a.reply(req, nil, err)
		return
	}
	ticket, err := a.documentFiles.MintUploadTicket(context.Background(), a.tenant, contextKey, in.ID, doc.ID, actor)
	a.reply(req, documentRegistrationResponse{
		Document: doc, Ticket: ticket, MaxBytes: domain.MaxDocumentFileBytes,
	}, err)
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

// --- TrackingCredential handlers (BR-TP51-BR-TP55) ---

// trackingCredentialRequest is the only place a secret crosses this
// boundary. It arrives over the browser's authenticated per-tenant NATS
// connection, is handed straight to the sealing store, and is never echoed:
// the response carries the non-secret record only.
type trackingCredentialRequest struct {
	ID             string                `json:"id"`
	Provider       domain.Provider       `json:"provider"`
	CredentialType domain.CredentialType `json:"credentialType"`
	// Payload is the credential material itself (an API key, a
	// username/password blob). BR-TP52: it goes to the sealed KV bucket and
	// nowhere else — not Postgres, not the event log, not any reply.
	Payload string `json:"payload"`
}

type trackingCredentialResponse struct {
	TrackingCredential domain.TrackingCredential `json:"trackingCredential"`
}

type trackingCredentialsResponse struct {
	TrackingCredentials []domain.TrackingCredential `json:"trackingCredentials"`
}

func (a *Adapter) handleTrackingCredentialConfigure(req micro.Request) {
	var in trackingCredentialRequest
	if err := json.Unmarshal(req.Data(), &in); err != nil {
		a.reply(req, nil, err)
		return
	}
	cred, err := a.trackingCreds.ConfigureTrackingCredential(
		context.Background(), in.ID, a.tenant, in.Provider, in.CredentialType, []byte(in.Payload),
	)
	// cred carries provider/credentialType/configured only — the request's
	// Payload is not part of it and cannot be echoed back.
	a.reply(req, trackingCredentialResponse{TrackingCredential: cred}, err)
}

func (a *Adapter) handleTrackingCredentialList(req micro.Request) {
	var in partnerIDRequest
	if err := json.Unmarshal(req.Data(), &in); err != nil {
		a.reply(req, nil, err)
		return
	}
	creds, err := a.trackingCreds.ListTrackingCredentials(context.Background(), in.ID)
	a.reply(req, trackingCredentialsResponse{TrackingCredentials: creds}, err)
}

// --- OperatingArea handlers (BR-TP46-BR-TP50) ---

func (a *Adapter) handleOperatingAreaAdd(req micro.Request) {
	var in operatingAreaRequest
	if err := json.Unmarshal(req.Data(), &in); err != nil {
		a.reply(req, nil, err)
		return
	}
	// Same span propagation as fleet-asset add (BR-037): BR-TP47's refdata
	// lookup is an outbound rpc.* call that should continue this trace.
	ctx := natstrace.ContextWithSpan(context.Background(), natstrace.SpanFrom(req))
	area, err := a.operatingAreas.AddOperatingArea(ctx, a.actor(req), in.ID, a.tenant, in.Level, in.Code)
	a.reply(req, operatingAreaResponse{OperatingArea: area}, err)
}

func (a *Adapter) handleOperatingAreaList(req micro.Request) {
	var in partnerIDRequest
	if err := json.Unmarshal(req.Data(), &in); err != nil {
		a.reply(req, nil, err)
		return
	}
	areas, err := a.operatingAreas.ListOperatingAreas(context.Background(), in.ID)
	a.reply(req, operatingAreasResponse{OperatingAreas: areas}, err)
}

func (a *Adapter) handleOperatingAreaRemove(req micro.Request) {
	var in operatingAreaRequest
	if err := json.Unmarshal(req.Data(), &in); err != nil {
		a.reply(req, nil, err)
		return
	}
	err := a.operatingAreas.RemoveOperatingArea(context.Background(), a.actor(req), in.ID, in.Level, in.Code)
	a.reply(req, operatingAreaResponse{}, err)
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
