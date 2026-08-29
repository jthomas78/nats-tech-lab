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
  getNatsClosedConnections: vi.fn(),
  getNatsUser: vi.fn(),
  revokeNatsUser: vi.fn(),
}))

import {
  getNatsClosedConnections,
  getNatsConnections,
  getNatsUser,
  listNatsUsers,
  revokeNatsUser,
} from '../api'

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
    expiresAt: iso(12 * 60000), // 12 minutes out — the `scheduled` band
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

// Hide-expired defaults OFF as of Phase 52 — the reaper (BR-AC44) bounds the
// expired set at one retention window, so it is no longer the rubble the
// default was hiding. showExpired is therefore a no-op on a freshly mounted
// panel; it is kept, and still called, so a spec reads as asserting on the
// whole roster rather than as depending on which way the default happens to
// point today. hideExpiredOn is its opposite, for the specs about the toggle.
async function showExpired(wrapper) {
  await wrapper.find('[data-testid="hide-expired"]').setValue(false)
}

async function hideExpiredOn(wrapper) {
  await wrapper.find('[data-testid="hide-expired"]').setValue(true)
}

// Hide-idle still defaults ON: an idle row is idle regardless of how many of
// them there are, so nothing in Phase 52 changes that one.
async function showIdle(wrapper) {
  await wrapper.find('[data-testid="hide-idle"]').setValue(false)
}

