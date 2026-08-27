import PrimeVue from 'primevue/config'
import ToastService from 'primevue/toastservice'
import { mount, flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import AccountsPanel from './AccountsPanel.vue'

// BR-061 — an NKey is never rendered in full in the Admin UI.
//
// This panel is the reason the rule is a shared helper rather than a clause in
// any one panel's rule: its Public Key column used to carry a `slice(0, 12)…`
// of its own, a third truncation idiom for the same fact that ConnectionsPanel
// and AccountsOverviewPanel each drew differently again. One helper, one
// enforcement point.
//
// The rest of this panel (create/suspend/reactivate, limits, business units)
// is covered by accounts-service's own Go specs, which are where those rules
// actually live — this file exists for the presentation rule.

vi.mock('../api', () => ({
  createAccount: vi.fn(),
  createBusinessUnit: vi.fn(),
  getAccountsUsage: vi.fn(),
  listAccounts: vi.fn(),
  listBusinessUnits: vi.fn(),
  reactivateAccount: vi.fn(),
  suspendAccount: vi.fn(),
  updateAccountLimits: vi.fn(),
  updateBusinessUnit: vi.fn(),
}))

import { getAccountsUsage, listAccounts, listBusinessUnits } from '../api'

const KEY = 'ADD65MOJPAWSPKI4EAGTJXBWRWRXTEGKMSHTMDXHVDCH2Q2RTQMFPL9X'

const ACCOUNTS = [
  { name: 'acme', publicKey: KEY, status: 'active', limits: {} },
]

function mountPanel() {
  return mount(AccountsPanel, { global: { plugins: [PrimeVue, ToastService] } })
}

describe('AccountsPanel', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    listAccounts.mockResolvedValue(ACCOUNTS)
    getAccountsUsage.mockResolvedValue([])
    listBusinessUnits.mockResolvedValue([])
  })

  it('BR-061: renders the account public key elided, never in full', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    expect(wrapper.find('[data-testid="nkey"]').text()).toBe('[ADD65...FPL9X]')
    // The old `slice(0, 12)…` put twelve characters on screen with no way to
    // get the rest; this puts ten on screen and the whole key on the clipboard
    // from a detail pane. What must not happen either way is the full render.
    expect(wrapper.html()).not.toContain(KEY)
  })

  it('BR-061: offers no copy affordance in the table — a column identifies a row', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    expect(wrapper.findAll('.nk-copy')).toHaveLength(0)
  })
})
