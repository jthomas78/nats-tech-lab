<script setup>
import { computed } from 'vue'

import { useTraceFeed } from '../nats/useTraceFeed.js'

// Phase 44 — first tab in the Request/Reply panel (Pulse | Traces |
// Messages), added to close two gaps design review found: nothing in this
// panel explained the _INBOX.<nuid> reply-routing mechanism, and nothing
// mentioned parentSpanId/spanId — the mechanism that actually chains a
// multi-hop call into the tree Traces' waterfall reconstructs (traceId
// alone only says the hops belong to the same call, not how they nest).
//
// This carries the request/error/latency histograms that used to sit above
// TraceWaterfall's own toolbar (Phase 28p) — moved here rather than left in
// place, per the mockup (diagrams/admin-rpc-overview-mockup.html).
//
// Bootstrap/subscribe/trace-grouping lives in useTraceFeed.js, shared with
// TraceWaterfall.vue and RpcPanel.vue's Messages tab (an architecture
// review replaced three drifted copies of the same adapter with this one
// seam). Unlike TraceWaterfall, this aggregates the FULL unfiltered
// `traces` Map the composable returns, not a toolbar-filtered view — once
// Pulse is a separate tab, it is no longer co-rendered with that toolbar,
// and sharing its filter state would mean either duplicating that toolbar
// here too or having Pulse silently reflect filters set on a tab you're not
// looking at. A tab that always shows the whole buffer is the simpler, more
// honest choice — the same "don't claim more than what was actually
// counted" principle the panel already lives by (ARCHITECTURE-ADMIN.md
// §4.5).

const { traces, bootstrapFailed, everDisconnected } = useTraceFeed()

function isRoot(span) {
  return !span.parentSpanId
}

// Trimmed relative to TraceWaterfall.vue's own summarize(): this only needs
// ok/replyMs/at for bucketing, not the waterfall's sub-millisecond ordering
// precision (preciseFinishMs there exists solely to fix parent-above-child
// render order, which this histogram has no equivalent of) or the account/
// span-count/kind fields TraceWaterfall's rail+detail view needs.
function ownStartMs(span) {
  return new Date(span.timestamp).getTime() - (span.durationMs || 0)
}
function summarize(traceId, spans) {
  const root = spans.find(isRoot) ?? spans.reduce((a, b) => (ownStartMs(a) <= ownStartMs(b) ? a : b), spans[0])
  const at = Math.min(...spans.map(ownStartMs))
  const replyMs = root?.durationMs ?? Math.max(...spans.map((s) => new Date(s.timestamp).getTime())) - at
  const ok = !spans.some((s) => s.statusCode === 'ERROR')
  return { traceId, ok, replyMs, at }
}

const traceSummaries = computed(() =>
  Array.from(traces.value.entries()).map(([traceId, spans]) => summarize(traceId, spans)),
)

