// Package browserrpc is pricing-service's api.* frontend-to-service adapter
// — named to match shipping-service's own `browserrpc` package even though
// the wire subject family is api.*, not rpc.* (shipping-service kept that
// name across its own Phase 16b rename; following the same convention here
// keeps "browserrpc" greppable as "the api.* adapter package" across every
// service in this repo). It mirrors shipping-service's
// dictionary/internal/browserrpc/adapter.go (Phase 15a/16b) and
// refdata-service's natsrpc/adapter.go before it: a second transport onto
// the exact same commands.*Handler methods the rest/ adapter already calls,
// built on github.com/nats-io/nats.go/micro (see
// ARCHITECTURE-COMMUNICATIONS.md §4).
//
// Unlike refdata-service's adapter, which always runs on a single permanent
// PLATFORM-account connection, an Adapter here is registered once per
// TENANT connection (see internal/tenants) — a Sea Freight Flow browser
// authenticated into one tenant's account must reach pricing-service's
// handlers on that same account. Unlike shipping-service's adapter, there is
// no per-tenant JetStream/KV/projector bundle behind this one: pricing data
// lives in one shared Postgres scoped by `context` (business unit), not by
// NATS account, so every tenant connection's Adapter shares the exact same
// FeeScales/RateSheets/FixedRates command handlers — only the NATS
// connection itself is per-tenant.
package browserrpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/micro"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/pricing-service/pricing/internal/application/commands"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/pricing-service/pricing/internal/domain"
)

// Subject constants — {context} is a wildcard token resolved per-request
// from the concrete subject a request arrived on (contextFromSubject),
// same convention as shipping-service's browserrpc and refdata-service's
// natsrpc. Context is the company/business-unit scope, a completely
// separate axis from which tenant NATS account this Adapter is registered
// on (see internal/tenants) — every context value exists identically
// inside every tenant's account; tenant isolation comes entirely from the
// account boundary, never from anything in this subject pattern.
const (
	FeeScaleRegisterSubject     = "api.*.pricing.fee-scale.register.v1"
	FeeScaleListSubject         = "api.*.pricing.fee-scale.list.v1"
	FeeScaleGetSubject          = "api.*.pricing.fee-scale.get.v1"
	FeeScaleCreateDraftSubject  = "api.*.pricing.fee-scale.create-draft.v1"
	FeeScaleAddRangeSubject     = "api.*.pricing.fee-scale.add-range.v1"
	FeeScalePublishSubject      = "api.*.pricing.fee-scale.publish.v1"
	FeeScaleRollbackSubject     = "api.*.pricing.fee-scale.rollback.v1"
	FeeScaleVersionsSubject     = "api.*.pricing.fee-scale.versions.v1"
	FeeScaleActiveSubject       = "api.*.pricing.fee-scale.active.v1"
	FeeScaleCalculateFeeSubject = "api.*.pricing.fee-scale.calculate-fee.v1"

	RateSheetRegisterSubject            = "api.*.pricing.rate-sheet.register.v1"
	RateSheetListSubject                = "api.*.pricing.rate-sheet.list.v1"
	RateSheetGetSubject                 = "api.*.pricing.rate-sheet.get.v1"
	RateSheetCreateDraftSubject         = "api.*.pricing.rate-sheet.create-draft.v1"
	RateSheetAddEntrySubject            = "api.*.pricing.rate-sheet.add-entry.v1"
	RateSheetSetFeeScaleOverrideSubject = "api.*.pricing.rate-sheet.set-fee-scale-override.v1"
	RateSheetPublishSubject             = "api.*.pricing.rate-sheet.publish.v1"
	RateSheetRollbackSubject            = "api.*.pricing.rate-sheet.rollback.v1"
	RateSheetVersionsSubject            = "api.*.pricing.rate-sheet.versions.v1"
	RateSheetActiveSubject              = "api.*.pricing.rate-sheet.active.v1"
	RateSheetDropsChargeSubject         = "api.*.pricing.rate-sheet.drops-charge.v1"

	FixedRateRegisterSubject    = "api.*.pricing.fixed-rate.register.v1"
	FixedRateListSubject        = "api.*.pricing.fixed-rate.list.v1"
	FixedRateGetSubject         = "api.*.pricing.fixed-rate.get.v1"
	FixedRateCreateDraftSubject = "api.*.pricing.fixed-rate.create-draft.v1"
	FixedRatePublishSubject     = "api.*.pricing.fixed-rate.publish.v1"
	FixedRateRollbackSubject    = "api.*.pricing.fixed-rate.rollback.v1"
	FixedRateVersionsSubject    = "api.*.pricing.fixed-rate.versions.v1"
	FixedRateActiveSubject      = "api.*.pricing.fixed-rate.active.v1"
	FixedRateDropsChargeSubject = "api.*.pricing.fixed-rate.drops-charge.v1"

	// Diesel price index and overlay (Phase 25i, BR-P17–BR-P23).
	DieselPriceIndexSubject       = "api.*.pricing.diesel-price.index.v1"
	DieselPriceListSubject        = "api.*.pricing.diesel-price.list.v1"
	RateSheetApplyOverlaySubject  = "api.*.pricing.rate-sheet.apply-overlay.v1"
)

