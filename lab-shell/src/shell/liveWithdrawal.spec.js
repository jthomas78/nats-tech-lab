import { flushPromises, mount } from '@vue/test-utils'
import { computed, defineComponent, h, inject } from 'vue'
import { describe, expect, it, vi } from 'vitest'

import { createPermissionEvaluator } from './auth/permissions.js'
import { PLUGIN_STATUS } from './registry/pluginStatus.js'
import { REGISTRY_SCHEMA_VERSION, SHELL_API_VERSION } from './versions.js'
import { SHELL } from './shellKey.js'
import { bootShell } from './bootShell.js'

/*
  A publisher's withdrawal, asserted where the user would see it (BR-AS54,
  BR-AS56, BR-AS59). Same reasoning as `liveChange.spec.js`: the shell's
  collections are getters that hand back copies, so a spec reading them twice
  can pass while a rendered shell never updates. These mount a component.

  The document carries a MARKER, never a gap. An entry that simply stops
  arriving is a removal — offered as a reload, because the shell cannot tell it
  from a filter or an outage.
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

const withdrawn = (id) => ({ id, withdrawn: true })
const client = (result) => ({ fetchRegistry: vi.fn(async () => result) })
const permissions = createPermissionEvaluator({ permissions: ['*'] })

const Frame = defineComponent({
  setup() {
    const shell = inject(SHELL)
    const navigation = computed(() => shell.contributions.navigation)
    const inventory = computed(() => shell.inventory)
    const pending = computed(() => shell.pendingReload)
    return () =>
      h('div', [
        h('nav', navigation.value.map((entry) => h('a', { key: entry.qualifiedId }, entry.label))),
        h('ul', inventory.value.map((row) => h('li', { key: row.id }, `${row.id}:${row.status}`))),
        h('footer', pending.value.map((p) => h('b', { key: `${p.id}:${p.reason}` }, p.reason))),
      ])
  },
})

const running = async (...ids) => {
  const shell = await bootShell({
    registryClient: client({ ok: true, revision: 7, heldRevision: 7, plugins: ids.map((id) => manifest(id)) }),
    permissions,
  })
  return { shell, w: mount(Frame, { global: { provide: { [SHELL]: shell } } }) }
}

describe('BR-AS56 — a withdrawal takes the plugin off the screen', () => {
  it('removes the nav entry of a withdrawn plugin, and offers no reload', async () => {
    const { shell, w } = await running('fleet-ops')
    expect(w.find('nav').text()).toBe('fleet-ops')

    shell.applyRegistry({ ok: true, revision: 8, heldRevision: 8, plugins: [withdrawn('fleet-ops')] })
    await flushPromises()

    expect(w.find('nav').text()).toBe('')
    expect(w.find('footer').text()).toBe('')
    expect(w.find('ul').text()).toContain(`fleet-ops:${PLUGIN_STATUS.WITHDRAWN}`)
  })

  it('leaves every sibling on the screen', async () => {
    const { shell, w } = await running('fleet-ops', 'billing')

    shell.applyRegistry({
      ok: true,
      revision: 8,
      heldRevision: 8,
      plugins: [withdrawn('fleet-ops'), manifest('billing')],
    })
    await flushPromises()

    expect(w.find('nav').text()).toBe('billing')
    expect(w.find('ul').text()).toContain(`billing:${PLUGIN_STATUS.AVAILABLE}`)
  })

  it('survives the same withdrawal arriving twice', async () => {
    const { shell, w } = await running('fleet-ops', 'billing')
    const document = { ok: true, revision: 8, plugins: [withdrawn('fleet-ops'), manifest('billing')] }

    shell.applyRegistry({ ...document, heldRevision: 8 })
    shell.applyRegistry({ ...document, revision: 9, heldRevision: 9 })
    await flushPromises()

    expect(w.find('nav').text()).toBe('billing')
  })

  it('takes a withdrawal from a degraded document, but no return', async () => {
    const { shell, w } = await running('fleet-ops')

    // Withdrawal is the safe direction to be wrong in, so a document the
    // service could not vouch for may still take a plugin away (BR-AS51).
    shell.applyRegistry({ ok: true, degraded: true, plugins: [withdrawn('fleet-ops')] })
    await flushPromises()
    expect(w.find('nav').text()).toBe('')

    shell.applyRegistry({ ok: true, degraded: true, plugins: [manifest('fleet-ops')] })
    await flushPromises()
    expect(w.find('nav').text()).toBe('')
  })

  it('does not read a missing entry as a withdrawal', async () => {
    const { shell, w } = await running('fleet-ops')

    shell.applyRegistry({ ok: true, revision: 8, heldRevision: 8, plugins: [] })
    await flushPromises()

    // Absence is not authoritative (BR-AS54): the plugin stays on screen and
    // the shell offers a reload instead.
    expect(w.find('nav').text()).toBe('fleet-ops')
    expect(w.find('footer').text()).toContain('entry-removed')
  })
})

describe('BR-AS59 — an unchanged return puts it back', () => {
  it('restores the nav entry and the status it left', async () => {
    const { shell, w } = await running('fleet-ops')

    shell.applyRegistry({ ok: true, revision: 8, heldRevision: 8, plugins: [withdrawn('fleet-ops')] })
    shell.applyRegistry({ ok: true, revision: 9, heldRevision: 9, plugins: [manifest('fleet-ops')] })
    await flushPromises()

    expect(w.find('nav').text()).toBe('fleet-ops')
    expect(w.find('ul').text()).toContain(`fleet-ops:${PLUGIN_STATUS.AVAILABLE}`)
    expect(w.find('footer').text()).toBe('')
  })

  it('offers a reload instead when the definition changed while it was away', async () => {
    const { shell, w } = await running('fleet-ops')

    shell.applyRegistry({ ok: true, revision: 8, heldRevision: 8, plugins: [withdrawn('fleet-ops')] })
    shell.applyRegistry({
      ok: true,
      revision: 9,
      heldRevision: 9,
      plugins: [manifest('fleet-ops', { name: 'Fleet Ops v2' })],
    })
    await flushPromises()

    expect(w.find('nav').text()).toBe('')
    expect(w.find('footer').text()).toContain('entry-changed')
  })
})
