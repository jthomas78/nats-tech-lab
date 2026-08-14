// Package rest exposes the trading-partner module over HTTP.
//
// Routes (all under /api/trading-partners/{context}/..., context-scoped
// like pricing/refdata — not accounts-service's flat /api/accounts/{name} —
// since TradingPartner carries a context field):
//
//	POST /                                   register a trading partner
//	GET  /                                   list trading partners in context
//	GET  /{id}                                get one trading partner
//	POST /{id}/activate                       BR-TP03
//	POST /{id}/suspend                        BR-TP04 (body: {reason})
//	POST /{id}/reactivate                     BR-TP05
//	GET  /{id}/audit                          BR-TP06 audit trail
//
//	POST /{id}/documents                      BR-TP07/BR-TP08 (body: {type, reference})
//	GET  /{id}/documents                      list compliance documents
//	POST /{id}/documents/{type}/approve       BR-TP09
//	POST /{id}/documents/{type}/reject        BR-TP10
//	POST /{id}/documents/{type}/resubmit      BR-TP11
//
//	POST /{id}/fleet-assets                   BR-TP12/13/14 (body includes tenant — see fleetAssetRequest)
//	GET  /{id}/fleet-assets                   list fleet assets
package rest

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/trading-partner-service/tradingpartner/internal/application/commands"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/trading-partner-service/tradingpartner/internal/domain"
)

type errorResponse struct {
	Error string `json:"error"`
}

type registerRequest struct {
	Name string             `json:"name"`
	Type domain.PartnerType `json:"type"`
}

type suspendRequest struct {
	Reason string `json:"reason"`
}

type tradingPartnersResponse struct {
	TradingPartners []domain.TradingPartner `json:"tradingPartners"`
}

type addDocumentRequest struct {
	Type      domain.DocumentType `json:"type"`
	Reference string              `json:"reference"`
}

type documentsResponse struct {
	Documents []domain.ComplianceDocument `json:"documents"`
}

// fleetAssetRequest's Tenant field is BR-TP14's one deliberate departure
// from every other route in this repo: no REST endpoint before this one has
// ever needed to know which NATS tenant account a request belongs to (see
// internal/domain/vehicle_type_validator.go's doc comment for why). The
// Admin UI supplies it from its existing tenant.js selection, already used
// to pick which tenant's NATS WebSocket connection is active.
type fleetAssetRequest struct {
	Tenant          string `json:"tenant"`
	RegistrationNo  string `json:"registrationNo"`
	VIN             string `json:"vin"`
	Make            string `json:"make"`
	Model           string `json:"model"`
	VehicleTypeCode string `json:"vehicleTypeCode"`
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

// Deps wires the command handlers this REST layer calls. Audit is read
// directly here (not via TradingPartnerHandler) since it's a query with no
// domain policy of its own.
type Deps struct {
	TradingPartners *commands.TradingPartnerHandler
	Documents       *commands.ComplianceDocumentHandler
	FleetAssets     *commands.FleetAssetHandler
	Audit           domain.AuditReader
	Log             *slog.Logger
}

type Handlers struct{ deps Deps }

func NewHandlers(deps Deps) *Handlers { return &Handlers{deps: deps} }

func (h *Handlers) Mount(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/trading-partners/{context}", h.register)
	mux.HandleFunc("GET /api/trading-partners/{context}", h.list)
	mux.HandleFunc("GET /api/trading-partners/{context}/{id}", h.get)
	mux.HandleFunc("POST /api/trading-partners/{context}/{id}/activate", h.activate)
	mux.HandleFunc("POST /api/trading-partners/{context}/{id}/suspend", h.suspend)
	mux.HandleFunc("POST /api/trading-partners/{context}/{id}/reactivate", h.reactivate)
	mux.HandleFunc("GET /api/trading-partners/{context}/{id}/audit", h.audit)

	mux.HandleFunc("POST /api/trading-partners/{context}/{id}/documents", h.addDocument)
	mux.HandleFunc("GET /api/trading-partners/{context}/{id}/documents", h.listDocuments)
	mux.HandleFunc("POST /api/trading-partners/{context}/{id}/documents/{type}/approve", h.approveDocument)
	mux.HandleFunc("POST /api/trading-partners/{context}/{id}/documents/{type}/reject", h.rejectDocument)
	mux.HandleFunc("POST /api/trading-partners/{context}/{id}/documents/{type}/resubmit", h.resubmitDocument)

	mux.HandleFunc("POST /api/trading-partners/{context}/{id}/fleet-assets", h.addFleetAsset)
	mux.HandleFunc("GET /api/trading-partners/{context}/{id}/fleet-assets", h.listFleetAssets)
}

// --- TradingPartner ---

func (h *Handlers) register(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid request body"})
		return
	}
	actor, sourceIP := auditActor(r)
	tp, err := h.deps.TradingPartners.Register(r.Context(), commands.Actor{Name: actor, SourceIP: sourceIP}, req.Name, req.Type, r.PathValue("context"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.writeJSON(w, http.StatusCreated, tp)
}

func (h *Handlers) get(w http.ResponseWriter, r *http.Request) {
	tp, err := h.deps.TradingPartners.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, tp)
}

func (h *Handlers) list(w http.ResponseWriter, r *http.Request) {
	all, err := h.deps.TradingPartners.List(r.Context(), r.PathValue("context"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, tradingPartnersResponse{TradingPartners: all})
}

func (h *Handlers) activate(w http.ResponseWriter, r *http.Request) {
	actor, sourceIP := auditActor(r)
	tp, err := h.deps.TradingPartners.Activate(r.Context(), commands.Actor{Name: actor, SourceIP: sourceIP}, r.PathValue("id"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, tp)
}

func (h *Handlers) suspend(w http.ResponseWriter, r *http.Request) {
	var req suspendRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid request body"})
		return
	}
	actor, sourceIP := auditActor(r)
	tp, err := h.deps.TradingPartners.Suspend(r.Context(), commands.Actor{Name: actor, SourceIP: sourceIP}, r.PathValue("id"), req.Reason)
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, tp)
}

func (h *Handlers) reactivate(w http.ResponseWriter, r *http.Request) {
	actor, sourceIP := auditActor(r)
	tp, err := h.deps.TradingPartners.Reactivate(r.Context(), commands.Actor{Name: actor, SourceIP: sourceIP}, r.PathValue("id"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, tp)
}

func (h *Handlers) audit(w http.ResponseWriter, r *http.Request) {
	entries, err := h.deps.Audit.ListByPartner(r.Context(), r.PathValue("id"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	out := make([]auditEntryResponse, len(entries))
	for i, e := range entries {
		out[i] = auditEntryResponse{
			ID: e.ID, Action: e.Action, Actor: e.Actor, SourceIP: e.SourceIP,
			Outcome: e.Outcome, Metadata: e.Metadata, CreatedAt: e.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		}
	}
	h.writeJSON(w, http.StatusOK, auditResponse{Events: out})
}

// --- ComplianceDocument ---

func (h *Handlers) addDocument(w http.ResponseWriter, r *http.Request) {
	var req addDocumentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid request body"})
		return
	}
	doc, err := h.deps.Documents.AddDocument(r.Context(), r.PathValue("id"), req.Type, req.Reference)
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.writeJSON(w, http.StatusCreated, doc)
}

func (h *Handlers) listDocuments(w http.ResponseWriter, r *http.Request) {
	docs, err := h.deps.Documents.ListDocuments(r.Context(), r.PathValue("id"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, documentsResponse{Documents: docs})
}

func (h *Handlers) approveDocument(w http.ResponseWriter, r *http.Request) {
	doc, err := h.deps.Documents.ApproveDocument(r.Context(), r.PathValue("id"), domain.DocumentType(r.PathValue("type")))
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, doc)
}

func (h *Handlers) rejectDocument(w http.ResponseWriter, r *http.Request) {
	doc, err := h.deps.Documents.RejectDocument(r.Context(), r.PathValue("id"), domain.DocumentType(r.PathValue("type")))
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, doc)
}

func (h *Handlers) resubmitDocument(w http.ResponseWriter, r *http.Request) {
	doc, err := h.deps.Documents.ResubmitDocument(r.Context(), r.PathValue("id"), domain.DocumentType(r.PathValue("type")))
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, doc)
}

// --- FleetAsset ---

func (h *Handlers) addFleetAsset(w http.ResponseWriter, r *http.Request) {
	var req fleetAssetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid request body"})
		return
	}
	asset, err := h.deps.FleetAssets.AddFleetAsset(r.Context(), r.PathValue("id"), req.Tenant, req.RegistrationNo, req.VIN, req.Make, req.Model, req.VehicleTypeCode)
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.writeJSON(w, http.StatusCreated, asset)
}

