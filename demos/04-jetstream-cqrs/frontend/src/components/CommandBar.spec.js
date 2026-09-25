import PrimeVue from 'primevue/config'
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'

import CommandBar from './CommandBar.vue'

// The result list is rendered from outcomes that commands/api.js has already
// shaped. These specs check only what the list draws from them.

const at = new Date('2026-09-25T10:00:00Z')

const mountBar = (outcomes) =>
  mount(CommandBar, { props: { outcomes }, global: { plugins: [PrimeVue] } })

describe('a malformed history in the result list', () => {
  // The seq is shown on its own, so a reader does not have to find it in the
  // message text. The list offers no repair: publishing an event is not one.
  it('names the seq the replay stopped at', () => {
    const w = mountBar([
      {
        kind: 'broken',
        command: 'travel',
        id: 'V1',
        error: 'MalformedHistory',
        message: 'history is malformed at seq 17',
        seq: 17,
        at,
      },
    ])
    const row = w.get('[data-testid="outcomes"] li')
    expect(row.get('[data-testid="outcome-seq"]').text()).toBe('seq 17')
    expect(row.find('[data-testid="outcome-rule"]').exists()).toBe(false)
    expect(row.text()).not.toContain('nats pub')
  })

  it('shows no seq on a broken answer that named none', () => {
    const w = mountBar([
      { kind: 'broken', command: 'travel', id: 'V1', error: 'Unavailable', message: 'nats down', at },
    ])
    expect(w.find('[data-testid="outcome-seq"]').exists()).toBe(false)
  })

  // An accepted row already says its seq in the message. It gets no chip,
  // so the chip means one thing only: where a replay stopped.
  it('shows no seq chip on an accepted row', () => {
    const w = mountBar([
      { kind: 'accepted', command: 'travel', id: 'V1', seq: 9, message: 'appended to ODOMETER at seq 9', at },
    ])
    expect(w.find('[data-testid="outcome-seq"]').exists()).toBe(false)
  })
})
