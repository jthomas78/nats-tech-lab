// Package rest exposes the pricing module over HTTP.
//
// Routes (all under /api/pricing/{context}/...):
//
//	FeeScale (BR-P01–BR-P06, BR-P16):
//	POST /fee-scales                                register a fee scale
//	GET  /fee-scales                                 list fee scales (BR-P16: excludes soft-deleted)
//	GET  /fee-scales/{name}                          get a fee scale
//	POST /fee-scales/{name}/draft                    create a draft version
//	PUT  /fee-scales/{name}/versions/{version}/ranges add a range to a draft
//	POST /fee-scales/{name}/publish                  publish the current draft
//	POST /fee-scales/{name}/rollback/{version}        roll back to a published version
//	GET  /fee-scales/{name}/versions                 list versions
//	GET  /fee-scales/{name}/active                   get the active (highest published) version
//	GET  /fee-scales/{name}/fee?bid={cents}           calculate the fee for a bid amount
//
//	RateSheet (BR-P07–BR-P12):
//	POST /rate-sheets                                                register a rate sheet
//	GET  /rate-sheets                                                list rate sheets
//	GET  /rate-sheets/{name}                                         get a rate sheet
//	POST /rate-sheets/{name}/draft                                   create a draft version
//	PUT  /rate-sheets/{name}/versions/{version}/entries              add a lane entry to a draft
//	PUT  /rate-sheets/{name}/versions/{version}/fee-scale-override    set the draft's fee scale override
//	POST /rate-sheets/{name}/publish                                 publish the current draft
//	POST /rate-sheets/{name}/rollback/{version}                      roll back to a published version
//	GET  /rate-sheets/{name}/versions                                list versions
//	GET  /rate-sheets/{name}/active                                  get the active version
//	GET  /rate-sheets/{name}/additional-drops-charge?route=&vehicleType=&addressCount=
//
//	FixedRate (BR-P13–BR-P15):
//	POST /fixed-rates                                        register a fixed rate
//	GET  /fixed-rates                                        list fixed rates
//	GET  /fixed-rates/{name}                                 get a fixed rate
//	POST /fixed-rates/{name}/draft                           create a draft version (body: rate fields)
//	POST /fixed-rates/{name}/publish                         publish the current draft
//	POST /fixed-rates/{name}/rollback/{version}               roll back to a published version
//	GET  /fixed-rates/{name}/versions                        list versions
//	GET  /fixed-rates/{name}/active                          get the active version
//	GET  /fixed-rates/{name}/additional-drops-charge?addressCount=
package rest

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/pricing-service/pricing/internal/application/commands"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/pricing-service/pricing/internal/domain"
)

type errorResponse struct {
	Error string `json:"error"`
}

var errInvalidVersion = errors.New("invalid version")

type feeScaleRequest struct {
	Name string `json:"name"`
}
type feeAmountResponse struct {
	CentFee int64 `json:"centFee"`
}
type feeScaleVersionsResponse struct {
	Versions []domain.FeeScaleVersion `json:"versions"`
}
type feeScalesResponse struct {
	FeeScales []domain.FeeScale `json:"feeScales"`
}

