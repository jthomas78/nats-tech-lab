import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { PLUGIN_SOURCE_VAR, resetPluginSourceForTests } from '../shell/pluginSource.js'
import { HEALTH_STATE } from '../shell/registry/healthPlane.js'
import { healthAttention } from '../shell/registry/healthText.js'
import { SHELL } from '../shell/shellKey.js'
import PluginsView from './PluginsView.vue'

/*
  BR-AS80 / F-2 — health is reported as measured, or reported as absent.

  `build` mode has no health plane, so there is no measurement to report. The
  substitution is one flag on the injected health object and one mapping at
  this render site; these specs hold that line from the outside, by reading
  the screen rather than the composition. The companion regression — that
  `healthPlane.js` and `healthText.js` are unchanged files — is asserted in
  `healthSourceIndependence.spec.js`.
*/
const row = (id) => ({
  id,
  name: id,
  version: '1.0.0',
  shellApiVersion: 1,
  status: 'available',
  contributionKinds: ['main'],
  refusals: [],
  clashes: [],
})

const mountView = (health) => mount(PluginsView, {
  global: {
    provide: {
      [SHELL]: {
        inventory: [row('demo-catalog'), row('demo-04')],
        registry: { revision: 'rev-1' },
        health,
      },
    },
  },
})

/* The two health cells of one row, read as the screen shows them. */
const cells = (wrapper) => wrapper.findAll('tbody tr')
  .map((tr) => tr.findAll('td').slice(4, 6).map((td) => ({
    text: td.text(),
    tone: td.find('.pill').classes(),
    title: td.find('.pill').attributes('title'),
  })))

describe('BR-AS80 — health in `build` mode, where nothing is monitored', () => {
  const built = () => mountView({ signals: {}, monitored: false })

  it('says `not configured` for every plugin, on both sides', () => {
    for (const rowCells of cells(built())) {
      for (const cell of rowCells) expect(cell.text).toBe(HEALTH_STATE.NOT_CONFIGURED)
    }
  })

  it('never rests a plugin at `unknown`', () => {
    /* `unknown` means monitoring exists and has no current reading. Nothing
       exists here, so that word would be a claim the shell cannot make. */
    const text = built().text()
    expect(text).not.toContain(HEALTH_STATE.UNKNOWN)
  })

  it('invents no `healthy`, and no other measured state', () => {
    const text = built().text()
    for (const state of [HEALTH_STATE.HEALTHY, HEALTH_STATE.UNAVAILABLE, HEALTH_STATE.STALE]) {
      expect(text).not.toContain(state)
    }
  })

  it('does not age, because it says nothing about when', () => {
    /* A configuration answer is not an observation, so there is no time to
       show and nothing that can grow old. */
    for (const rowCells of cells(built())) {
      for (const cell of rowCells) {
        expect(cell.title).toBe('no monitoring is configured for this deployment')
        expect(cell.title).not.toContain('last checked')
      }
    }
  })

  it('uses the shared quiet tone, so the wording cannot drift', () => {
    for (const rowCells of cells(built())) {
      for (const cell of rowCells) expect(cell.tone).toContain('off')
    }
  })

  it('leaves the nav quiet with no new code', () => {
    /* No signal is synthesised, so the nav reads an empty signal set, and
       `healthAttention` already marks only `unavailable`. */
    expect(healthAttention(undefined)).toBeNull()
    expect(healthAttention({})).toBeNull()
  })
})

describe('BR-AS80 — health in `registry` mode is unchanged', () => {
  const signals = {
    'demo-catalog': {
      frontend: { state: HEALTH_STATE.HEALTHY, lastCheckAt: '2026-09-24T10:00:00Z' },
      backend: { state: HEALTH_STATE.UNAVAILABLE, cause: 'timeout', lastCheckAt: '2026-09-24T10:00:00Z' },
    },
  }
  const monitored = () => mountView({ signals, monitored: true })

  it('still says what was measured', () => {
    const [catalogue] = cells(monitored())
    expect(catalogue[0].text).toBe(HEALTH_STATE.HEALTHY)
    expect(catalogue[1].text).toBe('unavailable (timeout)')
  })

  it('still ages, by naming when the reading was taken', () => {
    const [catalogue] = cells(monitored())
    expect(catalogue[0].title).toMatch(/^last checked /)
  })

  it('still rests an unmeasured plugin at `unknown`, not at `not configured`', () => {
    const [, other] = cells(monitored())
    for (const cell of other) {
      expect(cell.text).toBe(HEALTH_STATE.UNKNOWN)
      expect(cell.title).toBe('never checked')
    }
  })

  it('treats a health object with no flag as monitored', () => {
    /* Only `build` mode sets the flag. An older or partial health object must
       keep the measured semantics rather than silently going quiet. */
    const [catalogue] = cells(mountView({ signals }))
    expect(catalogue[0].text).toBe(HEALTH_STATE.HEALTHY)
  })
})

