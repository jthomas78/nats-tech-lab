// BR-AC35 grep audit: every REST call this app makes must declare the tab's
// Nats-Requestor identity, so accounts-service's / shipping-service's HTTP
// trace middleware can name the caller of a REST hop.
//
// This is a source-level audit rather than a behavioural test because the
// gap it guards is a *missed call site*, not a broken one: the credential
// fetches (`/api/auth/connectInfo` and friends) live in the connection
// modules rather than api.js's shared request() helper — they have to work
// before any connection exists — so they were invisible to a test that only
// exercised api.js, and shipped without the header. A new fetch() added
// anywhere in this app fails here until it either carries the header or is
// listed as a deliberate exemption below.
import { readFileSync, readdirSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

import { describe, expect, it } from 'vitest'

const SRC = join(dirname(fileURLToPath(import.meta.url)))

// Exempt call sites, by a substring of the fetched URL. Only for requests
// that reach no natstrace-instrumented HTTP entry point at all — a static
// asset served by nginx/vite, or a service with no HTTP trace middleware,
// where the header would be carried and then dropped on the floor.
const EXEMPT = [
  '/geo/', // static asset served by nginx/vite, not a traced service
  'DOCUMENT_FILE_URL', // organizations-service — no HTTP trace middleware, so no span
]

function jsFiles(dir) {
  return readdirSync(dir, { withFileTypes: true }).flatMap((e) => {
    const path = join(dir, e.name)
    if (e.isDirectory()) return e.name === 'node_modules' ? [] : jsFiles(path)
    if (!/\.(js|vue)$/.test(e.name) || e.name.endsWith('.spec.js')) return []
    return [path]
  })
}

describe('REST caller identity — call-site audit (BR-AC35)', () => {
  it('stamps Nats-Requestor on every fetch() in this app', () => {
    const offenders = []
    for (const file of jsFiles(SRC)) {
      const source = readFileSync(file, 'utf8')
      for (const match of source.matchAll(/fetch\(/g)) {
        // The window covers the whole call: url argument, options object,
        // and the header spread onto it.
        const call = source.slice(match.index, match.index + 400)
        // Skip a `fetch()` written inside a comment (prose about a call that
        // no longer exists, say) — it makes no request.
        const lineStart = source.lastIndexOf('\n', match.index) + 1
        const before = source.slice(lineStart, match.index).trimStart()
        if (before.startsWith('//') || before.startsWith('*')) continue
        if (EXEMPT.some((url) => call.includes(url))) continue
        if (call.includes('REQUESTOR_HEADER')) continue
        offenders.push(`${file.slice(SRC.length + 1)}: ${call.split('\n')[0].trim()}`)
      }
    }
    expect(offenders).toEqual([])
  })
})
