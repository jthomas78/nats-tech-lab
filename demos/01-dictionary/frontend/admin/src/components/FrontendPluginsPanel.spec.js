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
  describe('BR-AS45 / decisions 77 and 85 — manifest drift is display only', () => {
    it('refreshes the last observation from the registry without writing or fetching a plugin', async () => {
      const d = doc()
      d.plugins[0].source = 'preload'
      d.plugins[0].drift = { state: 'checked' }
      getRegistryEntries.mockResolvedValue(d)
      const w = mountPanel()
      await flushPromises()
      expect(w.get('[data-testid="entry-drift"]').text()).toBe('checked')
      getRegistryEntries.mockResolvedValue({ ...d, plugins: [{ ...d.plugins[0], drift: { state: 'not checked', stage: 'manifest-drift', cause: 'fetch-failed' } }] })
      await w.get('[data-testid="refresh-observations"]').trigger('click')
      await flushPromises()
      expect(w.get('[data-testid="entry-drift"]').text()).toBe('not checked')
      expect(getRegistryEntries).toHaveBeenCalledTimes(2)
      expect(upsertRegistryEntry).not.toHaveBeenCalled()
      expect(setRegistryEntryEnabled).not.toHaveBeenCalled()
      w.unmount()
    })

    it.each(['fetch-failed', 'timeout', 'http-status', 'invalid-manifest', 'origin-unmapped'])(
      'shows %s as not checked, never agreement', async (cause) => {
        const d = doc()
        d.plugins[0].source = 'preload'
        d.plugins[0].drift = { state: 'not checked', stage: 'manifest-drift', cause }
        getRegistryEntries.mockResolvedValue(d)
        const w = mountPanel()
        await flushPromises()
        const row = w.findAll('[data-testid="entry-row"]')[0]
        expect(row.get('[data-testid="entry-drift"]').text()).toBe('not checked')
        expect(row.get('[data-testid="drift-cause"]').text()).toBe(`manifest-drift: ${cause}`)
        expect(row.get('.pill').text()).toBe('enabled')
        w.unmount()
      },
    )

    it('does not claim agreement when no observation has arrived', async () => {
      const d = doc()
      d.plugins[0].source = 'preload'
      getRegistryEntries.mockResolvedValue(d)
      const w = mountPanel()
      await flushPromises()
      expect(w.get('[data-testid="entry-drift"]').text()).toBe('not checked')
      w.unmount()
    })

    it('names differing fields separately from source and state, leaving curation alone', async () => {
      const d = doc()
      d.plugins[0].source = 'preload'
      d.plugins[0].drift = { state: 'drift', fields: ['contributions', 'version'] }
      d.plugins[1].source = 'preload'
      d.plugins[1].drift = { state: 'checked' }
      getRegistryEntries.mockResolvedValue(d)
      const w = mountPanel()
      await flushPromises()
      const rows = w.findAll('[data-testid="entry-row"]')
      expect(rows[0].get('[data-testid="entry-drift"]').text()).toBe('drift')
      expect(rows[0].get('[data-testid="drift-fields"]').text()).toBe('contributions, version')
      expect(rows[0].get('[data-testid="entry-source"]').text()).toBe('preload')
      expect(rows[0].get('.pill').text()).toBe('enabled')
      expect(rows[0].get('[data-testid="toggle-enabled"]').text()).toBe('Disable')
      expect(rows[1].get('[data-testid="entry-drift"]').text()).toBe('checked')
      expect(rows[1].get('.pill').text()).toBe('disabled')
      expect(rows[2].find('[data-testid="entry-drift"]').exists()).toBe(false)
      expect(upsertRegistryEntry).not.toHaveBeenCalled()
      expect(setRegistryEntryEnabled).not.toHaveBeenCalled()
      w.unmount()
    })

    it('does not write derived diagnostics back when an operator edits an entry', async () => {
      const d = doc()
      d.plugins[0].source = 'preload'
      d.plugins[0].drift = { state: 'drift', fields: ['version'] }
      getRegistryEntries.mockResolvedValue(d)
      upsertRegistryEntry.mockResolvedValue(d)
      const w = mountPanel()
      await flushPromises()
      await w.findAll('[data-testid="edit-entry"]')[0].trigger('click')
      await w.get('[data-testid="entry-save"]').trigger('click')
      await flushPromises()
      expect(upsertRegistryEntry).toHaveBeenCalled()
      expect(upsertRegistryEntry.mock.calls[0][0]).not.toHaveProperty('drift')
      w.unmount()
    })
  })

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
    expect(panel.text()).toContain('notify._platform.mfe-registry.frontend-plugins.changed')
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