/*
  BR-AS81 — the active `plugin-source` is stated ONCE, at page level.

  These read the rendered page, not the module, because "once" and "not per
  row" are facts about what an operator sees. The two-row fixture above makes
  the per-row failure visible: a column or a badge would give two matches
  where the rule allows one.
*/
describe('the catalogue source statement', () => {
  afterEach(() => {
    vi.unstubAllEnvs()
    resetPluginSourceForTests()
  })

  const withSource = (value) => {
    vi.stubEnv(PLUGIN_SOURCE_VAR, value)
    resetPluginSourceForTests()
    return mountView({ signals: {}, monitored: value !== 'build' })
  }

  it('names the build source once, at page level', () => {
    const wrapper = withSource('build')
    const said = wrapper.findAll('.source')
    expect(said).toHaveLength(1)
    expect(said[0].text()).toBe('Catalogue source: Build')
    expect(wrapper.find('.page-head').element.contains(said[0].element)).toBe(true)
  })

  it('names the registry source once, in the same place', () => {
    const wrapper = withSource('registry')
    expect(wrapper.findAll('.source')).toHaveLength(1)
    expect(wrapper.find('.source').text()).toBe('Catalogue source: Registry')
  })

  it('never repeats the source in a row', () => {
    const body = withSource('build').find('tbody').text()
    expect(body).not.toMatch(/Catalogue source/u)
    expect(body.toLowerCase()).not.toMatch(/\bbuild\b/u)
  })

  it('words it as a property of this shell, not of a demo', () => {
    const detail = withSource('build').find('.source').attributes('title')
    expect(detail).toMatch(/this shell is configured/u)
  })

  it('does not show the registry\'s own announced / preload labels beside it', () => {
    const head = withSource('registry').find('.page-head').text()
    expect(head).not.toMatch(/announced|preload/u)
  })
})

/*
  16h — the display distinction, checked from the other side.

  Decision 9 lets shared UI tell "nothing is tracked" apart from "no reading
  yet", using existing health data only. The `build`-mode half of that is
  asserted above. This is the half that could rot quietly: `registry` mode can
  itself report `not configured`, for a plugin whose backend is not mapped,
  and the substitution above must neither swallow that nor manufacture it.
*/
describe('16h — `not configured` from the plane, not from the mapping', () => {
  const signals = {
    'demo-catalog': {
      frontend: { state: HEALTH_STATE.HEALTHY, lastCheckAt: '2026-09-24T10:00:00Z' },
      backend: { state: HEALTH_STATE.NOT_CONFIGURED },
    },
  }

  it('relays it, and does not round it down to `unknown`', () => {
    const [catalogue] = cells(mountView({ signals, monitored: true }))
    expect(catalogue[1].text).toBe(HEALTH_STATE.NOT_CONFIGURED)
  })

  it('says it with the same word and the same quiet tone as `build` mode', () => {
    /* One constant, one tone table. If these ever differ, an operator is
       reading two vocabularies and does not know it. */
    const [catalogue] = cells(mountView({ signals, monitored: true }))
    const [built] = cells(mountView({ signals: {}, monitored: false }))
    expect(catalogue[1].text).toBe(built[1].text)
    expect(catalogue[1].tone).toEqual(built[1].tone)
  })

  it('still keeps the measured side measured beside it', () => {
    /* The two cells are independent (BR-AS60). A backend nobody watches must
       not quiet the frontend reading in the same row. */
    const [catalogue] = cells(mountView({ signals, monitored: true }))
    expect(catalogue[0].text).toBe(HEALTH_STATE.HEALTHY)
    expect(catalogue[0].title).toMatch(/^last checked /)
  })
})

/*
  BR-AS87 — clashes are shown BESIDE refusals, never inside them.

  The two lists answer different questions: a refusal says what the shell did
  not place, a clash says what it placed twice. Merging them on this screen is
  exactly what D17-5 withdrew, so these specs read the rendered lists apart.
*/
describe('BR-AS87 — the clash channel on the Plugins screen', () => {
  const clash = {
    kind: 'group-label-conflict',
    groupId: 'ops',
    groupIds: ['ops'],
    label: 'Alpha Ops',
    pluginIds: ['alpha', 'zulu'],
    participants: [],
    message: 'Navigation group ops is shown as "Alpha Ops" from alpha; zulu asked for "Zulu Ops"',
  }
  const refusal = { qualifiedId: 'alpha/foot', code: 'region-full', pluginId: 'alpha' }

  const mountWith = (overrides) => mount(PluginsView, {
    global: {
      provide: {
        [SHELL]: {
          inventory: [{ ...row('alpha'), ...overrides }],
          registry: { revision: 'rev-1' },
          health: { monitored: false },
        },
      },
    },
  })

  it('says the kind and the message of each clash', () => {
    const items = mountWith({ clashes: [clash] }).findAll('ul.clashes li')

    expect(items).toHaveLength(1)
    expect(items[0].text()).toContain('group-label-conflict')
    expect(items[0].text()).toContain('zulu asked for "Zulu Ops"')
  })

  it('draws no clash list when a plugin has none', () => {
    expect(mountWith({ refusals: [refusal] }).findAll('ul.clashes')).toHaveLength(0)
  })

  /*
    Phase 17's acceptance check, screen half: a nav entry whose route was
    refused leaves a VISIBLE diagnostic. The rail half — that no dead link
    survives — is held in `shellNavSections.spec.js`. Together they say the
    entry went somewhere a reader can find, not nowhere (task 17i).
  */
  it('shows a nav entry whose route was never placed, by name and by cause', () => {
    const dangling = { qualifiedId: 'alpha/broken', code: 'unresolved-route', pluginId: 'alpha' }
    const view = mountWith({ refusals: [dangling] })
    const items = view.findAll('ul:not(.clashes) li')

    expect(items.some((li) => li.text().includes('unresolved-route'))).toBe(true)
    expect(items.some((li) => li.text().includes('alpha/broken'))).toBe(true)
    /* It is a refusal, not a clash. Reporting it as a clash would put it in
       the list D17-5 built for entries the shell DID place. */
    expect(view.findAll('ul.clashes')).toHaveLength(0)
  })

  it('keeps the two lists separate when a plugin has both', () => {
    const view = mountWith({ clashes: [clash], refusals: [refusal] })

    expect(view.findAll('ul.clashes li')).toHaveLength(1)
    expect(view.findAll('ul:not(.clashes) li').some((li) => li.text().includes('region-full'))).toBe(
      true,
    )
    expect(view.find('ul.clashes').text()).not.toContain('region-full')
  })
})
