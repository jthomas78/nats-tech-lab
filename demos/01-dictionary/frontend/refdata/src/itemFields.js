// Shared field accessors for list-API item shapes. `listItems`/`getItem` return
// items either flat (no locale requested) or wrapped as `{ item, label }` (a
// locale was resolved) — every consumer needs to read through both shapes, so
// the accessors live here once instead of being redefined per component.
export function codeFor(item) {
  return item.code || item.item?.code
}

export function statusFor(item) {
  return item.status || item.item?.status || 'active'
}

export function attrsFor(item) {
  return item.item?.attrs ?? item.attrs ?? {}
}

export function labelFor(item) {
  return item.label || attrsFor(item).name || codeFor(item)
}
