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
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/micro"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/trading-partner-service/tradingpartner/internal/application/commands"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/trading-partner-service/tradingpartner/internal/domain"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/trading-partner-service/tradingpartner/internal/natstrace"
)

// Subject constants. The {context} token is a wildcard here and resolved
// per-request by contextFromSubject — one registration serves every context in
// the account, exactly as pricing-service's do.
const (
	PartnerRegisterSubject   = "api.*.trading-partner.partner.register.v1"
	PartnerListSubject       = "api.*.trading-partner.partner.list.v1"
	PartnerGetSubject        = "api.*.trading-partner.partner.get.v1"
	PartnerActivateSubject   = "api.*.trading-partner.partner.activate.v1"
	PartnerSuspendSubject    = "api.*.trading-partner.partner.suspend.v1"
	PartnerReactivateSubject = "api.*.trading-partner.partner.reactivate.v1"
	PartnerAuditSubject      = "api.*.trading-partner.partner.audit.v1"

	DocumentAddSubject      = "api.*.trading-partner.document.add.v1"
	DocumentListSubject     = "api.*.trading-partner.document.list.v1"
	DocumentApproveSubject  = "api.*.trading-partner.document.approve.v1"
	DocumentRejectSubject   = "api.*.trading-partner.document.reject.v1"
	DocumentResubmitSubject = "api.*.trading-partner.document.resubmit.v1"

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
	FleetAssets     *commands.FleetAssetHandler
	Audit           domain.AuditReader

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

	tradingPartners *commands.TradingPartnerHandler
	documents       *commands.ComplianceDocumentHandler
	fleetAssets     *commands.FleetAssetHandler
	audit           domain.AuditReader
	tenant          string
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
		fleetAssets:     deps.FleetAssets,
		audit:           deps.Audit,
		tenant:          deps.Tenant,
	}

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
		{"partner-activate", a.handlePartnerActivate, PartnerActivateSubject},
		{"partner-suspend", a.handlePartnerSuspend, PartnerSuspendSubject},
		{"partner-reactivate", a.handlePartnerReactivate, PartnerReactivateSubject},
		{"partner-audit", a.handlePartnerAudit, PartnerAuditSubject},

		{"document-add", a.handleDocumentAdd, DocumentAddSubject},
		{"document-list", a.handleDocumentList, DocumentListSubject},
		{"document-approve", a.handleDocumentApprove, DocumentApproveSubject},
		{"document-reject", a.handleDocumentReject, DocumentRejectSubject},
		{"document-resubmit", a.handleDocumentResubmit, DocumentResubmitSubject},

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

type registerRequest struct {
	Name string             `json:"name"`
	Type domain.PartnerType `json:"type"`
}

type suspendRequest struct {
	ID     string `json:"id"`
	Reason string `json:"reason"`
}

