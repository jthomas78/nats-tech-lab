import { flushPromises, mount } from '@vue/test-utils'
import { computed, defineComponent, h, inject } from 'vue'
import { describe, expect, it, vi } from 'vitest'

import { createPermissionEvaluator } from './auth/permissions.js'
import { PLUGIN_STATUS } from './registry/pluginStatus.js'
import { RELOAD_REASON } from './registry/registryDiff.js'
import { REGISTRY_SCHEMA_VERSION, SHELL_API_VERSION } from './versions.js'
import { SHELL } from './shellKey.js'
import { bootShell } from './bootShell.js'

/*
  Decision 26's claim, asserted where it is actually made: on screen.

  Every other spec in this directory reads the shell's collections directly,
  and all of them passed while a live-added plugin was invisible in the
  browser. The reason is decision 47: `contributions.navigation` is a getter
  that returns `[...navigation]`, so a reader's `computed()` over it evaluated
  once against a PLAIN array, registered zero reactive dependencies, and was
  never invalidated again. A spec that calls the getter twice sees the new
  entry; a rendered shell never did.

  So this file mounts a component and asserts on rendered output. The readers
  below are App.vue's own expressions, copied deliberately rather than
  imported — mounting App.vue would drag in AppShell, the router and PrimeVue
  and test those instead. What is under test is the seam: a `computed()` over
  a shell getter, rendered, then re-read after `applyRegistry`.
*/

const manifest = (id, overrides = {}) => ({
  id,
  name: id,
  schemaVersion: REGISTRY_SCHEMA_VERSION,
  shellApiVersion: SHELL_API_VERSION,
  remote: { kind: 'federated', url: `http://localhost:7110/${id}.js`, module: './plugin' },
  contributions: [
    { kind: 'route', id: 'home', path: `/${id}`, title: id },
    { kind: 'navigation', id: 'nav', label: id, route: 'home' },
  ],
  ...overrides,
})

const client = (result) => ({ fetchRegistry: vi.fn(async () => result) })
const permissions = createPermissionEvaluator({ permissions: ['*'] })

/* App.vue's readers, verbatim in shape: a computed over each collection the
   frame renders from. */
const Frame = defineComponent({
  setup() {
    const shell = inject(SHELL)
    const navigation = computed(() => shell.contributions.navigation)
    const inventory = computed(() => shell.inventory)
    const home = computed(() => shell.contributions.extensionsFor('shell/home-main/v1'))
    const pending = computed(() => shell.pendingReload)
    return () =>
      h('div', [
        h('nav', navigation.value.map((entry) => h('a', { key: entry.qualifiedId }, entry.label))),
        h('ul', inventory.value.map((row) => h('li', { key: row.id }, `${row.id}:${row.status}`))),
        h('aside', home.value.map((c) => h('span', { key: c.qualifiedId }, c.qualifiedId))),
        h('footer', pending.value.map((p) => h('b', { key: `${p.id}:${p.reason}` }, p.reason))),
      ])
  },
})

const mountShell = (shell) => mount(Frame, { global: { provide: { [SHELL]: shell } } })

