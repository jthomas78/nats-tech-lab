/* The embedded entry's guards (task 16d).

   Demo 04 ships ONE build with TWO entries. These specs hold the parts that
   only break when somebody edits one entry and forgets the other: the shape
   the shell loads, the promise that the plugin entry renders no second frame,
   and the three places the plugin's identity is written down. */
import { readFileSync } from 'node:fs'
import { fileURLToPath, URL } from 'node:url'
import { mount } from '@vue/test-utils'
import PrimeVue from 'primevue/config'
import { describe, expect, it } from 'vitest'

import NavList from '@ui-shell/NavList.vue'

import PoolPanel from './components/PoolPanel.vue'
import StreamCqrsPanel from './components/StreamCqrsPanel.vue'
import * as config from './config.js'
import { EMBEDDED_COMMAND_API } from './config.js'
import { activate, components } from './plugin.js'
import OdometerRoute from './plugin/OdometerRoute.vue'
import { railSections } from './view/lessons.js'

const read = (path) => readFileSync(fileURLToPath(new URL(path, import.meta.url)), 'utf8')
const manifest = JSON.parse(read('../public/manifest.json'))
const viteConfig = read('../vite.config.js')

describe('the entry module the shell loads', () => {
  it('exports a component map and an activate function', () => {
    expect(typeof components).toBe('object')
    expect(typeof activate).toBe('function')
  })

  it('exports exactly the components the manifest names, and no more', () => {
    const named = manifest.contributions
      .filter((c) => c.component !== undefined)
      .map((c) => c.component)
    expect(Object.keys(components).sort()).toEqual([...new Set(named)].sort())
  })

  /* F-4's resolution: a single route, because splitting the two lessons into
     two routes would change how the demo is navigated. */
  it('makes exactly one route contribution', () => {
    expect(manifest.contributions.filter((c) => c.kind === 'route')).toHaveLength(1)
  })
})

describe('the embedded route component', () => {
  const mountRoute = () => mount(OdometerRoute, { global: { plugins: [PrimeVue] } })

  /* BR-AS09: lab-shell owns the outer chrome when the demo is embedded. A
     second AppShell inside the first would nest two topbars and two rails. */
  it('renders no AppShell of its own', () => {
    expect(mountRoute().find('.app-shell').exists()).toBe(false)
  })

  it('never reaches for AppShell in its sources either', () => {
    expect(read('./plugin/OdometerRoute.vue')).not.toContain('AppShell.vue')
    expect(read('./plugin.js')).not.toContain('AppShell.vue')
  })

  it('keeps the same lesson rail, with both lessons on it', () => {
    const rail = mountRoute().findComponent(NavList)
    expect(rail.exists()).toBe(true)
    for (const section of railSections()) {
      for (const item of section.items) expect(rail.text()).toContain(item.label)
    }
  })

  it('opens on lesson 01, the same lesson the standalone app opens on', () => {
    const w = mountRoute()
    expect(w.findComponent(StreamCqrsPanel).exists()).toBe(true)
    expect(w.findComponent(PoolPanel).exists()).toBe(false)
  })

  it('reports the connection state, as the standalone topbar does', () => {
    expect(mountRoute().find('[data-testid="connection-status"]').exists()).toBe(true)
  })
})

describe('the plugin\'s identity, written in three files', () => {
  /* The container name is a global identifier in some federation output
     formats, so it is snake_case while the id stays kebab-case. The shell
     addresses the remote by the manifest's name; the build stamps it by the
     config's. A mismatch fails only at load time, in the browser. */
  it('names the same federation container in the manifest and the build', () => {
    expect(viteConfig).toContain(`name: '${manifest.remote.name}'`)
  })

  /* BR-AS77: the shell serves every one of this plugin's assets under
     /plugins/<id>/ — the entry, the chunks, the CSS, the fonts and the
     images. Vite's `base` is what makes the emitted URLs relative to it. */
  it('builds every asset under the public path the manifest points at', () => {
    const base = `/plugins/${manifest.id}/`
    expect(viteConfig).toContain(`base: '${base}'`)
    expect(manifest.remote.url).toBe(`${base}remoteEntry.js`)
  })

  /* BR-AS78: a manifest is byte-identical whichever source found the plugin,
     so the dev-server port — meaningless to a registry plugin — lives in the
     sibling file the shell's scan reads instead. */
  it('keeps the dev-server port out of the manifest', () => {
    const demoJson = JSON.parse(read('../public/demo.json'))
    expect(demoJson.devServer.port).toBe(20401)
    expect(JSON.stringify(manifest)).not.toContain('20401')
  })
})

describe('the command API base, when the demo is embedded', () => {
  /* Standalone, the page is on 20401 and calls 20402 absolutely, and
     `cqrs/names.go` grants that one origin. Embedded, the page is on the
     shell's origin, the grant does not name it, and the browser throws away
     every answer — every button said `Failed to fetch`. The fix is the shell
     serving these routes on its own origin. Widening the CORS grant is
     forbidden by this demo's CLAUDE.md and by the phase's F-3. */
  it('is an absolute URL until the shell activates the plugin', () => {
    expect(config.COMMAND_API).toMatch(/^http:\/\//)
  })

  it('is the shell-origin path the shell generates (app-shell BR-AS82)', () => {
    expect(EMBEDDED_COMMAND_API).toBe('/demo-api/04-jetstream-cqrs')
  })

  /* The demo is a sealed unit, so it cannot import the shell's own constant.
     The shell's route is built from this demo's DIRECTORY name, which is why
     the literal above is a directory name and not the plugin id. */
  it('names the demo directory the shell scans, not the plugin id', async () => {
    expect(EMBEDDED_COMMAND_API.endsWith('/04-jetstream-cqrs')).toBe(true)
    expect(EMBEDDED_COMMAND_API).not.toContain(manifest.id)
  })

  /* `COMMAND_API` is exported with `let` and read through the module
     namespace here on purpose. Every call site reads it as a default
     argument, evaluated per call, so moving it once moves every later call
     — and a destructured copy, in a spec or anywhere else, would not see it
     move. This spec is the place that shows if somebody makes it `const`. */
  it('moves the base onto the shell\'s origin when activate runs', () => {
    activate()
    expect(config.COMMAND_API).toBe(EMBEDDED_COMMAND_API)
  })

  /* Only the HTTP side moves. The live NATS view is a WebSocket, which CORS
     does not gate at all, so proxying it would buy nothing — it is gated by
     `allowed_origins` in deploy/nats.conf, which names the shell's origin
     itself. A different door, and this spec holds the two apart. */
  it('leaves the NATS WebSocket URL alone', () => {
    expect(read('./plugin.js')).not.toContain('NATS_WS')
  })
})
