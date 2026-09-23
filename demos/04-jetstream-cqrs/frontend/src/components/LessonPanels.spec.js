/* The plugin's half of BR-AS79 (task 16e).

   The shell checks this demo BEFORE it mounts it. After the mount the plugin
   owns the report, and 16e asks for ONE presentation component across that
   boundary — the shell's pre-mount panel and this running-state error must be
   the same component, so one outage does not look like two products.

   These specs hold that, and hold the wording rules that come with it. */
import { mount } from '@vue/test-utils'
import PrimeVue from 'primevue/config'
import { describe, expect, it, vi } from 'vitest'

import DemoStatePanel from '@ui-shell/DemoStatePanel.vue'

import LessonPanels from './LessonPanels.vue'

/* Enough state for the header and the footer; the lessons are switched off so
   these specs exercise the outage path and nothing else. */
const baseState = (over = {}) => ({
  crumb: { heading: 'Odometer', lesson: 'Lesson 01', title: 'Stream and CQRS' },
  isLesson01: false,
  isLesson02: false,
  error: null,
  status: 'connected',
  connect: vi.fn(),
  watching: 'ODOMETER',
  wiring: { NATS_WS: 'ws://localhost:20444', COMMAND_API: 'http://localhost:20402' },
  ...over,
})

const draw = (state) => mount(LessonPanels, {
  props: { state },
  global: { plugins: [[PrimeVue, { theme: 'none' }]] },
})

describe('the demo\'s own running-state error', () => {
  it('draws nothing while the connection holds', () => {
    expect(draw(baseState()).findComponent(DemoStatePanel).exists()).toBe(false)
  })

  /* The single claim task 16e makes about this file. */
  it('is drawn with the SAME component the shell uses before the mount', () => {
    const wrapper = draw(baseState({ error: 'CONNECTION_REFUSED', status: 'error' }))
    expect(wrapper.findComponent(DemoStatePanel).exists()).toBe(true)
  })

  it('says the screen may be stale, and shows the client error as a chip', () => {
    const wrapper = draw(baseState({ error: 'CONNECTION_REFUSED', status: 'error' }))
    expect(wrapper.text()).toContain('Lost the connection to NATS.')
    expect(wrapper.text()).toContain('may be out of date')
    expect(wrapper.find('.demo-state-items').text()).toBe('CONNECTION_REFUSED')
  })

  /* A retry while the client is already retrying is not a new attempt. */
  it('reads as reconnecting, and disables its own retry, while nats-core retries', () => {
    const wrapper = draw(baseState({ error: 'DISCONNECT', status: 'reconnecting' }))
    expect(wrapper.text()).toContain('Reconnecting to NATS.')
    expect(wrapper.find('.demo-state-retry').attributes('disabled')).toBeDefined()
  })

  it('reconnects when the reader asks it to', async () => {
    const state = baseState({ error: 'CONNECTION_REFUSED', status: 'error' })
    const wrapper = draw(state)
    await wrapper.find('.demo-state-retry').trigger('click')
    expect(state.connect).toHaveBeenCalledTimes(1)
  })

  /* It is reported whichever lesson is open: a dropped connection stops both
     lessons updating, so it is not lesson 01's panel. */
  it('is reported on lesson 02 as well', () => {
    const state = baseState({ error: 'DISCONNECT', status: 'closed', isLesson02: true })
    /* Lesson 02's panel is stubbed: this spec is about WHERE the outage is
       drawn, and PoolPanel would open a real connection to say so. */
    const wrapper = mount(LessonPanels, {
      props: { state },
      global: { plugins: [[PrimeVue, { theme: 'none' }]], stubs: { PoolPanel: true } },
    })
    expect(wrapper.findComponent(DemoStatePanel).exists()).toBe(true)
  })
})