describe('BR-AS19 / decision 26 — a live addition reaches the screen, not just the registry object', () => {
  it('renders the nav entry of a plugin added to a running shell', async () => {
    const shell = await bootShell({
      registryClient: client({ ok: true, revision: 7, etag: '"7"', plugins: [manifest('fleet-ops')] }),
      permissions,
    })
    const w = mountShell(shell)
    expect(w.find('nav').text()).toBe('fleet-ops')

    shell.applyRegistry({
      ok: true,
      revision: 8,
      etag: '"8"',
      plugins: [manifest('fleet-ops'), manifest('billing')],
    })
    await flushPromises()

    // The assertion that was failing in the browser while every unit spec
    // was green.
    expect(w.find('nav').text()).toContain('billing')
  })

  it('renders the inventory row of a plugin added to a running shell', async () => {
    const shell = await bootShell({
      registryClient: client({ ok: true, revision: 7, etag: '"7"', plugins: [manifest('fleet-ops')] }),
      permissions,
    })
    const w = mountShell(shell)

    shell.applyRegistry({ ok: true, revision: 8, etag: '"8"', plugins: [manifest('fleet-ops'), manifest('billing')] })
    await flushPromises()

    expect(w.find('ul').text()).toContain(`billing:${PLUGIN_STATUS.AVAILABLE}`)
  })

  it('renders an extension a live-added plugin placed into a shell-owned region', async () => {
    const shell = await bootShell({
      registryClient: client({ ok: true, revision: 7, etag: '"7"', plugins: [] }),
      permissions,
    })
    const w = mountShell(shell)
    expect(w.find('aside').text()).toBe('')

    shell.applyRegistry({
      ok: true,
      revision: 8,
      etag: '"8"',
      plugins: [
        manifest('billing', {
          contributions: [
            { kind: 'route', id: 'home', path: '/billing', title: 'billing' },
            { kind: 'extension', id: 'card', target: 'shell/home-main/v1', component: './Card' },
          ],
        }),
      ],
    })
    await flushPromises()

    expect(w.find('aside').text()).toContain('billing/card')
  })

  it('renders the reload offer for a change it may not apply', async () => {
    const shell = await bootShell({
      registryClient: client({ ok: true, revision: 7, etag: '"7"', plugins: [manifest('fleet-ops')] }),
      permissions,
    })
    const w = mountShell(shell)

    shell.applyRegistry({ ok: true, revision: 8, etag: '"8"', plugins: [] })
    await flushPromises()

    expect(w.find('footer').text()).toBe(RELOAD_REASON.REMOVED)
    // Offered, never applied: the withdrawn plugin is still in the nav.
    expect(w.find('nav').text()).toContain('fleet-ops')
  })

  /* Live finding, 2026-08-30: an operator disabled an entry and re-enabled it
     seconds later. The shell read the entry back — but the offer stayed, so
     every running shell kept saying a plugin it was still happily rendering
     had been withdrawn. An offer is a statement about the document the shell
     just read, so a read that no longer produces it must take it back. */
  it('retracts the reload offer when the withdrawn entry comes back unchanged', async () => {
    const shell = await bootShell({
      registryClient: client({ ok: true, revision: 7, etag: '"7"', plugins: [manifest('fleet-ops')] }),
      permissions,
    })
    const w = mountShell(shell)

    shell.applyRegistry({ ok: true, revision: 8, etag: '"8"', plugins: [] })
    await flushPromises()
    expect(w.find('footer').text()).toBe(RELOAD_REASON.REMOVED)

    shell.applyRegistry({ ok: true, revision: 9, etag: '"9"', plugins: [manifest('fleet-ops')] })
    await flushPromises()

    expect(w.find('footer').text()).toBe('')
    // Nothing was applied on the way through: the plugin never stopped running.
    expect(w.find('nav').text()).toContain('fleet-ops')
  })

  it('replaces the offer rather than clearing it when the entry comes back edited', async () => {
    const shell = await bootShell({
      registryClient: client({ ok: true, revision: 7, etag: '"7"', plugins: [manifest('fleet-ops')] }),
      permissions,
    })
    const w = mountShell(shell)

    shell.applyRegistry({ ok: true, revision: 8, etag: '"8"', plugins: [] })
    await flushPromises()

    shell.applyRegistry({
      ok: true,
      revision: 9,
      etag: '"9"',
      plugins: [manifest('fleet-ops', { name: 'Fleet Operations' })],
    })
    await flushPromises()

    expect(w.find('footer').text()).toBe(RELOAD_REASON.CHANGED)
  })

  it('keeps an offer the next read still produces, and drops only the one it does not', async () => {
    const shell = await bootShell({
      registryClient: client({
        ok: true,
        revision: 7,
        etag: '"7"',
        plugins: [manifest('fleet-ops'), manifest('billing')],
      }),
      permissions,
    })
    const w = mountShell(shell)

    shell.applyRegistry({ ok: true, revision: 8, etag: '"8"', plugins: [] })
    await flushPromises()
    expect(w.findAll('footer b')).toHaveLength(2)

    shell.applyRegistry({ ok: true, revision: 9, etag: '"9"', plugins: [manifest('billing')] })
    await flushPromises()

    expect(w.findAll('footer b').map((b) => b.text())).toEqual([RELOAD_REASON.REMOVED])
    expect(w.find('nav').text()).toContain('fleet-ops')
  })

  /* A 304 and a degraded read both carry no document, so neither is evidence
     that anything was taken back (decision 48). */
  it('does not retract an offer on an unchanged or degraded read', async () => {
    const shell = await bootShell({
      registryClient: client({ ok: true, revision: 7, etag: '"7"', plugins: [manifest('fleet-ops')] }),
      permissions,
    })
    const w = mountShell(shell)

    shell.applyRegistry({ ok: true, revision: 8, etag: '"8"', plugins: [] })
    await flushPromises()

    shell.applyRegistry({ ok: true, unchanged: true, etag: '"8"' })
    shell.applyRegistry({ ok: true, degraded: true, plugins: [] })
    await flushPromises()

    expect(w.find('footer').text()).toBe(RELOAD_REASON.REMOVED)
  })

  it('applies the addition and offers the edit when one read carried both', async () => {
    // Decision 46's interleaving, end to end. The old diff applied only the
    // addition, leaving the shell holding a catalog that existed at no
    // revision at all.
    const shell = await bootShell({
      registryClient: client({ ok: true, revision: 7, etag: '"7"', plugins: [manifest('fleet-ops')] }),
      permissions,
    })
    const w = mountShell(shell)

    shell.applyRegistry({
      ok: true,
      revision: 8,
      etag: '"8"',
      plugins: [manifest('fleet-ops', { name: 'Fleet Operations' }), manifest('billing')],
    })
    await flushPromises()

    expect(w.find('nav').text()).toContain('billing')
    expect(w.find('footer').text()).toBe(RELOAD_REASON.CHANGED)
  })
})
