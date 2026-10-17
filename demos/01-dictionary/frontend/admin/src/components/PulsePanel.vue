<script setup>
// Phase 44 — first tab in the Request/Reply panel (Pulse | Traces |
// Messages), added to close two gaps design review found: nothing in this
// panel explained the _INBOX.<nuid> reply-routing mechanism, and nothing
// mentioned parentSpanId/spanId — the mechanism that actually chains a
// multi-hop call into the tree Traces' waterfall reconstructs (traceId
// alone only says the hops belong to the same call, not how they nest).
//
// Phase 54c (decision 15, BR-077): Pulse is MARKED UNAVAILABLE until
// Phase 55. Its request/error/latency histograms counted the whole live
// trace set from the old KV feed (retired in 54d). The trace store now is
// Tempo, read through a search with a result limit, and a capped search is
// never shown as a total. So the tab keeps its explanation card and flow
// diagram, says why the numbers are gone, and points to Phase 55, which
// rebuilds Pulse on Prometheus metrics. The removed histogram code is in
// this file's git history.
</script>

<template>
  <div class="pulse-panel">
    <div
      class="pulse-unavailable"
      role="status"
      data-testid="pulse-unavailable"
    >
      <i class="pi pi-info-circle" />
      <div>
        <b>Pulse is unavailable until Phase 55.</b>
        Its request, error and latency counts came from the old trace feed, which held every recent trace. Traces now come from Tempo through a search that returns at most a set number of results, and a capped result is never shown as a total. Phase 55 rebuilds Pulse on Prometheus metrics. Until then, use the <b>Traces</b> tab to look at calls.
      </div>
    </div>
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

  </div>
</template>

<style scoped>
.pulse-unavailable {
  display: flex;
  gap: 10px;
  align-items: flex-start;
  padding: 10px 12px;
  border: 1px solid var(--p-content-border-color);
  border-left: 3px solid #9a7b1e;
  border-radius: 6px;
  background: var(--p-content-background);
}
.pulse-unavailable .pi {
  color: #9a7b1e;
  margin-top: 3px;
}
.pulse-panel {
  display: flex;
  flex-direction: column;
  gap: 10px;
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
  /* The reply paths are already authored right-to-left (service → client),
     so the dashes march the right way with the default direction — reversing
     the animation here would double-negate it and send them back left-to-right. */
  .flow-path {
    animation: pulse-flow 1.1s linear infinite;
  }
}
@keyframes pulse-flow {
  to {
    stroke-dashoffset: -33;
  }
}

</style>
