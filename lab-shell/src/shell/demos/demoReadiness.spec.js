/* The runtime half of demo readiness (BR-AS79, task 16e).

   The specs that matter here are the ones that hold a LINE rather than a
   behaviour: that `unknown` never becomes "stopped", that a demo's own words
   never reach the screen, that the catalogue's absence is not a fault, and
   that nothing in this layer can tell which plugin source is running. */
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { afterEach, describe, expect, it, vi } from 'vitest'


import {
  AUDIENCE_OPERATOR,
  AUDIENCE_VAR,
  AUDIENCE_VISITOR,
  readAudience,
  resetAudienceForTests,
} from './audience.js'
import { createDemoCatalogueClient, DEMO_CATALOGUE_UNREADABLE } from './demoCatalogueClient.js'
import { DEMO_CATALOGUE_PATH, demoReadinessPath } from './demoCatalogueLocation.js'
import {
  demoDetail,
  demoFailingNames,
  demoHeadline,
  demoRunInstruction,
  demoStatusLabel,
  demoTone,
} from './demoReadinessText.js'
import { createDemoStore } from './demoStore.js'
import { DEMO_STATE, PROBE_CAUSE, probeDemo } from './readinessProbe.js'

/* Read as TEXT, not imported, with the comments removed. The claims below are
   about what the CODE may mention at all, and an import would only show what
   a module exports — but the comments explain these very rules by name, so
   matching them would fail on the prose that documents the rule. */