// ObsSubjectWildcard is the subject filter for the obs.api.* observability
// side-channel (BR-D26 parity) — events published under this wildcard from
// a TENANT connection stay inside that tenant's isolated NATS account, same
// caveat as shipping-service's adapter doc comment.
const ObsSubjectWildcard = "obs.api.>"

// Deps are Adapter's collaborators — the exact same command handlers
// composition.go's Startup already builds once and shares across every
// tenant connection (see this package's doc comment).
type Deps struct {
	FeeScales  *commands.FeeScaleHandler
	RateSheets *commands.RateSheetHandler
	FixedRates *commands.FixedRateHandler
	Log        *slog.Logger
	// Tenant is the friendly tenant name this connection belongs to,
	// attached to the micro registration as metadata purely for an admin
	// Services panel to label same-named instances by tenant (mirrors
	// shipping-service's browserrpc.Deps.Tenant, Phase 17c).
	Tenant string
}

// Adapter is pricing-service's api.* frontend-to-service adapter for one
// tenant connection.
type Adapter struct {
	nc         *nats.Conn
	feeScales  *commands.FeeScaleHandler
	rateSheets *commands.RateSheetHandler
	fixedRates *commands.FixedRateHandler
	log        *slog.Logger
	svc        micro.Service
}

// errorResponse is the wire shape for every failed api.* call — same shape
// as shipping-service's/refdata-service's adapters, so a browser client
// handles every service's errors identically.
type errorResponse struct {
	Error    string `json:"error"`
	NotFound bool   `json:"notFound,omitempty"`
}

func isNotFoundErr(err error) bool {
	return errors.Is(err, domain.ErrFeeScaleNotFound) || errors.Is(err, domain.ErrRateSheetNotFound) ||
		errors.Is(err, domain.ErrFixedRateNotFound) || errors.Is(err, domain.ErrFeeScaleDraftNotFound) ||
		errors.Is(err, domain.ErrRateSheetDraftNotFound) || errors.Is(err, domain.ErrFixedRateDraftNotFound) ||
		errors.Is(err, domain.ErrNoActiveFeeScaleVersion) || errors.Is(err, domain.ErrNoActiveRateSheetVersion) ||
		errors.Is(err, domain.ErrNoActiveFixedRateVersion) || errors.Is(err, domain.ErrRateSheetEntryNotFound) ||
		errors.Is(err, domain.ErrEntryNotFound) || errors.Is(err, domain.ErrNoDieselPrice)
}

// --- request/response wire shapes ---
// Name (and Version, where relevant) travel in the request body — never in
// the subject — matching shipping-service's convention (e.g. a ship ID
// lives in commands.ShipInput, not in ShipArriveSubject's tokens): every
// subject family here has fixed arity, and only {context} is read from it.

type nameRequest struct {
	Name string `json:"name"`
}

type feeScaleRegisterRequest struct {
	Name string `json:"name"`
}
type feeScaleAddRangeRequest struct {
	Name    string               `json:"name"`
	Version int                  `json:"version"`
	Range   domain.FeeScaleRange `json:"range"`
}
type feeScaleRollbackRequest struct {
	Name    string `json:"name"`
	Version int    `json:"version"`
}
type feeScaleCalculateFeeRequest struct {
	Name    string `json:"name"`
	CentBid int64  `json:"centBid"`
}
type feeScaleResponse struct {
	FeeScale domain.FeeScale `json:"feeScale"`
}
type feeScaleVersionResponse struct {
	Version domain.FeeScaleVersion `json:"version"`
}
type feeScaleVersionsResponse struct {
	Versions []domain.FeeScaleVersion `json:"versions"`
}
type feeScalesResponse struct {
	FeeScales []domain.FeeScale `json:"feeScales"`
}
type feeAmountResponse struct {
	CentFee int64 `json:"centFee"`
}

