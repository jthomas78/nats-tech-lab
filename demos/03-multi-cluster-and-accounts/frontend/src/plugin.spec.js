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
import OverviewRoute from './plugin/OverviewRoute.vue'
import PlaygroundRoute from './plugin/PlaygroundRoute.vue'

const read = (path) => readFileSync(fileURLToPath(new URL(path, import.meta.url)), 'utf8')
const manifest = JSON.parse(read('../public/manifest.json'))
const viteConfig = read('../vite.config.js')

const ROUTE_SOURCES = ['./plugin/PlaygroundRoute.vue', './plugin/OverviewRoute.vue', './plugin.js']

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
    expect(routes.map((r) => r.path)).toEqual(['/demo-03/playground', '/demo-03/overview'])
    for (const route of routes) expect(route.path.startsWith(`/${manifest.routePrefix}/`)).toBe(true)
  })

  it('declares exactly one default route, and it is the playground', () => {
    const defaults = manifest.contributions.filter((c) => c.kind === 'route' && c.default === true)
    expect(defaults.map((r) => r.id)).toEqual(['playground'])
  })

  it('contributes one nav entry per route, all in one group', () => {
    const routeIds = manifest.contributions.filter((c) => c.kind === 'route').map((c) => c.id)
    const nav = manifest.contributions.filter((c) => c.kind === 'navigation')
    expect(nav.map((n) => n.route)).toEqual(routeIds)
    expect(new Set(nav.map((n) => n.group.id))).toEqual(new Set(['multi-cluster']))
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
    for (const C of [PlaygroundRoute, OverviewRoute]) {
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
