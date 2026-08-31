import PrimeVue from 'primevue/config'
import { mount, flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import RegistryPublishersPanel from './RegistryPublishersPanel.vue'

// Phase 7b — the publisher trust table's operator surface. What these specs
// hold in place is the two separations the panel exists to make visible:
//
//   · a publisher holds many keys, so keys nest under a publisher rather than
//     standing alone as identities (decision 103);
//   · retired and revoked are different in kind, and are shown, counted and
//     acted on apart (BR-AS38).
//
// Plus BR-AS46: ownership is a stated column with its own transfer, and
// BR-AS18: a stale write is refused with a reload, never merged.

vi.mock('../api', () => ({
  getRegistryPublishers: vi.fn(),
  upsertPublisher: vi.fn(),
  addPublisherKey: vi.fn(),
  setPublisherKeyState: vi.fn(),
  transferPlugin: vi.fn(),
}))

vi.mock('../nats/usePlatformConnection.js', () => ({
  usePlatformConnection: () => ({ epoch: { value: 0 } }),
}))

import {
  addPublisherKey,
  getRegistryPublishers,
  setPublisherKeyState,
  transferPlugin,
  upsertPublisher,
} from '../api'

const KEY_OLD = 'UAOLDKEYAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAOLD1'
const KEY_NEW = 'UANEWKEYBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBNEW2'
const KEY_BAD = 'UABADKEYCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCBAD3'

const DOC = {
  revision: 7,
  publishers: [
    {
      id: 'platform-team',
      name: 'Platform Team',
      plugins: ['pricing-plugin', 'seafreight-flow'],
      keys: [
        { publicKey: KEY_NEW, state: 'enabled', addedAt: '2026-08-30T09:00:00Z' },
        { publicKey: KEY_OLD, state: 'retired', addedAt: '2026-01-04T09:00:00Z' },
      ],
    },
    {
      id: 'partner-co',
      name: 'Partner Co',
      plugins: [],
      keys: [{ publicKey: KEY_BAD, state: 'revoked', addedAt: '2026-05-11T09:00:00Z' }],
    },
  ],
}

const doc = () => JSON.parse(JSON.stringify(DOC))
const mountPanel = () => mount(RegistryPublishersPanel, { global: { plugins: [PrimeVue] } })

beforeEach(() => {
  vi.clearAllMocks()
  getRegistryPublishers.mockResolvedValue(doc())
  setPublisherKeyState.mockResolvedValue(doc())
  addPublisherKey.mockResolvedValue(doc())
  transferPlugin.mockResolvedValue(doc())
  upsertPublisher.mockResolvedValue(doc())
})

describe('RegistryPublishersPanel', () => {
  it('nests every key under the publisher that holds it', async () => {
    const w = mountPanel()
    await flushPromises()
    expect(w.findAll('[data-testid="publisher-row"]')).toHaveLength(2)
    expect(w.findAll('[data-testid="publisher-key-row"]')).toHaveLength(3)
    expect(w.findAll('[data-testid="publisher-id"]').map((n) => n.text())).toEqual([
      'platform-team',
      'partner-co',
    ])
  })

  it('shows revoked apart from retired rather than lumping both into "not enabled"', async () => {
    const w = mountPanel()
    await flushPromises()
    expect(w.find('[data-testid="key-state-retired"]').text()).toContain('retired')
    expect(w.find('[data-testid="key-state-revoked"]').text()).toContain('revoked')
    // The distinction is stated, not just coloured: a retired key's work stands.
    expect(w.find('[data-testid="key-state-retired"]').element.parentElement.textContent)
      .toContain('what it signed stays valid')
    expect(w.find('[data-testid="key-state-revoked"]').element.parentElement.textContent)
      .toContain('trust withdrawn')
  })

  it('counts revoked keys on their own, so a leaked key is visible at a glance', async () => {
    const w = mountPanel()
    await flushPromises()
    expect(w.find('[data-testid="publisher-revoked-count"]').text()).toContain('1')
  })

  it('shows the trust table revision, which is not the plugin document revision', async () => {
    const w = mountPanel()
    await flushPromises()
    expect(w.find('[data-testid="publisher-revision"]').text()).toBe('7')
  })

  it('states ownership as plugin ids on the publisher, never derived from an origin', async () => {
    const w = mountPanel()
    await flushPromises()
    expect(w.findAll('[data-testid="publisher-plugin"]').map((n) => n.text())).toEqual([
      'pricing-plugin',
      'seafreight-flow',
    ])
    expect(w.find('[data-testid="publisher-ownership-note"]').exists()).toBe(true)
  })

  it('offers retire and revoke on an enabled key, and only revoke on a retired one', async () => {
    const w = mountPanel()
    await flushPromises()
    expect(w.find(`[data-testid="retire-${KEY_NEW}"]`).exists()).toBe(true)
    expect(w.find(`[data-testid="revoke-${KEY_NEW}"]`).exists()).toBe(true)
    // Retiring what is already retired is not an action; revoking it still is.
    expect(w.find(`[data-testid="retire-${KEY_OLD}"]`).exists()).toBe(false)
    expect(w.find(`[data-testid="revoke-${KEY_OLD}"]`).exists()).toBe(true)
  })

  it('offers a revoked key restoration instead of a second revoke', async () => {
    const w = mountPanel()
    await flushPromises()
    expect(w.find(`[data-testid="revoke-${KEY_BAD}"]`).exists()).toBe(false)
    expect(w.find(`[data-testid="restore-${KEY_BAD}"]`).exists()).toBe(true)
  })

  it('writes a key state change against the revision it was read at', async () => {
    const w = mountPanel()
    await flushPromises()
    await w.find(`[data-testid="revoke-${KEY_NEW}"]`).trigger('click')
    await flushPromises()
    expect(setPublisherKeyState).toHaveBeenCalledWith('platform-team', KEY_NEW, 'revoked', 7)
  })

  it('transfers a plugin id on its own, carrying no key with it', async () => {
    const w = mountPanel()
    await flushPromises()
    await w.findAll('button').find((b) => b.text() === 'Transfer plugin').trigger('click')
    await w.find('[data-testid="field-plugin-id"]').setValue('pricing-plugin')
    await w.find('[data-testid="publisher-form-save"]').trigger('click')
    await flushPromises()
    expect(transferPlugin).toHaveBeenCalledWith('platform-team', 'pricing-plugin', 7)
    expect(addPublisherKey).not.toHaveBeenCalled()
    expect(upsertPublisher).not.toHaveBeenCalled()
  })

  it('adds a key to the publisher whose row opened the form', async () => {
    const w = mountPanel()
    await flushPromises()
    await w.findAll('button').filter((b) => b.text() === 'Add key')[1].trigger('click')
    await w.find('[data-testid="field-public-key"]').setValue(KEY_NEW)
    await w.find('[data-testid="publisher-form-save"]').trigger('click')
    await flushPromises()
    expect(addPublisherKey).toHaveBeenCalledWith('partner-co', KEY_NEW, 7)
  })

  it('offers a reload rather than a merge when the table moved on mid-edit', async () => {
    const w = mountPanel()
    await flushPromises()
    setPublisherKeyState.mockRejectedValueOnce({
      conflict: true,
      body: { yourRevision: 7, currentRevision: 9 },
      message: 'stale',
    })
    await w.find(`[data-testid="revoke-${KEY_NEW}"]`).trigger('click')
    await flushPromises()
    const stale = w.find('[data-testid="publisher-stale"]')
    expect(stale.exists()).toBe(true)
    expect(stale.text()).toContain('9')
    expect(w.find('[data-testid="publisher-stale-reload"]').exists()).toBe(true)
  })

  it('shows a write refusal inside the form that caused it, not as a page error', async () => {
    const w = mountPanel()
    await flushPromises()
    addPublisherKey.mockRejectedValueOnce({ message: 'registry: that is not a valid publisher key' })
    await w.findAll('button').filter((b) => b.text() === 'Add key')[0].trigger('click')
    await w.find('[data-testid="field-public-key"]').setValue('nonsense')
    await w.find('[data-testid="publisher-form-save"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-testid="publisher-form-refused"]').text()).toContain('not a valid publisher key')
    // The form stays open on a refusal — the operator fixes the value in place.
    expect(w.find('[data-testid="publisher-form"]').exists()).toBe(true)
  })

  it('offers no way to delete a publisher or a key', async () => {
    const w = mountPanel()
    await flushPromises()
    const labels = w.findAll('button').map((b) => b.text().toLowerCase())
    expect(labels.some((l) => l.includes('delete') || l.includes('remove'))).toBe(false)
  })

  it('says plainly that nothing is admitted while the table is empty', async () => {
    getRegistryPublishers.mockResolvedValue({ revision: 0, publishers: [] })
    const w = mountPanel()
    await flushPromises()
    expect(w.text()).toContain('No publishers are trusted yet')
  })
})