type rateSheetRegisterRequest struct {
	Name        string               `json:"name"`
	CustomerKey string               `json:"customerKey"`
	Type        domain.RateSheetType `json:"type"`
	Active      bool                 `json:"active"`
}
type rateSheetAddEntryRequest struct {
	Name    string                `json:"name"`
	Version int                   `json:"version"`
	Entry   domain.RateSheetEntry `json:"entry"`
}
type rateSheetFeeScaleOverrideRequest struct {
	Name         string `json:"name"`
	Version      int    `json:"version"`
	FeeScaleName string `json:"feeScaleName"`
}
type rateSheetRollbackRequest struct {
	Name    string `json:"name"`
	Version int    `json:"version"`
}
type rateSheetDropsChargeRequest struct {
	Name         string `json:"name"`
	RouteKey     string `json:"routeKey"`
	VehicleType  string `json:"vehicleType"`
	AddressCount int    `json:"addressCount"`
}
type rateSheetResponse struct {
	RateSheet domain.RateSheet `json:"rateSheet"`
}
type rateSheetVersionResponse struct {
	Version domain.RateSheetVersion `json:"version"`
}
type rateSheetVersionsResponse struct {
	Versions []domain.RateSheetVersion `json:"versions"`
}
type rateSheetsResponse struct {
	RateSheets []domain.RateSheet `json:"rateSheets"`
}
type dropsChargeResponse struct {
	CentCharge int64 `json:"centCharge"`
}

type fixedRateRegisterRequest struct {
	Name        string `json:"name"`
	CustomerKey string `json:"customerKey"`
	RouteKey    string `json:"routeKey"`
	Active      bool   `json:"active"`
}
type fixedRateCreateDraftRequest struct {
	Name                   string `json:"name"`
	CentRate               int64  `json:"centRate"`
	PointCount             int    `json:"pointCount"`
	CentAdditionalDropRate int64  `json:"centAdditionalDropRate"`
}
type fixedRateRollbackRequest struct {
	Name    string `json:"name"`
	Version int    `json:"version"`
}
type fixedRateDropsChargeRequest struct {
	Name         string `json:"name"`
	AddressCount int    `json:"addressCount"`
}
type fixedRateResponse struct {
	FixedRate domain.FixedRate `json:"fixedRate"`
}
type fixedRateVersionResponse struct {
	Version domain.FixedRateVersion `json:"version"`
}
type fixedRateVersionsResponse struct {
	Versions []domain.FixedRateVersion `json:"versions"`
}
type fixedRatesResponse struct {
	FixedRates []domain.FixedRate `json:"fixedRates"`
}

type dieselPriceIndexRequest struct {
	ActiveDate   time.Time `json:"activeDate"`
	CoastalCents int64     `json:"coastalCents"`
	InlandCents  int64     `json:"inlandCents"`
}
type dieselPricesResponse struct {
	Prices []domain.DieselPrice `json:"prices"`
}
type rateSheetApplyOverlayRequest struct {
	Name       string    `json:"name"`
	ActiveDate time.Time `json:"activeDate"`
}

