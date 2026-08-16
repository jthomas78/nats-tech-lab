---
name: vue-todisplaystring-array-gotcha
description: Vue's {{ }} interpolation JSON.stringifies arrays/objects instead of toString — bit us rendering a NATS header value (string[]) directly
metadata:
  type: feedback
---

Vue 3's text interpolation (`{{ value }}`) calls `toDisplayString`, which
`JSON.stringify`s any non-primitive value (arrays, plain objects) rather than
using `.toString()`/string coercion. A computed or binding that forwards a
raw array straight into a template renders as `[ "foo" ]`, not `foo`.

**Why this bit us:** NATS headers are `map[string][]string` on the wire —
after `JSON.parse`, a header value in the frontend is a JS array even when
it only ever has one element (e.g. `Nats-Requestor: ["shipping-service/..."]`).
A new computed (`requestedBy`/`respondedBy` in `TraceWaterfall.vue`, added
during the Phase 28 detail-pane redesign — see
[[phase28_trace_detail_request_response_split]]) returned that array
straight from `selectedSpan.headers['Nats-Requestor']` and bound it directly
into `{{ }}` — rendered as `[ "shipping-service/mcMyd0n54DnUjKsyLFl6z2" ]`
instead of the plain string. Caught only by reading live rendered text
(`get_page_text`/`.innerText`) against real backend data, not by a screenshot
glance.

**How to apply:** any time a value crossing from Go's `map[string][]string`
(or any array-valued API field) gets bound directly into a Vue template
(not passed through a `v-for` or explicit `.join()`), join/coerce it to a
plain string first. This codebase already has the right pattern in
`headerRows()`/`splitRows()` (`Array.isArray(v) ? v.join(', ') : String(v)`)
— reuse that helper (or an equivalent) for every new single-value binding
that touches a header/array field, not just the multi-row list renderers.
