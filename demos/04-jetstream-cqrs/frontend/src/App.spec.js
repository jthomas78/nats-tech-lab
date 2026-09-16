import PrimeVue from 'primevue/config'
import { mount } from '@vue/test-utils'
import { ref } from 'vue'
import { describe, expect, it, vi } from 'vitest'
import NavList from '@ui-shell/NavList.vue'
import App from './App.vue'
import StreamCqrsPanel from './components/StreamCqrsPanel.vue'
import CommandBar from './components/CommandBar.vue'
import VehiclePicker from './components/VehiclePicker.vue'

vi.mock('./nats/useOdometer.js', () => ({
  useOdometer: () => ({
    status: ref('connected'), error: ref(''), head: ref(9), messages: ref(8), bytes: ref(2048),
    writes: new Map(), reads: new Map(), pool: new Map(), poolWorkers: new Map(),
    log: ref([]), vehicles: ref(['V1']), lags: ref({ head: 9, writeSeq: 8, readSeq: 7 }),
    connect: vi.fn(), disconnect: vi.fn(),
  }),
}))

describe('the shell leaves vehicle selection to the lesson', () => {
  it('has no pagehead picker or Guide row and passes stream size to the lesson', async () => {
    const w = mount(App, { global: { plugins: [PrimeVue] } })
    expect(w.get('.pagehead').text()).toBe('Lesson 01 - One event source + CQRS')
    expect(w.get('.pagehead').findComponent(VehiclePicker).exists()).toBe(false)
    expect(w.findComponent(NavList).text()).not.toContain('How it works')
    const panel = w.findComponent(StreamCqrsPanel)
    expect(panel.props('messages')).toBe(8)
    expect(panel.props('bytes')).toBe(2048)
    await w.get('[data-testid="lesson-01-tab-showcase"]').trigger('click')
    panel.findComponent(VehiclePicker).vm.$emit('update:modelValue', 'V1')
    await w.vm.$nextTick()
    expect(w.findComponent(CommandBar).props('vehicle')).toBe('V1')
    expect(w.get('.pagehead').text()).toBe('Lesson 01 - One event source + CQRS')
  })
})