// New starts the browserrpc microservice on nc and registers every
// endpoint. nc is expected to be a single tenant's NATS connection (see
// internal/tenants) — every subject registered here only ever resolves
// within that connection's own account. Callers should Stop() the returned
// Adapter when that tenant connection is torn down.
func New(nc *nats.Conn, deps Deps) (*Adapter, error) {
	a := &Adapter{
		nc:         nc,
		feeScales:  deps.FeeScales,
		rateSheets: deps.RateSheets,
		fixedRates: deps.FixedRates,
		log:        deps.Log,
	}

	svc, err := micro.AddService(nc, micro.Config{
		// Matches this connection's own nats.Name("pricing-service") so
		// Nats-Responder/Nats-Requestor agree on one identity string per
		// service, same invariant as shipping-service's adapter (Phase 18).
		Name:        "pricing-service",
		Version:     "1.0.0",
		Description: "pricing-service api.* frontend-to-service endpoints (Phase 25f)",
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
		{"fee-scale-register", a.handleFeeScaleRegister, FeeScaleRegisterSubject},
		{"fee-scale-list", a.handleFeeScaleList, FeeScaleListSubject},
		{"fee-scale-get", a.handleFeeScaleGet, FeeScaleGetSubject},
		{"fee-scale-create-draft", a.handleFeeScaleCreateDraft, FeeScaleCreateDraftSubject},
		{"fee-scale-add-range", a.handleFeeScaleAddRange, FeeScaleAddRangeSubject},
		{"fee-scale-publish", a.handleFeeScalePublish, FeeScalePublishSubject},
		{"fee-scale-rollback", a.handleFeeScaleRollback, FeeScaleRollbackSubject},
		{"fee-scale-versions", a.handleFeeScaleVersions, FeeScaleVersionsSubject},
		{"fee-scale-active", a.handleFeeScaleActive, FeeScaleActiveSubject},
		{"fee-scale-calculate-fee", a.handleFeeScaleCalculateFee, FeeScaleCalculateFeeSubject},

		{"rate-sheet-register", a.handleRateSheetRegister, RateSheetRegisterSubject},
		{"rate-sheet-list", a.handleRateSheetList, RateSheetListSubject},
		{"rate-sheet-get", a.handleRateSheetGet, RateSheetGetSubject},
		{"rate-sheet-create-draft", a.handleRateSheetCreateDraft, RateSheetCreateDraftSubject},
		{"rate-sheet-add-entry", a.handleRateSheetAddEntry, RateSheetAddEntrySubject},
		{"rate-sheet-set-fee-scale-override", a.handleRateSheetSetFeeScaleOverride, RateSheetSetFeeScaleOverrideSubject},
		{"rate-sheet-publish", a.handleRateSheetPublish, RateSheetPublishSubject},
		{"rate-sheet-rollback", a.handleRateSheetRollback, RateSheetRollbackSubject},
		{"rate-sheet-versions", a.handleRateSheetVersions, RateSheetVersionsSubject},
		{"rate-sheet-active", a.handleRateSheetActive, RateSheetActiveSubject},
		{"rate-sheet-drops-charge", a.handleRateSheetDropsCharge, RateSheetDropsChargeSubject},

		{"fixed-rate-register", a.handleFixedRateRegister, FixedRateRegisterSubject},
		{"fixed-rate-list", a.handleFixedRateList, FixedRateListSubject},
		{"fixed-rate-get", a.handleFixedRateGet, FixedRateGetSubject},
		{"fixed-rate-create-draft", a.handleFixedRateCreateDraft, FixedRateCreateDraftSubject},
		{"fixed-rate-publish", a.handleFixedRatePublish, FixedRatePublishSubject},
		{"fixed-rate-rollback", a.handleFixedRateRollback, FixedRateRollbackSubject},
		{"fixed-rate-versions", a.handleFixedRateVersions, FixedRateVersionsSubject},
		{"fixed-rate-active", a.handleFixedRateActive, FixedRateActiveSubject},
		{"fixed-rate-drops-charge", a.handleFixedRateDropsCharge, FixedRateDropsChargeSubject},

		{"diesel-price-index", a.handleDieselPriceIndex, DieselPriceIndexSubject},
		{"diesel-price-list", a.handleDieselPriceList, DieselPriceListSubject},
		{"rate-sheet-apply-overlay", a.handleRateSheetApplyOverlay, RateSheetApplyOverlaySubject},
	}
	for _, ep := range endpoints {
		if err := svc.AddEndpoint(ep.name, ep.handler, micro.WithEndpointSubject(ep.subject)); err != nil {
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

// --- FeeScale handlers ---

func (a *Adapter) handleFeeScaleRegister(req micro.Request) {
	subject := req.Subject()
	a.publishObs(subject, req.Reply(), "request", map[string][]string(req.Headers()), req.Data(), "")
	var in feeScaleRegisterRequest
	if err := json.Unmarshal(req.Data(), &in); err != nil {
		a.reply(req, nil, err)
		return
	}
	fs := domain.FeeScale{Context: contextFromSubject(subject), Name: in.Name}
	err := a.feeScales.Register(context.Background(), fs)
	a.reply(req, feeScaleResponse{FeeScale: fs}, err)
}

// handleFeeScaleList takes no input beyond {context} — BR-P16: the response
// excludes soft-deleted fee scales.
func (a *Adapter) handleFeeScaleList(req micro.Request) {
	subject := req.Subject()
	a.publishObs(subject, req.Reply(), "request", map[string][]string(req.Headers()), req.Data(), "")
	feeScales, err := a.feeScales.List(context.Background(), contextFromSubject(subject))
	a.reply(req, feeScalesResponse{FeeScales: feeScales}, err)
}

func (a *Adapter) handleFeeScaleGet(req micro.Request) {
	subject := req.Subject()
	a.publishObs(subject, req.Reply(), "request", map[string][]string(req.Headers()), req.Data(), "")
	var in nameRequest
	if err := json.Unmarshal(req.Data(), &in); err != nil {
		a.reply(req, nil, err)
		return
	}
	fs, err := a.feeScales.Get(context.Background(), contextFromSubject(subject), in.Name)
	a.reply(req, feeScaleResponse{FeeScale: fs}, err)
}

func (a *Adapter) handleFeeScaleCreateDraft(req micro.Request) {
	subject := req.Subject()
	a.publishObs(subject, req.Reply(), "request", map[string][]string(req.Headers()), req.Data(), "")
	var in nameRequest
	if err := json.Unmarshal(req.Data(), &in); err != nil {
		a.reply(req, nil, err)
		return
	}
	v, err := a.feeScales.CreateDraft(context.Background(), contextFromSubject(subject), in.Name)
	a.reply(req, feeScaleVersionResponse{Version: v}, err)
}

func (a *Adapter) handleFeeScaleAddRange(req micro.Request) {
	subject := req.Subject()
	a.publishObs(subject, req.Reply(), "request", map[string][]string(req.Headers()), req.Data(), "")
	var in feeScaleAddRangeRequest
	if err := json.Unmarshal(req.Data(), &in); err != nil {
		a.reply(req, nil, err)
		return
	}
	err := a.feeScales.AddRange(context.Background(), contextFromSubject(subject), in.Name, in.Version, in.Range)
	a.reply(req, struct{}{}, err)
}

func (a *Adapter) handleFeeScalePublish(req micro.Request) {
	subject := req.Subject()
	a.publishObs(subject, req.Reply(), "request", map[string][]string(req.Headers()), req.Data(), "")
	var in nameRequest
	if err := json.Unmarshal(req.Data(), &in); err != nil {
		a.reply(req, nil, err)
		return
	}
	v, err := a.feeScales.Publish(context.Background(), contextFromSubject(subject), in.Name)
	a.reply(req, feeScaleVersionResponse{Version: v}, err)
}

func (a *Adapter) handleFeeScaleRollback(req micro.Request) {
	subject := req.Subject()
	a.publishObs(subject, req.Reply(), "request", map[string][]string(req.Headers()), req.Data(), "")
	var in feeScaleRollbackRequest
	if err := json.Unmarshal(req.Data(), &in); err != nil {
		a.reply(req, nil, err)
		return
	}
	v, err := a.feeScales.Rollback(context.Background(), contextFromSubject(subject), in.Name, in.Version)
	a.reply(req, feeScaleVersionResponse{Version: v}, err)
}

func (a *Adapter) handleFeeScaleVersions(req micro.Request) {
	subject := req.Subject()
	a.publishObs(subject, req.Reply(), "request", map[string][]string(req.Headers()), req.Data(), "")
	var in nameRequest
	if err := json.Unmarshal(req.Data(), &in); err != nil {
		a.reply(req, nil, err)
		return
	}
	versions, err := a.feeScales.Versions(context.Background(), contextFromSubject(subject), in.Name)
	a.reply(req, feeScaleVersionsResponse{Versions: versions}, err)
}

func (a *Adapter) handleFeeScaleActive(req micro.Request) {
	subject := req.Subject()
	a.publishObs(subject, req.Reply(), "request", map[string][]string(req.Headers()), req.Data(), "")
	var in nameRequest
	if err := json.Unmarshal(req.Data(), &in); err != nil {
		a.reply(req, nil, err)
		return
	}
	v, err := a.feeScales.ActiveVersion(context.Background(), contextFromSubject(subject), in.Name)
	a.reply(req, feeScaleVersionResponse{Version: v}, err)
}

func (a *Adapter) handleFeeScaleCalculateFee(req micro.Request) {
	subject := req.Subject()
	a.publishObs(subject, req.Reply(), "request", map[string][]string(req.Headers()), req.Data(), "")
	var in feeScaleCalculateFeeRequest
	if err := json.Unmarshal(req.Data(), &in); err != nil {
		a.reply(req, nil, err)
		return
	}
	fee, err := a.feeScales.CalculateFee(context.Background(), contextFromSubject(subject), in.Name, in.CentBid)
	a.reply(req, feeAmountResponse{CentFee: fee}, err)
}

// --- RateSheet handlers ---

func (a *Adapter) handleRateSheetRegister(req micro.Request) {
	subject := req.Subject()
	a.publishObs(subject, req.Reply(), "request", map[string][]string(req.Headers()), req.Data(), "")
	var in rateSheetRegisterRequest
	if err := json.Unmarshal(req.Data(), &in); err != nil {
		a.reply(req, nil, err)
		return
	}
	rs := domain.RateSheet{Context: contextFromSubject(subject), Name: in.Name, CustomerKey: in.CustomerKey, Type: in.Type, Active: in.Active}
	err := a.rateSheets.Register(context.Background(), rs)
	a.reply(req, rateSheetResponse{RateSheet: rs}, err)
}

// handleRateSheetList takes no input beyond {context}.
func (a *Adapter) handleRateSheetList(req micro.Request) {
	subject := req.Subject()
	a.publishObs(subject, req.Reply(), "request", map[string][]string(req.Headers()), req.Data(), "")
	rateSheets, err := a.rateSheets.List(context.Background(), contextFromSubject(subject))
	a.reply(req, rateSheetsResponse{RateSheets: rateSheets}, err)
}

func (a *Adapter) handleRateSheetGet(req micro.Request) {
	subject := req.Subject()
	a.publishObs(subject, req.Reply(), "request", map[string][]string(req.Headers()), req.Data(), "")
	var in nameRequest
	if err := json.Unmarshal(req.Data(), &in); err != nil {
		a.reply(req, nil, err)
		return
	}
	rs, err := a.rateSheets.Get(context.Background(), contextFromSubject(subject), in.Name)
	a.reply(req, rateSheetResponse{RateSheet: rs}, err)
}

func (a *Adapter) handleRateSheetCreateDraft(req micro.Request) {
	subject := req.Subject()
	a.publishObs(subject, req.Reply(), "request", map[string][]string(req.Headers()), req.Data(), "")
	var in nameRequest
	if err := json.Unmarshal(req.Data(), &in); err != nil {
		a.reply(req, nil, err)
		return
	}
	v, err := a.rateSheets.CreateDraft(context.Background(), contextFromSubject(subject), in.Name)
	a.reply(req, rateSheetVersionResponse{Version: v}, err)
}

func (a *Adapter) handleRateSheetAddEntry(req micro.Request) {
	subject := req.Subject()
	a.publishObs(subject, req.Reply(), "request", map[string][]string(req.Headers()), req.Data(), "")
	var in rateSheetAddEntryRequest
	if err := json.Unmarshal(req.Data(), &in); err != nil {
		a.reply(req, nil, err)
		return
	}
	err := a.rateSheets.AddEntry(context.Background(), contextFromSubject(subject), in.Name, in.Version, in.Entry)
	a.reply(req, struct{}{}, err)
}

func (a *Adapter) handleRateSheetSetFeeScaleOverride(req micro.Request) {
	subject := req.Subject()
	a.publishObs(subject, req.Reply(), "request", map[string][]string(req.Headers()), req.Data(), "")
	var in rateSheetFeeScaleOverrideRequest
	if err := json.Unmarshal(req.Data(), &in); err != nil {
		a.reply(req, nil, err)
		return
	}
	err := a.rateSheets.SetFeeScaleOverride(context.Background(), contextFromSubject(subject), in.Name, in.Version, in.FeeScaleName)
	a.reply(req, struct{}{}, err)
}

func (a *Adapter) handleRateSheetPublish(req micro.Request) {
	subject := req.Subject()
	a.publishObs(subject, req.Reply(), "request", map[string][]string(req.Headers()), req.Data(), "")
	var in nameRequest
	if err := json.Unmarshal(req.Data(), &in); err != nil {
		a.reply(req, nil, err)
		return
	}
	v, err := a.rateSheets.Publish(context.Background(), contextFromSubject(subject), in.Name)
	a.reply(req, rateSheetVersionResponse{Version: v}, err)
}

func (a *Adapter) handleRateSheetRollback(req micro.Request) {
	subject := req.Subject()
	a.publishObs(subject, req.Reply(), "request", map[string][]string(req.Headers()), req.Data(), "")
	var in rateSheetRollbackRequest
	if err := json.Unmarshal(req.Data(), &in); err != nil {
		a.reply(req, nil, err)
		return
	}
	v, err := a.rateSheets.Rollback(context.Background(), contextFromSubject(subject), in.Name, in.Version)
	a.reply(req, rateSheetVersionResponse{Version: v}, err)
}

func (a *Adapter) handleRateSheetVersions(req micro.Request) {
	subject := req.Subject()
	a.publishObs(subject, req.Reply(), "request", map[string][]string(req.Headers()), req.Data(), "")
	var in nameRequest
	if err := json.Unmarshal(req.Data(), &in); err != nil {
		a.reply(req, nil, err)
		return
	}
	versions, err := a.rateSheets.Versions(context.Background(), contextFromSubject(subject), in.Name)
	a.reply(req, rateSheetVersionsResponse{Versions: versions}, err)
}

func (a *Adapter) handleRateSheetActive(req micro.Request) {
	subject := req.Subject()
	a.publishObs(subject, req.Reply(), "request", map[string][]string(req.Headers()), req.Data(), "")
	var in nameRequest
	if err := json.Unmarshal(req.Data(), &in); err != nil {
		a.reply(req, nil, err)
		return
	}
	v, err := a.rateSheets.ActiveVersion(context.Background(), contextFromSubject(subject), in.Name)
	a.reply(req, rateSheetVersionResponse{Version: v}, err)
}

func (a *Adapter) handleRateSheetDropsCharge(req micro.Request) {
	subject := req.Subject()
	a.publishObs(subject, req.Reply(), "request", map[string][]string(req.Headers()), req.Data(), "")
	var in rateSheetDropsChargeRequest
	if err := json.Unmarshal(req.Data(), &in); err != nil {
		a.reply(req, nil, err)
		return
	}
	charge, err := a.rateSheets.AdditionalDropsCharge(context.Background(), contextFromSubject(subject), in.Name, in.RouteKey, in.VehicleType, in.AddressCount)
	a.reply(req, dropsChargeResponse{CentCharge: charge}, err)
}

// --- FixedRate handlers ---

func (a *Adapter) handleFixedRateRegister(req micro.Request) {
	subject := req.Subject()
	a.publishObs(subject, req.Reply(), "request", map[string][]string(req.Headers()), req.Data(), "")
	var in fixedRateRegisterRequest
	if err := json.Unmarshal(req.Data(), &in); err != nil {
		a.reply(req, nil, err)
		return
	}
	fr := domain.FixedRate{Context: contextFromSubject(subject), Name: in.Name, CustomerKey: in.CustomerKey, RouteKey: in.RouteKey, Active: in.Active}
	err := a.fixedRates.Register(context.Background(), fr)
	a.reply(req, fixedRateResponse{FixedRate: fr}, err)
}

// handleFixedRateList takes no input beyond {context}.
func (a *Adapter) handleFixedRateList(req micro.Request) {
	subject := req.Subject()
	a.publishObs(subject, req.Reply(), "request", map[string][]string(req.Headers()), req.Data(), "")
	fixedRates, err := a.fixedRates.List(context.Background(), contextFromSubject(subject))
	a.reply(req, fixedRatesResponse{FixedRates: fixedRates}, err)
}

func (a *Adapter) handleFixedRateGet(req micro.Request) {
	subject := req.Subject()
	a.publishObs(subject, req.Reply(), "request", map[string][]string(req.Headers()), req.Data(), "")
	var in nameRequest
	if err := json.Unmarshal(req.Data(), &in); err != nil {
		a.reply(req, nil, err)
		return
	}
	fr, err := a.fixedRates.Get(context.Background(), contextFromSubject(subject), in.Name)
	a.reply(req, fixedRateResponse{FixedRate: fr}, err)
}

func (a *Adapter) handleFixedRateCreateDraft(req micro.Request) {
	subject := req.Subject()
	a.publishObs(subject, req.Reply(), "request", map[string][]string(req.Headers()), req.Data(), "")
	var in fixedRateCreateDraftRequest
	if err := json.Unmarshal(req.Data(), &in); err != nil {
		a.reply(req, nil, err)
		return
	}
	v, err := a.fixedRates.CreateDraft(context.Background(), contextFromSubject(subject), in.Name, in.CentRate, in.PointCount, in.CentAdditionalDropRate)
	a.reply(req, fixedRateVersionResponse{Version: v}, err)
}

func (a *Adapter) handleFixedRatePublish(req micro.Request) {
	subject := req.Subject()
	a.publishObs(subject, req.Reply(), "request", map[string][]string(req.Headers()), req.Data(), "")
	var in nameRequest
	if err := json.Unmarshal(req.Data(), &in); err != nil {
		a.reply(req, nil, err)
		return
	}
	v, err := a.fixedRates.Publish(context.Background(), contextFromSubject(subject), in.Name)
	a.reply(req, fixedRateVersionResponse{Version: v}, err)
}

func (a *Adapter) handleFixedRateRollback(req micro.Request) {
	subject := req.Subject()
	a.publishObs(subject, req.Reply(), "request", map[string][]string(req.Headers()), req.Data(), "")
	var in fixedRateRollbackRequest
	if err := json.Unmarshal(req.Data(), &in); err != nil {
		a.reply(req, nil, err)
		return
	}
	v, err := a.fixedRates.Rollback(context.Background(), contextFromSubject(subject), in.Name, in.Version)
	a.reply(req, fixedRateVersionResponse{Version: v}, err)
}

func (a *Adapter) handleFixedRateVersions(req micro.Request) {
	subject := req.Subject()
	a.publishObs(subject, req.Reply(), "request", map[string][]string(req.Headers()), req.Data(), "")
	var in nameRequest
	if err := json.Unmarshal(req.Data(), &in); err != nil {
		a.reply(req, nil, err)
		return
	}
	versions, err := a.fixedRates.Versions(context.Background(), contextFromSubject(subject), in.Name)
	a.reply(req, fixedRateVersionsResponse{Versions: versions}, err)
}

func (a *Adapter) handleFixedRateActive(req micro.Request) {
	subject := req.Subject()
	a.publishObs(subject, req.Reply(), "request", map[string][]string(req.Headers()), req.Data(), "")
	var in nameRequest
	if err := json.Unmarshal(req.Data(), &in); err != nil {
		a.reply(req, nil, err)
		return
	}
	v, err := a.fixedRates.ActiveVersion(context.Background(), contextFromSubject(subject), in.Name)
	a.reply(req, fixedRateVersionResponse{Version: v}, err)
}

func (a *Adapter) handleFixedRateDropsCharge(req micro.Request) {
	subject := req.Subject()
	a.publishObs(subject, req.Reply(), "request", map[string][]string(req.Headers()), req.Data(), "")
	var in fixedRateDropsChargeRequest
	if err := json.Unmarshal(req.Data(), &in); err != nil {
		a.reply(req, nil, err)
		return
	}
	charge, err := a.fixedRates.AdditionalDropsCharge(context.Background(), contextFromSubject(subject), in.Name, in.AddressCount)
	a.reply(req, dropsChargeResponse{CentCharge: charge}, err)
}

// --- Diesel price index and overlay handlers ---

func (a *Adapter) handleDieselPriceIndex(req micro.Request) {
	subject := req.Subject()
	a.publishObs(subject, req.Reply(), "request", map[string][]string(req.Headers()), req.Data(), "")
	var in dieselPriceIndexRequest
	if err := json.Unmarshal(req.Data(), &in); err != nil {
		a.reply(req, nil, err)
		return
	}
	price := domain.DieselPrice{ActiveDate: in.ActiveDate, CoastalCents: in.CoastalCents, InlandCents: in.InlandCents}
	err := a.rateSheets.IndexDieselPrice(context.Background(), contextFromSubject(subject), price)
	a.reply(req, struct{}{}, err)
}

func (a *Adapter) handleDieselPriceList(req micro.Request) {
	subject := req.Subject()
	a.publishObs(subject, req.Reply(), "request", map[string][]string(req.Headers()), req.Data(), "")
	prices, err := a.rateSheets.ListDieselPrices(context.Background(), contextFromSubject(subject))
	a.reply(req, dieselPricesResponse{Prices: prices}, err)
}

func (a *Adapter) handleRateSheetApplyOverlay(req micro.Request) {
	subject := req.Subject()
	a.publishObs(subject, req.Reply(), "request", map[string][]string(req.Headers()), req.Data(), "")
	var in rateSheetApplyOverlayRequest
	if err := json.Unmarshal(req.Data(), &in); err != nil {
		a.reply(req, nil, err)
		return
	}
	v, err := a.rateSheets.ApplyDieselOverlay(context.Background(), contextFromSubject(subject), in.Name, in.ActiveDate)
	a.reply(req, rateSheetVersionResponse{Version: v}, err)
}

// --- shared plumbing (mirrors shipping-service's browserrpc/adapter.go) ---

// contextFromSubject extracts the {context} token from an api.{context}...
// subject — the ONLY source of truth for which CONTEXT (company/
// business-unit scope) a request belongs to; every handler above derives
// it this way rather than trusting a request body's own context field.
// Context is NOT the tenant — the tenant boundary is the NATS account this
// connection authenticated into.
func contextFromSubject(subject string) string {
	parts := strings.Split(subject, ".")
	if len(parts) < 2 {
		return ""
	}
	return parts[1]
}

// reply is the shared tail end of every handler above: nil error replies
// with result, any error replies with the mapped error response.
func (a *Adapter) reply(req micro.Request, result any, err error) {
	subject := req.Subject()
	correlationID := req.Reply()
	if err != nil {
		a.respondError(req, subject, correlationID, err)
		return
	}
	a.respond(req, subject, correlationID, result)
}

const responderHeader = "Nats-Responder"

func (a *Adapter) responderIdentity() string {
	info := a.svc.Info()
	return fmt.Sprintf("%s/%s", info.Name, info.ID)
}

func (a *Adapter) respond(req micro.Request, subject, correlationID string, out any) {
	data, err := json.Marshal(out)
	if err != nil {
		a.respondError(req, subject, correlationID, err)
		return
	}
	headers := map[string][]string{responderHeader: {a.responderIdentity()}}
	a.publishObs(subject, correlationID, "reply", headers, data, "")
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
	a.publishObs(subject, correlationID, "reply", headers, data, err.Error())
	if respErr := req.Respond(data, micro.WithHeaders(micro.Headers(headers))); respErr != nil && a.log != nil {
		a.log.Error("browserrpc: respond failed", "subject", subject, "err", respErr)
	}
}

var versionSuffix = regexp.MustCompile(`\.v\d+$`)

func obsSubjectFor(apiSubject string) string {
	return "obs." + versionSuffix.ReplaceAllString(apiSubject, "")
}

type obsEnvelope struct {
	Direction     string              `json:"direction"`
	CorrelationID string              `json:"correlationId"`
	Subject       string              `json:"subject"`
	Payload       json.RawMessage     `json:"payload,omitempty"`
	Error         string              `json:"error,omitempty"`
	Headers       map[string][]string `json:"headers,omitempty"`
	Timestamp     time.Time           `json:"timestamp"`
	PayloadBytes  int                 `json:"payloadBytes"`
}

// publishObs fire-and-forget publishes an observability event (BR-D26
// parity) — must never block or fail the real API reply. pricing-service
// has no JetStream, so this is always a plain nc.Publish (no RPCTRACE
// replay retention, unlike shipping-service's/refdata-service's adapters
// when JetStream is configured).
func (a *Adapter) publishObs(apiSubject, correlationID, direction string, headers map[string][]string, payload []byte, errMsg string) {
	defer func() {
		if r := recover(); r != nil && a.log != nil {
			a.log.Error("browserrpc: obs publish panicked", "recovered", r)
		}
	}()
	data, err := json.Marshal(obsEnvelope{
		Direction:     direction,
		CorrelationID: correlationID,
		Subject:       apiSubject,
		Payload:       payload,
		Error:         errMsg,
		Headers:       headers,
		Timestamp:     time.Now().UTC(),
		PayloadBytes:  len(payload),
	})
	if err != nil {
		return
	}
	if pubErr := a.nc.Publish(obsSubjectFor(apiSubject), data); pubErr != nil && a.log != nil {
		a.log.Warn("browserrpc: obs publish failed", "err", pubErr)
	}
}
