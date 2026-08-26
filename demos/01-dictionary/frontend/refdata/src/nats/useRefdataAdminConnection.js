// PLATFORM-account NATS WebSocket connection (Phase 32) — this app's only
// connection. frontend/refdata is a cross-tenant, platform-operator tool
// (like the Admin UI, not like Sea Freight Flow — it edits _platform's
// shared standards and every tenant's contexts alike, with no
// tenant/account concept of its own), so it connects once at app boot and
// never reconnects on a context switch — its context Select changes only
// which {context} subject token business/admin calls carry, never which
// NATS account this connection authenticates into. Mirrors
// frontend/admin/src/nats/usePlatformConnection.js's shape, but with a wider
// permission profile: Admin may publish only three exact read subjects,
// while MintRefdataAdminToken grants this app the full api.*.refdata.>
// business/admin surface.
//
// Credentials come from GET /api/auth/refdataAdminConnectInfo (Phase 32,
// no tenant param — see accounts-service/auth/token.go's
// MintRefdataAdminToken doc comment for why this needed its own mint
// function rather than reusing MintAdminToken or MintBrowserToken).

import { createConnectionState } from './connectionFactory.js'
import { REQUESTOR_HEADER, REST_REQUESTOR_ID } from '../requestorId.js'

async function fetchRefdataAdminConnectInfo() {
  const res = await fetch('/api/auth/refdataAdminConnectInfo', {
    headers: { [REQUESTOR_HEADER]: REST_REQUESTOR_ID },
  })
  const body = await res.json().catch(() => ({}))
  if (!res.ok) throw new Error(body.error || `${res.status} ${res.statusText}`)
  return body
}

const state = createConnectionState({
  fetchConnectInfo: fetchRefdataAdminConnectInfo,
  connectionName: 'operator-app',
})

export function useRefdataAdminConnection() {
  return state
}
