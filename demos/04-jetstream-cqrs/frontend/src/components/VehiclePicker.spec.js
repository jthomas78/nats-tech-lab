import PrimeVue from 'primevue/config'
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'

import VehiclePicker from './VehiclePicker.vue'

// The vehicle list that left the rail (D9). Two things must hold, and the
// second one is the one that already broke once: "all vehicles" is `null`
// above this component, and PrimeVue reads a null model value as "nothing is
// selected" — so the picker came up blank. It uses an internal sentinel now
// and still emits null outwards.

const mountPicker = (props) =>
  mount(VehiclePicker, { props, global: { plugins: [PrimeVue] } })

const WRITES = new Map([
  ['truck-7', { status: 'registered', lastSeq: 8 }],
  ['van-2', { status: 'retired', lastSeq: 4 }],
])
const READS = new Map([['truck-7', { lastSeq: 7 }]])

describe('the vehicle picker', () => {
  it('says "All vehicles" when nothing is picked, instead of coming up blank', () => {
    const w = mountPicker({ modelValue: null, vehicles: ['truck-7'], writes: WRITES, reads: READS })
    expect(w.text()).toContain('All vehicles')
  })

  it('offers all vehicles plus every vehicle', () => {
    const w = mountPicker({ modelValue: null, vehicles: ['truck-7', 'van-2'], writes: WRITES, reads: READS })
    expect(w.vm.options.map((o) => o.label)).toEqual(['All vehicles', 'truck-7', 'van-2'])
  })

  it('emits null for all vehicles, not the sentinel it uses inside', () => {
    const w = mountPicker({ modelValue: 'truck-7', vehicles: ['truck-7'], writes: WRITES, reads: READS })
    w.vm.pick('__all__')
    expect(w.emitted('update:modelValue')[0]).toEqual([null])
  })

  it('emits the vehicle id when a vehicle is picked', () => {
    const w = mountPicker({ modelValue: null, vehicles: ['truck-7'], writes: WRITES, reads: READS })
    w.vm.pick('truck-7')
    expect(w.emitted('update:modelValue')[0]).toEqual(['truck-7'])
  })

  it('carries the rail badges over — retired says so, the rest count', () => {
    const w = mountPicker({ modelValue: null, vehicles: ['truck-7', 'van-2'], writes: WRITES, reads: READS })
    expect(w.vm.options.map((o) => o.badge)).toEqual(['2', '7', 'retired'])
  })
})
