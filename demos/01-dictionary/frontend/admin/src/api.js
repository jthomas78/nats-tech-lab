// Thin client over the demo backends. Diagnostics/control-plane calls use
// relative REST paths so Vite/nginx can route them. The one NATS exception is
// the Admin UI's narrowly-scoped refdata read below, sent on its single
// PLATFORM browser connection.

import { usePlatformConnection } from './nats/usePlatformConnection.js'
import { REQUESTOR_HEADER, REST_REQUESTOR_ID } from './requestorId.js'

// Every REST call carries this tab's Nats-Requestor identity, the same one
// the api.* calls below send (requestorId.js) — accounts-service's and
// shipping-service's HTTP trace middleware lift it onto their span's
// requester field, so a REST hop in the Traces panel names its caller
// instead of reading "no Nats-Requestor on this span". Observability only,
// never authorization (BR-041).
async function request(path, options = {}) {
  // Caller headers MERGE with the defaults rather than replacing them: the
  // registry writes below send If-Match, and losing Content-Type or the
  // requestor header along the way would be silent.
  const { headers, ...rest } = options
  const res = await fetch(path, {
    headers: {
      'Content-Type': 'application/json',
      [REQUESTOR_HEADER]: REST_REQUESTOR_ID,
      ...headers,
    },
    ...rest,
  })
  if (res.status === 204) return null
  const body = await res.json().catch(() => ({}))
  if (!res.ok) {
    const err = new Error(body.error || `${res.status} ${res.statusText}`)
    err.status = res.status
    // The parsed body rides along: a 409 from the registry carries the
    // revision the caller lost to, which the stale-revision panel needs.
    err.body = body
    throw err
  }
  return body
}

// Raw ports table rows (name + createdAt) for the admin Postgres Tables
// panel. This is an admin diagnostics route, not a business command surface.
export function getPortsTable(context) {
  return request(`/api/admin/ports/${context}`)
}

// Every event stream across every account this backend reaches (tagged with
// its account), with run-time status — backs the Streams view's stream rail.
// Deliberately NOT scoped to the topbar's active tenant, same as
// listKVBuckets below. KV_* backing streams are excluded server-side.
export function listStreams() {
  return request('/api/jetstream/streams')
}

// Every KV bucket across every account this backend reaches (tagged with
// its account), with run-time status — backs the KV inspector's bucket
// rail. Deliberately NOT scoped to the topbar's active tenant.
export function listKVBuckets() {
  return request('/api/kv/buckets')
}

// ── One-shot diagnostics reads ───────────────────────────────────────────────
// Each returns a point-in-time cross-account snapshot through
// observability-service. Raw tenant updates are not subscribed in the Admin
// browser; only centralized trace/pub-sub projections have a live continuation.

export function getKvBucketEntries(account, bucket) {
  return request(`/api/kv/buckets/${encodeURIComponent(account)}/${encodeURIComponent(bucket)}/entries`)
}

// account disambiguates the stream name, which is only unique WITHIN an
// account (every tenant provisions its own SHIPPING) — same reason
// getKvBucketEntries above takes one. Omit it to fall back to whichever tenant
// the backend currently has active.
export function getJetstreamReplay(account, stream = 'SHIPPING') {
  const query = new URLSearchParams({ stream })
  if (account) query.set('account', account)
  return request(`/api/jetstream/replay?${query}`)
}

// The backend's active account label is still used to select the legacy
// Overview snapshot. It does not cause the browser to connect to that account.
export function getTenant() {
  return request('/api/tenant')
}

