// Sea Freight Flow's data-access layer (Phase 15d) — NATS request/reply over
// the single browser WebSocket connection (nats/useNatsConnection.js)
// instead of REST + SSE. Every command/query below is the api.* frontend-
// to-service counterpart of what used to be a `fetch()` call — renamed from
// rpc.* in Phase 16b, since every caller here is the browser, never another
// backend service (rpc.* is now service-to-service only); see
// BUSINESS_RULES-SHIPPING.md's BR-023 for the transport contract and
// dictionary/internal/browserrpc/adapter.go for the server side.
//
// Refdata calls (below) are the one exception — they stay on REST + SSE,
// unchanged: refdata-service runs cross-tenant on the PLATFORM NATS account
// (BR-D08), which is out of scope for this phase (see Main-POC-Plan.md
// Phase 15's Context section).

import { request } from './nats/useNatsConnection'
import { REQUESTOR_HEADER, REST_REQUESTOR_ID } from './requestorId.js'

function apiSubject(context, entity, action) {
  return `api.${context}.shipping.${entity}.${action}.v1`
}

// ── Ship commands ─────────────────────────────────────────────────────────────

export function arrivePort(input) {
  return request(apiSubject(input.context, 'ship', 'arrive'), input)
}

export function departPort(input) {
  return request(apiSubject(input.context, 'ship', 'depart'), input)
}

// ── Container commands ────────────────────────────────────────────────────────

export function registerContainer(input) {
  return request(apiSubject(input.context, 'container', 'register'), input)
}

export function loadContainer(input) {
  return request(apiSubject(input.context, 'container', 'load'), input)
}

export function unloadContainer(input) {
  return request(apiSubject(input.context, 'container', 'unload'), input)
}

// ── Ports (Postgres-backed reference table, BR-017/BR-018) ───────────────────

export function getPorts(context) {
  return request(apiSubject(context, 'port', 'list'), {}).then((body) => body.values)
}

export function registerPort(context, name) {
  return request(apiSubject(context, 'port', 'register'), { name })
}

// ── Bootstrap queries (Phase 15d) — replace the SSE initial-snapshot the
// pre-Phase-15 stores relied on (KV WatchAll's replay-then-live semantics).
// Called once on connect/reconnect; notify.* (below) keeps state fresh
// after that. ─────────────────────────────────────────────────────────────

export function listShips(context) {
  return request(apiSubject(context, 'ship', 'list'), {}).then((body) => body.ships)
}

export function listContainers(context) {
  return request(apiSubject(context, 'container', 'list'), {}).then((body) => body.containers)
}

export function knownContainers(context) {
  return request(apiSubject(context, 'meta', 'known-containers'), {}).then((body) => body.values)
}

// ── notify.* subject builders (Phase 15b) — for stores to pass to
// useNatsConnection's subscribe(). Not REST URLs; kept here so every
// api.*/notify.* subject in this app is built in one place. ─────────────

export function notifySubject(context, entity) {
  return `notify.${context}.shipping.${entity}.changed`
}

// ── Business units (Phase 22, split Phase 22b) — replaces the old
// /api/refdata/contexts path. BU source of truth is now accounts-service.
// Returns the raw {id, name, context, visible, isDefault, createdAt} rows —
// unfiltered — so the store can tell the tenant's default apart from its real
// BUs by isDefault (BR-AC28) rather than by string-matching a reserved name,
// and can still find the default's context even when it's hidden (visible
// only filters the *selectable* list, not "does this account have a
// fallback"). ──

export async function getBusinessUnits(tenant) {
  // The only REST call left in this app — it still declares this tab's
  // Nats-Requestor identity (the same value the api.* calls send) so
  // accounts-service's HTTP span can name its caller. Observability only
  // (BR-041).
  const res = await fetch(`/api/platform/accounts/${encodeURIComponent(tenant)}/business-units`, {
    headers: { [REQUESTOR_HEADER]: REST_REQUESTOR_ID },
  })
  const body = await res.json().catch(() => ({}))
  if (!res.ok) throw new Error(body.error || `${res.status} ${res.statusText}`)
  return Array.isArray(body) ? body : []
}

// ── Pricing (Phase 25g/25h) — the "Pricing" tab: a landing panel (25g) plus
// manual entry (25h) for FeeScale/RateSheet/FixedRate's register/draft/add-
// range-or-entry/publish/rollback lifecycle. pricing-service is a separate
// backend from shipping-service, so its subjects carry "pricing" (not
// "shipping") as the service token; see
// backend/pricing-service/pricing/internal/browserrpc/adapter.go. There is
// no notifySubject counterpart — pricing-service publishes no notify.*
// change stream (BUSINESS_RULES-PRICING.md's Phase 25g note), so every call
// below is a one-shot api.* request; a panel updates its own local state
// from each call's result rather than waiting on a subscription. ─────────

function pricingApiSubject(context, entity, action) {
  return `api.${context}.pricing.${entity}.${action}.v1`
}

// ── FeeScale (BR-P01–BR-P06, BR-P16) ──────────────────────────────────────

export function listFeeScales(context) {
  return request(pricingApiSubject(context, 'fee-scale', 'list'), {}).then((body) => body.feeScales)
}

