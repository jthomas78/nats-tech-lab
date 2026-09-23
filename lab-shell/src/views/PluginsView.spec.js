import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'

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
