import PrimeVue from 'primevue/config'
import { mount, flushPromises } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import UsersPanel from './UsersPanel.vue'

// BR-060 (Phase 50c) — the Users panel's presentation rules:
//
//   · Health reads the user's own record (registry status + JWT expiry) and
//     never its connection count, so a credential nothing is using is
//     `valid`, not a warning.
//   · A `session` row is enumerable, because BR-AC38's registry records one
//     at mint time — the completeness caveat that used to attach to sessions
//     now attaches to the /connz JOIN, which is one page and says so.
//   · A grant the server discards (scoped signing key, BR-AC41) is rendered
//     struck through beneath the effective set, never omitted and never
//     mixed in with it.
//
// The backend halves have their own Go coverage (userclaims_test.go,
// usersrpc_test.go); this file covers the presentation the rule is about.

vi.mock('../api', () => ({
  listNatsUsers: vi.fn(),
  getNatsConnections: vi.fn(),
  getNatsUser: vi.fn(),
}))

import { getNatsConnections, getNatsUser, listNatsUsers } from '../api'

const NOW = new Date('2026-08-27T08:00:00Z').getTime()
const iso = (msFromNow) => new Date(NOW + msFromNow).toISOString()

const USERS = [
  {
    publicKey: 'UCRED_SHIPPING',
    name: 'shipping-admin',
    account: 'platform',
    accountKey: 'APLATFORM',
    kind: 'credential',
    status: 'active',
    bearer: false,
    source: 'bootstrap',
    issuedAt: iso(-86400000),
    // No expiresAt — an nsc credential carries no exp claim.
  },
  {
    publicKey: 'UCRED_SYS',
    name: 'sys',
    account: 'sys',
    kind: 'credential',
    status: 'active',
    bearer: false,
    source: 'bootstrap',
    issuedAt: iso(-86400000),
  },
  {
    publicKey: 'USESSION_SEAFREIGHT',
    name: 'seafreight-app',
    account: 'acme',
    kind: 'session',
    status: 'active',
    bearer: false,
    source: 'minted',
    issuedAt: iso(-1000),
    expiresAt: iso(12 * 60000), // 12 minutes out — inside the 1h window
  },
  {
    publicKey: 'USESSION_STALE',
    name: 'stale-app',
    account: 'acme',
    kind: 'session',
    status: 'active',
    bearer: false,
    source: 'minted',
    issuedAt: iso(-7200000),
    expiresAt: iso(-60000),
  },
]

// Only shipping-admin is connected. sys is a valid credential with nothing
// using it — the case Health must NOT report as a problem.
const CONNECTIONS = [
  { cid: 1, userKey: 'UCRED_SHIPPING', subscriptions: 2 },
  { cid: 2, userKey: 'UCRED_SHIPPING', subscriptions: 3 },
  { cid: 3, userKey: 'USESSION_SEAFREIGHT', subscriptions: 6 },
]

const SCOPED_VIEW = {
  publicKey: 'USESSION_SEAFREIGHT',
  name: 'seafreight-app',
  account: 'acme',
  kind: 'session',
  status: 'active',
  bearer: false,
  source: 'minted',
  issuedAt: iso(-1000),
  expiresAt: iso(12 * 60000),
  issuerKey: 'ASIGNINGKEY',
  scoped: true,
  scopeRole: 'browser',
  effective: {
    pub: { allow: ['api.acme.>', '_INBOX.>'] },
    sub: { allow: ['api.acme.>'], deny: ['$JS.API.>'] },
    subs: 50,
    payload: 1048576,
    allowed_connection_types: ['WEBSOCKET'],
  },
  jwtGrants: { pub: { allow: ['api.>'] }, sub: { allow: ['api.>'] } },
}

const UNSCOPED_VIEW = {
  publicKey: 'UCRED_SHIPPING',
  name: 'shipping-admin',
  account: 'platform',
  kind: 'credential',
  status: 'active',
  bearer: false,
  source: 'bootstrap',
  issuedAt: iso(-86400000),
  issuerKey: 'APLATFORM',
  scoped: false,
  effective: { pub: { allow: ['$SRV.>'] }, sub: { allow: ['_INBOX.>'] } },
}

