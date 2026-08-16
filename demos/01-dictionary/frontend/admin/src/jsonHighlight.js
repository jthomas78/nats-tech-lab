// Pretty-printed, syntax-tinted JSON for a detail pane — shared by RpcPanel
// (request/reply bodies) and TraceWaterfall (span payload/body, Phase 28g).
// Escapes HTML first, then tints via regex over the already-escaped string —
// the standard safe pattern: tinting never introduces raw user content into
// the DOM, only wraps already-escaped text in <span>.

export function escapeHtml(str) {
  return str.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
}

/**
 * Renders value as indented JSON with `<span class="jk|js|jn|jp">` tints for
 * keys, strings, numbers, and true/false/null respectively — the caller
 * supplies the CSS for those classes (RpcPanel.vue and TraceWaterfall.vue
 * both style `.json :deep(.jk)` etc. identically). Returns '' for
 * null/undefined so callers can fall back to a placeholder ('—') themselves.
 */
export function highlightJson(value) {
  if (value === null || value === undefined) return ''
  // A truncated payload (natstrace's 4 KiB cap) arrives here already as JSON
  // *text* — the server re-encodes the cut bytes as a JSON string rather
  // than leaving invalid mid-object syntax inline (see natstrace.go's
  // finish()) — so `value` is a plain JS string once the outer envelope is
  // parsed. Running it through JSON.stringify again would re-quote it and
  // backslash-escape every embedded ", producing visibly double-encoded
  // output. Only a non-string value needs stringifying; a string is shown
  // as-is.
  const json = typeof value === 'string' ? escapeHtml(value) : escapeHtml(JSON.stringify(value, null, 2))
  return json.replace(
    /("(\\u[a-zA-Z0-9]{4}|\\[^u]|[^\\"])*"(\s*:)?|\b(true|false|null)\b|-?\d+(?:\.\d*)?(?:[eE][+-]?\d+)?)/g,
    (match) => {
      let cls = 'jn'
      if (/^"/.test(match)) {
        cls = /:$/.test(match) ? 'jk' : 'js'
      } else if (/true|false|null/.test(match)) {
        cls = 'jp'
      }
      return `<span class="${cls}">${match}</span>`
    },
  )
}