type documentRequest struct {
	ID        string              `json:"id"`
	Type      domain.DocumentType `json:"type"`
	Reference string              `json:"reference,omitempty"`
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

type documentResponse struct {
	domain.ComplianceDocument
}

type documentsResponse struct {
	Documents []domain.ComplianceDocument `json:"documents"`
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

// errorResponse is the wire shape for every failed api.* call — same shape as
// pricing-service's/shipping-service's adapters, so a browser client handles
// every service's errors identically.
type errorResponse struct {
	Error    string `json:"error"`
	NotFound bool   `json:"notFound,omitempty"`
}

func isNotFoundErr(err error) bool {
	return errors.Is(err, domain.ErrTradingPartnerNotFound) ||
		errors.Is(err, domain.ErrDocumentNotFound)
}

// --- TradingPartner handlers (BR-TP01-BR-TP06) ---

func (a *Adapter) handlePartnerRegister(req micro.Request) {
	subject := req.Subject()
	var in registerRequest
	if err := json.Unmarshal(req.Data(), &in); err != nil {
		a.reply(req, nil, err)
		return
	}
	tp, err := a.tradingPartners.Register(
		context.Background(), a.actor(req), in.Name, in.Type, contextFromSubject(subject),
	)
	a.reply(req, tradingPartnerResponse{tp}, err)
}

func (a *Adapter) handlePartnerList(req micro.Request) {
	subject := req.Subject()
	partners, err := a.tradingPartners.List(context.Background(), contextFromSubject(subject))
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
	tp, err := a.tradingPartners.Activate(context.Background(), a.actor(req), in.ID)
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
// take (partnerID, docType) and return the updated document, so the only thing
// that varies is which command handler runs.
func (a *Adapter) documentTransition(
	req micro.Request,
	fn func(context.Context, string, domain.DocumentType) (domain.ComplianceDocument, error),
) {
	var in documentRequest
	if err := json.Unmarshal(req.Data(), &in); err != nil {
		a.reply(req, nil, err)
		return
	}
	doc, err := fn(context.Background(), in.ID, in.Type)
	a.reply(req, documentResponse{doc}, err)
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

// --- shared plumbing (mirrors pricing-service's browserrpc/adapter.go) ---

// contextFromSubject extracts the {context} token from an api.{context}...
// subject — the ONLY source of truth for which CONTEXT (company/business-unit
// scope) a request belongs to. Every handler above derives it this way rather
// than trusting a request body's own context field, so a client cannot spoof
// context scoping via the body. Context is NOT the tenant — the tenant
// boundary is the NATS account this connection authenticated into.
func contextFromSubject(subject string) string {
	parts := strings.Split(subject, ".")
	if len(parts) < 2 {
		return ""
	}
	return parts[1]
}

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
// result, any error replies with the mapped error response.
func (a *Adapter) reply(req micro.Request, result any, err error) {
	subject := req.Subject()
	correlationID := req.Reply()
	if err != nil {
		a.respondError(req, subject, correlationID, err)
		return
	}
	a.respond(req, subject, correlationID, result)
}

const (
	responderHeader = "Nats-Responder"
	requestorHeader = "Nats-Requestor"
)

func (a *Adapter) responderIdentity() string {
	info := a.svc.Info()
	return fmt.Sprintf("%s/%s", info.Name, info.ID)
}

// respond and respondError are the two request-tail exit points every
// handler reaches through reply(). Both finish this request's natstrace span
// (BR-036/BR-037/BR-TP15) via natstrace.SpanFrom — nil-safe, so a handler
// invoked directly (not through Tracer.Middleware, e.g. a unit test calling
// a.handleX(req) with a bare micro.Request) still replies correctly, it just
// publishes no span.
func (a *Adapter) respond(req micro.Request, subject, correlationID string, out any) {
	data, err := json.Marshal(out)
	if err != nil {
		a.respondError(req, subject, correlationID, err)
		return
	}
	headers := map[string][]string{responderHeader: {a.responderIdentity()}}
	natstrace.SpanFrom(req).End(data, headers)
	if err := req.Respond(data, micro.WithHeaders(micro.Headers(headers))); err != nil && a.log != nil {
		a.log.Error("browserrpc: respond failed", "subject", subject, "err", err)
	}
}

func (a *Adapter) respondError(req micro.Request, subject, correlationID string, err error) {
	data, _ := json.Marshal(errorResponse{Error: err.Error(), NotFound: isNotFoundErr(err)})
	code := "500"
	if isNotFoundErr(err) {
		code = "404"
	}
	headers := map[string][]string{
		micro.ErrorHeader:     {err.Error()},
		micro.ErrorCodeHeader: {code},
		responderHeader:       {a.responderIdentity()},
	}
	natstrace.SpanFrom(req).Fail(err, data, headers)
	if respErr := req.Respond(data, micro.WithHeaders(micro.Headers(headers))); respErr != nil && a.log != nil {
		a.log.Error("browserrpc: respond failed", "subject", subject, "err", respErr)
	}
}
