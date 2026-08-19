// NATS client for refdata-service's api.* business + admin surface (Phase
// 32) — replaces this app's previous REST client entirely. Every call goes
// over the single PLATFORM-account connection useRefdataAdminConnection.js
// manages (this app is a cross-tenant, platform-operator tool, like the
// Admin UI, not a Sea Freight Flow-style tenant app — see
// accounts-service/auth/token.go's MintRefdataAdminToken doc comment).
//
// Two subject namespaces (BR-D41), matching refdata/internal/browserrpc/
// adapter.go exactly:
//   - business: api.{context}.refdata.{name}.v1 — reads.
//   - admin:    api.{context}.refdata.admin.{name}.v1 — corpus/context/type/
//     locale/item/reference/localization mutations.
// {context} is a real subject token (not a body field) for every operation
// actually scoped to one context; a handful of operations with no context
// to route by (dictionary types, and the context registry itself) use the
// fixed literal "_platform" instead — see businessSubject/adminSubject
// below and the matching Go-side doc comments (TypesListSubject,
// TypeRegisterSubject, ContextListSubject, ContextRegisterSubject).

import { useRefdataAdminConnection } from './nats/useRefdataAdminConnection.js'
import { useTenantConnection } from './nats/useTenantConnection.js'

// assertContextToken guards against a context value that isn't a legal
// single NATS subject token — mirrors frontend/admin's api.js tpSubject:
// silently producing "api.acme.north.refdata..." would shift every later
// token by one and make the service resolve the wrong context.
function assertContextToken(context) {
  if (!context || /[.\s*>]/.test(context)) {
    throw new Error(`invalid context for a NATS subject token: ${JSON.stringify(context)}`)
  }
}

function businessSubject(context, name) {
  assertContextToken(context)
  return `api.${context}.refdata.${name}.v1`
}

function adminSubject(context, name) {
  assertContextToken(context)
  return `api.${context}.refdata.admin.${name}.v1`
}

function request(subject, payload) {
  return useRefdataAdminConnection().request(subject, payload)
}

// ── Types ──────────────────────────────────────────────────────────────────────
// Global — never scoped by context (fixed-literal "_platform" subject,
// matching TypesListSubject/TypeRegisterSubject on the Go side).

export function listTypes() {
  return request('api._platform.refdata.types.list.v1', {})
}

export function registerType(input) {
  return request('api._platform.refdata.admin.type.register.v1', input)
}

// ── Items ──────────────────────────────────────────────────────────────────────

export function listItems(context, typeKey, { all = false, locale = '' } = {}) {
  // type.list is the business subject's name for "list this type's items"
  // (matches internal/natsrpc's own precedent — see TypeListSubject's Go
  // doc comment). all mirrors BR-D06's REST ?all=true (include deprecated).
  return request(businessSubject(context, 'type.list'), { typeKey, locale, all })
}

export function getItem(context, typeKey, code, { locale = '' } = {}) {
  // item.get has no ?expand= counterpart (References.Expand) — this app's
  // detail panel doesn't use expand today; add an expand field/handler if
  // that changes.
  return request(businessSubject(context, 'item.get'), { typeKey, code, locale })
}

export function registerItem(input) {
  const { context, ...rest } = input
  return request(adminSubject(context, 'item.register'), rest)
}

export function deprecateItem(typeKey, context, code) {
  return request(adminSubject(context, 'item.deprecate'), { typeKey, code })
}

export function reactivateItem(typeKey, context, code) {
  return request(adminSubject(context, 'item.reactivate'), { typeKey, code })
}

export function deleteItem(typeKey, context, code) {
  return request(adminSubject(context, 'item.delete'), { typeKey, code })
}

// Full replace of an item's attrs map (BR-D18) — not a per-key merge, so
// callers must send the complete desired map (spread the current attrs and
// override just the keys changing).
export function updateItem(typeKey, context, code, attrs) {
  return request(adminSubject(context, 'item.update-attrs'), { typeKey, code, attrs })
}

// ── References ─────────────────────────────────────────────────────────────────

export function createReference(input) {
  const { context, ...rest } = input
  return request(adminSubject(context, 'reference.create'), rest)
}