export function registerFeeScale(context, name) {
  return request(pricingApiSubject(context, 'fee-scale', 'register'), { name }).then((body) => body.feeScale)
}

export function createFeeScaleDraft(context, name) {
  return request(pricingApiSubject(context, 'fee-scale', 'create-draft'), { name }).then((body) => body.version)
}

export function addFeeScaleRange(context, name, version, range) {
  return request(pricingApiSubject(context, 'fee-scale', 'add-range'), { name, version, range })
}

export function publishFeeScale(context, name) {
  return request(pricingApiSubject(context, 'fee-scale', 'publish'), { name }).then((body) => body.version)
}

export function rollbackFeeScale(context, name, version) {
  return request(pricingApiSubject(context, 'fee-scale', 'rollback'), { name, version }).then((body) => body.version)
}

export function feeScaleVersions(context, name) {
  return request(pricingApiSubject(context, 'fee-scale', 'versions'), { name }).then((body) => body.versions)
}

export function activeFeeScaleVersion(context, name) {
  return request(pricingApiSubject(context, 'fee-scale', 'active'), { name }).then((body) => body.version)
}

// ── RateSheet (BR-P07–BR-P12) ─────────────────────────────────────────────

export function listRateSheets(context) {
  return request(pricingApiSubject(context, 'rate-sheet', 'list'), {}).then((body) => body.rateSheets)
}

export function registerRateSheet(context, input) {
  return request(pricingApiSubject(context, 'rate-sheet', 'register'), input).then((body) => body.rateSheet)
}

export function createRateSheetDraft(context, name) {
  return request(pricingApiSubject(context, 'rate-sheet', 'create-draft'), { name }).then((body) => body.version)
}

export function addRateSheetEntry(context, name, version, entry) {
  return request(pricingApiSubject(context, 'rate-sheet', 'add-entry'), { name, version, entry })
}

export function setRateSheetFeeScaleOverride(context, name, version, feeScaleName) {
  return request(pricingApiSubject(context, 'rate-sheet', 'set-fee-scale-override'), { name, version, feeScaleName })
}

export function publishRateSheet(context, name) {
  return request(pricingApiSubject(context, 'rate-sheet', 'publish'), { name }).then((body) => body.version)
}

export function rollbackRateSheet(context, name, version) {
  return request(pricingApiSubject(context, 'rate-sheet', 'rollback'), { name, version }).then((body) => body.version)
}

export function rateSheetVersions(context, name) {
  return request(pricingApiSubject(context, 'rate-sheet', 'versions'), { name }).then((body) => body.versions)
}

export function activeRateSheetVersion(context, name) {
  return request(pricingApiSubject(context, 'rate-sheet', 'active'), { name }).then((body) => body.version)
}

// ── FixedRate (BR-P13–BR-P15) ─────────────────────────────────────────────

export function listFixedRates(context) {
  return request(pricingApiSubject(context, 'fixed-rate', 'list'), {}).then((body) => body.fixedRates)
}

export function registerFixedRate(context, input) {
  return request(pricingApiSubject(context, 'fixed-rate', 'register'), input).then((body) => body.fixedRate)
}

// Unlike FeeScale/RateSheet, a FixedRate draft's rate fields are set at
// creation, not added incrementally (domain.FixedRateRepository.CreateDraft)
// — there is no separate add-entry step.
export function createFixedRateDraft(context, name, centRate, pointCount, centAdditionalDropRate) {
  return request(pricingApiSubject(context, 'fixed-rate', 'create-draft'), {
    name,
    centRate,
    pointCount,
    centAdditionalDropRate,
  }).then((body) => body.version)
}

export function publishFixedRate(context, name) {
  return request(pricingApiSubject(context, 'fixed-rate', 'publish'), { name }).then((body) => body.version)
}

export function rollbackFixedRate(context, name, version) {
  return request(pricingApiSubject(context, 'fixed-rate', 'rollback'), { name, version }).then((body) => body.version)
}

export function fixedRateVersions(context, name) {
  return request(pricingApiSubject(context, 'fixed-rate', 'versions'), { name }).then((body) => body.versions)
}

export function activeFixedRateVersion(context, name) {
  return request(pricingApiSubject(context, 'fixed-rate', 'active'), { name }).then((body) => body.version)
}

// ── Diesel price index + overlay (BR-P17–BR-P23, Phase 25i) ──────────────
// IndexDieselPrice upserts on (context, activeDate); ApplyDieselOverlay is a
// separate per-rate-sheet step (resolves the price in effect on activeDate,
// appends a minor-version overlay) — see adapter.go's handleRateSheetApplyOverlay.

export function indexDieselPrice(context, price) {
  return request(pricingApiSubject(context, 'diesel-price', 'index'), price)
}

export function listDieselPrices(context) {
  return request(pricingApiSubject(context, 'diesel-price', 'list'), {}).then((body) => body.prices)
}

export function applyDieselOverlay(context, name, activeDate) {
  return request(pricingApiSubject(context, 'rate-sheet', 'apply-overlay'), { name, activeDate }).then((body) => body.version)
}
