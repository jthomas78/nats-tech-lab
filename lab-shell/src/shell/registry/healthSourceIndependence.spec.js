import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

/*
  BR-AS80's regression half, and the one decision 9 draws a line under.

  The `build`-mode answer is a DISPLAY carve-out: one flag at the composition
  site, one mapping at the render site. If measurement code had to learn about
  it, the carve-out would have been exceeded and a new decision would be
  required. These specs are what makes that exceeding loud rather than quiet:
  the two measurement files must stay ignorant of how a plugin was discovered,
  and no `HealthSignal` may be manufactured anywhere to stand in for a reading.
*/
const src = (path) => readFileSync(resolve(process.cwd(), path), 'utf8')

const MEASUREMENT = [
  'src/shell/registry/healthPlane.js',
  'src/shell/registry/healthText.js',
]

describe('BR-AS80 — the measurement layer stays source-unaware', () => {
  it.each(MEASUREMENT)('%s never names the plugin source', (path) => {
    const text = src(path)
    expect(text).not.toContain('pluginSource')
    expect(text).not.toContain('PLUGIN_SOURCE')
    expect(text).not.toContain('VITE_PLUGIN_SOURCE')
  })

  it.each(MEASUREMENT)('%s never reads the monitoring flag', (path) => {
    /* `monitored: false` is a fact about the deployment, answered once for
       the page. A measurement file that branched on it would be answering a
       question it was not asked. */
    expect(src(path)).not.toContain('monitored')
  })
})

describe('BR-AS80 — no health signal is synthesised', () => {
  /* The flag is the whole substitution. A fabricated signal would be
     indistinguishable from a real reading one file further on, and would age
     like one. */
  it('the composition site builds no plane and no stand-in signal', () => {
    const main = src('src/main.js')

    expect(main).toContain('const monitored = source === PLUGIN_SOURCE_REGISTRY')
    expect(main).toContain('reactive({ signals: {}, monitored })')
    // The plane, its epoch watch and both its timers are all gated on the flag.
    expect(main).toContain('const healthPlane = monitored')
    expect(main).toContain('const healthAgeTimer = monitored ?')
    expect(main).toContain('const healthReconcileTimer = monitored')
    // Nothing is placed INTO the signal map to fill the gap.
    expect(main).not.toMatch(/signals\s*:\s*\{\s*[a-zA-Z'"]/)
    expect(main).not.toContain('lastCheckAt')
  })

  it('the render site maps the flag, and does not construct a signal', () => {
    const view = src('src/views/PluginsView.vue')

    expect(view).toContain('HEALTH_STATE.NOT_CONFIGURED')
    expect(view).not.toContain('lastCheckAt')
    expect(view).not.toContain('state:')
  })
})