export function listItemReferences(context, typeKey, code) {
  return request(businessSubject(context, 'item.references-list'), { typeKey, code })
}

// ── Localization ───────────────────────────────────────────────────────────────

export function setLocalization(input) {
  const { context, ...rest } = input
  return request(adminSubject(context, 'localization.set'), rest)
}

export function listItemLocalizations(context, typeKey, code) {
  return request(businessSubject(context, 'item.localizations-list'), { typeKey, code })
}

// Drafts candidate label/description text per target locale (BR-D07) —
// nothing is persisted by this call; save an accepted draft via
// setLocalization({ ..., source: 'ai' }). Returns { drafts: [{ locale, label,
// description, error }] } — a per-locale `error` means that locale's draft
// failed without aborting the others in the same request.
export function draftTranslation(typeKey, code, context, targetLocales) {
  return request(adminSubject(context, 'translation.draft'), { typeKey, code, targetLocales })
}

export function listLocales(context) {
  return request(businessSubject(context, 'locales.list'), {})
}

export function addLocale(context, locale, isDefault) {
  return request(adminSubject(context, 'locale.add'), { locale, isDefault })
}

export function getCompleteness(context, typeKey, locale) {
  return request(businessSubject(context, 'completeness'), { typeKey, locale })
}

export function getCacheStatus(context, typeKey) {
  return request(businessSubject(context, 'cache-status'), { typeKey })
}

// ── Change notification (Phase 32 — replaces the SSE watch stream) ──────────────
// See stores/dictionary.js's connect()/disconnect(): subscribes to
// notify._platform.refdata.> (the same PLATFORM-wide bridge the Admin UI's
// [messages]/context feed already used since Phase 23 — shipping-service's
// RegisterRefdataNotify) and parses {context, typeKey} off the subject
// itself, not the payload.

// ── Contexts (Phase 12.1 — context hierarchy) ───────────────────────────────────
// list/register have no single context to route by (fixed-literal
// "_platform"); get/set-visible are scoped to the context being read/toggled.

export function listContexts() {
  return request('api._platform.refdata.context.list.v1', {})
}

export function getContext(context) {
  return request(businessSubject(context, 'context.get'), {})
}

export function registerContext(input) {
  return request('api._platform.refdata.admin.context.register.v1', input)
}

// ── Corpus versioning (Phase 12.2–12.5) ─────────────────────────────────────────

export function listCorpusVersions(context) {
  return request(adminSubject(context, 'corpus.list-versions'), {})
}

export function getCorpusVersion(context, version) {
  return request(adminSubject(context, 'corpus.get-version'), { version })
}

export function getDraft(context) {
  return request(adminSubject(context, 'corpus.get-draft'), {})
}

export function createDraft(context, notes = '') {
  return request(adminSubject(context, 'corpus.create-draft'), { notes })
}

export function putDraftItem(context, item) {
  return request(adminSubject(context, 'corpus.put-draft-item'), item)
}

export function putDraftLocalization(context, localization) {
  return request(adminSubject(context, 'corpus.put-draft-localization'), localization)
}

export function publishCorpus(context) {
  return request(adminSubject(context, 'corpus.publish'), {})
}

export function rollbackCorpus(context, version, notes = '') {
  return request(adminSubject(context, 'corpus.rollback'), { version, notes })
}

export function diffCorpusVersions(context, fromVersion, toVersion) {
  return request(adminSubject(context, 'corpus.diff'), { from: fromVersion, to: toVersion })
}

// ── Trading partner tenant scoping (Phase 36.2) ─────────────────────────────────
// context.list.v1 accepts an explicit `tenant` filter in the body (BR-D35 —
// refdata-service has no server-supplied caller identity to derive it from,
// and the per-tenant connection only proves account membership, a different
// axis from the `tenant` column contexts are tagged with). listContexts()
// above omits it to browse every tenant's contexts platform-wide; this variant
// scopes to one tenant for the Trading Partners tenant+context selector.
// Filters "_"-reserved contexts (platform roots, not real fleet scopes),
// mirroring frontend/admin's stores/dictionary.js loadContexts().
export async function listContextsForTenant(tenant) {
  const { contexts } = await request('api._platform.refdata.context.list.v1', { tenant })
  return (contexts ?? []).filter((c) => !c.context.startsWith('_'))
}