// Decision 80 — the source badge. It answers "how did this row get here",
// which is a different kind of fact from the State column beside it: State is
// a judgement that can change, source is history and cannot. The service
// derives it from the first audit row; the panel's whole job is to show it and
// to never dress up an absent one.
describe('FrontendPluginsPanel — the source badge (decision 80)', () => {
  const withSources = () => {
    const d = doc()
    d.plugins[0].source = 'announced'
    d.plugins[1].source = 'preload'
    d.plugins[2].source = 'curated'
    return d
  }

  it('shows the tier each entry registered through', async () => {
    getRegistryEntries.mockResolvedValue(withSources())
    const w = mountPanel()
    await flushPromises()
    const badges = w.findAll('[data-testid="entry-source"]').map((n) => n.text())
    expect(badges).toEqual(['announced', 'preload', 'curated'])
  })

  it('says unknown rather than nothing when the service could not tell', async () => {
    // A blank cell in one row among many reads as a rendering fault, and the
    // one thing this column must never look like is agreement.
    getRegistryEntries.mockResolvedValue(doc())
    const w = mountPanel()
    await flushPromises()
    const badges = w.findAll('[data-testid="entry-source"]').map((n) => n.text())
    expect(badges).toEqual(['unknown', 'unknown', 'unknown'])
  })

  it('does not render it as a state pill', async () => {
    // The two columns must not be read as the same kind of claim.
    getRegistryEntries.mockResolvedValue(withSources())
    const w = mountPanel()
    await flushPromises()
    const badge = w.find('[data-testid="entry-source"]')
    expect(badge.classes()).not.toContain('pill')
  })
})

// Phase 8c — the pending tier. An announced entry that is not enabled is
// waiting on an operator: either nobody has reviewed it, or BR-AS40 bounced it
// back when its remote left the origin it was approved on. The panel's job is
// to make that a different fact from "disabled", because the two have opposite
// consequences — a disabled plugin is still running in shells, and a pending
// one has never run anywhere.
describe('FrontendPluginsPanel — the pending tier', () => {
  const announced = (over = {}) => ({
    id: 'acme-flow',
    name: 'Acme Flow',
    version: '2.0.0',
    shellApiVersion: 1,
    routePrefix: '/acme',
    enabled: false,
    conforming: true,
    source: 'announced',
    registeredBy: 'pub_7f3a91c4',
    announcedAt: new Date(Date.now() - 3 * 3600 * 1000).toISOString(),
    remote: { kind: 'federated', url: 'https://plugins.acme.internal/acme/remoteEntry.js', module: './Plugin' },
    contributions: [],
    ...over,
  })

  const withPending = (over) => {
    const d = doc()
    d.plugins.push(announced(over))
    return d
  }

  const rowFor = (w, id) =>
    w.findAll('[data-testid="entry-row"]').find((r) => r.text().includes(id))

  it('says pending, and does not claim it is running anywhere', async () => {
    getRegistryEntries.mockResolvedValue(withPending())
    const w = mountPanel()
    await flushPromises()
    const row = rowFor(w, 'acme-flow')
    expect(row.text()).toContain('pending')
    expect(row.text()).toContain('awaiting your review')
    expect(row.text()).not.toContain('still running in shells')
  })

  it('names the publisher, because approving is a decision about them', async () => {
    getRegistryEntries.mockResolvedValue(withPending())
    const w = mountPanel()
    await flushPromises()
    expect(rowFor(w, 'acme-flow').get('[data-testid="entry-publisher"]').text()).toBe('pub_7f3a91c4')
  })

  it('says how long it has been waiting, as an age rather than an instant', async () => {
    getRegistryEntries.mockResolvedValue(withPending())
    const w = mountPanel()
    await flushPromises()
    expect(rowFor(w, 'acme-flow').get('[data-testid="entry-announced-age"]').text()).toBe('3h ago')
  })

  it('counts what is waiting, so an operator sees it without reading the table', async () => {
    getRegistryEntries.mockResolvedValue(withPending())
    const w = mountPanel()
    await flushPromises()
    expect(w.get('[data-testid="pending-count"]').text()).toContain('1 awaiting review')
  })

  it('is not pending once enabled — it is simply enabled', async () => {
    getRegistryEntries.mockResolvedValue(withPending({ enabled: true }))
    const w = mountPanel()
    await flushPromises()
    expect(rowFor(w, 'acme-flow').text()).toContain('enabled')
    expect(rowFor(w, 'acme-flow').text()).not.toContain('pending')
    expect(w.find('[data-testid="pending-count"]').exists()).toBe(false)
  })

  it('is revoked, not pending, when the publisher key was withdrawn', async () => {
    // A revocation is not a review queue. BR-AS49 outranks it.
    getRegistryEntries.mockResolvedValue(withPending({ withheld: true }))
    const w = mountPanel()
    await flushPromises()
    expect(rowFor(w, 'acme-flow').text()).toContain('revoked')
    expect(rowFor(w, 'acme-flow').text()).not.toContain('pending')
  })

  it('offers enable on a pending entry, which is the whole review action', async () => {
    getRegistryEntries.mockResolvedValue(withPending())
    setRegistryEntryEnabled.mockResolvedValue(withPending({ enabled: true }))
    const w = mountPanel()
    await flushPromises()
    await rowFor(w, 'acme-flow').get('[data-testid="toggle-enabled"]').trigger('click')
    await flushPromises()
    expect(setRegistryEntryEnabled).toHaveBeenCalledWith('acme-flow', true, 50)
  })
})