// ── Pulse — request/error/latency histograms over the full live-buffered
// trace set (see the Phase 44 note above for why this doesn't filter).
// Buckets are fixed-count, not fixed-duration: this panel has no historical
// metrics backend, just the live-buffered trace set, so the window is
// "however far back the buffer reaches" rather than a calendar interval.
const PULSE_BUCKETS = 20
const pulse = computed(() => {
  const list = traceSummaries.value
  const empty = { total: 0, errCount: 0, errPct: '0%', avgLatency: 0, currentLatency: 0, reqBars: [], errBars: [], latPoints: '', latArea: '', hasLat: false }
  if (list.length === 0) return empty

  const sorted = [...list].sort((a, b) => b.at - a.at)
  const ats = sorted.map((t) => t.at)
  const minAt = Math.min(...ats)
  const maxAt = Math.max(...ats)
  const span = maxAt > minAt ? maxAt - minAt : 30000 // degenerate single-instant window: widen so bucketing doesn't divide by zero

  const reqCounts = new Array(PULSE_BUCKETS).fill(0)
  const errCounts = new Array(PULSE_BUCKETS).fill(0)
  const latSums = new Array(PULSE_BUCKETS).fill(0)
  const latCounts = new Array(PULSE_BUCKETS).fill(0)
  for (const t of sorted) {
    const idx = Math.min(PULSE_BUCKETS - 1, Math.max(0, Math.floor(((t.at - minAt) / span) * PULSE_BUCKETS)))
    reqCounts[idx] += 1
    if (!t.ok) errCounts[idx] += 1
    latSums[idx] += t.replyMs
    latCounts[idx] += 1
  }

  const reqMax = Math.max(1, ...reqCounts)
  const errMax = Math.max(1, ...errCounts)
  const reqBars = reqCounts.map((v, i) => ({ h: v > 0 ? Math.max(3, (v / reqMax) * 34) : 1, now: i === PULSE_BUCKETS - 1 }))
  const errBars = errCounts.map((v, i) => ({ h: v > 0 ? Math.max(3, (v / errMax) * 34) : 1, now: i === PULSE_BUCKETS - 1, empty: v === 0 }))

  const latPointsRaw = []
  for (let i = 0; i < PULSE_BUCKETS; i += 1) {
    if (latCounts[i] > 0) latPointsRaw.push([i, latSums[i] / latCounts[i]])
  }
  let latPoints = ''
  let latArea = ''
  if (latPointsRaw.length > 0) {
    const vals = latPointsRaw.map(([, v]) => v)
    const latMin = Math.min(...vals)
    const latMax = Math.max(...vals)
    const xy = latPointsRaw.map(([i, v]) => {
      const x = (i / (PULSE_BUCKETS - 1)) * 200
      const y = latMax > latMin ? 32 - ((v - latMin) / (latMax - latMin)) * 30 : 17
      return `${x},${y}`
    })
    latPoints = xy.join(' ')
    latArea = `0,34 ${xy.join(' ')} 200,34`
  }

  const total = sorted.length
  const errCount = sorted.filter((t) => !t.ok).length
  return {
    total,
    errCount,
    errPct: `${((errCount / total) * 100).toFixed(1)}%`,
    avgLatency: Math.round(sorted.reduce((sum, t) => sum + t.replyMs, 0) / total),
    currentLatency: Math.round(sorted[0].replyMs), // sorted newest-first
    reqBars,
    errBars,
    latPoints,
    latArea,
    hasLat: latPointsRaw.length > 0,
  }
})
</script>