// ── Refdata contexts (Phase 16f; moved onto api.* in Phase 32) ────────────────
// Replaces the previously hardcoded CONTEXTS array in stores/dictionary.js.
//
// Phase 32 repointed this from shipping-service's retired /api/refdata/contexts
// relay to refdata-service's own api.* subject — this call is the reason that
// relay route existed, and removing the conduit is the point of the phase.
// context.list is a business subject, not an admin one (BR-D41): reading the
// context tree for a dropdown is a plain read, so a browser credential
// reaches it without the denied api.*.refdata.admin.> prefix.
//
// The tenant filter travels in the request body (BR-D35) — refdata-service
// has no server-supplied caller identity to derive it from (BR-D34). The
// browser connection is PLATFORM-scoped, so the body is the only tenant
// selector on this read.
// Returns bare context names, matching the shape the REST relay returned so
// stores/dictionary.js's loadContexts() is unchanged.
export function getRefdataContexts(tenant) {
  return usePlatformConnection().request('api._platform.refdata.context.list.v1', { tenant }).then((body) =>
    (body.contexts ?? []).map((c) => c.context),
  )
}

// ── NATS users (Phase 50b/50c, BR-AC40) ──────────────────────────────────────
// The user registry accounts-service writes at mint time (BR-AC38), read back
// over the same PLATFORM browser connection as the refdata call above. These
// are the only two accounts-service subjects MintAdminToken allowlists, and
// they are granted individually rather than as a prefix — so a typo here is a
// permissions error from the server, not a call to a neighbouring endpoint.
//
// Deliberately NOT the /api/platform/ REST proxy the account calls below use:
// a browser business path in this repo is NATS-only (Phases 31–34), and the
// registry never had a REST surface to begin with.
export function listNatsUsers() {
  return usePlatformConnection().request('api._platform.accounts.user.list.v1', {}).then(
    (body) => body.users ?? [],
  )
}

// One user's claims, resolved against its issuing signing key (BR-AC41) — the
// drill-in read. Separate from the list because permissions are the expensive
// half and no roster row displays them.
export function getNatsUser(publicKey) {
  return usePlatformConnection().request('api._platform.accounts.user.get.v1', { publicKey })
}

// Revoke one credential (Phase 51b, BR-AC43). The only write on this surface,
// and terminal: there is no un-revoke call to pair with it, by design —
// recovery from a mis-revocation is minting a replacement credential.
//
// Takes the public NKey and nothing else. A name is not unique enough to
// revoke by (BR-058), which is the same reason the panel joins on the key.
export function revokeNatsUser(publicKey) {
  return usePlatformConnection().request('api._platform.accounts.user.revoke.v1', { publicKey })
}

// ── Accounts (Phase 14c) ───────────────────────────────────────────────────────
// Dynamic tenant provisioning via accounts-service, proxied at /api/platform/
// (nginx.conf / vite.config.js inject the shared basic-auth secret — the
// browser never handles it). Distinct from getTenant above: that reports
// shipping-service's current snapshot account; these calls manage the full
// accounts-service roster.

export function listAccounts() {
  return request('/api/platform/accounts')
}

export function createAccount(input) {
  return request('/api/platform/accounts', { method: 'POST', body: JSON.stringify(input) })
}

export function getAccount(name) {
  return request(`/api/platform/accounts/${encodeURIComponent(name)}`)
}

export function suspendAccount(name) {
  return request(`/api/platform/accounts/${encodeURIComponent(name)}/suspend`, { method: 'POST' })
}

export function reactivateAccount(name) {
  return request(`/api/platform/accounts/${encodeURIComponent(name)}/reactivate`, { method: 'POST' })
}

export function updateAccountLimits(name, limits) {
  return request(`/api/platform/accounts/${encodeURIComponent(name)}/jslimits`, { method: 'POST', body: JSON.stringify(limits) })
}

export function getAccountsUsage() {
  return request('/api/platform/accounts/usage')
}

// Live export/import edges across every account, read from each account's
// current resolver JWT (not the bootstrap-time convention) — powers the
// Topology tab.
export function getAccountsTopology() {
  return request('/api/platform/accounts/topology')
}

// ── System settings (BR-AC20) ──────────────────────────────────────────────
// Platform-global config, not account-scoped — served under the same
// /api/platform/accounts proxy prefix (basic-auth injected by nginx/vite) as
// the collection-level /usage and /topology endpoints. Currently just the
// browser/admin JWT expiry TTL policy; the response also carries the read-only
// envelope bounds so the UI can constrain its editors.