// Phase 8c — adding an entry by hand. The panel could read and write entries
// but never create one, which left the curated tier reachable only by seeding a
// file. The id and route prefix are editable exactly once: afterwards the id is
// what every audit row and every shell's held catalogue refers to, and the
// route prefix is something a shell may already have placed.
describe('FrontendPluginsPanel — adding an entry by hand', () => {
  it('opens an empty drawer', async () => {
    const w = mountPanel()
    await flushPromises()
    await w.get('[data-testid="add-entry"]').trigger('click')
    expect(w.get('[data-testid="entry-new-id"]').element.value).toBe('')
    expect(w.get('[data-testid="entry-name"]').element.value).toBe('')
  })

  it('lets the id and route prefix be set while creating, and never after', async () => {
    const w = mountPanel()
    await flushPromises()
    await w.get('[data-testid="add-entry"]').trigger('click')
    expect(w.get('[data-testid="entry-new-id"]').attributes('disabled')).toBeUndefined()
    expect(w.get('[data-testid="entry-new-route"]').attributes('disabled')).toBeUndefined()

    await w.get('[data-testid="add-entry"]').trigger('click')
    await w.findAll('[data-testid="edit-entry"]')[0].trigger('click')
    expect(w.get('[data-testid="entry-new-id"]').attributes('disabled')).toBeDefined()
    expect(w.get('[data-testid="entry-new-route"]').attributes('disabled')).toBeDefined()
  })

  it('writes the new entry disabled, against the current revision', async () => {
    // Adding an entry and serving it to every shell are two decisions, taken
    // one at a time — the same reason a publisher cannot self-activate.
    upsertRegistryEntry.mockResolvedValue(doc())
    const w = mountPanel()
    await flushPromises()
    await w.get('[data-testid="add-entry"]').trigger('click')
    await w.get('[data-testid="entry-new-id"]').setValue('by-hand')
    await w.get('[data-testid="entry-name"]').setValue('By Hand')
    await w.get('[data-testid="entry-new-route"]').setValue('/by-hand')
    await w.get('[data-testid="entry-url"]').setValue('https://plugins.acme.internal/x/remoteEntry.js')
    await w.get('[data-testid="entry-save"]').trigger('click')
    await flushPromises()

    const [entry, rev] = upsertRegistryEntry.mock.calls[0]
    expect(entry.id).toBe('by-hand')
    expect(entry.routePrefix).toBe('/by-hand')
    expect(entry.enabled).toBe(false)
    expect(rev).toBe(50)
  })

  it('sends no read-side or derived fields back to the server', async () => {
    // `conforming`, `source` and `registeredBy` are the server's answers about
    // the entry, not part of it. `creating` is this panel's own bookkeeping.
    upsertRegistryEntry.mockResolvedValue(doc())
    const w = mountPanel()
    await flushPromises()
    await w.get('[data-testid="add-entry"]').trigger('click')
    await w.get('[data-testid="entry-new-id"]').setValue('by-hand')
    await w.get('[data-testid="entry-save"]').trigger('click')
    await flushPromises()

    const [entry] = upsertRegistryEntry.mock.calls[0]
    expect(entry).not.toHaveProperty('creating')
    expect(entry).not.toHaveProperty('conforming')
    expect(entry).not.toHaveProperty('source')
    expect(entry).not.toHaveProperty('registeredBy')
  })
})

