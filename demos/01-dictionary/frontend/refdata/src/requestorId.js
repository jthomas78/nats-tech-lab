// One per-tab caller identity for every self-declared `Nats-Requestor` this
// app emits — the header on `api.*`/`rpc.*` requests (`nats/`) and, since
// BR-AC35, the same header on REST calls (`api.js`). Both transports get
// their identity from here so one tab's traffic is attributable as one actor
// across both, rather than a REST span and an `api.*` span from the same
// click looking like two unrelated callers.
//
// Format is BR-027/BR-D37's instance-qualified `"<name>/<instance ID>"` —
// the service.name / service.instance.id split OpenTelemetry's resource
// conventions use. The instance half is generated once per module load, i.e.
// per browser tab, so two tabs of this app stay distinguishable where a bare
// app name never could be. Tenant is deliberately absent: that's the NATS
// account boundary, already visible server-side.
//
// BR-041: this value is self-declared and carried for observability only. No
// handler in any service may branch on it.

import { newInstanceID } from '@identity/instanceId.js'

export const REQUESTOR_HEADER = 'Nats-Requestor'

// TAB_ID is the instance half, shared by every identity below.
export const TAB_ID = newInstanceID()

// requestorID qualifies TAB_ID with a caller name — a connection name for a
// NATS connection ('refdata-app-tenant'), the app's own name for REST, which has
// no connection to name.
export function requestorID(name) {
  return `${name}/${TAB_ID}`
}

// REST_REQUESTOR_ID is the value api.js stamps on every fetch. The app name
// rather than a connection name: a REST call belongs to the tab, not to
// either of its NATS connections.
export const REST_REQUESTOR_ID = requestorID('refdata-app')
