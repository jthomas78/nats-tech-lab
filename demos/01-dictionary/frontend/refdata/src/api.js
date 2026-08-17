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
