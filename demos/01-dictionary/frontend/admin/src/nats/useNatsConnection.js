// Tenant-account NATS WebSocket connection (Phase 23) — replaces the tenant-
// scoped half of frontend/admin's EventSource/SSE streams (dictionary watch,
// KV inspector, JetStream raw watch, obs.api.* live tail). Reconnects on
// every tenant switch, same as seafreight-app's own useNatsConnection.js —
// this is the SECOND of Phase 23's two browser connections; see
// usePlatformConnection.js for the PLATFORM-account one, which never
// reconnects.
//
// Credentials come from GET /api/auth/connectInfo?tenant=... (unchanged from
// Phase 15c). Originally subscribe-only here; Phase 26h's Trading Partners
// screens and Phase 32's refdata label/UI-copy reads both use its api.>
// publish permission too.

import { ref } from 'vue'
import { createConnectionState } from './connectionFactory.js'

async function fetchConnectInfo(forTenant) {
  const res = await fetch(`/api/auth/connectInfo?tenant=${encodeURIComponent(forTenant)}`)
  const body = await res.json().catch(() => ({}))
  if (!res.ok) throw new Error(body.error || `${res.status} ${res.statusText}`)
  return body
}

const tenant = ref('')
const state = createConnectionState({
  fetchConnectInfo: () => fetchConnectInfo(tenant.value),
  connectionName: 'admin-tenant',
})

// switchTenant sets which tenant the next (re)connect authenticates into,
// then reconnects — call this whenever the topbar tenant Select changes,
// alongside (not instead of) the existing POST /api/tenant/switch call that
// reconnects the backend's own tenant connection.
async function switchTenant(newTenant) {
  tenant.value = newTenant
  await state.connect()
}

export function useNatsConnection() {
  return { ...state, tenant, switchTenant }
}
