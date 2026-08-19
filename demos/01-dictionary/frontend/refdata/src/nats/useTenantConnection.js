// Tenant-account NATS WebSocket connection (Phase 36.2) — this app's SECOND
// connection, added solely for Trading Partners. useRefdataAdminConnection.js
// remains "this app's only connection" for everything else: a single
// cross-tenant PLATFORM credential with no tenant/account concept. Trading
// Partners is the one feature that talks to a service (trading-partner-
// service) which derives tenant identity from *which NATS account the
// connection authenticated as* — mirroring frontend/admin's own
// useNatsConnection.js, minus the backend-reconnect half.
//
// admin's tenant.js store also calls POST /api/tenant/switch on every switch,
// which reconnects shipping-service's own backend-side tenant NATS
// connection — needed there because admin's dictionary store depends on that
// backend connection for ship/container data. Tech Lab Operator has no such
// dependency, so this module intentionally does NOT call that endpoint: a
// tenant switch here only reconnects the browser's own credential. Reusing
// shipping-service's endpoint anyway would reconnect a connection admin
// relies on as a side effect of an unrelated app's UI (2026-08-19 design
// decision — see phase36_tech_lab_operator_rebrand.md).
//
// Credentials come from GET /api/auth/connectInfo?tenant=... (same
// accounts-service route admin uses; refdata's vite.config.js/nginx.conf
// already proxy /api/auth to accounts-service for the PLATFORM connection).

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
  connectionName: 'refdata-tenant',
})

async function switchTenant(newTenant) {
  tenant.value = newTenant
  await state.connect()
}

export function useTenantConnection() {
  return { ...state, tenant, switchTenant }
}
