import PrimeVue from 'primevue/config'
import { mount, flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import AccountActivityPanel from './AccountActivityPanel.vue'

// BR-034 (Main-POC-Plan.md Phase 27) — Account Activity panel proxies
// /accstatz per account. The one deliberate rule under test here: slow
// consumers gets no routine tile/stat at zero (silent, matching
// ServicesPanel's .dot.ok convention) and turns into a named alarm — red dot,
// tinted card border, a "slow" stat replacing "subs", and an alarm line in
// the expansion — the moment it's nonzero.

vi.mock('../api', () => ({
  getNatsAccountActivity: vi.fn(),
}))

import { getNatsAccountActivity } from '../api'

const HEALTHY_ACCOUNTS = [
  {
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
  },
  {
    account: 'AAFACMEKEY',
    tenantLabel: 'acme',
    connections: 4,
    leafNodes: 0,
    totalConnections: 4,
    subscriptions: 143,
    inMsgs: 475,
    inBytes: 197_000,
    outMsgs: 113,
    outBytes: 21_000,
    slowConsumers: 0,
  },
]

function mountPanel() {
  return mount(AccountActivityPanel, {
    global: { plugins: [PrimeVue] },
  })
}

describe('AccountActivityPanel', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    getNatsAccountActivity.mockResolvedValue({ accounts: HEALTHY_ACCOUNTS })
  })

  it('renders the resolved tenantLabel as the account name', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    const names = wrapper.findAll('.acct-name').map((el) => el.text())
    expect(names).toEqual(['PLATFORM', 'acme'])
  })

  it('falls back to a truncated raw account NKey when no tenantLabel was resolved', async () => {
    getNatsAccountActivity.mockResolvedValue({
      accounts: [{ ...HEALTHY_ACCOUNTS[0], tenantLabel: undefined, account: 'AAFUNRESOLVEDKEY' }],
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

  it('gives every summary card value one type treatment', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    const values = wrapper.findAll('.summary-value')
    expect(values).toHaveLength(4)
    values.forEach((v) => expect(v.classes()).toEqual(['summary-value']))
  })

  it('says nothing about slow consumers while every account is healthy', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    // No routine "0 slow" tile competing with real numbers, and no banner.
    expect(wrapper.find('.alarm-banner').exists()).toBe(false)
    expect(wrapper.find('.stat.crit').exists()).toBe(false)
    expect(wrapper.find('.dot.crit').exists()).toBe(false)
    // Healthy accounts show a "subs" stat instead.
    const subLabels = wrapper.findAll('.stat label').map((el) => el.text())
    expect(subLabels).toContain('subs')
  })

  it('flags an account with slow_consumers > 0: red dot, tinted card, slow stat replacing subs', async () => {
    getNatsAccountActivity.mockResolvedValue({
      accounts: [...HEALTHY_ACCOUNTS, { ...HEALTHY_ACCOUNTS[1], account: 'AAFGLOBEXKEY', tenantLabel: 'globex', slowConsumers: 2 }],
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

  it('opens an alarm line in the expansion for a flagged account', async () => {
    getNatsAccountActivity.mockResolvedValue({
      accounts: [{ ...HEALTHY_ACCOUNTS[1], account: 'AAFGLOBEXKEY', tenantLabel: 'globex', slowConsumers: 2 }],
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

  it('expands a healthy account with no alarm line present', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    await wrapper.findAll('.acct-head')[0].trigger('click')
    await flushPromises()

    expect(wrapper.find('.detail').exists()).toBe(true)
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
})
