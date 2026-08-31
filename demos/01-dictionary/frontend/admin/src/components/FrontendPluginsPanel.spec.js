import PrimeVue from 'primevue/config'
import { mount, flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import FrontendPluginsPanel from './FrontendPluginsPanel.vue'

// Phase 2b — the curation surface for the frontend plugin registry. What the
// specs below are actually about is the *refusals*, because they are the part
// of the design that has to survive contact with a UI:
//
//   · BR-AS18 — a write carries the revision it was made against, and a stale
//     write is refused with the revision that won. Nothing is merged, and the
//     panel offers a reload, not a retry-with-force.
//   · BR-AS20 — an origin refusal names the entry and its cause, never a
//     credential and never the configured origins (BR-AS04); widening the
//     allowlist is a deployment change, not a screen.
//   · BR-AS24 — disable is the only lifecycle control. `active` has no exit
//     transition, so there is no delete affordance to find.

vi.mock('../api', () => ({
  getRegistryEntries: vi.fn(),
  upsertRegistryEntry: vi.fn(),
  setRegistryEntryEnabled: vi.fn(),
}))

import { getRegistryEntries, setRegistryEntryEnabled, upsertRegistryEntry } from '../api'

const DOC = {
  schemaVersion: 1,
  revision: 50,
  allowedOrigins: ['https://plugins.acme.internal', 'http://localhost:7110'],
  plugins: [
    {
      id: 'seafreight-flow',
      name: 'Seafreight Flow',
      version: '1.4.0',
      shellApiVersion: 1,
      routePrefix: '/seafreight',
      enabled: true,
      conforming: true,
      remote: { kind: 'federated', url: 'https://plugins.acme.internal/seafreight/remoteEntry.js', module: './Plugin' },
      contributions: [{ kind: 'nav-item', id: 'nav' }, { kind: 'route', id: 'route' }],
    },
    {
      id: 'example-plugin-slow',
      name: 'Example (slow)',
      version: '0.2.0',
      shellApiVersion: 1,
      routePrefix: '/example-slow',
      enabled: false,
      conforming: true,
      remote: { kind: 'federated', url: 'http://localhost:7110/remoteEntry.js', module: './Plugin' },
      contributions: [],
    },
    {
      id: 'legacy-plugin',
      name: 'Legacy',
      version: '0.9.0',
      shellApiVersion: 1,
      routePrefix: '/legacy',
      enabled: true,
      conforming: false,
      remote: { kind: 'federated', url: 'https://cdn.plugins.example.net/legacy/remoteEntry.js', module: './Plugin' },
      contributions: [],
    },
  ],
}

const doc = () => JSON.parse(JSON.stringify(DOC))

function mountPanel() {
  return mount(FrontendPluginsPanel, { global: { plugins: [PrimeVue] } })
}

beforeEach(() => {
  vi.clearAllMocks()
  getRegistryEntries.mockResolvedValue(doc())
})

describe('FrontendPluginsPanel', () => {
  it('reports the revision it is reading and writing against', async () => {
    const w = mountPanel()
    await flushPromises()
    expect(w.get('[data-testid="registry-revision"]').text()).toContain('50')
  })

  it('lists every curated entry, disabled ones included', async () => {
    const w = mountPanel()
    await flushPromises()
    const ids = w.findAll('[data-testid="entry-id"]').map((n) => n.text())
    expect(ids).toEqual(['seafreight-flow', 'example-plugin-slow', 'legacy-plugin'])
  })

  it('marks an entry the allowlist no longer permits as withheld from shells (BR-AS20)', async () => {
    const w = mountPanel()
    await flushPromises()
    const rows = w.findAll('[data-testid="entry-row"]')
    expect(rows[2].text()).toContain('withheld')
    expect(rows[0].text()).not.toContain('withheld')
  })

  it('offers no delete control anywhere — disable is the whole lifecycle (BR-AS24)', async () => {
    const w = mountPanel()
    await flushPromises()
    expect(w.html().toLowerCase()).not.toContain('delete')
    expect(w.findAll('[data-testid="toggle-enabled"]')).toHaveLength(3)
  })

  it('sends the revision it read with an enable/disable write (BR-AS18)', async () => {
    setRegistryEntryEnabled.mockResolvedValue({ ...doc(), revision: 51 })
    const w = mountPanel()
    await flushPromises()
    await w.findAll('[data-testid="toggle-enabled"]')[0].trigger('click')
    await flushPromises()
    expect(setRegistryEntryEnabled).toHaveBeenCalledWith('seafreight-flow', false, 50)
  })

  it('refuses a stale write with the revision that won, and offers a reload rather than a merge (BR-AS18)', async () => {
    const err = new Error('the registry moved on while you were editing')
    err.status = 409
    err.body = { currentRevision: 53, yourRevision: 50 }
    setRegistryEntryEnabled.mockRejectedValue(err)
    const w = mountPanel()
    await flushPromises()
    await w.findAll('[data-testid="toggle-enabled"]')[0].trigger('click')
    await flushPromises()

    const stale = w.get('[data-testid="stale-revision"]')
    expect(stale.text()).toContain('50')
    expect(stale.text()).toContain('53')
    expect(w.find('[data-testid="stale-reload"]').exists()).toBe(true)
    expect(stale.text().toLowerCase()).not.toContain('force')

    getRegistryEntries.mockResolvedValue({ ...doc(), revision: 53 })
    await w.get('[data-testid="stale-reload"]').trigger('click')
    await flushPromises()
    expect(w.get('[data-testid="registry-revision"]').text()).toContain('53')
    expect(w.find('[data-testid="stale-revision"]').exists()).toBe(false)
  })

  it('names the cause of an origin refusal without echoing a URL or the allowlist (BR-AS04, BR-AS20)', async () => {
    const err = new Error('the plugin’s remote origin is not one this platform is configured to load code from')
    err.status = 422
    err.body = { error: err.message }
    upsertRegistryEntry.mockRejectedValue(err)
    const w = mountPanel()
    await flushPromises()

    await w.findAll('[data-testid="edit-entry"]')[0].trigger('click')
    await w.get('[data-testid="entry-url"]').setValue('https://cdn.plugins.example.net/x/remoteEntry.js')
    await w.get('[data-testid="entry-save"]').trigger('click')
    await flushPromises()

    const refusal = w.get('[data-testid="origin-refused"]')
    expect(refusal.text()).toContain('not one this platform is configured to load code from')
    expect(refusal.text()).not.toContain('cdn.plugins.example.net')
    expect(refusal.text()).not.toContain('plugins.acme.internal')
  })

  it('writes an edited entry against the revision on screen, and never edits its contributions', async () => {
    upsertRegistryEntry.mockResolvedValue({ ...doc(), revision: 51 })
    const w = mountPanel()
    await flushPromises()
    await w.findAll('[data-testid="edit-entry"]')[0].trigger('click')
    await w.get('[data-testid="entry-version"]').setValue('1.5.0')
    await w.get('[data-testid="entry-save"]').trigger('click')
    await flushPromises()

    const [entry, rev] = upsertRegistryEntry.mock.calls[0]
    expect(rev).toBe(50)
    expect(entry.id).toBe('seafreight-flow')
    expect(entry.version).toBe('1.5.0')
    expect(entry.contributions).toEqual(DOC.plugins[0].contributions)
  })

  it('colours each state apart — withheld is a refusal, disabled is a decision', async () => {
    const w = mountPanel()
    await flushPromises()
    const pills = w.findAll('[data-testid="entry-row"]').map((r) => r.get('.pill'))
    expect(pills[0].classes()).toContain('ok')
    expect(pills[1].classes()).toContain('off')
    expect(pills[2].classes()).toContain('bad')
  })

  it('says a disabled entry keeps running in shells until they reload (BR-AS19)', async () => {
    const w = mountPanel()
    await flushPromises()
    expect(w.findAll('[data-testid="entry-row"]')[1].text()).toContain('until they reload')
  })

  it('lists the configured origins as what they are — service configuration, not a screen (BR-AS20)', async () => {
    const w = mountPanel()
    await flushPromises()
    const panel = w.get('[data-testid="origins-panel"]')
    expect(panel.text()).toContain('https://plugins.acme.internal')
    expect(panel.text()).toContain('http://localhost:7110')
    expect(panel.text().toLowerCase()).toContain('deployment change')
  })

  it('states what a write does and does not do to a shell already running the plugin', async () => {
    const w = mountPanel()
    await flushPromises()
    const panel = w.get('[data-testid="write-effects-panel"]')
    expect(panel.text()).toContain('indexed live')
    expect(panel.text()).toContain('never applied under the user')
    expect(panel.text()).toContain('notify._platform.registry.frontend-plugins.changed')
  })
})

// BR-AS49 — a revoked publisher key withholds what it signed. The panel already
// spends the word "withheld" on a non-conforming origin, so a revocation gets
// its own word: an operator must be able to tell a narrowed allowlist from a
// withdrawn key, because only the second is a security event.
describe('FrontendPluginsPanel — a withheld entry (BR-AS49)', () => {
  const withRevoked = () => {
    const d = doc()
    d.plugins.push({
      id: 'revoked-plugin',
      name: 'Revoked',
      version: '1.0.0',
      shellApiVersion: 1,
      routePrefix: '/revoked',
      enabled: false,
      conforming: true,
      withheld: true,
      remote: { kind: 'federated', url: 'https://plugins.acme.internal/revoked/remoteEntry.js', module: './Plugin' },
      contributions: [],
    })
    return d
  }

  const rowFor = (w, id) =>
    w.findAll('[data-testid="entry-row"]').find((r) => r.text().includes(id))

  it('reads as revoked, not as withheld', async () => {
    getRegistryEntries.mockResolvedValue(withRevoked())
    const w = mountPanel()
    await flushPromises()
    const row = rowFor(w, 'revoked-plugin')
    expect(row.text()).toContain('revoked')
  })

  it('names the publisher key as the cause, and re-enabling as the way back', async () => {
    getRegistryEntries.mockResolvedValue(withRevoked())
    const w = mountPanel()
    await flushPromises()
    expect(rowFor(w, 'revoked-plugin').text()).toContain('publisher key revoked')
    expect(rowFor(w, 'revoked-plugin').text()).toContain('enable to restore')
  })

  it('leaves a non-conforming origin reading withheld, so the two causes stay apart', async () => {
    getRegistryEntries.mockResolvedValue(withRevoked())
    const w = mountPanel()
    await flushPromises()
    const legacy = rowFor(w, 'legacy-plugin')
    expect(legacy.text()).toContain('withheld')
    expect(legacy.text()).not.toContain('revoked')
  })

  it('says nothing about a revocation on an ordinary disabled entry', async () => {
    getRegistryEntries.mockResolvedValue(withRevoked())
    const w = mountPanel()
    await flushPromises()
    expect(rowFor(w, 'example-plugin-slow').text()).not.toContain('revoked')
  })
})
