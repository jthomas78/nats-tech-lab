// resolveWsUrl turns accounts-service's connectInfo.wsUrl into an address
// this browser can actually dial.
//
// accounts-service returns NATS_WS_URL verbatim (auth/token.go) — it has no
// way to know the origin the page was served from, so anything absolute it
// could name is a guess. Before Phase 45 that guess was the literal
// `ws://localhost:9222`, which is correct only when the stack and the
// browser are on the same machine. On any remote deployment it fails twice
// over: `localhost` resolves to the *viewer's* machine rather than the
// Docker host, and a `ws://` dial from an `https://` page is blocked as
// mixed content before it leaves the browser.
//
// So the deployed value is now the path `/nats`, which each frontend's
// nginx (and Vite in dev) proxies to the NATS server's WebSocket listener.
// The browser is the only party that knows its own origin, so resolving
// that path is its job — hence this lives here and not in Go.
//
// An absolute ws:// or wss:// value is still honoured unchanged, so a
// deployment that really does want a dedicated NATS hostname can set one.
export function resolveWsUrl(wsUrl, location = globalThis.location) {
  if (!wsUrl) throw new Error('connectInfo.wsUrl is empty')
  if (/^wss?:\/\//i.test(wsUrl)) return wsUrl
  if (!wsUrl.startsWith('/')) {
    throw new Error(
      `connectInfo.wsUrl must be ws://, wss://, or a same-origin path starting with "/" — got "${wsUrl}"`,
    )
  }
  if (!location?.host) throw new Error('cannot resolve a same-origin wsUrl outside a browser')
  // Match the page's own scheme: an https:// page must dial wss://, or the
  // browser blocks the connection as mixed content.
  const scheme = location.protocol === 'https:' ? 'wss:' : 'ws:'
  return `${scheme}//${location.host}${wsUrl}`
}