// ── Tenant listing (Phase 36.2) ─────────────────────────────────────────────────
// REST, not NATS — GET /api/auth/tenants (accounts-service, already proxied
// for the PLATFORM connection's own credential fetch). Deliberately not
// GET /api/accounts (which frontend/admin uses for its own Accounts admin
// view): that endpoint returns every account row, including the reserved
// "platform"/"sys" infrastructure accounts (BR-AC06) — neither is a real
// tenant, and trading-partner-service has no meaningful Shippers/
// Transporters list for either. /api/auth/tenants already excludes them
// (accounts.Store.ListActiveTenantNames), so this reuses that filtering
// instead of re-implementing it here.
async function restRequest(path, options = {}) {
  const res = await fetch(path, { headers: { 'Content-Type': 'application/json' }, ...options })
  const body = await res.json().catch(() => ({}))
  if (!res.ok) throw new Error(body.error || `${res.status} ${res.statusText}`)
  return body
}

export function listAvailableTenants() {
  return restRequest('/api/auth/tenants').then((body) => body.tenants ?? [])
}

// ── Trading partners (Phase 26h origin, migrated from frontend/admin in
// Phase 36.2) ────────────────────────────────────────────────────────────────
// Ported verbatim from frontend/admin/src/api.js, swapped onto this app's own
// tenant connection (useTenantConnection.js) — see that module's doc comment
// for why Tech Lab Operator needs a second, tenant-scoped connection
// alongside the PLATFORM one every other call in this file uses.

// tpSubject builds one trading-partner api.* subject, failing loudly if
// context isn't a legal single subject token. Silently producing
// "api.acme.north.trading-partner..." would shift every later token by one
// and make the service resolve the wrong context.
function tpSubject(context, entity, action) {
  if (!context || /[.\s*>]/.test(context)) {
    throw new Error(`invalid context for a NATS subject token: ${JSON.stringify(context)}`)
  }
  return `api.${context}.trading-partner.${entity}.${action}.v1`
}

function tpRequest(context, entity, action, payload) {
  return useTenantConnection().request(tpSubject(context, entity, action), payload)
}

export function listTradingPartners(context) {
  return tpRequest(context, 'partner', 'list')
}

export function registerTradingPartner(context, input) {
  return tpRequest(context, 'partner', 'register', input)
}

export function activateTradingPartner(context, id) {
  return tpRequest(context, 'partner', 'activate', { id })
}

export function suspendTradingPartner(context, id, reason) {
  return tpRequest(context, 'partner', 'suspend', { id, reason })
}

export function reactivateTradingPartner(context, id) {
  return tpRequest(context, 'partner', 'reactivate', { id })
}

export function getTradingPartnerAudit(context, id) {
  return tpRequest(context, 'partner', 'audit', { id })
}

export function listComplianceDocuments(context, id) {
  return tpRequest(context, 'document', 'list', { id })
}

export function addComplianceDocument(context, id, input) {
  return tpRequest(context, 'document', 'add', { id, ...input })
}

export function approveComplianceDocument(context, id, type) {
  return tpRequest(context, 'document', 'approve', { id, type })
}

export function rejectComplianceDocument(context, id, type) {
  return tpRequest(context, 'document', 'reject', { id, type })
}

export function resubmitComplianceDocument(context, id, type) {
  return tpRequest(context, 'document', 'resubmit', { id, type })
}

export function listFleetAssets(context, id) {
  return tpRequest(context, 'fleet-asset', 'list', { id })
}

// Signature keeps `tenant` so TradingPartnersPanel.vue's call site is
// unchanged, but the value is deliberately unused: the service derives the
// tenant from the connection's own NATS account. Dropping the parameter
// would silently change the meaning of the 4th positional argument (input)
// at every call site, which is a worse trade than one documented unused
// parameter.
 
export function addFleetAsset(context, id, tenant, input) {
  return tpRequest(context, 'fleet-asset', 'add', { id, ...input })
}
