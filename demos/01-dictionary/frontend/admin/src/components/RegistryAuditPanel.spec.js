import PrimeVue from 'primevue/config'
import { mount, flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import RegistryAuditPanel from './RegistryAuditPanel.vue'

// Phase 2b — the registry's write history. Two things this panel exists to
// say, both from BR-AS23:
//
//   · A refused write is recorded, not just rejected, and it consumes no
//     revision — the number would otherwise lie about how many documents have
//     existed. So a refusal row shows no revision, on purpose.
//   · The actor is the shared `admin` identity and nothing stronger. The
//     surface must not imply an attribution the auth model cannot make.

vi.mock('../api', () => ({ getRegistryAudit: vi.fn() }))

import { getRegistryAudit } from '../api'

const ROWS = [
  { revision: 50, op: 'set-enabled', entryId: 'example-plugin-slow', actor: 'admin', outcome: 'accepted', detail: 'disabled', at: '2026-08-28T14:12:40Z' },
  { revision: null, op: 'upsert', entryId: 'seafreight-flow', actor: 'admin', outcome: 'refused', detail: 'origin-not-allowlisted', at: '2026-08-28T13:57:02Z' },
  { revision: 49, op: 'upsert', entryId: 'pricing-plugin', actor: 'admin', outcome: 'accepted', detail: '', at: '2026-08-28T14:11:06Z' },
]

const mountPanel = () => mount(RegistryAuditPanel, { global: { plugins: [PrimeVue] } })

beforeEach(() => {
  vi.clearAllMocks()
  getRegistryAudit.mockResolvedValue(ROWS.map((r) => ({ ...r })))
})

describe('RegistryAuditPanel', () => {
  it('lists accepted and refused writes together, in the order the server returned them', async () => {
    const w = mountPanel()
    await flushPromises()
    const ids = w.findAll('[data-testid="audit-entry"]').map((n) => n.text())
    expect(ids).toEqual(['example-plugin-slow', 'seafreight-flow', 'pricing-plugin'])
  })

  it('shows no revision against a refused write, because it consumed none (BR-AS23)', async () => {
    const w = mountPanel()
    await flushPromises()
    const revs = w.findAll('[data-testid="audit-revision"]').map((n) => n.text())
    expect(revs[0]).toBe('50')
    expect(revs[1]).not.toMatch(/\d/)
    expect(revs[2]).toBe('49')
  })

  it('names the cause of a refusal', async () => {
    const w = mountPanel()
    await flushPromises()
    expect(w.findAll('[data-testid="audit-row"]')[1].text()).toContain('origin-not-allowlisted')
  })

  it('shows the shared admin actor and claims nothing stronger (BR-AS23)', async () => {
    const w = mountPanel()
    await flushPromises()
    expect(w.findAll('[data-testid="audit-actor"]').map((n) => n.text())).toEqual(['admin', 'admin', 'admin'])
    expect(w.get('[data-testid="audit-actor-note"]').text().toLowerCase()).toContain('shared')
  })
})
