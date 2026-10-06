<script setup>
// Six gateway arrows over the panel grid, one per direction (D03-R32). Each
// arrow is the service's: its state and its n/3 tag are read from the
// /gatewayz of the cluster it leaves. This component computes no state; it
// only places the arrows between the panels it finds in its parent.
//
// An unknown arrow is dashed. It is never drawn as "not listed": no fresh
// reading is not a missing gateway (rule 3).
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'

const props = defineProps({
  arrows: { type: Array, default: () => [] },
})

const svg = ref(null)
const box = ref({ arb: null, za: null, au: null })

function measure() {
  const host = svg.value?.parentElement
  if (!host) return
  const T = host.getBoundingClientRect()
  const R = (c) => {
    const e = host.querySelector(`[data-testid="cp-${c}"]`)
    if (!e) return { l: 0, r: 0, t: 0, b: 0 }
    const b = e.getBoundingClientRect()
    return { l: b.left - T.left, r: b.right - T.left, t: b.top - T.top, b: b.bottom - T.top }
  }
  box.value = { arb: R('arb'), za: R('za'), au: R('au') }
}

let ro = null
onMounted(() => {
  measure()
  const host = svg.value?.parentElement
  if (host && typeof ResizeObserver !== 'undefined') {
    ro = new ResizeObserver(measure)
    ro.observe(host)
    host.querySelectorAll('[data-testid^="cp-"]').forEach((e) => ro.observe(e))
  }
})
onBeforeUnmount(() => ro?.disconnect())
watch(() => props.arrows, measure)

const find = (from, to) =>
  props.arrows.find((a) => a.from === from && a.to === to) ?? { from, to, state: 'unknown', tag: `${from} → ${to} no reading`, servers: [] }

function tip(a) {
  const lines = (a.servers ?? []).map((s) => {
    if (s.status === 'listed') return `${s.server}: listed`
    if (s.status === 'not_listed') return `${s.server}: not listed`
    return `${s.server}: no fresh reading${s.never ? ' (never read)' : ''}`
  })
  return [`${a.from} → ${a.to}, read from ${a.from}'s /gatewayz`, ...lines].join('\n')
}

const tagW = (text) => text.length * 5.8 + 10

const tracks = computed(() => {
  const { arb: a, za: z, au: u } = box.value
  if (!a || !z || !u) return []
  const out = []
  const down = (R2, c) => {
    const x = (Math.max(a.l, R2.l) + Math.min(a.r, R2.r)) / 2
    const y1 = a.b + 4
    const y2 = R2.t - 4
    const ym = (y1 + y2) / 2
    out.push({ a: find('arb', c), d: `M${x - 6},${y1} L${x - 6},${y2}`, tx: x - 14, ty: ym, anchor: 'end' })
    out.push({ a: find(c, 'arb'), d: `M${x + 6},${y2} L${x + 6},${y1}`, tx: x + 14, ty: ym, anchor: 'start' })
  }
  down(z, 'za')
  down(u, 'au')
  const y0 = (Math.max(z.t, u.t) + Math.min(z.b, u.b)) / 2
  const x1 = z.r + 4
  const x2 = u.l - 4
  const xm = (x1 + x2) / 2
  out.push({ a: find('za', 'au'), d: `M${x1},${y0 - 6} L${x2},${y0 - 6}`, tx: xm, ty: y0 - 22, anchor: 'middle' })
  out.push({ a: find('au', 'za'), d: `M${x2},${y0 + 6} L${x1},${y0 + 6}`, tx: xm, ty: y0 + 22, anchor: 'middle' })
  return out.map((t) => {
    const w = tagW(t.a.tag)
    const x0 = t.anchor === 'middle' ? t.tx - w / 2 : t.anchor === 'end' ? t.tx - w : t.tx
    return { ...t, w, x0 }
  })
})

const caption = computed(() => {
  const { arb: a, za: z, au: u } = box.value
  if (!a || !z || !u) return null
  return { yc: (a.b + z.t) / 2, xl: z.l, xr: u.r }
})

const STATES = ['listed', 'partial', 'not_listed', 'unknown']
</script>

<template>
  <svg
    ref="svg"
    class="gwsvg"
    aria-label="Gateway links between the three clusters"
    data-testid="gateways"
  >
    <defs>
      <marker
        v-for="st in STATES"
        :id="`pg-ah-${st}`"
        :key="st"
        viewBox="0 0 8 8"
        refX="7"
        refY="4"
        markerWidth="7"
        markerHeight="7"
        orient="auto-start-reverse"
      >
        <path
          d="M0,0 L8,4 L0,8 z"
          :class="`ah-${st}`"
        />
      </marker>
    </defs>
    <template v-if="caption">
      <text
        class="cap"
        :x="caption.xl"
        :y="caption.yc - 4"
      >Gateways, each arrow read from the</text>
      <text
        class="cap"
        :x="caption.xl"
        :y="caption.yc + 10"
      >/gatewayz of the cluster it leaves</text>
      <text
        class="cap"
        :x="caption.xr"
        :y="caption.yc - 4"
        text-anchor="end"
      >n/3: servers whose fresh reading lists it.</text>
      <text
        class="cap"
        :x="caption.xr"
        :y="caption.yc + 10"
        text-anchor="end"
      >Hover an arrow for each server.</text>
    </template>
    <g
      v-for="t in tracks"
      :key="`${t.a.from}-${t.a.to}`"
      :data-testid="`arrow-${t.a.from}-${t.a.to}`"
      :data-state="t.a.state"
    >
      <title>{{ tip(t.a) }}</title>
      <path
        class="trk"
        :class="t.a.state"
        :d="t.d"
        :marker-end="`url(#pg-ah-${t.a.state})`"
      />
      <path
        class="hit"
        :d="t.d"
      />
      <rect
        class="tagbg"
        :x="t.x0"
        :y="t.ty - 8"
        :width="t.w"
        height="16"
        rx="3"
      />
      <text
        :class="t.a.state"
        :x="t.x0 + t.w / 2"
        :y="t.ty + 4"
        text-anchor="middle"
      >{{ t.a.tag }}</text>
    </g>
  </svg>
</template>
