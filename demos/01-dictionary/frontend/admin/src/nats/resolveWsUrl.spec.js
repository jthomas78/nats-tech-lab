import { describe, expect, it } from 'vitest'

import { resolveWsUrl } from '@nats-shared/resolveWsUrl.js'

// Phase 45 — accounts-service returns connectInfo.wsUrl verbatim, so this is
// the only place the deployed value is turned into a dialable address. The
// spec lives in the admin app because shared/nats/ has no runner of its own
// (same arrangement as AppShell.spec.js for shared/ui-shell/).

const at = (href) => new URL(href)

describe('resolveWsUrl', () => {
  it('resolves a same-origin path against the page origin', () => {
    expect(resolveWsUrl('/nats', at('http://localhost:7100/'))).toBe('ws://localhost:7100/nats')
  })

  it('dials wss:// from an https:// page', () => {
    // The bug that motivated all of this: a ws:// dial from an https:// page
    // is blocked as mixed content before it ever leaves the browser.
    expect(resolveWsUrl('/nats', at('https://lb-admin.xacthomelab.net/'))).toBe(
      'wss://lb-admin.xacthomelab.net/nats',
    )
  })

  it('keeps a non-default port in the resolved host', () => {
    expect(resolveWsUrl('/nats', at('https://example.test:8443/admin'))).toBe(
      'wss://example.test:8443/nats',
    )
  })

  it('passes an absolute ws:// or wss:// value through unchanged', () => {
    expect(resolveWsUrl('ws://localhost:9222', at('http://localhost:7100/'))).toBe(
      'ws://localhost:9222',
    )
    expect(resolveWsUrl('wss://nats.example.test', at('https://app.example.test/'))).toBe(
      'wss://nats.example.test',
    )
  })

  it('rejects a value that is neither absolute nor a path', () => {
    expect(() => resolveWsUrl('localhost:9222', at('https://example.test/'))).toThrow(/must be ws/)
    expect(() => resolveWsUrl('', at('https://example.test/'))).toThrow(/empty/)
  })
})
