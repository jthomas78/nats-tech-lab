// newInstanceID mints the instance half of BR-027/BR-D37's instance-qualified
// requestor identity `"<name>/<instance ID>"` — 16 lowercase hex characters,
// i.e. 64 random bits.
//
// Deliberately NOT crypto.randomUUID(). That method is gated on the page being
// a *secure context* — https://, localhost/127.0.0.1, or file:// — and is
// simply undefined anywhere else, so a build served over plain http:// from a
// LAN address (http://10.10.10.5:7100, the Docker host's own IP) threw
// `crypto.randomUUID is not a function` at module load and took the whole app
// down with it. crypto.getRandomValues() carries no such restriction and is
// available in every context, which makes this work identically on localhost,
// on a LAN IP, and behind a remote TLS proxy — the same origin-portability
// property Phase 45 gave the NATS WebSocket URL (see ../nats/resolveWsUrl.js).
//
// The UUID was being discarded almost entirely anyway: each caller stripped
// the dashes and kept the first 16 hex characters, so eight random bytes is
// the same 64 bits by a shorter route, with no version/variant bits spent.
//
// Not Math.random(): this is a collision domain shared by every tab of every
// app in the lab, and a non-cryptographic PRNG is the usual way that quietly
// stops being true.
export function newInstanceID() {
  const bytes = new Uint8Array(8)
  crypto.getRandomValues(bytes)
  return Array.from(bytes, (b) => b.toString(16).padStart(2, '0')).join('')
}
