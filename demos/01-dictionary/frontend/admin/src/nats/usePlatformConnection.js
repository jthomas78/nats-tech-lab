// The Admin UI's single browser NATS WebSocket connection. It authenticates
// into PLATFORM, receives centralized observability/refdata notifications,
// and issues only the exact read-only refdata api.* requests MintAdminToken
// allowlists. It connects once at app boot and has no tenant lifecycle.
//
// Credentials come from GET /api/auth/adminConnectInfo (no tenant parameter).

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
  connectionName: 'admin-app',
})

export function usePlatformConnection() {
  return state
}