function mountPanel() {
  return mount(UsersPanel, { global: { plugins: [PrimeVue] } })
}

function rowTexts(wrapper) {
  return wrapper.findAll('tbody tr').map((r) => r.text())
}

describe('UsersPanel', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.setSystemTime(NOW)
    vi.clearAllMocks()
    listNatsUsers.mockResolvedValue(USERS)
    getNatsConnections.mockResolvedValue({
      connections: CONNECTIONS,
      page: { numConnections: 3, total: 3, offset: 0, limit: 1024 },
    })
    getNatsUser.mockResolvedValue(UNSCOPED_VIEW)
  })
  afterEach(() => vi.useRealTimers())

  it('lists credentials and sessions in one roster, credentials first', async () => {
    const w = mountPanel()
    await flushPromises()
    const texts = rowTexts(w)
    expect(texts).toHaveLength(4)
    expect(texts[0]).toContain('shipping-admin')
    expect(texts[1]).toContain('sys')
    // Sessions are enumerable now that the registry records them at mint
    // time (BR-AC38) — the pre-50a reading of BR-060, where a session existed
    // only while it was connected, is what the registry replaced.
    expect(texts.some((t) => t.includes('seafreight-app'))).toBe(true)
    expect(texts.some((t) => t.includes('stale-app'))).toBe(true)
  })

  // BR-060 — health reads the record, not the connection.
  it('reports a credential with no connections as valid, not as a problem', async () => {
    const w = mountPanel()
    await flushPromises()
    const sysRow = w.findAll('tbody tr').find((r) => r.text().includes('sys'))
    expect(sysRow.find('[data-testid="health"]').text()).toBe('valid')
    // ...and its connection count is where "nothing is using it" is said.
    expect(sysRow.findAll('td').at(-2).text()).toBe('0')
    expect(sysRow.findAll('td').at(-1).text()).toBe('0')
  })

  it('derives expiring and expired health from the JWT expiry', async () => {
    const w = mountPanel()
    await flushPromises()
    const rows = w.findAll('tbody tr')
    const seafreight = rows.find((r) => r.text().includes('seafreight-app'))
    const stale = rows.find((r) => r.text().includes('stale-app'))
    expect(seafreight.find('[data-testid="health"]').text()).toBe('expiring')
    expect(seafreight.text()).toContain('in 12m')
    expect(stale.find('[data-testid="health"]').text()).toBe('expired')
  })

  it('reports a pending row as pending rather than hiding it (BR-AC38)', async () => {
    listNatsUsers.mockResolvedValue([{ ...USERS[1], publicKey: 'UPENDING', name: 'half-minted', status: 'pending' }])
    const w = mountPanel()
    await flushPromises()
    const row = w.find('tbody tr')
    expect(row.text()).toContain('half-minted')
    expect(row.find('[data-testid="health"]').text()).toBe('pending')
  })

  it('joins live connection and subscription counts on the user NKey', async () => {
    const w = mountPanel()
    await flushPromises()
    const shipping = w.findAll('tbody tr').find((r) => r.text().includes('shipping-admin'))
    // Two connections share the one credential — the BR-058 case: the join is
    // on the NKey, and both connections land on the same row.
    expect(shipping.findAll('td').at(-2).text()).toBe('2')
    expect(shipping.findAll('td').at(-1).text()).toBe('5')
  })

  it('keeps the roster when /connz fails, since it is the counts that were lost', async () => {
    getNatsConnections.mockRejectedValue(new Error('connz unreachable'))
    const w = mountPanel()
    await flushPromises()
    expect(rowTexts(w)).toHaveLength(4)
    expect(w.text()).not.toContain('connz unreachable')
  })

  // BR-060 — the completeness caveat, now on the join rather than on sessions.
  it('says so when /connz paged, because a row then undercounts', async () => {
    getNatsConnections.mockResolvedValue({
      connections: CONNECTIONS,
      page: { numConnections: 3, total: 900, offset: 0, limit: 3 },
    })
    const w = mountPanel()
    await flushPromises()
    expect(w.text()).toContain('/connz paged')
  })

  it('does not claim a partial join in the steady state', async () => {
    const w = mountPanel()
    await flushPromises()
    expect(w.text()).not.toContain('/connz paged')
  })

  it('filters by kind and by free text', async () => {
    const w = mountPanel()
    await flushPromises()
    await w.findAll('.chip').find((c) => c.text() === 'session').trigger('click')
    expect(rowTexts(w)).toHaveLength(2)
    await w.find('.search-box input').setValue('seafreight')
    expect(rowTexts(w)).toHaveLength(1)
  })

  // BR-AC40 — the roster carries no permissions, so a drill-in is a second call.
  it('fetches one user’s claims on row click', async () => {
    const w = mountPanel()
    await flushPromises()
    await w.findAll('tbody tr')[0].trigger('click')
    await flushPromises()
    expect(getNatsUser).toHaveBeenCalledWith('UCRED_SHIPPING')
    const detail = w.find('[data-testid="user-detail"]')
    expect(detail.exists()).toBe(true)
    expect(detail.text()).toContain('$SRV.>')
    expect(detail.text()).toContain('no — JWT permissions apply')
    // Nothing was discarded, so nothing is struck through — an identical
    // struck-through copy would imply an override that did not happen.
    expect(detail.findAll('[data-testid="discarded"]')).toHaveLength(0)
    expect(detail.find('[data-testid="override"]').exists()).toBe(false)
  })

  // BR-AC41 — the rule this panel exists to not violate.
  it('shows the scope template as effective and strikes through the discarded JWT grants', async () => {
    getNatsUser.mockResolvedValue(SCOPED_VIEW)
    const w = mountPanel()
    await flushPromises()
    await w.findAll('tbody tr').find((r) => r.text().includes('seafreight-app')).trigger('click')
    await flushPromises()

    const detail = w.find('[data-testid="user-detail"]')
    expect(detail.text()).toContain('yes — template applies')
    expect(detail.text()).toContain('browser')
    expect(detail.find('[data-testid="override"]').exists()).toBe(true)

    const struck = detail.findAll('[data-testid="discarded"]')
    expect(struck).toHaveLength(2)
    struck.forEach((r) => expect(r.text()).toContain('not enforced'))
    // The discarded grant is present but never presented as enforced: the
    // effective rows above it must not contain it.
    const effective = detail.findAll('.claims tbody tr').filter((r) => !r.classes('struck'))
    expect(effective.map((r) => r.find('.subject').text())).toEqual([
      'api.acme.>',
      '_INBOX.>',
      'api.acme.>',
      '$JS.API.>',
    ])
    expect(effective.map((r) => r.find('.eff').text())).toEqual(['ALLOW', 'ALLOW', 'ALLOW', 'DENY'])
    expect(detail.text()).toContain('Max subs 50')
    expect(detail.text()).toContain('1 MiB')
    expect(detail.text()).toContain('WEBSOCKET')
    // An absent limit is unlimited, not zero (subs/data/payload carry omitempty).
    expect(detail.text()).toContain('Data unlimited')
  })

  it('repeats the service’s own reason when a scope could not be resolved', async () => {
    getNatsUser.mockResolvedValue({
      ...UNSCOPED_VIEW,
      effective: null,
      unresolved: 'this service has no record of the permissions this user was granted',
    })
    const w = mountPanel()
    await flushPromises()
    await w.findAll('tbody tr')[0].trigger('click')
    await flushPromises()
    const detail = w.find('[data-testid="user-detail"]')
    expect(detail.find('[data-testid="unresolved"]').text()).toContain('no record of the permissions')
    expect(detail.text()).toContain('no permissions recorded')
    // Crucially: nothing is labelled effective when nothing could be resolved.
    expect(detail.findAll('.claims').length).toBe(0)
  })

  it('surfaces a failed claims read on the row instead of an empty pane', async () => {
    getNatsUser.mockRejectedValue(new Error('no responders'))
    const w = mountPanel()
    await flushPromises()
    await w.findAll('tbody tr')[0].trigger('click')
    await flushPromises()
    expect(w.find('[data-testid="user-detail"]').text()).toContain('no responders')
  })

  it('counts bearer credentials, because a yes is the finding', async () => {
    listNatsUsers.mockResolvedValue([{ ...USERS[0], bearer: true }])
    const w = mountPanel()
    await flushPromises()
    expect(w.find('tbody tr').text()).toContain('yes')
    expect(w.find('.summary-value.flagged').text()).toBe('1')
  })
  // ── BR-061 — an NKey is never rendered in full ────────────────────────────
  // These override the fixtures above with realistic 56-character keys: the
  // short stubs elsewhere in this file are below the elision floor, so they
  // would pass this rule by accident rather than by obeying it.
  const LONG_USER = 'UCREDSHIPPINGHFMQ7ZJZ4VNJ7Y3LWRR2PZLK4WRLK3PJ4DHCQFPL2JD55'
  const LONG_ACCT = 'APLATFORMXXXHFMQ7ZJZ4VNJ7Y3LWRR2PZLK4WRLK3PJ4DHCQFPLADD65'
  const LONG_ISSUER = 'ASIGNINGKEYXHFMQ7ZJZ4VNJ7Y3LWRR2PZLK4WRLK3PJ4DHCQFPL2RTQM'
  const elided = (k) => `[${k.slice(0, 5)}...${k.slice(-5)}]`

  it('BR-061: shows the user NKey in the Name cell, elided, with no full value on a title', async () => {
    listNatsUsers.mockResolvedValue([{ ...USERS[0], publicKey: LONG_USER }])
    const w = mountPanel()
    await flushPromises()

    const body = w.find('tbody')
    expect(body.find('[data-testid="nkey"]').text()).toBe(elided(LONG_USER))
    // The full key used to hang off this cell's title (pre-BR-061). The rule
    // is an absence, so it is asserted as one.
    expect(body.html()).not.toContain(LONG_USER)
    for (const el of body.findAll('[title]')) {
      expect(el.attributes('title')).not.toContain(LONG_USER)
    }
    expect(body.findAll('.nk-copy')).toHaveLength(0)
  })

  it('BR-061: elides all three keys in the claims pane — account, user, and issuer', async () => {
    listNatsUsers.mockResolvedValue([{ ...USERS[0], publicKey: LONG_USER }])
    getNatsUser.mockResolvedValue({
      ...UNSCOPED_VIEW,
      publicKey: LONG_USER,
      accountKey: LONG_ACCT,
      issuerKey: LONG_ISSUER,
    })
    const w = mountPanel()
    await flushPromises()
    await w.findAll('tbody tr')[0].trigger('click')
    await flushPromises()

    const detail = w.find('[data-testid="user-detail"]')
    for (const key of [LONG_USER, LONG_ACCT, LONG_ISSUER]) {
      expect(detail.text()).toContain(elided(key))
      expect(detail.text()).not.toContain(key)
    }
  })

  it('BR-061: the claims pane can copy a full key even though it never shows one', async () => {
    const writeText = vi.fn().mockResolvedValue()
    vi.stubGlobal('navigator', { ...navigator, clipboard: { writeText } })

    listNatsUsers.mockResolvedValue([{ ...USERS[0], publicKey: LONG_USER }])
    getNatsUser.mockResolvedValue({ ...UNSCOPED_VIEW, publicKey: LONG_USER, accountKey: LONG_ACCT })
    const w = mountPanel()
    await flushPromises()
    await w.findAll('tbody tr')[0].trigger('click')
    await flushPromises()

    const copies = w.find('[data-testid="user-detail"]').findAll('.nk-copy')
    await copies[1].trigger('click')
    expect(writeText).toHaveBeenCalledWith(LONG_USER)

    vi.unstubAllGlobals()
  })
})