func (h *Handlers) listFleetAssets(w http.ResponseWriter, r *http.Request) {
	assets, err := h.deps.FleetAssets.ListFleetAssets(r.Context(), r.PathValue("id"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, fleetAssetsResponse{FleetAssets: assets})
}

// --- helpers ---

func (h *Handlers) writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if body != nil {
		_ = json.NewEncoder(w).Encode(body)
	}
}

func (h *Handlers) writeError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, domain.ErrTradingPartnerNotFound), errors.Is(err, domain.ErrDocumentNotFound):
		status = http.StatusNotFound
	case errors.Is(err, domain.ErrNotRegistered), errors.Is(err, domain.ErrNotActive), errors.Is(err, domain.ErrNotSuspended),
		errors.Is(err, domain.ErrDocumentNotPending), errors.Is(err, domain.ErrDocumentNotRejected),
		errors.Is(err, domain.ErrRegistrationNoAlreadyExists):
		status = http.StatusConflict
	case errors.Is(err, domain.ErrNameRequired), errors.Is(err, domain.ErrContextRequired), errors.Is(err, domain.ErrInvalidPartnerType),
		errors.Is(err, domain.ErrSuspendReasonRequired), errors.Is(err, domain.ErrInvalidDocumentType),
		errors.Is(err, domain.ErrDocumentTypeNotAllowedForPartnerType), errors.Is(err, domain.ErrReferenceRequired),
		errors.Is(err, domain.ErrFleetAssetRequiresTransporter), errors.Is(err, domain.ErrRegistrationNoRequired),
		errors.Is(err, domain.ErrVehicleTypeCodeRequired), errors.Is(err, domain.ErrUnknownVehicleTypeCode):
		status = http.StatusBadRequest
	}
	if h.deps.Log != nil && status == http.StatusInternalServerError {
		h.deps.Log.Error("trading-partner request failed", "err", err)
	}
	h.writeJSON(w, status, errorResponse{Error: err.Error()})
}
