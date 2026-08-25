// PLATFORM-account NATS WebSocket connection (Phase 23) — replaces the
// PLATFORM-scoped half of frontend/admin's EventSource/SSE streams (REFDATA
// change notify, RPCTRACE live tail). Connects once at app boot and never
// reconnects on tenant/BU switch — PLATFORM has no tenant lifecycle (see
// accounts-service/auth/token.go's MintAdminToken doc comment) — which is
// exactly why this connection, not the tenant one in useNatsConnection.js,
// drives the topbar's connection indicator: "connected" stops being a side
// effect of which BU/tenant happens to be selected.
//
// Credentials come from GET /api/auth/adminConnectInfo (Phase 23, no tenant
// param) — subscribe-only, publish denied entirely at the JWT level.

import { createConnectionState } from './connectionFactory.js'
import { REQUESTOR_HEADER, REST_REQUESTOR_ID } from '../requestorId.js'

async function fetchAdminConnectInfo() {
  const res = await fetch('/api/auth/adminConnectInfo', {
    headers: { [REQUESTOR_HEADER]: REST_REQUESTOR_ID },
  })
  const body = await res.json().catch(() => ({}))
  if (!res.ok) throw new Error(body.error || `${res.status} ${res.statusText}`)
  return body
}

const state = createConnectionState({
  fetchConnectInfo: fetchAdminConnectInfo,
  connectionName: 'admin-platform',
})

export function usePlatformConnection() {
  return state
}