type rateSheetRequest struct {
	Name        string               `json:"name"`
	CustomerKey string               `json:"customerKey"`
	Type        domain.RateSheetType `json:"type"`
	Active      bool                 `json:"active"`
}
type feeScaleOverrideRequest struct {
	FeeScaleName string `json:"feeScaleName"`
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

type dieselPriceRequest struct {
	ActiveDate   time.Time `json:"activeDate"`
	CoastalCents int64     `json:"coastalCents"`
	InlandCents  int64     `json:"inlandCents"`
}
type dieselPricesResponse struct {
	Prices []domain.DieselPrice `json:"prices"`
}
type dieselOverlayRequest struct {
	ActiveDate time.Time `json:"activeDate"`
}

type fixedRateRequest struct {
	Name        string `json:"name"`
	CustomerKey string `json:"customerKey"`
	RouteKey    string `json:"routeKey"`
	Active      bool   `json:"active"`
}
type fixedRateDraftRequest struct {
	CentRate               int64 `json:"centRate"`
	PointCount             int   `json:"pointCount"`
	CentAdditionalDropRate int64 `json:"centAdditionalDropRate"`
}
type fixedRateVersionsResponse struct {
	Versions []domain.FixedRateVersion `json:"versions"`
}
type fixedRatesResponse struct {
	FixedRates []domain.FixedRate `json:"fixedRates"`
}

// Deps wires the command handlers this REST layer calls. All three are
// required — pricing-service has no "not configured" state the way
// refdata-service's optional AI-translation feature does.
type Deps struct {
	FeeScales  *commands.FeeScaleHandler
	RateSheets *commands.RateSheetHandler
	FixedRates *commands.FixedRateHandler
	Log        *slog.Logger
}

type Handlers struct{ deps Deps }

func NewHandlers(deps Deps) *Handlers { return &Handlers{deps: deps} }

func (h *Handlers) Mount(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/pricing/{context}/fee-scales", h.registerFeeScale)
	mux.HandleFunc("GET /api/pricing/{context}/fee-scales", h.listFeeScales)
	mux.HandleFunc("GET /api/pricing/{context}/fee-scales/{name}", h.getFeeScale)
	mux.HandleFunc("POST /api/pricing/{context}/fee-scales/{name}/draft", h.createFeeScaleDraft)
	mux.HandleFunc("PUT /api/pricing/{context}/fee-scales/{name}/versions/{version}/ranges", h.addFeeScaleRange)
	mux.HandleFunc("POST /api/pricing/{context}/fee-scales/{name}/publish", h.publishFeeScale)
	mux.HandleFunc("POST /api/pricing/{context}/fee-scales/{name}/rollback/{version}", h.rollbackFeeScale)
	mux.HandleFunc("GET /api/pricing/{context}/fee-scales/{name}/versions", h.listFeeScaleVersions)
	mux.HandleFunc("GET /api/pricing/{context}/fee-scales/{name}/active", h.activeFeeScaleVersion)
	mux.HandleFunc("GET /api/pricing/{context}/fee-scales/{name}/fee", h.calculateFee)

	mux.HandleFunc("POST /api/pricing/{context}/rate-sheets", h.registerRateSheet)
	mux.HandleFunc("GET /api/pricing/{context}/rate-sheets", h.listRateSheets)
	mux.HandleFunc("GET /api/pricing/{context}/rate-sheets/{name}", h.getRateSheet)
	mux.HandleFunc("POST /api/pricing/{context}/rate-sheets/{name}/draft", h.createRateSheetDraft)
	mux.HandleFunc("PUT /api/pricing/{context}/rate-sheets/{name}/versions/{version}/entries", h.addRateSheetEntry)
	mux.HandleFunc("PUT /api/pricing/{context}/rate-sheets/{name}/versions/{version}/fee-scale-override", h.setFeeScaleOverride)
	mux.HandleFunc("POST /api/pricing/{context}/rate-sheets/{name}/publish", h.publishRateSheet)
	mux.HandleFunc("POST /api/pricing/{context}/rate-sheets/{name}/rollback/{version}", h.rollbackRateSheet)
	mux.HandleFunc("GET /api/pricing/{context}/rate-sheets/{name}/versions", h.listRateSheetVersions)
	mux.HandleFunc("GET /api/pricing/{context}/rate-sheets/{name}/active", h.activeRateSheetVersion)
	mux.HandleFunc("GET /api/pricing/{context}/rate-sheets/{name}/additional-drops-charge", h.rateSheetDropsCharge)

	// Diesel price index and overlay (Phase 25i, BR-P17–BR-P23).
	mux.HandleFunc("POST /api/pricing/{context}/diesel-prices", h.indexDieselPrice)
	mux.HandleFunc("GET /api/pricing/{context}/diesel-prices", h.listDieselPrices)
	mux.HandleFunc("POST /api/pricing/{context}/rate-sheets/{name}/diesel-overlay", h.applyRateSheetOverlay)

	mux.HandleFunc("POST /api/pricing/{context}/fixed-rates", h.registerFixedRate)
	mux.HandleFunc("GET /api/pricing/{context}/fixed-rates", h.listFixedRates)
	mux.HandleFunc("GET /api/pricing/{context}/fixed-rates/{name}", h.getFixedRate)
	mux.HandleFunc("POST /api/pricing/{context}/fixed-rates/{name}/draft", h.createFixedRateDraft)
	mux.HandleFunc("POST /api/pricing/{context}/fixed-rates/{name}/publish", h.publishFixedRate)
	mux.HandleFunc("POST /api/pricing/{context}/fixed-rates/{name}/rollback/{version}", h.rollbackFixedRate)
	mux.HandleFunc("GET /api/pricing/{context}/fixed-rates/{name}/versions", h.listFixedRateVersions)
	mux.HandleFunc("GET /api/pricing/{context}/fixed-rates/{name}/active", h.activeFixedRateVersion)
	mux.HandleFunc("GET /api/pricing/{context}/fixed-rates/{name}/additional-drops-charge", h.fixedRateDropsCharge)
}

// --- FeeScale ---

func (h *Handlers) registerFeeScale(w http.ResponseWriter, r *http.Request) {
	var req feeScaleRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	fs := domain.FeeScale{Context: r.PathValue("context"), Name: req.Name}
	if err := h.deps.FeeScales.Register(r.Context(), fs); err != nil {
		h.writeError(w, err)
		return
	}
	h.writeJSON(w, http.StatusCreated, fs)
}

func (h *Handlers) getFeeScale(w http.ResponseWriter, r *http.Request) {
	fs, err := h.deps.FeeScales.Get(r.Context(), r.PathValue("context"), r.PathValue("name"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, fs)
}

func (h *Handlers) listFeeScales(w http.ResponseWriter, r *http.Request) {
	feeScales, err := h.deps.FeeScales.List(r.Context(), r.PathValue("context"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, feeScalesResponse{FeeScales: feeScales})
}

func (h *Handlers) createFeeScaleDraft(w http.ResponseWriter, r *http.Request) {
	v, err := h.deps.FeeScales.CreateDraft(r.Context(), r.PathValue("context"), r.PathValue("name"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.writeJSON(w, http.StatusCreated, v)
}

func (h *Handlers) addFeeScaleRange(w http.ResponseWriter, r *http.Request) {
	version, err := pathVersion(r)
	if err != nil {
		h.writeJSON(w, http.StatusBadRequest, errorResponse{Error: err.Error()})
		return
	}
	var rng domain.FeeScaleRange
	if err := json.NewDecoder(r.Body).Decode(&rng); err != nil {
		h.writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid request body"})
		return
	}
	if err := h.deps.FeeScales.AddRange(r.Context(), r.PathValue("context"), r.PathValue("name"), version, rng); err != nil {
		h.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) publishFeeScale(w http.ResponseWriter, r *http.Request) {
	v, err := h.deps.FeeScales.Publish(r.Context(), r.PathValue("context"), r.PathValue("name"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, v)
}

func (h *Handlers) rollbackFeeScale(w http.ResponseWriter, r *http.Request) {
	version, err := pathVersion(r)
	if err != nil {
		h.writeJSON(w, http.StatusBadRequest, errorResponse{Error: err.Error()})
		return
	}
	v, err := h.deps.FeeScales.Rollback(r.Context(), r.PathValue("context"), r.PathValue("name"), version)
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, v)
}

func (h *Handlers) listFeeScaleVersions(w http.ResponseWriter, r *http.Request) {
	versions, err := h.deps.FeeScales.Versions(r.Context(), r.PathValue("context"), r.PathValue("name"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, feeScaleVersionsResponse{Versions: versions})
}

func (h *Handlers) activeFeeScaleVersion(w http.ResponseWriter, r *http.Request) {
	v, err := h.deps.FeeScales.ActiveVersion(r.Context(), r.PathValue("context"), r.PathValue("name"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, v)
}

func (h *Handlers) calculateFee(w http.ResponseWriter, r *http.Request) {
	bid, err := strconv.ParseInt(r.URL.Query().Get("bid"), 10, 64)
	if err != nil {
		h.writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid or missing ?bid="})
		return
	}
	fee, err := h.deps.FeeScales.CalculateFee(r.Context(), r.PathValue("context"), r.PathValue("name"), bid)
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, feeAmountResponse{CentFee: fee})
}

// --- RateSheet ---

func (h *Handlers) registerRateSheet(w http.ResponseWriter, r *http.Request) {
	var req rateSheetRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	rs := domain.RateSheet{Context: r.PathValue("context"), Name: req.Name, CustomerKey: req.CustomerKey, Type: req.Type, Active: req.Active}
	if err := h.deps.RateSheets.Register(r.Context(), rs); err != nil {
		h.writeError(w, err)
		return
	}
	h.writeJSON(w, http.StatusCreated, rs)
}

func (h *Handlers) getRateSheet(w http.ResponseWriter, r *http.Request) {
	rs, err := h.deps.RateSheets.Get(r.Context(), r.PathValue("context"), r.PathValue("name"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, rs)
}

func (h *Handlers) listRateSheets(w http.ResponseWriter, r *http.Request) {
	rateSheets, err := h.deps.RateSheets.List(r.Context(), r.PathValue("context"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, rateSheetsResponse{RateSheets: rateSheets})
}

func (h *Handlers) createRateSheetDraft(w http.ResponseWriter, r *http.Request) {
	v, err := h.deps.RateSheets.CreateDraft(r.Context(), r.PathValue("context"), r.PathValue("name"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.writeJSON(w, http.StatusCreated, v)
}

func (h *Handlers) addRateSheetEntry(w http.ResponseWriter, r *http.Request) {
	version, err := pathVersion(r)
	if err != nil {
		h.writeJSON(w, http.StatusBadRequest, errorResponse{Error: err.Error()})
		return
	}
	var entry domain.RateSheetEntry
	if err := json.NewDecoder(r.Body).Decode(&entry); err != nil {
		h.writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid request body"})
		return
	}
	if err := h.deps.RateSheets.AddEntry(r.Context(), r.PathValue("context"), r.PathValue("name"), version, entry); err != nil {
		h.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) setFeeScaleOverride(w http.ResponseWriter, r *http.Request) {
	version, err := pathVersion(r)
	if err != nil {
		h.writeJSON(w, http.StatusBadRequest, errorResponse{Error: err.Error()})
		return
	}
	var req feeScaleOverrideRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid request body"})
		return
	}
	if err := h.deps.RateSheets.SetFeeScaleOverride(r.Context(), r.PathValue("context"), r.PathValue("name"), version, req.FeeScaleName); err != nil {
		h.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) publishRateSheet(w http.ResponseWriter, r *http.Request) {
	v, err := h.deps.RateSheets.Publish(r.Context(), r.PathValue("context"), r.PathValue("name"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, v)
}

func (h *Handlers) rollbackRateSheet(w http.ResponseWriter, r *http.Request) {
	version, err := pathVersion(r)
	if err != nil {
		h.writeJSON(w, http.StatusBadRequest, errorResponse{Error: err.Error()})
		return
	}
	v, err := h.deps.RateSheets.Rollback(r.Context(), r.PathValue("context"), r.PathValue("name"), version)
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, v)
}

func (h *Handlers) listRateSheetVersions(w http.ResponseWriter, r *http.Request) {
	versions, err := h.deps.RateSheets.Versions(r.Context(), r.PathValue("context"), r.PathValue("name"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, rateSheetVersionsResponse{Versions: versions})
}

func (h *Handlers) activeRateSheetVersion(w http.ResponseWriter, r *http.Request) {
	v, err := h.deps.RateSheets.ActiveVersion(r.Context(), r.PathValue("context"), r.PathValue("name"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, v)
}

func (h *Handlers) rateSheetDropsCharge(w http.ResponseWriter, r *http.Request) {
	addressCount, err := strconv.Atoi(r.URL.Query().Get("addressCount"))
	if err != nil {
		h.writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid or missing ?addressCount="})
		return
	}
	charge, err := h.deps.RateSheets.AdditionalDropsCharge(r.Context(), r.PathValue("context"), r.PathValue("name"), r.URL.Query().Get("route"), r.URL.Query().Get("vehicleType"), addressCount)
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, dropsChargeResponse{CentCharge: charge})
}

// --- Diesel price index and overlay ---

func (h *Handlers) indexDieselPrice(w http.ResponseWriter, r *http.Request) {
	var req dieselPriceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid request body"})
		return
	}
	price := domain.DieselPrice{ActiveDate: req.ActiveDate, CoastalCents: req.CoastalCents, InlandCents: req.InlandCents}
	if err := h.deps.RateSheets.IndexDieselPrice(r.Context(), r.PathValue("context"), price); err != nil {
		h.writeError(w, err)
		return
	}
	h.writeJSON(w, http.StatusCreated, price)
}

func (h *Handlers) listDieselPrices(w http.ResponseWriter, r *http.Request) {
	prices, err := h.deps.RateSheets.ListDieselPrices(r.Context(), r.PathValue("context"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, dieselPricesResponse{Prices: prices})
}

func (h *Handlers) applyRateSheetOverlay(w http.ResponseWriter, r *http.Request) {
	var req dieselOverlayRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid request body"})
		return
	}
	v, err := h.deps.RateSheets.ApplyDieselOverlay(r.Context(), r.PathValue("context"), r.PathValue("name"), req.ActiveDate)
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, v)
}

// --- FixedRate ---

func (h *Handlers) registerFixedRate(w http.ResponseWriter, r *http.Request) {
	var req fixedRateRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	fr := domain.FixedRate{Context: r.PathValue("context"), Name: req.Name, CustomerKey: req.CustomerKey, RouteKey: req.RouteKey, Active: req.Active}
	if err := h.deps.FixedRates.Register(r.Context(), fr); err != nil {
		h.writeError(w, err)
		return
	}
	h.writeJSON(w, http.StatusCreated, fr)
}

func (h *Handlers) getFixedRate(w http.ResponseWriter, r *http.Request) {
	fr, err := h.deps.FixedRates.Get(r.Context(), r.PathValue("context"), r.PathValue("name"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, fr)
}

func (h *Handlers) listFixedRates(w http.ResponseWriter, r *http.Request) {
	fixedRates, err := h.deps.FixedRates.List(r.Context(), r.PathValue("context"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, fixedRatesResponse{FixedRates: fixedRates})
}

func (h *Handlers) createFixedRateDraft(w http.ResponseWriter, r *http.Request) {
	var req fixedRateDraftRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid request body"})
		return
	}
	v, err := h.deps.FixedRates.CreateDraft(r.Context(), r.PathValue("context"), r.PathValue("name"), req.CentRate, req.PointCount, req.CentAdditionalDropRate)
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.writeJSON(w, http.StatusCreated, v)
}

func (h *Handlers) publishFixedRate(w http.ResponseWriter, r *http.Request) {
	v, err := h.deps.FixedRates.Publish(r.Context(), r.PathValue("context"), r.PathValue("name"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, v)
}

func (h *Handlers) rollbackFixedRate(w http.ResponseWriter, r *http.Request) {
	version, err := pathVersion(r)
	if err != nil {
		h.writeJSON(w, http.StatusBadRequest, errorResponse{Error: err.Error()})
		return
	}
	v, err := h.deps.FixedRates.Rollback(r.Context(), r.PathValue("context"), r.PathValue("name"), version)
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, v)
}

func (h *Handlers) listFixedRateVersions(w http.ResponseWriter, r *http.Request) {
	versions, err := h.deps.FixedRates.Versions(r.Context(), r.PathValue("context"), r.PathValue("name"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, fixedRateVersionsResponse{Versions: versions})
}

func (h *Handlers) activeFixedRateVersion(w http.ResponseWriter, r *http.Request) {
	v, err := h.deps.FixedRates.ActiveVersion(r.Context(), r.PathValue("context"), r.PathValue("name"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, v)
}

func (h *Handlers) fixedRateDropsCharge(w http.ResponseWriter, r *http.Request) {
	addressCount, err := strconv.Atoi(r.URL.Query().Get("addressCount"))
	if err != nil {
		h.writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid or missing ?addressCount="})
		return
	}
	charge, err := h.deps.FixedRates.AdditionalDropsCharge(r.Context(), r.PathValue("context"), r.PathValue("name"), addressCount)
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, dropsChargeResponse{CentCharge: charge})
}

// --- helpers ---

func pathVersion(r *http.Request) (int, error) {
	var version int
	if _, err := fmt.Sscanf(r.PathValue("version"), "%d", &version); err != nil {
		return 0, errInvalidVersion
	}
	return version, nil
}

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
	case errors.Is(err, domain.ErrFeeScaleNotFound), errors.Is(err, domain.ErrRateSheetNotFound),
		errors.Is(err, domain.ErrFixedRateNotFound), errors.Is(err, domain.ErrFeeScaleDraftNotFound),
		errors.Is(err, domain.ErrRateSheetDraftNotFound), errors.Is(err, domain.ErrFixedRateDraftNotFound),
		errors.Is(err, domain.ErrNoActiveFeeScaleVersion), errors.Is(err, domain.ErrNoActiveRateSheetVersion),
		errors.Is(err, domain.ErrNoActiveFixedRateVersion), errors.Is(err, domain.ErrRateSheetEntryNotFound),
		errors.Is(err, domain.ErrEntryNotFound), errors.Is(err, domain.ErrNoDieselPrice):
		status = http.StatusNotFound
	case errors.Is(err, domain.ErrDraftAlreadyExists), errors.Is(err, domain.ErrOnlyDraftCanPublish),
		errors.Is(err, domain.ErrRollbackTargetNotPublished),
		errors.Is(err, domain.ErrRateSheetDraftAlreadyExists), errors.Is(err, domain.ErrRateSheetOnlyDraftCanPublish),
		errors.Is(err, domain.ErrRateSheetRollbackTargetNotPublished),
		errors.Is(err, domain.ErrFixedRateDraftAlreadyExists), errors.Is(err, domain.ErrFixedRateOnlyDraftCanPublish),
		errors.Is(err, domain.ErrFixedRateRollbackTargetNotPublished):
		status = http.StatusConflict
	case errors.Is(err, domain.ErrInvalidRateType), errors.Is(err, domain.ErrBidAboveHighestRange),
		errors.Is(err, errInvalidVersion):
		status = http.StatusBadRequest
	}
	if h.deps.Log != nil && status == http.StatusInternalServerError {
		h.deps.Log.Error("pricing request failed", "err", err)
	}
	h.writeJSON(w, status, errorResponse{Error: err.Error()})
}
