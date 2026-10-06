/* The page's rules, held on fixture /state answers (CLAUDE.md, "The
   playground", rules 1–10). Each spec names the rule it holds. */
import { mount } from '@vue/test-utils'
import PrimeVue from 'primevue/config'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { AT, arrow, event, pageState, serverState, snapshot, spyActions } from '../fixtures.js'
import PlaygroundRoute from '../plugin/PlaygroundRoute.vue'

import DarkControl from './DarkControl.vue'
import Exercise10Guide from './Exercise10Guide.vue'
import GatewayLinks from './GatewayLinks.vue'
import HistoryLog from './HistoryLog.vue'
import MetaSummary from './MetaSummary.vue'
import RigBar from './RigBar.vue'
import ServerRow from './ServerRow.vue'
import StreamLane from './StreamLane.vue'

const opts = (props) => ({ props, global: { plugins: [PrimeVue] } })

describe('gateway arrows (rule 3)', () => {
  it('draws an arrow with no fresh reading dashed, and never as not listed', async () => {
    const unknown = arrow('arb', 'za', {
      state: 'unknown',
      listed: 0,
      fresh: 0,
      tag: 'arb → za no reading',
      servers: [1, 2, 3].map((n) => ({ server: `t-arb-${n}`, status: 'no_fresh_reading', ageNs: 0, never: true })),
    })
    const w = mount(GatewayLinks, { ...opts({ arrows: [unknown] }), attachTo: document.body })
    await w.vm.$nextTick()
    const g = w.find('[data-testid="arrow-arb-za"]')
    expect(g.attributes('data-state')).toBe('unknown')
    expect(g.find('path.trk').classes()).toContain('unknown')
    expect(g.text()).not.toMatch(/not listed/)
    expect(g.find('title').text()).toContain('no fresh reading')
  })

  it('draws all six directions, even when the service sent none', async () => {
    const w = mount(GatewayLinks, { ...opts({ arrows: [] }), attachTo: document.body })
    await w.vm.$nextTick()
    expect(w.findAll('g[data-state="unknown"]')).toHaveLength(6)
  })
})

describe('a server row (rules 1, 2, 3)', () => {
  it('greys a stale reading and says how old it is', () => {
    const w = mount(ServerRow, opts({ server: serverState({ server: 't-za-1', cluster: 'za', fresh: false, readingAgeNs: 4e9 }), pid: 1 }))
    const belief = w.find('[data-testid="belief"]')
    expect(belief.classes()).toContain('stale')
    expect(belief.text()).toMatch(/stale/)
  })

  it('says a monitor gave no answer, never that the server is down', () => {
    const w = mount(ServerRow, opts({ server: serverState({ server: 't-za-1', cluster: 'za', attemptFailed: true, fresh: false }), pid: 1 }))
    const mon = w.find('[data-testid="monitor"]').text()
    expect(mon).toMatch(/no answer/)
    expect(w.text()).not.toMatch(/\bdown\b/i)
  })

  it('keeps process state apart from the monitor', () => {
    const w = mount(ServerRow, opts({ server: serverState({ server: 't-za-1', cluster: 'za', process: 'stopped' }), pid: 1 }))
    expect(w.find('[data-testid="proc"]').text()).toMatch(/stopped/)
    expect(w.find('[data-testid="monitor"]').text()).toMatch(/answered/)
  })

  it('names the meta group in the leader badge, and leaves a follower as it is', () => {
    const reading = (state) => ({ server: 't-arb-1', kind: 'ok', leader: 't-arb-1', term: 4, state, why: '' })
    const lead = mount(ServerRow, opts({ server: serverState({ server: 't-arb-1', cluster: 'arb', reading: reading('leader') }), pid: 1 }))
    expect(lead.find('.pill.solid').text()).toBe('meta-leader')
    const follow = mount(ServerRow, opts({ server: serverState({ server: 't-za-1', cluster: 'za' }), pid: 1 }))
    expect(follow.find('[data-testid="belief"] .pill').text()).toBe('follower')
  })
})

describe('the meta summary (rule 1)', () => {
  it('names a leader only when the readings agree', () => {
    const agreed = mount(MetaSummary, opts({ state: pageState() }))
    expect(agreed.find('[data-testid="summary-leader"]').text()).toBe('t-arb-1')

    const split = pageState({
      summary: { ...snapshot().summary, state: 'disagreement', leader: '', views: [{ leader: 't-arb-1', term: 4, servers: ['t-arb-1'] }, { leader: 't-za-1', term: 5, servers: ['t-za-1'] }] },
    })
    const w = mount(MetaSummary, opts({ state: split }))
    expect(w.find('[data-testid="summary-leader"]').exists()).toBe(false)
  })
})