describe('BR-AS52 — the withdrawal class an operator can see and set', () => {
  const rowFor = (w, id) => w.findAll('[data-testid="entry-row"]').find((r) => r.text().includes(id))

  it('reads an unclassified entry as static rather than showing a blank', async () => {
    // The service backfills a legacy row on migration, but a document read
    // before that must not leave the column empty: a blank cell among
    // filled ones reads as a rendering fault, not as "we do not know".
    const d = doc()
    delete d.plugins[0].lifecycle
    getRegistryEntries.mockResolvedValue(d)
    const w = mountPanel()
    await flushPromises()
    expect(rowFor(w, 'seafreight-flow').get('[data-testid="entry-lifecycle"]').text()).toBe('static')
    w.unmount()
  })

  it('shows each stated class in its own word', async () => {
    const d = doc()
    d.plugins[0].lifecycle = 'static'
    d.plugins[1].lifecycle = 'dynamic'
    getRegistryEntries.mockResolvedValue(d)
    const w = mountPanel()
    await flushPromises()
    expect(rowFor(w, 'seafreight-flow').get('[data-testid="entry-lifecycle"]').text()).toBe('static')
    expect(rowFor(w, 'example-plugin-slow').get('[data-testid="entry-lifecycle"]').text()).toBe('dynamic')
    w.unmount()
  })

  it('sends the edited class with the write', async () => {
    const d = doc()
    d.plugins[0].lifecycle = 'static'
    getRegistryEntries.mockResolvedValue(d)
    upsertRegistryEntry.mockResolvedValue(d)
    const w = mountPanel()
    await flushPromises()
    await rowFor(w, 'seafreight-flow').get('[data-testid="edit-entry"]').trigger('click')
    await w.get('[data-testid="entry-lifecycle-input"]').setValue('dynamic')
    await w.get('[data-testid="entry-save"]').trigger('click')
    await flushPromises()
    const [entry] = upsertRegistryEntry.mock.calls[0]
    expect(entry.lifecycle).toBe('dynamic')
    w.unmount()
  })

  it('says a class change only takes effect when a shell reloads', async () => {
    // Q12: a running shell keeps the class it admitted. Saying so on the
    // screen that makes the change is the difference between an operator
    // waiting and an operator filing a bug.
    const w = mountPanel()
    await flushPromises()
    await w.findAll('[data-testid="edit-entry"]')[0].trigger('click')
    expect(w.get('[data-testid="entry-lifecycle-note"]').text()).toMatch(/reload/i)
    w.unmount()
  })

  it('offers only the two classes the shell has behavior for', async () => {
    const w = mountPanel()
    await flushPromises()
    await w.findAll('[data-testid="edit-entry"]')[0].trigger('click')
    const options = w.get('[data-testid="entry-lifecycle-input"]').findAll('option').map((o) => o.element.value)
    expect(options).toEqual(['static', 'dynamic'])
    w.unmount()
  })
})