<template>
  <div class="pulse-panel">
    <p
      v-if="bootstrapFailed || everDisconnected"
      class="err-line"
    >
      {{ bootstrapFailed ? 'Initial trace snapshot failed to load.' : 'Live feed dropped at least once — some traces may be missing.' }}
    </p>
    <div class="grid-2">
      <div class="card about">
        <h2>What request/reply covers</h2>
        <p>A client publishes to a subject and waits on a private, single-use reply subject it generates itself — <code>_INBOX.&lt;nuid&gt;</code>. Whoever answers publishes the reply to that inbox, and only the original client is subscribed to it, so the response routes back to exactly the requester who asked. No application-level correlation bookkeeping is needed for the network hop itself.</p>
        <p>In this lab that pattern carries two subject families: <b>rpc.*</b> (service-to-service calls) and <b>api.*</b> (browser-to-service calls). Reply-routing and request-tracing solve different problems, and NATS only automates the first one — so every hop also carries a W3C <code>traceparent</code> header: a <code>traceId</code> shared by the whole call, plus a <code>spanId</code> the receiving hop mints for itself and a <code>parentSpanId</code> copied from the caller's own <code>spanId</code>. That parent/child pair is what lets the <b>Traces</b> tab rebuild the call tree — including any further hop a service makes on its own behalf — even though NATS itself never looks inside the header. <code>correlationId</code> is carried too, but it is a separate per-hop field, not the span id.</p>
        <div class="covers">
          <div><b>rpc.*</b><span>service ↔ service</span></div>
          <div><b>api.*</b><span>browser ↔ service</span></div>
          <div><b>_INBOX.&lt;nuid&gt;</b><span>per-request reply subject</span></div>
          <div><b>parentSpanId</b><span>caller's spanId, chains the tree</span></div>
        </div>
      </div>

      <div class="card flow-card">
        <h2>How a call round-trips</h2>
        <svg
          viewBox="0 0 476 190"
          preserveAspectRatio="xMidYMid meet"
        >
          <text
            x="105"
            y="20"
            text-anchor="middle"
            class="subj-lbl subj-req"
          >rpc.acme.ship-status.list.v1</text>
          <text
            x="105"
            y="176"
            text-anchor="middle"
            class="subj-lbl subj-rep"
          >_INBOX.4f2a09c1</text>
          <text
            x="355"
            y="20"
            text-anchor="middle"
            class="subj-lbl subj-req"
          >rpc.acme.ship-status.list.v1</text>
          <text
            x="355"
            y="176"
            text-anchor="middle"
            class="subj-lbl subj-rep"
          >_INBOX.4f2a09c1</text>

          <path
            d="M 92 78 C 130 60, 170 60, 208 76"
            class="flow-path flow-req"
            marker-end="url(#pulseArrowReq)"
          />
          <path
            d="M 252 76 C 290 60, 330 60, 368 78"
            class="flow-path flow-req"
            marker-end="url(#pulseArrowReq)"
          />
          <path
            d="M 368 112 C 330 130, 290 130, 252 114"
            class="flow-path flow-rep"
            marker-end="url(#pulseArrowRep)"
          />
          <path
            d="M 208 114 C 170 130, 130 130, 92 112"
            class="flow-path flow-rep"
            marker-end="url(#pulseArrowRep)"
          />
          <circle
            cx="90"
            cy="78"
            r="3"
            fill="var(--lab-accent)"
          />
          <circle
            cx="370"
            cy="112"
            r="3"
            fill="#2fbf71"
          />

          <defs>
            <marker
              id="pulseArrowReq"
              markerWidth="8"
              markerHeight="8"
              refX="4"
              refY="4"
              orient="auto"
            >
              <path
                d="M0,0 L8,4 L0,8 Z"
                fill="var(--lab-accent)"
              />
            </marker>
            <marker
              id="pulseArrowRep"
              markerWidth="8"
              markerHeight="8"
              refX="4"
              refY="4"
              orient="auto"
            >
              <path
                d="M0,0 L8,4 L0,8 Z"
                fill="#2fbf71"
              />
            </marker>
          </defs>

          <rect
            x="10"
            y="65"
            width="80"
            height="74"
            rx="8"
            class="node-box"
          />
          <rect
            x="16"
            y="73"
            width="17"
            height="17"
            rx="4"
            fill="var(--lab-accent)"
          />
          <text
            x="24.5"
            y="85.5"
            text-anchor="middle"
            class="badge-txt"
          >C</text>
          <text
            x="50"
            y="102"
            text-anchor="middle"
            class="node-label"
          >Client</text>
          <text
            x="50"
            y="115"
            text-anchor="middle"
            class="node-sub"
          >seafreight-app</text>
          <text
            x="50"
            y="128"
            text-anchor="middle"
            class="node-span"
          >spanId a1c9f0</text>

          <rect
            x="185"
            y="55"
            width="90"
            height="80"
            rx="8"
            class="node-box"
          />
          <rect
            x="221"
            y="65"
            width="18"
            height="18"
            rx="4"
            fill="var(--lab-accent)"
          />
          <text
            x="230"
            y="78"
            text-anchor="middle"
            class="badge-txt"
          >N</text>
          <text
            x="230"
            y="103"
            text-anchor="middle"
            class="node-label"
          >NATS Server</text>
          <text
            x="230"
            y="116"
            text-anchor="middle"
            class="node-sub"
          >opaque relay</text>

          <rect
            x="370"
            y="65"
            width="80"
            height="88"
            rx="8"
            class="node-box"
          />
          <rect
            x="376"
            y="73"
            width="17"
            height="17"
            rx="4"
            fill="#7c5cff"
          />
          <text
            x="384.5"
            y="85.5"
            text-anchor="middle"
            class="badge-txt"
          >S</text>
          <text
            x="410"
            y="102"
            text-anchor="middle"
            class="node-label"
          >Service</text>
          <text
            x="410"
            y="115"
            text-anchor="middle"
            class="node-sub"
          >refdata-service</text>
          <text
            x="410"
            y="128"
            text-anchor="middle"
            class="node-span"
          >parentSpanId a1c9f0</text>
          <text
            x="410"
            y="141"
            text-anchor="middle"
            class="node-span accent2"
          >spanId 7e2f31</text>
        </svg>
        <div class="flow-legend">
          <span><i class="sw req" />request</span>
          <span><i class="sw rep" />reply</span>
        </div>
        <p class="flow-note">Service mints its own <code>spanId</code> and copies the Client's <code>spanId</code> in as its <code>parentSpanId</code> — the same pair it would hand onward in a <code>traceparent</code> header if it called a third service. That chain, not the reply, is what <b>Traces</b> follows to draw the waterfall.</p>
      </div>
    </div>

    <div
      v-if="traceSummaries.length > 0"
      class="pulse-row"
    >
      <div class="pulse-card">
        <div class="pulse-head">
          <span class="pulse-label">requests</span>
          <span class="pulse-window">last {{ pulse.total }} trace{{ pulse.total === 1 ? '' : 's' }}</span>
        </div>
        <div class="pulse-value-row">
          <span class="pulse-value accent">{{ pulse.total }}</span>
        </div>
        <div class="pulse-chart">
          <div
            v-for="(b, i) in pulse.reqBars"
            :key="i"
            class="pulse-bar"
            :class="{ now: b.now }"
            :style="{ height: b.h + 'px' }"
          />
        </div>
      </div>

      <div class="pulse-card">
        <div class="pulse-head">
          <span class="pulse-label">errors</span>
          <span class="pulse-window">{{ pulse.errPct }} of window</span>
        </div>
        <div class="pulse-value-row">
          <span class="pulse-value err">{{ pulse.errCount }}</span>
        </div>
        <div class="pulse-chart">
          <div
            v-for="(b, i) in pulse.errBars"
            :key="i"
            class="pulse-bar err"
            :class="{ now: b.now, empty: b.empty }"
            :style="{ height: b.h + 'px' }"
          />
        </div>
      </div>

      <div class="pulse-card">
        <div class="pulse-head">
          <span class="pulse-label">avg latency</span>
          <span class="pulse-window">{{ pulse.currentLatency }}ms now</span>
        </div>
        <div class="pulse-value-row">
          <span class="pulse-value accent">{{ pulse.avgLatency }}</span>
          <span class="pulse-unit">ms avg</span>
        </div>
        <div class="pulse-line-wrap">
          <svg
            viewBox="0 0 200 34"
            preserveAspectRatio="none"
          >
            <template v-if="pulse.hasLat">
              <polygon
                :points="pulse.latArea"
                class="pulse-area"
              />
              <polyline
                :points="pulse.latPoints"
                class="pulse-line"
              />
            </template>
          </svg>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.pulse-panel {
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.err-line {
  flex: none;
  margin: 0;
  font-size: 12px;
  color: #e5484d;
}
.grid-2 {
  display: grid;
  grid-template-columns: minmax(0, 1.05fr) minmax(0, 1fr);
  gap: 10px;
  align-items: stretch;
}
@media (max-width: 900px) {
  .grid-2 {
    grid-template-columns: 1fr;
  }
}
.card {
  background: var(--lab-panel-bg);
  border: 1px solid var(--lab-panel-border);
  border-radius: 4px;
  padding: 14px 16px;
}
.card h2 {
  font-size: 11px;
  font-weight: 600;
  letter-spacing: 0.04em;
  text-transform: uppercase;
  color: var(--lab-accent);
  margin: 0 0 8px;
}
.about p {
  margin: 0 0 10px;
  color: var(--p-text-muted-color);
}
.about p:last-child {
  margin-bottom: 0;
}
.about code {
  font-family: ui-monospace, 'SF Mono', 'JetBrains Mono', Menlo, Consolas, monospace;
  font-size: 12px;
  color: var(--lab-accent);
  background: rgba(0, 111, 255, 0.1);
  border-radius: 3px;
  padding: 0 4px;
}
.about .covers {
  display: flex;
  flex-wrap: wrap;
  gap: 14px;
  margin-top: 12px;
  padding-top: 10px;
  border-top: 1px solid var(--lab-panel-border);
  font-size: 12px;
}
.about .covers div b {
  display: block;
  font-family: ui-monospace, 'SF Mono', 'JetBrains Mono', Menlo, Consolas, monospace;
  font-size: 12px;
}
.about .covers div span {
  color: var(--p-text-disabled-color);
  font-size: 11px;
}

/* ── animated flow diagram — same marching-dash technique as
   SharingPanel.vue's .topo-edge, opt-in under prefers-reduced-motion. */
.flow-card svg {
  display: block;
  width: 100%;
  height: auto;
}
.flow-legend {
  display: flex;
  gap: 16px;
  margin-top: 8px;
  font-size: 11px;
  color: var(--p-text-muted-color);
}
.flow-legend .sw {
  display: inline-block;
  width: 16px;
  height: 2px;
  border-radius: 1px;
  margin-right: 5px;
  vertical-align: 1px;
}
.flow-legend .sw.req {
  background: var(--lab-accent);
}
.flow-legend .sw.rep {
  background: #2fbf71;
}
.node-box {
  fill: var(--lab-nested-bg);
  stroke: var(--lab-panel-border);
}
.node-label {
  fill: var(--p-text-color);
  font: 600 12px 'Inter', sans-serif;
}
.node-sub {
  fill: var(--p-text-disabled-color);
  font: 10px 'Inter', sans-serif;
}
.badge-txt {
  font: 700 10px 'Inter', sans-serif;
  fill: #fff;
}
.node-span {
  font: 9px ui-monospace, 'SF Mono', 'JetBrains Mono', Menlo, Consolas, monospace;
  fill: var(--p-text-disabled-color);
}
.node-span.accent2 {
  fill: #2fbf71;
}
.flow-note {
  font-size: 11px;
  color: var(--p-text-muted-color);
  margin: 6px 0 0;
}
.flow-note code {
  font-family: ui-monospace, 'SF Mono', 'JetBrains Mono', Menlo, Consolas, monospace;
  color: var(--p-text-color);
}
.subj-lbl {
  font: 11px ui-monospace, 'SF Mono', 'JetBrains Mono', Menlo, Consolas, monospace;
}
.subj-req {
  fill: var(--lab-accent);
}
.subj-rep {
  fill: #2fbf71;
}
.flow-path {
  fill: none;
  stroke-width: 1.6;
  stroke-linecap: round;
  stroke-dasharray: 5 6;
  opacity: 0.85;
}
.flow-req {
  stroke: var(--lab-accent);
}
.flow-rep {
  stroke: #2fbf71;
}
@media (prefers-reduced-motion: no-preference) {
  .flow-path {
    animation: pulse-flow 1.1s linear infinite;
  }
  .flow-rep {
    animation-direction: reverse;
  }
}
@keyframes pulse-flow {
  to {
    stroke-dashoffset: -33;
  }
}

/* ── pulse cards — same visual language TraceWaterfall's Phase 28p strip
   used, given more room now that they own the whole tab instead of a strip
   above another view's toolbar. */
.pulse-row {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 10px;
}
.pulse-card {
  background: var(--lab-panel-bg);
  border: 1px solid var(--lab-panel-border);
  border-radius: 4px;
  padding: 14px 16px;
}
.pulse-head {
  display: flex;
  justify-content: space-between;
  align-items: baseline;
  margin-bottom: 8px;
}
.pulse-label {
  font-size: 11px;
  font-weight: 600;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: var(--p-text-disabled-color);
}
.pulse-window {
  font-size: 10px;
  color: var(--p-text-disabled-color);
}
.pulse-value-row {
  display: flex;
  align-items: baseline;
  gap: 6px;
  margin-bottom: 10px;
}
.pulse-value {
  font-family: ui-monospace, 'SF Mono', 'JetBrains Mono', Menlo, Consolas, monospace;
  font-size: 26px;
  font-weight: 600;
  line-height: 1;
}
.pulse-value.accent {
  color: var(--lab-accent);
}
.pulse-value.err {
  color: #e5484d;
}
.pulse-unit {
  font-size: 11px;
  color: var(--p-text-muted-color);
}
.pulse-chart {
  height: 48px;
  display: flex;
  align-items: flex-end;
  gap: 3px;
}
.pulse-bar {
  flex: 1;
  border-radius: 2px 2px 0 0;
  background: var(--lab-accent);
  opacity: 0.85;
  min-height: 2px;
}
.pulse-bar.err {
  background: #e5484d;
}
.pulse-bar.err.empty {
  opacity: 0.16;
}
.pulse-bar.now {
  opacity: 1;
  box-shadow: 0 0 0 1px rgba(255, 255, 255, 0.15);
}
.pulse-line-wrap {
  height: 48px;
}
.pulse-line-wrap svg {
  display: block;
  width: 100%;
  height: 100%;
  overflow: visible;
}
.pulse-area {
  fill: var(--lab-accent);
  opacity: 0.12;
}
.pulse-line {
  fill: none;
  stroke: var(--lab-accent);
  stroke-width: 1.6;
  stroke-linejoin: round;
  stroke-linecap: round;
}
</style>