// A spec about anything other than the filters themselves clears both first
// and then asserts on the whole roster.
async function showAll(wrapper) {
  await showExpired(wrapper)
  await showIdle(wrapper)
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
    getNatsClosedConnections.mockResolvedValue({
      connections: [],
      page: { numConnections: 0, total: 0, offset: 0, limit: 1024 },
    })
    getNatsUser.mockResolvedValue(UNSCOPED_VIEW)
    revokeNatsUser.mockResolvedValue({ publicKey: 'UCRED_SHIPPING', revokedAt: iso(0) })
  })
  afterEach(() => vi.useRealTimers())

  it('lists credentials and sessions in one roster, credentials first', async () => {
    const w = mountPanel()
    await flushPromises()
    await showAll(w)
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
    await showIdle(w)
    const sysRow = w.findAll('tbody tr').find((r) => r.text().includes('sys'))
    expect(sysRow.find('[data-testid="health"]').text()).toBe('valid')
    // ...and its connection count is where "nothing is using it" is said.
    expect(sysRow.findAll('td.num-cell').at(0).text()).toBe('0')
    expect(sysRow.findAll('td.num-cell').at(1).text()).toBe('0')
  })

  it('derives the expiry health bands from the JWT expiry', async () => {
    const w = mountPanel()
    await flushPromises()
    await showAll(w)
    const rows = w.findAll('tbody tr')
    const seafreight = rows.find((r) => r.text().includes('seafreight-app'))
    const stale = rows.find((r) => r.text().includes('stale-app'))
    expect(seafreight.find('[data-testid="health"]').text()).toBe('scheduled')
    expect(seafreight.text()).toContain('in 12m')
    expect(stale.find('[data-testid="health"]').text()).toBe('expired')
  })

  // BR-060 (2026-08-27) — the four expiry bands and their boundaries. Each
  // case is written at the edge rather than in the middle of its band, since
  // an off-by-one in the nesting order is the failure this guards.
  it.each([
    ['no expiry at all', undefined, 'valid'],
    ['an hour out', 60 * 60000, 'valid'],
    ['exactly 15m out', 15 * 60000, 'scheduled'],
    ['a second inside 15m', 15 * 60000 - 1000, 'scheduled'],
    ['exactly 2m out', 2 * 60000, 'expiring'],
    ['a second inside 2m', 2 * 60000 - 1000, 'expiring'],
    ['on the expiry instant', 0, 'expired'],
    ['a second past expiry', -1000, 'expired'],
  ])('reports %s as %s', async (_label, offsetMs, expected) => {
    listNatsUsers.mockResolvedValue([
      { ...USERS[1], publicKey: 'UBAND', name: 'band-app', expiresAt: offsetMs === undefined ? undefined : iso(offsetMs) },
    ])
    const w = mountPanel()
    await flushPromises()
    await showAll(w)
    expect(w.find('[data-testid="health"]').text()).toBe(expected)
  })

  it('counts both live bands in the expiring tile, and neither expired nor valid', async () => {
    const w = mountPanel()
    await flushPromises()
    // seafreight is `scheduled` at 12m; nothing else in the fixture is inside
    // 15m — shipping-admin and sys carry no exp, stale is already gone.
    const tile = w.findAll('.summary-card').find((c) => c.text().includes('Expiring'))
    expect(tile.text()).toContain('15m')
    expect(tile.find('.summary-value').text()).toBe('1')
  })

  it('reports a pending row as pending rather than hiding it (BR-AC38)', async () => {
    listNatsUsers.mockResolvedValue([{ ...USERS[1], publicKey: 'UPENDING', name: 'half-minted', status: 'pending' }])
    const w = mountPanel()
    await flushPromises()
    await showIdle(w)
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
    expect(shipping.findAll('td.num-cell').at(0).text()).toBe('2')
    expect(shipping.findAll('td.num-cell').at(1).text()).toBe('5')
  })

  // Load shape (2026-08-29) — the panel felt slower than Accounts on every
  // tab switch. Two causes, one per spec below: three serialized round trips,
  // and a spinner that fired on the interval refresh as well as on mount.
  describe('load shape', () => {
    it('issues the roster and both /connz calls together, not in series', async () => {
      let resolveUsers
      listNatsUsers.mockReturnValue(new Promise((r) => { resolveUsers = r }))
      mountPanel()
      // The /connz calls are in flight while the roster is still pending — if
      // they were awaited after it, neither would have been called yet.
      expect(getNatsConnections).toHaveBeenCalled()
      expect(getNatsClosedConnections).toHaveBeenCalled()
      resolveUsers(USERS)
      await flushPromises()
    })

    it('shows the overlay once a mount fetch outruns the threshold', async () => {
      let resolveUsers
      listNatsUsers.mockReturnValue(new Promise((r) => { resolveUsers = r }))
      const w = mountPanel()
      expect(w.findComponent({ name: 'DataTable' }).props('loading')).toBe(false)
      // A load slow enough to need acknowledging still gets it.
      await vi.advanceTimersByTimeAsync(200)
      expect(w.findComponent({ name: 'DataTable' }).props('loading')).toBe(true)
      resolveUsers(USERS)
      await flushPromises()
      expect(w.findComponent({ name: 'DataTable' }).props('loading')).toBe(false)
    })

    it('holds the overlay back on a fast mount fetch, and never shows it after', async () => {
      const w = mountPanel()
      // Nothing on the first frame — the 300ms mask fade-out means an overlay
      // raised for a 40ms fetch stays on screen far longer than the work.
      expect(w.findComponent({ name: 'DataTable' }).props('loading')).toBe(false)
      await flushPromises()
      expect(w.findComponent({ name: 'DataTable' }).props('loading')).toBe(false)
      // Ten seconds on, the timer re-runs refresh() against a table that
      // already has rows. The assertion is made while that refresh is still
      // in flight — a spinner that only flashes mid-fetch is exactly the
      // regression this guards, and it would be invisible after the fetch
      // settles.
      let resolveUsers
      listNatsUsers.mockReturnValue(new Promise((r) => { resolveUsers = r }))
      vi.advanceTimersByTime(10000)
      await Promise.resolve()
      expect(getNatsConnections).toHaveBeenCalledTimes(2)
      expect(w.findComponent({ name: 'DataTable' }).props('loading')).toBe(false)
      resolveUsers(USERS)
      await flushPromises()
    })
  })

  it('keeps the roster when /connz fails, since it is the counts that were lost', async () => {
    getNatsConnections.mockRejectedValue(new Error('connz unreachable'))
    const w = mountPanel()
    await flushPromises()
    await showAll(w)
    expect(rowTexts(w)).toHaveLength(4)
    expect(w.text()).not.toContain('connz unreachable')
  })

  // BR-060 (2026-08-27) — the hide-expired toggle.
  describe('hiding expired users', () => {
    // Phase 52 reversed this default. The reaper (BR-AC44) bounds the expired
    // set at one retention window rather than at the stack's whole uptime, and
    // at that size a recently-expired session is the row an operator is
    // reading when they ask why a tab dropped — not noise to hide by default.
    it('shows expired rows by default now that the reaper bounds them', async () => {
      const w = mountPanel()
      await flushPromises()
      await showIdle(w)
      const texts = rowTexts(w)
      expect(texts).toHaveLength(4)
      expect(texts.some((t) => t.includes('stale-app'))).toBe(true)
      expect(w.find('[data-testid="hide-expired"]').element.checked).toBe(false)
    })

    it('hides them on request, and says how many it is holding back', async () => {
      const w = mountPanel()
      await flushPromises()
      await showIdle(w)
      await hideExpiredOn(w)
      const texts = rowTexts(w)
      expect(texts).toHaveLength(3)
      expect(texts.some((t) => t.includes('stale-app'))).toBe(false)
      expect(w.find('[data-testid="hide-expired-count"]').text()).toBe('1')
    })

    it('restores the expired rows when unchecked, and hides them again when re-checked', async () => {
      const w = mountPanel()
      await flushPromises()
      await showAll(w)
      expect(rowTexts(w).some((t) => t.includes('stale-app'))).toBe(true)
      await w.find('[data-testid="hide-expired"]').setValue(true)
      expect(rowTexts(w).some((t) => t.includes('stale-app'))).toBe(false)
    })

    // The filter is a filter, not a deletion: the tiles above the table are
    // the population count and must not move when a row is hidden from view.
    it('does not change the summary counts', async () => {
      const w = mountPanel()
      await flushPromises()
      const populationBefore = w.find('.summary-card .summary-value').text()
      await showAll(w)
      expect(w.find('.summary-card .summary-value').text()).toBe(populationBefore)
      // Four users in the fixture, one of them expired and hidden — the tile
      // still says four.
      expect(populationBefore).toBe('4')
    })

    // A revoked credential is not an expired one, and the two states have
    // nothing to do with each other — hiding expiry must not quietly hide the
    // rows an operator most needs to see.
    it('keeps a revoked row visible', async () => {
      listNatsUsers.mockResolvedValue([
        { ...USERS[0], publicKey: 'UREVOKED', name: 'gone-app', revokedAt: iso(-3600000) },
      ])
      const w = mountPanel()
      await flushPromises()
      await showIdle(w)
      expect(rowTexts(w).some((t) => t.includes('gone-app'))).toBe(true)
      expect(w.find('[data-testid="health"]').text()).toBe('revoked')
    })

    // A count beside an unchecked box reads as "51 hidden" when nothing is
    // hidden — the same lie in the other direction as a hidden row that says
    // nothing about being hidden.
    it('reports no held-back count while it is not holding anything back', async () => {
      const w = mountPanel()
      await flushPromises()
      await showIdle(w)
      expect(w.find('[data-testid="hide-expired-count"]').exists()).toBe(false)
      await hideExpiredOn(w)
      expect(w.find('[data-testid="hide-expired-count"]').text()).toBe('1')
    })

    it('composes with the kind filter rather than overriding it', async () => {
      const w = mountPanel()
      await flushPromises()
      await showAll(w)
      await w.findAll('.chip').find((c) => c.text() === 'session').trigger('click')
      expect(rowTexts(w)).toHaveLength(2)
      await w.find('[data-testid="hide-expired"]').setValue(true)
      const texts = rowTexts(w)
      expect(texts).toHaveLength(1)
      expect(texts[0]).toContain('seafreight-app')
    })
  })

  // BR-060 (2026-08-27) — the hide-idle toggle. It is the only filter here that
  // reads /connz, which is what every one of these specs is really about.
  describe('hiding idle users', () => {
    it('hides rows with no live connections by default and says how many', async () => {
      const w = mountPanel()
      await flushPromises()
      const texts = rowTexts(w)
      // shipping-admin (2 conns) and seafreight-app (1) survive; sys and
      // stale-app are both quietly idle and both held back. Since Phase 52
      // flipped hide-expired off, stale-app is now hidden by THIS filter
      // rather than by that one.
      expect(texts).toHaveLength(2)
      expect(texts.some((t) => t.includes('sys'))).toBe(false)
      expect(texts.some((t) => t.includes('stale-app'))).toBe(false)
      expect(w.find('[data-testid="hide-idle"]').element.checked).toBe(true)
      expect(w.find('[data-testid="hide-idle-count"]').text()).toBe('2')
    })

    // The non-double-counting rule, which only has teeth once BOTH filters are
    // on: stale-app is expired AND idle, and exactly one chip may claim it.
    it('does not count a row that is both expired and idle against both chips', async () => {
      const w = mountPanel()
      await flushPromises()
      await hideExpiredOn(w)
      expect(w.find('[data-testid="hide-expired-count"]').text()).toBe('1')
      // One, not two: stale-app is idle too, but hide-expired is already
      // holding it back.
      expect(w.find('[data-testid="hide-idle-count"]').text()).toBe('1')
    })

    it('restores the idle rows when unchecked', async () => {
      const w = mountPanel()
      await flushPromises()
      await showIdle(w)
      expect(rowTexts(w).some((t) => t.includes('sys'))).toBe(true)
    })

    // The point of BR-062: a credential being refused reads 0 connections and
    // is the single row an operator most needs. Hiding it would make this
    // checkbox cancel out the column beside it.
    it('never hides an idle row whose last outcome was a refusal', async () => {
      getNatsClosedConnections.mockResolvedValue({
        connections: [
          { cid: 9, userKey: 'UCRED_SYS', reason: 'Authentication Failure', stop: iso(-30000) },
        ],
        page: { numConnections: 1, total: 1, offset: 0, limit: 1024 },
      })
      const w = mountPanel()
      await flushPromises()
      const sysRow = w.findAll('tbody tr').find((r) => r.text().includes('sys'))
      expect(sysRow).toBeTruthy()
      expect(sysRow.find('[data-testid="outcome"]').text()).toBe('Authentication Failure')
      // ...and it is not counted as held back either. The chip still reads 1,
      // for stale-app — the assertion is that sys is not the second.
      expect(w.find('[data-testid="hide-idle-count"]').text()).toBe('1')
    })

    it('still hides an idle row whose last outcome was a clean close', async () => {
      getNatsClosedConnections.mockResolvedValue({
        connections: [{ cid: 9, userKey: 'UCRED_SYS', reason: 'Client Closed', stop: iso(-30000) }],
        page: { numConnections: 1, total: 1, offset: 0, limit: 1024 },
      })
      const w = mountPanel()
      await flushPromises()
      expect(rowTexts(w).some((t) => t.includes('sys'))).toBe(false)
    })

    // BR-060's "a /connz failure must not empty the roster" — which this
    // filter would break outright, since every row reads 0 when the join is
    // gone.
    it('suspends itself when /connz is unavailable rather than emptying the roster', async () => {
      getNatsConnections.mockRejectedValue(new Error('connz unreachable'))
      const w = mountPanel()
      await flushPromises()
      await showExpired(w)
      expect(rowTexts(w)).toHaveLength(4)
      // Suspended, not unchecked: the box still says what the operator asked
      // for, and says why it is not acting.
      expect(w.find('[data-testid="hide-idle"]').element.checked).toBe(true)
      expect(w.find('[data-testid="hide-idle-suspended"]').text()).toBe('suspended')
    })

    // A connection on an unread page counts as zero here, so hiding the row
    // would be a louder version of the lie the amber paged note prevents.
    it('suspends itself when /connz answered only one page', async () => {
      getNatsConnections.mockResolvedValue({
        connections: CONNECTIONS,
        page: { numConnections: 3, total: 400, offset: 0, limit: 3 },
      })
      const w = mountPanel()
      await flushPromises()
      expect(rowTexts(w).some((t) => t.includes('sys'))).toBe(true)
      expect(w.find('[data-testid="hide-idle-suspended"]').exists()).toBe(true)
    })

    // Phase 52 — `Authentication Expired` matches the refusal pattern
    // textually, but it is the server's word for a session whose TTL ran out:
    // the ordinary end of every browser session here, and by volume the
    // commonest reason in the closed ring. Grouping it with a real refusal
    // made this filter unable to hide the largest class of dead rows.
    it('still hides an idle row whose last outcome was an expiry, not a refusal', async () => {
      getNatsClosedConnections.mockResolvedValue({
        connections: [
          { cid: 9, userKey: 'UCRED_SYS', reason: 'Authentication Expired', stop: iso(-30000) },
        ],
        page: { numConnections: 1, total: 1, offset: 0, limit: 1024 },
      })
      const w = mountPanel()
      await flushPromises()
      expect(rowTexts(w).some((t) => t.includes('sys'))).toBe(false)
    })

    it('composes with hide expired rather than overriding it', async () => {
      const w = mountPanel()
      await flushPromises()
      await showAll(w)
      expect(rowTexts(w)).toHaveLength(4)
      // hide-expired alone: everything but stale-app.
      await hideExpiredOn(w)
      expect(rowTexts(w)).toHaveLength(3)
      // ...and then hide-idle on top of it, which takes sys as well.
      await w.find('[data-testid="hide-idle"]').setValue(true)
      expect(rowTexts(w)).toHaveLength(2)
    })
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
    await showAll(w)
    await w.findAll('.chip').find((c) => c.text() === 'session').trigger('click')
    expect(rowTexts(w)).toHaveLength(2)
    await w.find('.search-box input').setValue('seafreight')
    expect(rowTexts(w)).toHaveLength(1)
  })

  // BR-AC40 — the roster carries no permissions, so a drill-in is a second call.
  it('fetches one user’s claims on row click', async () => {
    const w = mountPanel()
    await flushPromises()
    await showIdle(w)
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
    await showIdle(w)
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
    await showIdle(w)
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
    await showIdle(w)

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
    await showIdle(w)
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
    await showIdle(w)
    await w.findAll('tbody tr')[0].trigger('click')
    await flushPromises()

    const copies = w.find('[data-testid="user-detail"]').findAll('.nk-copy')
    await copies[1].trigger('click')
    expect(writeText).toHaveBeenCalledWith(LONG_USER)

    vi.unstubAllGlobals()
  })

  // ── BR-062 (Phase 51a) — last connection outcome ────────────────────────
  // The question the live counts cannot answer: an idle credential and one
  // being refused every few seconds both read 0 connections.
  describe('BR-062 — last connection outcome', () => {
    const closed = (over) => ({
      cid: 1,
      name: 'sys',
      account: 'SYS',
      user: 'sys',
      userKey: 'UCRED_SYS',
      reason: 'Client Closed',
      start: iso(-600000),
      stop: iso(-60000),
      ...over,
    })

    it('shows the reason for a credential that has no live connections', async () => {
      getNatsClosedConnections.mockResolvedValue({
        connections: [closed({ reason: 'Authentication Failure' })],
        page: { numConnections: 1, total: 1, offset: 0, limit: 1024 },
      })
      const w = mountPanel()
      await flushPromises()
      const sysRow = w.findAll('tbody tr').find((r) => r.text().includes('sys'))
      expect(sysRow.find('[data-testid="outcome"]').text()).toBe('Authentication Failure')
      // A refusal is the case the column exists for, so it is the one that
      // gets a colour.
      expect(sysRow.find('[data-testid="outcome"]').classes()).toContain('refused')
    })

    it('carries the stop timestamp for a credential but not for a session', async () => {
      getNatsClosedConnections.mockResolvedValue({
        connections: [
          closed(),
          // USESSION_STALE, not the seafreight session — that one is
          // connected in the fixture, and a connected row shows no outcome.
          closed({ cid: 2, userKey: 'USESSION_STALE', reason: 'Client Closed' }),
        ],
        page: { numConnections: 2, total: 2, offset: 0, limit: 1024 },
      })
      const w = mountPanel()
      await flushPromises()
      await showAll(w)
      const rows = w.findAll('tbody tr')
      const sysRow = rows.find((r) => r.text().includes('sys'))
      const session = rows.find((r) => r.text().includes('stale-app'))
      // The reason is worth showing on both — a session being refused is the
      // same bug as a credential being refused.
      expect(sysRow.find('[data-testid="outcome"]').exists()).toBe(true)
      expect(session.find('[data-testid="outcome"]').exists()).toBe(true)
      // The timestamp is not: a session's last stop is a fact about a mint
      // that was always going to end.
      expect(sysRow.find('[data-testid="outcome-stop"]').exists()).toBe(true)
      expect(session.find('[data-testid="outcome-stop"]').exists()).toBe(false)
    })

    it('says nothing about the outcome while the credential is connected', async () => {
      getNatsClosedConnections.mockResolvedValue({
        connections: [closed({ userKey: 'UCRED_SHIPPING', reason: 'Authentication Failure' })],
        page: { numConnections: 1, total: 1, offset: 0, limit: 1024 },
      })
      const w = mountPanel()
      await flushPromises()
      const shipping = w.findAll('tbody tr').find((r) => r.text().includes('shipping-admin'))
      // It has two live connections in the default fixture; its last closed
      // one is history and the Conns column is the answer.
      expect(shipping.find('[data-testid="outcome"]').exists()).toBe(false)
    })

    it('reads an absent row as outside the retained window, not as a clean history', async () => {
      const w = mountPanel()
      await flushPromises()
      await showIdle(w)
      const sysRow = w.findAll('tbody tr').find((r) => r.text().includes('sys'))
      expect(sysRow.find('[data-testid="outcome-none"]').text()).toContain('outside the retained window')
    })

    it('flags a paged closed ring rather than under-reporting a failure', async () => {
      getNatsClosedConnections.mockResolvedValue({
        connections: [closed()],
        page: { numConnections: 1, total: 400, offset: 0, limit: 1 },
      })
      const w = mountPanel()
      await flushPromises()
      expect(w.find('[data-testid="closed-paged"]').exists()).toBe(true)
    })

    it('keeps the roster when the closed ring is unavailable', async () => {
      getNatsClosedConnections.mockRejectedValue(new Error('connz unreachable'))
      const w = mountPanel()
      await flushPromises()
      await showAll(w)
      await showAll(w)
    expect(w.findAll('tbody tr')).toHaveLength(4)
    })
  })

  // ── BR-AC43 (Phase 51b) — revocation ────────────────────────────────────
  describe('BR-AC43 — revoking a credential', () => {
    const openDetail = async (w, text) => {
      const row = w.findAll('tbody tr').find((r) => r.text().includes(text))
      await row.trigger('click')
      await flushPromises()
      return w
    }

    it('offers Revoke on a credential, behind a confirmation naming it and its live count', async () => {
      const w = mountPanel()
      await flushPromises()
      await openDetail(w, 'shipping-admin')
      expect(w.find('[data-testid="revoke"]').exists()).toBe(true)

      await w.find('[data-testid="revoke"]').trigger('click')
      const dialog = w.find('[data-testid="revoke-confirm"]')
      expect(dialog.exists()).toBe(true)
      expect(dialog.text()).toContain('shipping-admin')
      // The live count is the fact that tells an operator something is using
      // this credential right now.
      expect(dialog.find('[data-testid="revoke-live"]').text()).toBe('2 connections')
      // Nothing is revoked by opening the dialog.
      expect(revokeNatsUser).not.toHaveBeenCalled()
    })

    it('revokes by public key, never by name', async () => {
      const w = mountPanel()
      await flushPromises()
      await openDetail(w, 'shipping-admin')
      await w.find('[data-testid="revoke"]').trigger('click')
      await w.find('[data-testid="revoke-confirm-btn"]').trigger('click')
      await flushPromises()
      expect(revokeNatsUser).toHaveBeenCalledWith('UCRED_SHIPPING')
    })

    it('offers no Revoke for a session, which expires on its own TTL', async () => {
      getNatsUser.mockResolvedValue({ ...UNSCOPED_VIEW, publicKey: 'USESSION_SEAFREIGHT', kind: 'session' })
      const w = mountPanel()
      await flushPromises()
      await openDetail(w, 'seafreight-app')
      expect(w.find('[data-testid="revoke"]').exists()).toBe(false)
    })

    it('offers no Revoke for a credential that is already revoked — there is no un-revoke', async () => {
      getNatsUser.mockResolvedValue({ ...UNSCOPED_VIEW, revokedAt: iso(-3600000) })
      const w = mountPanel()
      await flushPromises()
      await openDetail(w, 'shipping-admin')
      expect(w.find('[data-testid="revoke"]').exists()).toBe(false)
      expect(w.find('[data-testid="revoked-note"]').exists()).toBe(true)
    })

    it('surfaces a failed revocation instead of closing as if it worked', async () => {
      revokeNatsUser.mockRejectedValue(new Error('claims update timed out'))
      const w = mountPanel()
      await flushPromises()
      await openDetail(w, 'shipping-admin')
      await w.find('[data-testid="revoke"]').trigger('click')
      await w.find('[data-testid="revoke-confirm-btn"]').trigger('click')
      await flushPromises()
      expect(w.find('[data-testid="revoke-error"]').text()).toContain('claims update timed out')
      expect(w.find('[data-testid="revoke-confirm"]').exists()).toBe(true)
    })

    it('reports a revoked credential as revoked, outranking pending and expiry', async () => {
      listNatsUsers.mockResolvedValue([
        { ...USERS[0], status: 'pending', expiresAt: iso(-1000), revokedAt: iso(-3600000) },
      ])
      const w = mountPanel()
      await flushPromises()
      expect(w.find('[data-testid="health"]').text()).toBe('revoked')
    })
  })
})
