import { afterEach, describe, expect, it } from 'vitest'

import { newInstanceID } from '@identity/instanceId.js'

// The instance half of BR-027/BR-D37's `"<name>/<instance ID>"` requestor
// identity. The spec lives in the admin app because shared/identity/ has no
// runner of its own (same arrangement as resolveWsUrl.spec.js for
// shared/nats/, and AppShell.spec.js for shared/ui-shell/).

// withoutRandomUUID simulates an insecure context — a build served over plain
// http:// from anything other than localhost, where the browser leaves
// crypto.randomUUID undefined. That is the regression this module exists to
// prevent, so it is worth reproducing rather than trusting by inspection.
const saved = Object.getOwnPropertyDescriptor(globalThis.crypto, 'randomUUID')

function withoutRandomUUID(fn) {
  Object.defineProperty(globalThis.crypto, 'randomUUID', {
    value: undefined,
    configurable: true,
  })
  return fn()
}

afterEach(() => {
  if (saved) Object.defineProperty(globalThis.crypto, 'randomUUID', saved)
  else delete globalThis.crypto.randomUUID
})

describe('newInstanceID', () => {
  it('returns 16 lowercase hex characters', () => {
    // The exact shape seafreight-app's requestorId.js validates a persisted
    // value against before reusing it — a mismatch there would silently mint a
    // fresh identity on every page load.
    expect(newInstanceID()).toMatch(/^[0-9a-f]{16}$/)
  })

  it('returns a different value on each call', () => {
    const ids = new Set(Array.from({ length: 200 }, () => newInstanceID()))
    expect(ids.size).toBe(200)
  })

  it('works in an insecure context, where crypto.randomUUID is undefined', () => {
    withoutRandomUUID(() => {
      expect(globalThis.crypto.randomUUID).toBeUndefined()
      expect(newInstanceID()).toMatch(/^[0-9a-f]{16}$/)
    })
  })
})