export function getSystemConfig() {
  return request('/api/platform/accounts/system-config')
}

export function updateSystemConfig(input) {
  return request('/api/platform/accounts/system-config', { method: 'PUT', body: JSON.stringify(input) })
}

// ── Business Units (Phase 22) ─────────────────────────────────────────────────

export function listBusinessUnits(name) {
  return request(`/api/platform/accounts/${encodeURIComponent(name)}/business-units`)
}

export function createBusinessUnit(name, input) {
  return request(`/api/platform/accounts/${encodeURIComponent(name)}/business-units`, {
    method: 'POST',
    body: JSON.stringify(input),
  })
}

export function updateBusinessUnit(name, buName, input) {
  return request(`/api/platform/accounts/${encodeURIComponent(name)}/business-units/${encodeURIComponent(buName)}`, {
    method: 'PATCH',
    body: JSON.stringify(input),
  })
}

// ── NATS Connections + Services (Phase 17c) ──────────────────────────────────
// Both are snapshot reads, not SSE — /connz and $SRV.STATS are inherently
// poll-based (a single request/reply round-trip), unlike the KV/JetStream
// watches above which have a native "replay then live" model.

export function getNatsConnections() {
  return request('/api/nats/connections')
}

// The server's ring of recently-closed connections (Phase 51a, BR-062) — a
// credential's last connection OUTCOME, which the live list above cannot
// supply: it knows only who is connected now, so it cannot tell an idle
// credential from one being refused every few seconds.
//
// Its own route rather than a field on getNatsConnections: the closed ring is
// much larger than the live list and only the Users panel reads it.
export function getNatsClosedConnections() {
  return request('/api/nats/connections/closed')
}

export function getNatsServices() {
  return request('/api/nats/services')
}

export function getNatsAccountActivity() {
  return request('/api/nats/account-activity')
}

// Overview tab's trend charts (Phase 45, BR-043) — bucketed history from
// observability-service's 60-minute ring buffer, not the live snapshot
// above. duration must be '5m', '30m', or '1h'.
export function getNatsAccountActivityHistory(duration) {
  return request(`/api/nats/account-activity/history?duration=${encodeURIComponent(duration)}`)
}

// Log panel — tails NATS's own log_file server-side (level/q filters, tail
// hard-capped at 1000 regardless of what's requested). REST-polled like the
// two above, not a push/follow transport.
export function getNatsLog({ level, q, tail } = {}) {
  const params = new URLSearchParams()
  if (level) params.set('level', level)
  if (q) params.set('q', q)
  if (tail) params.set('tail', tail)
  const qs = params.toString()
  return request(`/api/nats/log${qs ? `?${qs}` : ''}`)
}

// ── Frontend plugin registry (accounts-service `registry` module) ────────────
// Curated registry state, proxied at /api/platform/registry. Writes are
// optimistically concurrent: every write carries the revision it was made
// against in If-Match, and a refusal (409) names the revision that won
// instead of merging (BR-AS18). There is no delete — an entry is disabled,
// never torn out from under a shell that already loaded it (BR-AS24).

export function getRegistryEntries() {
  return request('/api/platform/registry/entries')
}

export function upsertRegistryEntry(entry, ifRevision) {
  return request('/api/platform/registry/entries', {
    method: 'POST',
    headers: { 'If-Match': `"${ifRevision}"` },
    body: JSON.stringify({ entry }),
  })
}

export function setRegistryEntryEnabled(id, enabled, ifRevision) {
  return request(`/api/platform/registry/entries/${encodeURIComponent(id)}/enabled`, {
    method: 'POST',
    headers: { 'If-Match': `"${ifRevision}"` },
    body: JSON.stringify({ enabled }),
  })
}

export function getRegistryAudit(limit) {
  return request(`/api/platform/registry/audit${limit ? `?limit=${limit}` : ''}`)
}
