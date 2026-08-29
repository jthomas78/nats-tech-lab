import PrimeVue from 'primevue/config'
import { mount, flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import ConnectionsPanel from './ConnectionsPanel.vue'

// BR-028 (Main-POC-Plan.md Phase 17c) — in the Admin UI, a connection's
// account should resolve to a friendly name (its tenant, or "PLATFORM")
// wherever the backend could determine one, falling back to the raw account
// NKey otherwise. The backend's resolution logic (nats_ops.go's
// tenantLabelsByAccount) already has its own Go test coverage; this file
// covers the frontend half of that same rule — the component must actually
// prefer tenantLabel when the API supplies it, not just carry the field
// through unrendered — plus the panel's filtering and detail-pane behavior.

vi.mock('../api', () => ({
  getNatsConnections: vi.fn(),
  listAccounts: vi.fn(),
}))

import { getNatsConnections, listAccounts } from '../api'

const CONNECTIONS = [
  {
    cid: 1,
    name: 'refdata-service',
    type: 'nats',
    lang: 'go',
    version: '1.52.0',
    ip: '172.19.0.11',
    port: 48046,
    account: 'AA57B6BPPV3JQPCHSSCEALTMKL7YXGTT4WZI4CVVAHSO2TDQK6PYK2H6',
    tenantLabel: 'PLATFORM',
    // The credential's own `name` claim — one platform.creds user JWT shared
    // by several services, so it diverges from this connection's name.
    user: 'platform',
    userKey: 'UASXO6QQZGVBTMHYQ7ZJZ4VNJ7Y3LWRR2PZLK4WRLK3PJ4DHCQFPL2JD55',
    rtt: '779µs',
    uptime: '1h56m',
    idle: '16s',
    inMsgs: 313,
    outMsgs: 841,
    subscriptions: 2,
    subscriptionsList: ['rpc.*.refdata.type.list.v1', '$SRV.STATS.refdata-service'],
  },
  {
    cid: 2,
    name: 'accounts-service',
    type: 'nats',
    lang: 'go',
    version: '1.52.0',
    ip: '172.19.0.10',
    port: 49001,
    account: 'AB56H4HBPU4ZVCTWCY6RZIVEAIE37CE7VKCQMJANMLO7YJZ2IELZAFJT',
    // No tenantLabel — the SYS-account gap (BR-028's "wherever possible").
    user: 'accounts-service',
    userKey: 'UBSYSKEY123HFMQ7ZJZ4VNJ7Y3LWRR2PZLK4WRLK3PJ4DHCQFPL2AB99',
    rtt: '962µs',
    uptime: '11h',
    idle: '11h',
    inMsgs: 0,
    outMsgs: 0,
    subscriptions: 9,
    subscriptionsList: [],
  },
  {
    cid: 3,
    name: '',
    type: 'websocket',
    lang: 'nats.ws',
    version: '3.4.0',
    ip: '192.168.65.1',
    port: 52520,
    account: 'AAFBCA52VV7PAJSYANHENP4XR7PPY2ACIJLVDMW2YLGV24VD6MWAPPNX',
    // No user/userKey — a connection whose auth carried no user JWT and whose
    // account JWT had no name_tag either, so the backend sent nothing.
    tenantLabel: 'acme',
    rtt: '1.76ms',
    uptime: '1m',
    idle: '1m',
    inMsgs: 0,
    outMsgs: 0,
    subscriptions: 0,
    subscriptionsList: [],
  },
]

function mountPanel() {
  return mount(ConnectionsPanel, {
    global: { plugins: [PrimeVue] },
  })
}

describe('ConnectionsPanel', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    getNatsConnections.mockResolvedValue({
      connections: CONNECTIONS,
      page: { numConnections: 3, total: 3, offset: 0, limit: 1024 },
      server: { maxConnections: 65536 },
    })
    // Was missing until 2026-08-29, so every spec in this file raised an
    // unhandled "No listAccounts export is defined on the ../api mock" that
    // the panel's own try/catch swallowed — the account-name resolution path
    // was never actually exercised here.
    listAccounts.mockResolvedValue([])
  })

  // Load shape (2026-08-29) — the two calls used to be awaited in series even
  // though the account list only supplies names for rows the connection list
  // returns. See UsersPanel for the same fix and useDeferredLoading for why
  // the overlay is held back rather than shown for a load this short.
  describe('load shape', () => {
    it('issues the connection list and the account list together, not in series', async () => {
      let resolveConns
      getNatsConnections.mockReturnValue(new Promise((r) => { resolveConns = r }))
      mountPanel()
      // In flight while /connz is still pending — awaited after it, this would
      // not have been called yet.
      expect(listAccounts).toHaveBeenCalled()
      resolveConns({ connections: CONNECTIONS, page: {}, server: {} })
      await flushPromises()
    })

    it('clears loading once both halves have settled', async () => {
      const w = mountPanel()
      await flushPromises()
      expect(w.findComponent({ name: 'DataTable' }).props('loading')).toBe(false)
    })

    it('keeps the rows when the account list fails, losing only the names', async () => {
      listAccounts.mockRejectedValue(new Error('accounts-service down'))
      const w = mountPanel()
      await flushPromises()
      expect(w.findAll('tbody tr').length).toBe(CONNECTIONS.length)
      expect(w.text()).not.toContain('accounts-service down')
    })
  })

  it('PROBE clears loading', async () => {
    const w = mountPanel()
    await flushPromises()
    expect(w.findComponent({ name: 'DataTable' }).props('loading')).toBe(false)
  })

  it('BR-028: renders the resolved tenantLabel as a tag instead of the raw account NKey', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    const labels = wrapper.findAll('.tenant-label').map((el) => el.text())
    expect(labels).toEqual(expect.arrayContaining(['PLATFORM', 'acme']))
  })

  it('BR-028/BR-061: falls back to an ELIDED raw account NKey when no tenantLabel was resolved', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    const raw = wrapper.findAll('tbody [data-testid="nkey"]')
    const acct = CONNECTIONS[1].account
    const shown = raw.map((el) => el.text())
    expect(shown).toContain(`[${acct.slice(0, 5)}...${acct.slice(-5)}]`)
  })

  it('BR-061: never puts a full NKey on a table cell, on screen or in a tooltip', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    // The rule is an absence, so it is asserted as one. Before BR-061 the
    // Account cell carried `title=account` and the Credential cell carried
    // `title="name\nuserKey"` — 56 characters one hover deep, which is the
    // full render the rule forbids, merely delayed.
    const body = wrapper.find('tbody')
    for (const key of [CONNECTIONS[0].account, CONNECTIONS[1].account, CONNECTIONS[0].userKey]) {
      expect(body.html()).not.toContain(key)
    }
    expect(body.findAll('[title]')).toHaveLength(0)
  })

  it('BR-061: offers no copy affordance in a table cell — a column is for recognising a row', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    expect(wrapper.find('tbody').findAll('.nk-copy')).toHaveLength(0)
  })

  it('shows the summary counts derived from the fetched connections', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    expect(wrapper.text()).toContain('3') // total
    // nats: refdata-service + accounts-service = 2; websocket: 1
    const values = wrapper.findAll('.summary-value').map((el) => el.text().replace(/\s+/g, ' '))
    expect(values[0]).toBe('3 / 65,536') // total, against the server ceiling
    expect(values[1]).toBe('2')
    expect(values[2]).toBe('1')
  })

  it('gives every card value one type treatment, pairs included', async () => {
    getNatsConnections.mockResolvedValue({
      connections: CONNECTIONS,
      page: { numConnections: 3, total: 3, offset: 0, limit: 1024 },
      server: { maxConnections: 65536 },
    })
    const wrapper = mountPanel()
    await flushPromises()

    // No card opts out with a smaller font: the old `.small` modifier on the
    // msgs pair put three value sizes in one row.
    const values = wrapper.findAll('.summary-value')
    expect(values).toHaveLength(4)
    values.forEach((v) => expect(v.classes()).toEqual(['summary-value']))
  })

  it('shortens a runaway counter instead of letting it set the card width', async () => {
    const busy = CONNECTIONS.map((c) => ({ ...c, inMsgs: 411_522, outMsgs: 2_967_078 }))
    getNatsConnections.mockResolvedValue({
      connections: busy,
      page: { numConnections: 3, total: 3, offset: 0, limit: 1024 },
      server: { maxConnections: 65536 },
    })
    const wrapper = mountPanel()
    await flushPromises()

    const msgs = wrapper.findAll('.summary-value')[3]
    expect(msgs.text().replace(/\s+/g, ' ')).toBe('1.2M / 8.9M')
    // The exact figures stay reachable rather than being lost to rounding.
    expect(msgs.attributes('title')).toBe('1,234,566 in / 8,901,234 out')
  })

  it('reads Total as connections over max_connections, the ceiling from /varz', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    const total = wrapper.findAll('.summary-value')[0]
    expect(total.text().replace(/\s+/g, ' ')).toBe('3 / 65,536')
    expect(total.attributes('title')).toContain('max_connections')
    // True proportion (3 of 65,536); .gauge-fill's min-width keeps it visible.
    expect(wrapper.find('.gauge-fill').attributes('style')).toContain('width: 0%')
    expect(wrapper.find('.gauge').classes()).not.toContain('hot')
  })

  it('warns on the capacity bar once connections reach 80% of max_connections', async () => {
    getNatsConnections.mockResolvedValue({
      connections: CONNECTIONS,
      page: { numConnections: 3, total: 80, offset: 0, limit: 1024 },
      server: { maxConnections: 100 },
    })
    const wrapper = mountPanel()
    await flushPromises()

    expect(wrapper.findAll('.summary-value')[0].text().replace(/\s+/g, ' ')).toBe('80 / 100')
    expect(wrapper.find('.gauge').classes()).toContain('hot')
    expect(wrapper.find('.gauge-fill').attributes('style')).toContain('width: 80%')
  })

  it('counts every connection the server reported, not just the rows on this page', async () => {
    getNatsConnections.mockResolvedValue({
      connections: CONNECTIONS, // one page of rows…
      page: { numConnections: 1024, total: 2100, offset: 0, limit: 1024 },
      server: { maxConnections: 65536 },
    })
    const wrapper = mountPanel()
    await flushPromises()

    expect(wrapper.findAll('.summary-value')[0].text().replace(/\s+/g, ' ')).toBe('2,100 / 65,536')
  })

  it('drops the ceiling and the bar when /varz reported no max_connections', async () => {
    getNatsConnections.mockResolvedValue({
      connections: CONNECTIONS,
      page: { numConnections: 3, total: 3, offset: 0, limit: 1024 },
      server: { maxConnections: 0 },
    })
    const wrapper = mountPanel()
    await flushPromises()

    expect(wrapper.findAll('.summary-value')[0].text()).toBe('3')
    expect(wrapper.find('.gauge').exists()).toBe(false)
  })

  it('says nothing about /connz paging while the snapshot is complete', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    // On a server under one page there is nothing to warn about, and a
    // permanent "nothing hidden" line is the noise this replaced.
    expect(wrapper.find('.paged-note').exists()).toBe(false)
  })

  it('flags a paged /connz snapshot as showing only one page of several', async () => {
    getNatsConnections.mockResolvedValue({
      connections: CONNECTIONS,
      page: { numConnections: 1024, total: 2100, offset: 0, limit: 1024 },
      server: { maxConnections: 65536 },
    })
    const wrapper = mountPanel()
    await flushPromises()

    const note = wrapper.find('.paged-note')
    expect(note.text()).toBe('1,024 of 2,100 shown · page 1 of 3')
    expect(note.attributes('title')).toContain('not a limit on connections')
  })

  it('filters rows by tenantLabel text', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    await wrapper.find('.search-box input').setValue('acme')
    await flushPromises()

    const names = wrapper.findAll('tbody tr').map((row) => row.text())
    expect(names).toHaveLength(1)
    expect(names[0]).toContain('acme')
  })

  it('filters rows by subscription subject text', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    await wrapper.find('.search-box input').setValue('refdata.type.list')
    await flushPromises()

    expect(wrapper.findAll('tbody tr')).toHaveLength(1)
    expect(wrapper.find('tbody tr').text()).toContain('refdata-service')
  })

  it('filters rows by the websocket type chip', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    const wsChip = wrapper.findAll('.chip').find((c) => c.text() === 'websocket')
    await wsChip.trigger('click')
    await flushPromises()

    const rows = wrapper.findAll('tbody tr')
    expect(rows).toHaveLength(1)
    expect(rows[0].text()).toContain('(unnamed)')
  })

  it('opens the detail pane on row click, showing the resolved tenantLabel, and closes on the close control', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    expect(wrapper.find('.detail').exists()).toBe(false)

    await wrapper.findAll('tbody tr')[0].trigger('click')
    await flushPromises()

    const detail = wrapper.find('.detail')
    expect(detail.exists()).toBe(true)
    expect(detail.text()).toContain('PLATFORM')
    expect(detail.text()).toContain('refdata-service')
    expect(detail.findAll('.subs-table tbody td').map((el) => el.text())).toContain('rpc.*.refdata.type.list.v1')

    await detail.find('.close').trigger('click')
    await flushPromises()
    expect(wrapper.find('.detail').exists()).toBe(false)
  })

  // The selected row is the detail pane's anchor, so its highlight has to
  // outlive the pointer. Before this was bound, `selection` was left unset and
  // PrimeVue — which keeps no selection state of its own — rendered only
  // :hover, so the "selection" appeared to clear the moment the pointer moved
  // down into the pane.
  it('marks the selected row as selected for as long as the detail pane is open, not just while hovered', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    const rowAt = (i) => wrapper.findAll('tbody tr')[i]
    expect(rowAt(0).attributes('aria-selected')).not.toBe('true')

    await rowAt(0).trigger('click')
    await flushPromises()
    expect(rowAt(0).attributes('aria-selected')).toBe('true')
    expect(rowAt(1).attributes('aria-selected')).not.toBe('true')

    // A click inside the pane must not disturb it — this is the reported bug.
    await wrapper.find('.detail .pane-body').trigger('click')
    await flushPromises()
    expect(wrapper.find('.detail').exists()).toBe(true)
    expect(rowAt(0).attributes('aria-selected')).toBe('true')

    // Selecting another row moves the mark rather than adding a second.
    await rowAt(1).trigger('click')
    await flushPromises()
    expect(rowAt(0).attributes('aria-selected')).not.toBe('true')
    expect(rowAt(1).attributes('aria-selected')).toBe('true')

    await wrapper.find('.detail .close').trigger('click')
    await flushPromises()
    expect(rowAt(1).attributes('aria-selected')).not.toBe('true')
  })

  it('renders the credential name the backend decoded out of the user JWT', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    const creds = wrapper.findAll('tbody .cred').map((el) => el.text())
    expect(creds).toEqual(['platform', 'accounts-service'])
  })

  it('marks a credential that diverges from the connection name, and leaves a matching one plain', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    const creds = wrapper.findAll('tbody .cred')
    // refdata-service connects with the shared `platform` credential…
    expect(creds[0].classes()).toContain('diverged')
    // …accounts-service's credential is named for the service itself.
    expect(creds[1].classes()).not.toContain('diverged')
  })

  it('BR-058 as amended: shows the user NKey IN the credential cell, elided, not on its title', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    // BR-058 originally hung `name\nuserKey` off this cell's title. BR-061
    // relocated the key into the cell — the same fact, no longer 56 characters
    // deep in a hover, and no longer complete.
    const cell = wrapper.findAll('tbody td').find((td) => td.find('.cred').exists())
    const key = CONNECTIONS[0].userKey
    expect(cell.find('.cred').text()).toBe('platform')
    expect(cell.find('[data-testid="nkey"]').text()).toBe(`[${key.slice(0, 5)}...${key.slice(-5)}]`)
    expect(cell.attributes('title')).toBeUndefined()
  })

  it('shows an em-dash for a connection the backend could not name a credential for', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    const rows = wrapper.findAll('tbody tr')
    expect(rows[2].find('.cred').exists()).toBe(false)
    expect(rows[2].text()).toContain('\u2014')
  })

  it('filters rows by credential text', async () => {
    // The fixture's own credential names all echo a name or tenantLabel that
    // was already searchable, so this row carries a credential that appears
    // nowhere else — matching it can only have come from the new field.
    getNatsConnections.mockResolvedValue({
      connections: [{ ...CONNECTIONS[0], user: 'seafreight-app' }, CONNECTIONS[1], CONNECTIONS[2]],
      page: { numConnections: 3, total: 3, offset: 0, limit: 1024 },
      server: { maxConnections: 65536 },
    })
    const wrapper = mountPanel()
    await flushPromises()

    await wrapper.find('.search-box input').setValue('seafreight')
    await flushPromises()

    const rows = wrapper.findAll('tbody tr')
    expect(rows).toHaveLength(1)
    expect(rows[0].text()).toContain('refdata-service')
  })

  it('splits Account and Account NKey into their own rows, matching the Credential pair', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    await wrapper.findAll('tbody tr')[0].trigger('click')
    await flushPromises()

    const rows = wrapper.findAll('.detail .kv .row').map((r) => [
      r.find('.k').text(),
      r.find('.v').text().trim().replace(/\s+/g, ' '),
    ])
    const acct = CONNECTIONS[0].account
    expect(rows).toContainEqual(['Account', 'PLATFORM'])
    expect(rows).toContainEqual(['Account NKey', `[${acct.slice(0, 5)}...${acct.slice(-5)}]copy`])
  })

  it('shows an em-dash for an account whose label could not be resolved, keeping the NKey on its own row', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    // Row 2 is the SYS-account gap — no tenantLabel to render.
    await wrapper.findAll('tbody tr')[1].trigger('click')
    await flushPromises()

    const rows = wrapper.findAll('.detail .kv .row').map((r) => [
      r.find('.k').text(),
      r.find('.v').text().trim().replace(/\s+/g, ' '),
    ])
    const acct = CONNECTIONS[1].account
    expect(rows).toContainEqual(['Account', '\u2014'])
    expect(rows).toContainEqual(['Account NKey', `[${acct.slice(0, 5)}...${acct.slice(-5)}]copy`])
  })

  it('shows the credential and its user NKey in the detail pane', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    await wrapper.findAll('tbody tr')[0].trigger('click')
    await flushPromises()

    const detail = wrapper.find('.detail')
    const key = CONNECTIONS[0].userKey
    expect(detail.find('.cred').text()).toBe('platform')
    expect(detail.text()).toContain(`[${key.slice(0, 5)}...${key.slice(-5)}]`)
    expect(detail.text()).not.toContain(key)
  })

  it('BR-061: a detail pane can COPY the full key even though it never shows one', async () => {
    const writeText = vi.fn().mockResolvedValue()
    vi.stubGlobal('navigator', { ...navigator, clipboard: { writeText } })

    const wrapper = mountPanel()
    await flushPromises()
    await wrapper.findAll('tbody tr')[0].trigger('click')
    await flushPromises()

    // The rule is that a key is never SHOWN in full, not that it can never be
    // OBTAINED: the pane is where an operator goes to fetch a key for `nsc`.
    // The clipboard gets all 56 characters; the screen never does.
    const copies = wrapper.find('.detail').findAll('.nk-copy')
    expect(copies).toHaveLength(2)
    await copies[1].trigger('click')
    expect(writeText).toHaveBeenCalledWith(CONNECTIONS[0].userKey)

    vi.unstubAllGlobals()
  })

  it('lists subscriptions as a single-column table, always sorted alphabetically', async () => {
    const wrapper = mountPanel()
    await flushPromises()
    await wrapper.findAll('tbody tr')[0].trigger('click')
    await flushPromises()

    const table = wrapper.find('.detail .subs-table')
    // One column. The family IS the leading token, so a Family gutter would
    // only restate the characters beside it (CLAUDE.md § "Subject families").
    expect(table.findAll('thead th').map((th) => th.text())).toEqual(['Subject'])

    // The fixture is deliberately in /connz's order, which is not sorted:
    // without the sort the same connection reorders between refreshes and the
    // pane reads as if it were changing when nothing has.
    const shown = table.findAll('tbody td').map((td) => td.text())
    expect(CONNECTIONS[0].subscriptionsList).not.toEqual([...shown])
    expect(shown).toEqual([...shown].sort())
  })

  it('shows an error message when the fetch fails', async () => {
    getNatsConnections.mockRejectedValue(new Error('boom'))
    const wrapper = mountPanel()
    await flushPromises()

    expect(wrapper.find('.err-line').text()).toContain('boom')
  })
})
