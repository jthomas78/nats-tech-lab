import PrimeVue from 'primevue/config'
import { mount, flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

import JetStreamPanel from './JetStreamPanel.vue'

// Phase 38e follow-on — the stream rail became an honest stream-budget view:
// KV_* and OBJ_* backing streams are no longer dropped server-side, they
// arrive tagged with a `kind` and are opt-in per kind in the UI. ADR-048
// budgets against MaxStreams: 10, and this rail is the only place that
// number is visible, so hiding two of the three kinds outright made it
// unable to answer the question it exists for.
//
// These specs cover the frontend half: the opt-in default, the tag's
// conditional appearance, prefix stripping, and the two filter behaviours
// that would each read as a bug if guessed wrong (collapsed groups hiding
// their own matches; empty groups crowding a 250px rail).

vi.mock('../api', () => ({ listStreams: vi.fn() }))
vi.mock('./StreamView.vue', () => ({
  default: { name: 'StreamView', template: '<div class="stream-view-stub" />' },
}))

import { listStreams } from '../api'

const RESPONSE = {
  accounts: [
    { name: 'ACME', status: 'active' },
    { name: 'GLOBEX', status: 'active' },
  ],
  streams: [
    { account: 'ACME', stream: 'SHIPPING', kind: 'stream', messages: 0, subjects: 1 },
    { account: 'ACME', stream: 'TRANSPORTER', kind: 'stream', messages: 0, subjects: 1 },
    { account: 'ACME', stream: 'KV_organizations', kind: 'kv', messages: 6, subjects: 1 },
    { account: 'ACME', stream: 'OBJ_organizations-docs', kind: 'objstore', messages: 2, subjects: 2 },
    { account: 'GLOBEX', stream: 'REFDATA', kind: 'stream', messages: 11, subjects: 1 },
  ],
}

function railNames(wrapper) {
  return wrapper.findAll('.rail-item .rail-name').map((n) => n.text())
}

async function mountPanel() {
  const wrapper = mount(JetStreamPanel, { global: { plugins: [PrimeVue] } })
  await flushPromises()
  return wrapper
}

describe('JetStreamPanel — kind tags and filtering', () => {
  beforeEach(() => {
    vi.mocked(listStreams).mockReset()
    vi.mocked(listStreams).mockResolvedValue(RESPONSE)
  })

  it('hides kv and objstore rows by default and names what it withheld', async () => {
    const wrapper = await mountPanel()

    expect(railNames(wrapper)).toEqual(['SHIPPING', 'TRANSPORTER', 'REFDATA'])
    // Silently shorter would read as data having gone away.
    expect(wrapper.find('.hidden-note').text()).toBe('1 KV · 1 objstore hidden')
  })

  it('renders no kind tag while every visible row is the same kind', async () => {
    const wrapper = await mountPanel()

    expect(wrapper.findAll('.kind-tag')).toHaveLength(0)
  })

  it('reveals one kind at a time, tagging every row once the list is mixed', async () => {
    const wrapper = await mountPanel()

    await wrapper.find('.chip[data-k="objstore"]').trigger('click')

    expect(railNames(wrapper)).toContain('organizations-docs')
    expect(railNames(wrapper)).not.toContain('organizations') // kv still off
    // Mixed list — now every row states its kind, streams included.
    expect(wrapper.findAll('.kind-tag')).toHaveLength(4)
    expect(wrapper.find('.kind-tag.objstore').text()).toBe('obj')
  })

  it('strips the OBJ_/KV_ prefix for display once the tag carries it', async () => {
    const wrapper = await mountPanel()
    await wrapper.find('.chip[data-k="kv"]').trigger('click')

    // The rail is 250px; the raw name is also not the name configured in
    // the owning service. The `stream` field keeps it for selection.
    expect(railNames(wrapper)).toContain('organizations')
    expect(railNames(wrapper)).not.toContain('KV_organizations')
  })

  it('drops accounts with no match while filtering, and restores them when it clears', async () => {
    const wrapper = await mountPanel()

    await wrapper.find('.search-box input').setValue('shipping')
    expect(wrapper.findAll('.account-head')).toHaveLength(1)
    expect(railNames(wrapper)).toEqual(['SHIPPING'])
    expect(wrapper.find('.rail-summary').text()).toContain('1 of 3 streams')

    await wrapper.find('.search-box input').setValue('')
    expect(wrapper.findAll('.account-head')).toHaveLength(2)
  })

  it('auto-expands a collapsed account so a filter cannot hide its own match', async () => {
    const wrapper = await mountPanel()

    await wrapper.findAll('.account-head')[0].trigger('click')
    expect(railNames(wrapper)).toEqual(['REFDATA']) // ACME collapsed

    await wrapper.find('.search-box input').setValue('transporter')
    expect(railNames(wrapper)).toEqual(['TRANSPORTER'])

    // The user's own collapse choice is borrowed, never overwritten.
    await wrapper.find('.search-box input').setValue('')
    expect(railNames(wrapper)).toEqual(['REFDATA'])
  })

  it('says so when a filter matches nothing, rather than looking like an empty backend', async () => {
    const wrapper = await mountPanel()

    await wrapper.find('.search-box input').setValue('nothing-matches-this')
    expect(wrapper.text()).toContain('No stream name matches')
  })

  it('shows a placeholder instead of StreamView for a kv or objstore row', async () => {
    const wrapper = await mountPanel()
    await wrapper.find('.chip[data-k="kv"]').trigger('click')

    const kvRow = wrapper.findAll('.rail-item').find((r) => r.text().includes('organizations'))
    await kvRow.trigger('click')

    // StreamView would render key revisions as if they were events.
    expect(wrapper.find('.stream-view-stub').exists()).toBe(false)
    expect(wrapper.find('.kind-placeholder').text()).toContain('KV bucket')
  })
})

// The rail is an overflow-y:auto flex column, and every .account-group inside
// it is `overflow: hidden`. A flex item in a column container shrinks by
// default, so without an explicit flex-shrink:0 the groups compress to fit the
// rail's height instead of overflowing it — the last rows of each group get
// clipped and there is no scrollback to reach them. Reported against the fully
// expanded rail with both the KV and OBJSTORE filters on, which is exactly
// when total content first exceeds the rail. jsdom does not apply <style
// scoped>, so this asserts against the SFC's own CSS block.
describe('JetStreamPanel — rail overflow', () => {
  it('keeps account groups from shrinking, so a full rail scrolls instead of clipping', () => {
    const sfc = readFileSync(resolve('src/components/JetStreamPanel.vue'), 'utf8')
    const rule = /\.account-group\s*\{([^}]*)\}/.exec(sfc)

    expect(rule).not.toBeNull()
    expect(rule[1]).toMatch(/flex-shrink:\s*0\s*;|flex:\s*none\s*;/)
  })
})