describe('controls that must never be blocked by other work', () => {
  const darkZa = () =>
    pageState({
      clusters: [
        { cluster: 'za', wanted: true, phase: 'dark', stopped: 3, running: 0, sinceNs: 1e9, pending: false },
        { cluster: 'arb', wanted: false, phase: 'off', stopped: 0, running: 3, sinceNs: 0, pending: false },
        { cluster: 'au', wanted: false, phase: 'off', stopped: 0, running: 3, sinceNs: 0, pending: false },
      ],
      pending: [{ id: 9, kind: 'publish', cluster: 'arb', lane: 'arb', via: 'auto', at: AT, limitNs: 5e9 }],
    })

  it('keeps Resume enabled while a publish is pending', () => {
    const actions = spyActions(vi)
    const w = mount(DarkControl, opts({ state: darkZa(), actions, cluster: 'za' }))
    const resume = w.find('[data-testid="resume"]')
    expect(resume.attributes('disabled')).toBeUndefined()
    resume.trigger('click')
    expect(actions.resume).toHaveBeenCalledWith('za')
  })

  it('keeps Restore enabled while a publish is pending', () => {
    const w = mount(RigBar, opts({ state: darkZa(), actions: spyActions(vi) }))
    expect(w.find('[data-testid="restore"]').attributes('disabled')).toBeUndefined()
  })
})

describe('the stream lane (rule 6)', () => {
  const timedOut = () =>
    pageState({
      ledger: {
        za: [{ id: 'PG_ZA_1', site: 'za', stream: 'ODOMETER_ZA', body: '{}', attempts: [{ cmd: 3, at: AT, via: 'auto', timeoutS: 5, outcome: 'timed_out_unknown', seq: 0, text: '' }] }],
        arb: [],
        au: [],
      },
    })

  it('calls a timed-out publish outcome unknown', () => {
    const w = mount(StreamLane, opts({ state: timedOut(), actions: spyActions(vi), cluster: 'za' }))
    expect(w.find('[data-testid="msg-PG_ZA_1"]').text()).toMatch(/outcome unknown/)
  })

  it('retries with the same message ID', () => {
    const actions = spyActions(vi)
    const w = mount(StreamLane, opts({ state: timedOut(), actions, cluster: 'za' }))
    w.find('[data-testid="retry"]').trigger('click')
    expect(actions.retry).toHaveBeenCalledWith('za', 'PG_ZA_1')
    expect(actions.publish).not.toHaveBeenCalled()
  })
})

describe('the exercise 10 guide (rule 10)', () => {
  it('has no button but its own hide and show', () => {
    const w = mount(Exercise10Guide, opts({ state: pageState() }))
    expect(w.findAll('button').map((b) => b.attributes('data-testid'))).toEqual(['guide-toggle'])
  })

  it('ticks a step only from what the page saw done', () => {
    const state = pageState({}, { history: [event(1, 'action', 'leadership arb', { cmd: 1 })] })
    const w = mount(Exercise10Guide, opts({ state }))
    const steps = w.findAll('.steps li')
    expect(steps[0].classes()).toContain('done') // the fixture summary is agreed
    expect(steps[1].classes()).toContain('done')
    expect(steps[2].classes()).toContain('next')
    expect(steps.slice(3).every((s) => !s.classes().includes('done'))).toBe(true)
  })
})

describe('the history (rule 9)', () => {
  it('shows a result line by how it ended, and an error in red', () => {
    const state = pageState({}, {
      history: [event(1, 'action', 'probe', { cmd: 1 }), event(2, 'result', 'PG_PROBE_1 via t-za-1: create failed 10008: x', { cmd: 1, end: 'error' })],
    })
    const w = mount(HistoryLog, opts({ state }))
    const err = w.find('.ev[data-kind="error"]')
    expect(err.exists()).toBe(true)
    expect(err.classes()).toContain('err')
  })

  it('shows a pending command with its lane and limit', () => {
    const state = pageState({ pending: [{ id: 4, kind: 'publish', cluster: 'za', lane: 'za', via: 'auto', at: AT, limitNs: 5e9, text: 'publish za' }] })
    const w = mount(HistoryLog, opts({ state }))
    expect(w.find('.ev[data-kind="pending"]').text()).toMatch(/of 5 s, za lane/)
  })
})

describe('the whole page (rules 4 and 5)', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('never says pin, move or set for the leadership request, nor partition for dark', async () => {
    const s = snapshot({
      leadership: { cmd: 2, cluster: 'arb', via: 'auto', at: AT, before: { leader: 't-za-1', term: 3 }, reply: 'accepted', observed: { leader: 't-arb-1', term: 4 }, tookNs: 1e9, text: '' },
      history: [event(1, 'action', 'leadership arb', { cmd: 2 })],
      historySeq: 1,
    })
    vi.stubGlobal('fetch', () => Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve(s) }))
    const w = mount(PlaygroundRoute, opts({}))
    await new Promise((r) => setTimeout(r, 0))
    await w.vm.$nextTick()
    const text = w.text()
    expect(text).toContain('Request leadership here')
    expect(text).toContain('Nothing keeps it there')
    expect(text).not.toMatch(/\bpin(ned|s)?\b/i)
    expect(text).not.toMatch(/\b(move|set) (the )?lead/i)
    expect(text).not.toMatch(/partition/i)
    w.unmount()
  })
})
