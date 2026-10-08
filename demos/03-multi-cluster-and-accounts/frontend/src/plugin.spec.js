/* The embedded entry's guards, adapted from demo 04.

   One build, two entries. These specs hold the parts that only break when
   somebody edits one entry and forgets the other: the shape the shell loads,
   the promise that the plugin entry renders no second frame, and the places
   the plugin's identity is written down. */
import { readFileSync } from 'node:fs'
import { fileURLToPath, URL } from 'node:url'
import { mount } from '@vue/test-utils'
import PrimeVue from 'primevue/config'
import { afterEach, describe, expect, it, vi } from 'vitest'

import AppShell from '@ui-shell/AppShell.vue'
import NavList from '@ui-shell/NavList.vue'

import * as config from './config.js'
import { EMBEDDED_COMMAND_API } from './config.js'
import { activate, components } from './plugin.js'
import LegacyRoute from './plugin/LegacyRoute.vue'
import MetaLeaderRoute from './plugin/MetaLeaderRoute.vue'
import { LEGACY_TABS, PAGE_PATH } from './plugin/tabs.js'

const read = (path) => readFileSync(fileURLToPath(new URL(path, import.meta.url)), 'utf8')
const manifest = JSON.parse(read('../public/manifest.json'))
const viteConfig = read('../vite.config.js')

const ROUTE_SOURCES = ['./plugin/MetaLeaderRoute.vue', './plugin/LegacyRoute.vue', './plugin/tabs.js', './plugin.js']

describe('the entry module the shell loads', () => {
  it('exports a component map and an activate function', () => {
    expect(typeof components).toBe('object')
    expect(typeof activate).toBe('function')
  })

  it('exports exactly the components the manifest names, and no more', () => {
    const named = manifest.contributions.filter((c) => c.component !== undefined).map((c) => c.component)
    expect(Object.keys(components).sort()).toEqual([...new Set(named)].sort())
  })

  it('makes every route under its own prefix', () => {
    const routes = manifest.contributions.filter((c) => c.kind === 'route')
    expect(routes.map((r) => r.path)).toEqual([
      `${PAGE_PATH}/:tab(overview|live)?`,
      '/demo-03/playground',
      '/demo-03/overview',
    ])
    for (const route of routes) expect(route.path.startsWith(`/${manifest.routePrefix}/`)).toBe(true)
  })

  /* The old playground link is the default, so Home and the bare /demo-03
     open the Live tab through it. */
  it('declares exactly one default route, and it is the old playground link', () => {
    const defaults = manifest.contributions.filter((c) => c.kind === 'route' && c.default === true)
    expect(defaults.map((r) => r.id)).toEqual(['playground'])
    expect(LEGACY_TABS.playground).toBe('live')
  })

  it('contributes one nav entry, Meta-Leader in Clusters', () => {
    const nav = manifest.contributions.filter((c) => c.kind === 'navigation')
    expect(nav.map((n) => [n.route, n.label, n.group.id, n.group.label])).toEqual([
      ['meta-leader', 'Meta-Leader', 'clusters', 'Clusters'],
    ])
  })

  it('keeps the old links as routes with no rail entry, each forwarding to a tab', () => {
    const hidden = manifest.contributions
      .filter((c) => c.kind === 'route' && c.id !== 'meta-leader')
      .map((r) => [r.id, r.component])
    expect(hidden).toEqual(Object.keys(LEGACY_TABS).map((id) => [id, 'legacy']))
  })
})

describe('the embedded route components', () => {
  // The playground polls on mount. A fetch that never answers keeps the
  // mount quiet; nothing here is about the service.
  const quiet = () => vi.stubGlobal('fetch', () => new Promise(() => {}))
  afterEach(() => vi.unstubAllGlobals())

  const mountRoute = (C) => mount(C, { global: { plugins: [PrimeVue] } })

  it('renders no AppShell and no rail of its own', () => {
    quiet()
    for (const C of [MetaLeaderRoute, LegacyRoute]) {
      const w = mountRoute(C)
      expect(w.findComponent(AppShell).exists()).toBe(false)
      expect(w.findComponent(NavList).exists()).toBe(false)
      expect(w.find('.app').exists()).toBe(false)
      w.unmount()
    }
  })

  it('never names AppShell, NavList or vue-router in its sources', () => {
    for (const src of ROUTE_SOURCES) {
      const text = read(src)
      expect(text).not.toContain('AppShell.vue')
      expect(text).not.toContain('NavList.vue')
      expect(text).not.toContain("from 'vue-router'")
    }
  })
})

describe("the plugin's identity, written in three files", () => {
  it('names the same federation container in the manifest and the build', () => {
    expect(viteConfig).toContain(`name: '${manifest.remote.name}'`)
  })

  it('builds every asset under the public path the manifest points at', () => {
    const base = `/plugins/${manifest.id}/`
    expect(viteConfig).toContain(`base: '${base}'`)
    expect(manifest.remote.url).toBe(`${base}remoteEntry.js`)
  })

  it('keeps the dev-server port out of the manifest', () => {
    const demoJson = JSON.parse(read('../public/demo.json'))
    expect(demoJson.devServer.port).toBe(20301)
    expect(JSON.stringify(manifest)).not.toContain('20301')
  })
})

describe('the command API base, when the demo is embedded', () => {
  it('names the demo directory the shell scans, not the plugin id', () => {
    expect(EMBEDDED_COMMAND_API).toBe('/demo-api/03-multi-cluster-and-accounts')
    expect(EMBEDDED_COMMAND_API).not.toContain(manifest.id)
  })

  // Order matters: the first spec reads the base before activate moves it.
  it('is an absolute URL until the shell activates the plugin, then the shell path', () => {
    expect(config.COMMAND_API).toMatch(/^http:\/\//)
    activate()
    expect(config.COMMAND_API).toBe(EMBEDDED_COMMAND_API)
  })
})
