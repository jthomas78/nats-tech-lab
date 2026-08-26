// One per-browser caller identity for every self-declared `Nats-Requestor` this
// app emits — the header on `api.*`/`rpc.*` requests (`nats/`) and, since
// BR-AC35, the same header on REST calls (`api.js`). Both transports get
// their identity from here so one browser's traffic is attributable as one actor
// across both, rather than a REST span and an `api.*` span from the same
// click looking like two unrelated callers.
//
// Format is BR-027/BR-D37's instance-qualified `"<name>/<instance ID>"` —
// the service.name / service.instance.id split OpenTelemetry's resource
// conventions use. The instance half is persisted in localStorage so it
// survives page refreshes and is shared by tabs in the same browser profile.
// Tenant is deliberately absent: that's the NATS account boundary, already
// visible server-side.
//
// BR-041: this value is self-declared and carried for observability only. No
// handler in any service may branch on it.
export const REQUESTOR_HEADER = 'Nats-Requestor'

export const BROWSER_ID_STORAGE_KEY = 'seafreight-app.browserInstanceId'

function newBrowserID() {
  return crypto.randomUUID().replaceAll('-', '').slice(0, 16)
}

function loadOrCreateBrowserID() {
  try {
    const stored = localStorage.getItem(BROWSER_ID_STORAGE_KEY)
    if (/^[0-9a-f]{16}$/.test(stored ?? '')) return stored

    const id = newBrowserID()
    localStorage.setItem(BROWSER_ID_STORAGE_KEY, id)
    return id
  } catch {
    // Storage may be disabled by browser privacy settings. Requests still need
    // a valid observability identity even though it cannot survive a refresh.
    return newBrowserID()
  }
}

// BROWSER_ID is the instance half, shared by every identity below.
export const BROWSER_ID = loadOrCreateBrowserID()

// requestorID qualifies BROWSER_ID with a caller name — a connection name for a
// NATS connection ('seafreight-app-tenant'), the app's own name for REST, which has
// no connection to name.
export function requestorID(name) {
  return `${name}/${BROWSER_ID}`
}

// REST_REQUESTOR_ID is the value api.js stamps on every fetch. The app name
// rather than a connection name: a REST call belongs to the browser actor,
// not to either of its NATS connections.
export const REST_REQUESTOR_ID = requestorID('seafreight-app')
