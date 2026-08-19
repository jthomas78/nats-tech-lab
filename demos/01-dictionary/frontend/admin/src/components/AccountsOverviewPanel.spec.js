import PrimeVue from 'primevue/config'
import { mount, flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import AccountsOverviewPanel from './AccountsOverviewPanel.vue'

// BR-034 (carried over from the old AccountActivityPanel, now this tab) —
// slow consumers gets no routine tile/stat at zero and turns into a named
// alarm the moment it's nonzero. BR-043/BR-044 (Phase 45) — the duration
// selector re-fetches history, and the search box is gated on account count.

vi.mock('../api', () => ({
  getNatsAccountActivity: vi.fn(),
  getNatsAccountActivityHistory: vi.fn(),
}))

import { getNatsAccountActivity, getNatsAccountActivityHistory } from '../api'

function acct(overrides) {
  return {
    account: 'AAFPLATFORMKEY',
    tenantLabel: 'PLATFORM',
    connections: 5,
    leafNodes: 0,
    totalConnections: 5,
    subscriptions: 117,
    inMsgs: 2510,
    inBytes: 650_000,
    outMsgs: 664,
    outBytes: 285_000,
    slowConsumers: 0,
    ...overrides,
  }
}

const HEALTHY_ACCOUNTS = [
  acct({}),
  acct({ account: 'AAFACMEKEY', tenantLabel: 'acme', connections: 4, subscriptions: 143, inMsgs: 475, inBytes: 197_000, outMsgs: 113, outBytes: 21_000 }),
]

function mountPanel() {
  return mount(AccountsOverviewPanel, {
    global: { plugins: [PrimeVue] },
  })
}

describe('AccountsOverviewPanel', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    getNatsAccountActivity.mockResolvedValue({ accounts: HEALTHY_ACCOUNTS })
    getNatsAccountActivityHistory.mockResolvedValue({ duration: '30m', bucketSeconds: 120, accounts: [] })
  })

  it('renders the resolved tenantLabel as the account name', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    const names = wrapper.findAll('.acct-name').map((el) => el.text())
    expect(names).toEqual(['PLATFORM', 'acme'])
  })

  it('falls back to a truncated raw account NKey when no tenantLabel was resolved', async () => {
    getNatsAccountActivity.mockResolvedValue({
      accounts: [acct({ tenantLabel: undefined, account: 'AAFUNRESOLVEDKEY' })],
    })
    const wrapper = mountPanel()
    await flushPromises()

    const name = wrapper.find('.acct-name')
    expect(name.text()).toBe('AAFUNRESOL…')
    expect(name.attributes('title')).toBe('AAFUNRESOLVEDKEY')
  })

  it('tags reserved accounts (platform/sys) but not tenant accounts', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    const cards = wrapper.findAll('.acct-card')
    expect(cards[0].find('.acct-tag').exists()).toBe(true) // PLATFORM
    expect(cards[1].find('.acct-tag').exists()).toBe(false) // acme
  })

  it('says nothing about slow consumers while every account is healthy', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    expect(wrapper.find('.alarm-banner').exists()).toBe(false)
    expect(wrapper.find('.stat.crit').exists()).toBe(false)
    expect(wrapper.find('.dot.crit').exists()).toBe(false)
    const subLabels = wrapper.findAll('.stat label').map((el) => el.text())
    expect(subLabels).toContain('subs')
  })

  it('flags an account with slow_consumers > 0: red dot, tinted card, slow stat replacing subs', async () => {
    getNatsAccountActivity.mockResolvedValue({
      accounts: [...HEALTHY_ACCOUNTS, acct({ account: 'AAFGLOBEXKEY', tenantLabel: 'globex', slowConsumers: 2 })],
    })
    const wrapper = mountPanel()
    await flushPromises()

    expect(wrapper.find('.alarm-banner').text()).toContain('2 slow consumers')
    expect(wrapper.find('.alarm-banner').text()).toContain('1 account')

    const globexCard = wrapper.findAll('.acct-card').find((c) => c.text().includes('globex'))
    expect(globexCard.classes()).toContain('crit')
    expect(globexCard.find('.dot').classes()).toContain('crit')
    const slowStat = globexCard.findAll('.stat').find((s) => s.text().includes('slow'))
    expect(slowStat.find('b').text()).toBe('2')
  })

  it('opens an alarm line tied to the live slow-consumer count, not a scripted duration, in the expansion', async () => {
    getNatsAccountActivity.mockResolvedValue({
      accounts: [acct({ account: 'AAFGLOBEXKEY', tenantLabel: 'globex', slowConsumers: 2 })],
    })
    const wrapper = mountPanel()
    await flushPromises()

    await wrapper.find('.acct-head').trigger('click')
    await flushPromises()

    const alarm = wrapper.find('.alarm-line')
    expect(alarm.exists()).toBe(true)
    expect(alarm.text()).toContain('2 slow consumers')
    expect(alarm.text()).toContain('globex')
  })

  it('expands a healthy account into trend charts, not a restated number grid, with no alarm line', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    await wrapper.findAll('.acct-head')[0].trigger('click')
    await flushPromises()

    expect(wrapper.find('.detail').exists()).toBe(true)
    expect(wrapper.findAll('.chart-card')).toHaveLength(2)
    expect(wrapper.find('.alarm-line').exists()).toBe(false)
  })

  it('shows an error message when the fetch fails', async () => {
    getNatsAccountActivity.mockRejectedValue(new Error('boom'))
    const wrapper = mountPanel()
    await flushPromises()

    expect(wrapper.find('.err-line').text()).toContain('boom')
  })

  it('shows an empty state when no accounts are reported', async () => {
    getNatsAccountActivity.mockResolvedValue({ accounts: [] })
    const wrapper = mountPanel()
    await flushPromises()

    expect(wrapper.find('.empty-line').exists()).toBe(true)
  })

  // ── BR-043: duration selector ──────────────────────────────────────────
  it('defaults to a 30m trend window and re-fetches history when the selector changes', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    expect(getNatsAccountActivityHistory).toHaveBeenCalledWith('30m')
    const active = wrapper.find('.duration-btn.active')
    expect(active.text()).toBe('30m')

    const fiveMin = wrapper.findAll('.duration-btn').find((b) => b.text() === '5m')
    await fiveMin.trigger('click')
    await flushPromises()

    expect(getNatsAccountActivityHistory).toHaveBeenCalledWith('5m')
  })

  // ── BR-044: gated search ─────────────────────────────────────────────
  it('hides the search box at 3 or fewer accounts', async () => {
    getNatsAccountActivity.mockResolvedValue({ accounts: HEALTHY_ACCOUNTS })
    const wrapper = mountPanel()
    await flushPromises()

    expect(wrapper.find('.search-wrap').exists()).toBe(false)
  })

  it('shows the search box once there are more than 3 accounts and filters by name', async () => {
    getNatsAccountActivity.mockResolvedValue({
      accounts: [
        ...HEALTHY_ACCOUNTS,
        acct({ account: 'AAFGLOBEXKEY', tenantLabel: 'globex' }),
        acct({ account: 'AAFHOOLIKEY', tenantLabel: 'hooli' }),
      ],
    })
    const wrapper = mountPanel()
    await flushPromises()

    const search = wrapper.find('.search-wrap input')
    expect(search.exists()).toBe(true)
    expect(wrapper.find('.acct-count').text()).toBe('4 accounts')

    await search.setValue('o')
    await flushPromises()

    // "o" matches PLATFORM/globex/hooli (each contains a lowercase "o"),
    // not acme — a real substring match, not a coincidence of fixture order.
    const names = wrapper.findAll('.acct-name').map((el) => el.text())
    expect(names).toEqual(['PLATFORM', 'globex', 'hooli'])
    expect(wrapper.find('.acct-count').text()).toBe('3 of 4 accounts')
  })

  it('shows a named empty state, not a blank list, when the search matches nothing', async () => {
    getNatsAccountActivity.mockResolvedValue({
      accounts: [
        ...HEALTHY_ACCOUNTS,
        acct({ account: 'AAFGLOBEXKEY', tenantLabel: 'globex' }),
        acct({ account: 'AAFHOOLIKEY', tenantLabel: 'hooli' }),
      ],
    })
    const wrapper = mountPanel()
    await flushPromises()

    await wrapper.find('.search-wrap input').setValue('zzz')
    await flushPromises()

    expect(wrapper.find('.empty-line').text()).toContain('No accounts match "zzz"')
    expect(wrapper.findAll('.acct-card')).toHaveLength(0)
  })
})