const sourceOf = (file) => readFileSync(join(import.meta.dirname, file), 'utf8')
  .replace(/\/\*[\s\S]*?\*\//g, '')
  .replace(/^\s*\/\/.*$/gm, '')

const entry = {
  demo: '04-jetstream-cqrs',
  pluginId: 'demo-04',
  name: 'Odometer',
  readiness: { url: '/demo-readiness/04-jetstream-cqrs', timeoutMs: 50 },
  runCommand: 'docker compose up -d',
}

/** A fetch that answers one JSON body. */
const answering = (body, { status = 200 } = {}) => vi.fn().mockResolvedValue({
  ok: status >= 200 && status < 300,
  status,
  json: async () => body,
})

const readiness = { url: '/demo-readiness/d', timeoutMs: 50 }

afterEach(() => {
  resetAudienceForTests()
  vi.useRealTimers()
})

describe('the declared audience (F-5)', () => {
  it('defaults to visitor when the deployment declares nothing', () => {
    expect(readAudience({})).toBe(AUDIENCE_VISITOR)
    expect(readAudience({ [AUDIENCE_VAR]: '' })).toBe(AUDIENCE_VISITOR)
  })

  it('takes the declared value', () => {
    expect(readAudience({ [AUDIENCE_VAR]: 'operator' })).toBe(AUDIENCE_OPERATOR)
  })

  /* A typo that silently picks the other audience is the one outcome the rule
     exists to prevent. */
  it('fails boot on an unknown value rather than falling back', () => {
    expect(() => readAudience({ [AUDIENCE_VAR]: 'Operator' })).toThrow(AUDIENCE_VAR)
  })

  /* Dev, preview and build can each be shown to either audience, and the
     words must not change because somebody ran a different npm script. */
  it('is never inferred from the build mode', () => {
    expect(sourceOf('audience.js')).not.toContain('import.meta.env.DEV')
  })
})

describe('the demo catalogue client', () => {
  it('reads the shell\'s own static file, uncached', async () => {
    const fetch = answering({ revision: 'r1', demos: [entry] })
    await createDemoCatalogueClient({ fetch }).fetchDemoCatalogue()
    expect(fetch).toHaveBeenCalledWith(DEMO_CATALOGUE_PATH, { cache: 'no-store' })
  })

  /* The plugin catalogue's absence is a fault, because it IS the shell's
     plugins. This one is decoration: absent, every demo is `unknown` and
     every plugin still mounts. Nothing for a reader to fix. */
  it('treats a missing catalogue as an empty catalogue, not a fault', async () => {
    for (const fetch of [
      vi.fn().mockRejectedValue(new Error('offline')),
      answering(null, { status: 404 }),
    ]) {
      const answer = await createDemoCatalogueClient({ fetch }).fetchDemoCatalogue()
      expect(answer).toEqual({ ok: true, revision: null, demos: [] })
    }
  })

  it('reports a catalogue that is there and will not parse', async () => {
    const fetch = vi.fn().mockResolvedValue({
      ok: true, status: 200, json: async () => { throw new Error('bad') },
    })
    const answer = await createDemoCatalogueClient({ fetch }).fetchDemoCatalogue()
    expect(answer.ok).toBe(false)
    expect(answer.code).toBe(DEMO_CATALOGUE_UNREADABLE)
    expect(answer.demos).toEqual([])
  })

  it('drops an entry that lacks what a probe needs', async () => {
    const fetch = answering({ demos: [{ demo: 'a' }, { demo: 'b', pluginId: 'b' }, entry] })
    const { demos } = await createDemoCatalogueClient({ fetch }).fetchDemoCatalogue()
    expect(demos.map((d) => d.pluginId)).toEqual(['demo-04'])
  })

  /* F-3: the probe is same-origin. A catalogue that somehow named another
     host is dropped rather than followed. */
  it('refuses a readiness url that is not a path on this origin', async () => {
    const fetch = answering({
      demos: [{ ...entry, readiness: { url: 'http://demo04:20402/readyz', timeoutMs: 100 } }],
    })
    const { demos } = await createDemoCatalogueClient({ fetch }).fetchDemoCatalogue()
    expect(demos).toEqual([])
  })
})

describe('the readiness probe', () => {
  it('says available only when the demo asserted it', async () => {
    const fetch = answering({ ready: true, checks: [] })
    const result = await probeDemo({ readiness, fetch })
    expect(result.state).toBe(DEMO_STATE.AVAILABLE)
  })

  /* A process that is up with no stream and no buckets serves the endpoint
     perfectly and is not ready. Readiness is asserted, not reachability. */
  it('is not satisfied by the demo merely answering', async () => {
    const fetch = answering({ ready: false, checks: [{ name: 'stream ODOMETER', ok: false, code: 'missing' }] }, { status: 503 })
    const result = await probeDemo({ readiness, fetch })
    expect(result.state).toBe(DEMO_STATE.UNAVAILABLE)
    expect(result.failing).toEqual([{ name: 'stream ODOMETER', code: 'missing' }])
  })

  it('reads a 200 with no verdict as unknown, never as yes', async () => {
    const result = await probeDemo({ readiness, fetch: answering({ hello: true }) })
    expect(result.state).toBe(DEMO_STATE.UNKNOWN)
  })

  it('reads a refused connection as unknown, not as unavailable', async () => {
    const fetch = vi.fn().mockRejectedValue(new Error('ECONNREFUSED'))
    const result = await probeDemo({ readiness, fetch })
    expect(result.state).toBe(DEMO_STATE.UNKNOWN)
    expect(result.cause).toBe(PROBE_CAUSE.UNREACHABLE)
  })

  it('reads a timeout as unknown, and says so', async () => {
    const fetch = vi.fn((url, init) => new Promise((_resolve, reject) => {
      init.signal?.addEventListener('abort', () => reject(new Error('aborted')))
    }))
    const result = await probeDemo({ readiness: { url: '/x', timeoutMs: 10 }, fetch })
    expect(result.state).toBe(DEMO_STATE.UNKNOWN)
    expect(result.cause).toBe(PROBE_CAUSE.TIMEOUT)
  })

  /* Both environments close the readiness prefix with a 404 precisely so this
     case is distinguishable from the SPA answering 200 with a page of HTML. */
  it('reads a 404 as a missing route, not as a stopped demo', async () => {
    const result = await probeDemo({ readiness, fetch: answering(null, { status: 404 }) })
    expect(result.state).toBe(DEMO_STATE.UNKNOWN)
    expect(result.cause).toBe(PROBE_CAUSE.NO_ROUTE)
  })

  it('reduces an unrecognised check code to one the shell knows', async () => {
    const fetch = answering({ ready: false, checks: [{ name: 'x', ok: false, code: 'on fire' }] }, { status: 503 })
    const { failing } = await probeDemo({ readiness, fetch })
    expect(failing).toEqual([{ name: 'x', code: 'unreachable' }])
  })
})

describe('how a demo state is said', () => {
  /* The one wording rule in the whole task. */
  it('never says a demo is stopped when it could not be reached', () => {
    for (const cause of Object.values(PROBE_CAUSE)) {
      const result = { state: DEMO_STATE.UNKNOWN, cause, failing: [] }
      expect(demoHeadline(result)).toBe('Cannot reach demo services.')
      expect(demoHeadline(result)).not.toMatch(/stopped|off|down/i)
      expect(demoDetail(result)).toMatch(/could not tell/)
    }
  })

  it('is specific when the demo answered, because the demo told us', () => {
    const result = {
      state: DEMO_STATE.UNAVAILABLE,
      failing: [{ name: 'kv odometer-read', code: 'missing' }],
    }
    expect(demoHeadline(result)).toBe('This demo is not ready.')
    expect(demoFailingNames(result)).toEqual(['kv odometer-read'])
  })

  it('gives an unchecked demo the quiet unknown state', () => {
    expect(demoStatusLabel(undefined)).toBe('Unknown')
    expect(demoTone(undefined)).toBe('off')
  })

  it('shows no run instruction to a visitor deployment', () => {
    const result = { state: DEMO_STATE.UNAVAILABLE, failing: [] }
    expect(demoRunInstruction(result, { runCommand: 'x', operator: false })).toBeNull()
  })

  it('offers the run command to an operator, including when it could not tell', () => {
    for (const state of [DEMO_STATE.UNAVAILABLE, DEMO_STATE.UNKNOWN]) {
      expect(demoRunInstruction({ state, failing: [] }, { runCommand: 'x', operator: true })).toBe('x')
    }
    expect(demoRunInstruction({ state: DEMO_STATE.AVAILABLE }, { runCommand: 'x', operator: true })).toBeNull()
  })
})

describe('the demo store', () => {
  const storeWith = (result, catalogue = [entry]) => {
    const probe = vi.fn().mockResolvedValue(result)
    const client = { fetchDemoCatalogue: vi.fn().mockResolvedValue({ ok: true, revision: 'r', demos: catalogue }) }
    return { probe, client, store: createDemoStore({ client, probe, fetch: vi.fn() }) }
  }
  const ready = { state: DEMO_STATE.AVAILABLE, cause: null, failing: [], checkedAt: 't' }

  it('keys demos by plugin, because a route knows its plugin', async () => {
    const { store } = storeWith(ready)
    await store.load()
    expect(store.entryFor('demo-04')?.demo).toBe('04-jetstream-cqrs')
    expect(store.entryFor('something-else')).toBeNull()
  })

  /* The registry plugin with no lab demo behind it: mounts normally, reports
     no fault. */
  it('lets a plugin with no associated demo through unchecked', async () => {
    const { store, probe } = storeWith(ready, [])
    const result = await store.checkBeforeMount('demo-04')
    expect(result.state).toBe(DEMO_STATE.AVAILABLE)
    expect(probe).not.toHaveBeenCalled()
  })

  it('probes on a pre-mount check, so an opened demo is checked fresh', async () => {
    const { store, probe } = storeWith(ready)
    await store.checkBeforeMount('demo-04')
    expect(probe).toHaveBeenCalledTimes(1)
  })

  /* Not an optimisation: it is what stops the pre-mount check and the poll
     from racing to write two different answers. */
  it('shares one request between callers asking at once', async () => {
    const { store, probe } = storeWith(ready)
    await store.load()
    await Promise.all([store.refresh('demo-04'), store.refresh('demo-04')])
    expect(probe).toHaveBeenCalledTimes(1)
  })

  it('populates every card without anybody opening a demo', async () => {
    const setTimer = vi.fn().mockReturnValue(1)
    const probe = vi.fn().mockResolvedValue(ready)
    const client = { fetchDemoCatalogue: vi.fn().mockResolvedValue({ ok: true, revision: 'r', demos: [entry] }) }
    const store = createDemoStore({ client, probe, fetch: vi.fn(), setInterval: setTimer, clearInterval: vi.fn() })
    await store.startMenuRefresh()
    expect(store.resultFor('demo-04')).toEqual(ready)
    expect(setTimer).toHaveBeenCalledTimes(1)
  })

  it('does not start a second timer when asked twice', async () => {
    const setTimer = vi.fn().mockReturnValue(1)
    const probe = vi.fn().mockResolvedValue(ready)
    const client = { fetchDemoCatalogue: vi.fn().mockResolvedValue({ ok: true, revision: 'r', demos: [entry] }) }
    const store = createDemoStore({ client, probe, fetch: vi.fn(), setInterval: setTimer, clearInterval: vi.fn() })
    await store.startMenuRefresh()
    await store.startMenuRefresh()
    expect(setTimer).toHaveBeenCalledTimes(1)
  })
})

/* R-1, stated as a property of the code rather than of a run: nothing in this
   layer can tell which plugin source is running, so the panel, its states, its
   retry and the card status cannot differ between them. */
describe('the readiness layer and the plugin source', () => {
  it('builds the same route for a demo whichever source found its plugin', () => {
    expect(demoReadinessPath('04-jetstream-cqrs')).toBe('/demo-readiness/04-jetstream-cqrs')
  })

  it('names the plugin source nowhere in the layer', () => {
    const files = [
      'demoStore.js', 'readinessProbe.js', 'demoReadinessText.js',
      'demoCatalogueClient.js', 'demoGate.js', 'demoCatalogueLocation.js',
    ]
    for (const file of files) {
      expect(sourceOf(file), file).not.toMatch(/pluginSource|PLUGIN_SOURCE/)
    }
  })
})

/* One ordering rule inside the probe, found in the browser rather than here:
   a proxy whose upstream refused the connection answers 500 with a line of
   plain text, and reading the body first blamed the DEMO for a sentence the
   demo never sent. */
describe('a refusal between the shell and the demo', () => {
  const answering = (status, body) => ({
    ok: status >= 200 && status < 300,
    status,
    json: async () => {
      if (body === undefined) throw new SyntaxError('not json')
      return body
    },
  })

  it('reads a proxy error as unreachable, not as an unreadable answer', async () => {
    const result = await probeDemo({
      readiness: { url: '/demo-readiness/x', timeoutMs: 50 },
      fetch: async () => answering(500),
    })
    expect(result.state).toBe(DEMO_STATE.UNKNOWN)
    expect(result.cause).toBe(PROBE_CAUSE.UNREACHABLE)
  })

  /* 503 is still the demo's own voice, and its body is still read. */
  it('still reads the demo\'s own 503 as a verdict', async () => {
    const result = await probeDemo({
      readiness: { url: '/demo-readiness/x', timeoutMs: 50 },
      fetch: async () => answering(503, { ready: false, checks: [{ name: 'stream ODOMETER', ok: false, code: 'missing' }] }),
    })
    expect(result.state).toBe(DEMO_STATE.UNAVAILABLE)
    expect(result.failing).toEqual([{ name: 'stream ODOMETER', code: 'missing' }])
  })
})
