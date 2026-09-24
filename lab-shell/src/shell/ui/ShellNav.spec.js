/*
  BR-AS89 — the rail, mounted.

  `shellNavSections.spec.js` asserts the SHAPE the rail is built from. This
  file asserts the part only a DOM can answer: that a plugin entry is a real
  `<a href>`, and that a mark is reachable as WORDS and not as a colour
  (amendment A2's acceptance check).
*/
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import { reactive } from 'vue'
import { createMemoryHistory, createRouter } from 'vue-router'

import { createPermissionEvaluator } from '../auth/permissions.js'
import { createContributionRegistry } from '../contributions/contributionRegistry.js'
import { declareShellExtensionPoints } from '../extensions/extensionPoints.js'
import { validateManifest } from '../registry/manifestSchema.js'
import { PLUGIN_STATUS } from '../registry/pluginStatus.js'
import { REGISTRY_SCHEMA_VERSION, SHELL_API_VERSION } from '../versions.js'
import { SHELL } from '../shellKey.js'
import ShellNav from './ShellNav.vue'

const Blank = { template: '<div />' }

const navPlugin = (id, entries) => {
  const result = validateManifest({
    id,
    name: id,
    schemaVersion: REGISTRY_SCHEMA_VERSION,
    shellApiVersion: SHELL_API_VERSION,
    remote: { kind: 'federated', url: `http://localhost:7110/${id}.js`, module: './plugin' },
    contributions: entries.flatMap((entry, index) => [
      { kind: 'route', id: `r${index}`, path: `/${id}/r${index}`, title: `r${index}` },
      {
        kind: 'navigation',
        id: `n${index}`,
        label: entry.label,
        route: `r${index}`,
        ...(entry.group === undefined ? {} : { group: entry.group }),
      },
    ]),
  })
  if (!result.ok) throw new Error(`fixture is invalid: ${result.message}`)
  return result.plugin
}

const indexed = (...plugins) =>
  createContributionRegistry({
    extensionPoints: declareShellExtensionPoints(),
    permissions: createPermissionEvaluator({ permissions: ['*'] }),
  }).index(plugins)

/* The shell's real router owns one route per admitted contribution, named by
   qualified id (BR-AS06). Built here from the same list, so a rename of the
   naming rule breaks this file rather than passing against a stale fixture. */
const routerFor = async (contributions) => {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: Blank },
      { path: '/plugins', component: Blank },
      ...contributions.routes.map((route) => ({
        path: route.path,
        name: route.qualifiedId,
        component: Blank,
      })),
    ],
  })
  router.push('/')
  await router.isReady()
  return router
}

const mountRail = async (contributions, shell = {}) => {
  const router = await routerFor(contributions)
  return mount(ShellNav, {
    global: {
      plugins: [router],
      provide: {
        [SHELL]: reactive({
          contributions,
          statuses: new Map(),
          health: { signals: {} },
          inventory: [],
          ...shell,
        }),
      },
    },
  })
}

const itemNamed = (w, label) =>
  w.findAll('.nav-item').find((node) => node.text().includes(label))

describe('BR-AS89 — one rail, drawn by the shared renderer', () => {
  it('renders a single nav region the shell owns', async () => {
    const w = await mountRail(indexed(navPlugin('demo', [{ label: 'One' }])))
    const navs = w.findAll('nav')

    expect(navs).toHaveLength(1)
    expect(navs[0].attributes('aria-label')).toBe('Shell navigation')
  })

  it('bands the shell above the plugin, by eyebrow', async () => {
    const w = await mountRail(indexed(navPlugin('demo', [{ label: 'One' }])))

    expect(w.findAll('.eyebrow').map((n) => n.text())).toEqual(['Shell', 'Features'])
  })

  it('gives a plugin entry a REAL link, not a button', async () => {
    const w = await mountRail(indexed(navPlugin('demo', [{ label: 'One' }])))
    const item = itemNamed(w, 'One')

    expect(item.element.tagName).toBe('A')
    expect(item.attributes('href')).toBe('/demo/r0')
    expect(item.attributes('aria-pressed')).toBeUndefined()
  })

  it('links the shell own screens too', async () => {
    const w = await mountRail(indexed())

    expect(itemNamed(w, 'Home').attributes('href')).toBe('/')
    expect(itemNamed(w, 'Plugins').attributes('href')).toBe('/plugins')
  })

  it('marks only the page you are on, never every page under it', async () => {
    const contributions = indexed(navPlugin('demo', [{ label: 'One' }]))
    const router = await routerFor(contributions)
    const w = mount(ShellNav, {
      global: {
        plugins: [router],
        provide: {
          [SHELL]: reactive({ contributions, statuses: new Map(), health: { signals: {} }, inventory: [] }),
        },
      },
    })

    await router.push('/demo/r0')
    await w.vm.$nextTick()

    expect(itemNamed(w, 'One').classes()).toContain('active')
    expect(itemNamed(w, 'Home').classes()).not.toContain('active')
  })
})

describe('BR-AS89 — a mark is a dot AND words', () => {
  const failed = (contributions) =>
    mountRail(contributions, {
      statuses: new Map([['demo', { status: PLUGIN_STATUS.FAILED }]]),
    })

  it('draws nothing on an entry with nothing to say', async () => {
    const w = await mountRail(indexed(navPlugin('demo', [{ label: 'One' }])))

    expect(itemNamed(w, 'One').find('.nav-dot').exists()).toBe(false)
    expect(itemNamed(w, 'One').find('.sr-only').exists()).toBe(false)
  })

  it('draws the dot in the tone the rule chose', async () => {
    const w = await failed(indexed(navPlugin('demo', [{ label: 'One' }])))
    const dot = itemNamed(w, 'One').find('.nav-dot')

    expect(dot.exists()).toBe(true)
    expect(dot.classes()).toContain('err')
  })

  it('says the same thing in words, for a reader that has no colour', async () => {
    const w = await failed(indexed(navPlugin('demo', [{ label: 'One' }])))

    expect(itemNamed(w, 'One').find('.sr-only').text()).toBe(PLUGIN_STATUS.FAILED)
  })

  /* A2's acceptance check: a clash can LOSE the dot to a louder signal, so the
     words are where it has to survive — on both entries, not just one. */
  it('keeps a clash in the words on BOTH entries, even when one lost the dot', async () => {
    const contributions = indexed(
      navPlugin('demo', [{ label: 'Rates', group: 'ops' }]),
      navPlugin('other', [{ label: 'Rates', group: 'ops' }]),
    )
    const w = await mountRail(contributions, {
      statuses: new Map([['demo', { status: PLUGIN_STATUS.FAILED }]]),
    })
    const said = w.findAll('.sr-only').map((n) => n.text())

    expect(said).toHaveLength(2)
    for (const text of said) expect(text).toContain('Rates')
    expect(said.some((text) => text.startsWith(PLUGIN_STATUS.FAILED))).toBe(true)
  })

  it('leaves a sibling of the failed plugin unmarked (BR-AS04)', async () => {
    const w = await mountRail(
      indexed(navPlugin('demo', [{ label: 'One' }]), navPlugin('other', [{ label: 'Two' }])),
      { statuses: new Map([['demo', { status: PLUGIN_STATUS.FAILED }]]) },
    )

    expect(itemNamed(w, 'One').find('.nav-dot').exists()).toBe(true)
    expect(itemNamed(w, 'Two').find('.nav-dot').exists()).toBe(false)
  })
})
