<script setup>
// Lesson 02 — scaling a consumer. Four tabs (plan section 10.6.2).
//
// One durable consumer, many workers. The position belongs to the CONSUMER,
// not to a worker, so adding workers adds throughput and removes order. Each
// tab is the same pool under a different condition, which is why they are tabs
// and not four rail rows.
//
// Every tab prints the command that produced it, so nothing on this screen is
// unreproducible. And every tab is allowed to say "not run yet": the pool is a
// lesson somebody chooses to run, the two pool buckets do not exist until they
// do, and an empty state that names the command is the honest picture.
//
// NO NUMBER ON THIS PANEL IS INVENTED. Everything drawn is read live from KV
// odometer-pool-workers and KV odometer-pool. The "1 vs 4" tab has no drawing
// at all, because `-drain` has not been run yet (plan task 04.7.4) and a
// plausible-looking table of times this demo never measured would break the
// one promise it makes.
//
// The tab strip is a real PrimeVue `Tabs` carrying `class="panel-tabs"` — the
// repo's one style for a top tab strip, the same as AboutPanel.vue. Never a
// chip or pill toggle for this role; chips stay reserved for filters.
import { computed, ref } from 'vue'

import Tab from 'primevue/tab'
import TabList from 'primevue/tablist'
import TabPanel from 'primevue/tabpanel'
import TabPanels from 'primevue/tabpanels'
import Tabs from 'primevue/tabs'
import Tag from 'primevue/tag'

import { POOL_KV, READ_KV } from '../config.js'
import { ackBars, foldDamage, poolHealth, poolLaneRows, redelivery, workerRows } from '../view/pool.js'
import { tabsFor } from '../view/lessons.js'
import LagLane from './LagLane.vue'

const props = defineProps({
  head: { type: Number, default: 0 },
  // KV odometer-pool-workers, one entry per worker.
  workers: { type: Object, default: () => new Map() },
  // KV odometer-pool — the pool's own, deliberately damaged fold.
  pool: { type: Object, default: () => new Map() },
  // KV odometer-read — the correct fold, to measure the damage against.
  reads: { type: Object, default: () => new Map() },
})

const TABS = tabsFor('lesson-02')
const tab = ref('live')
const current = computed(() => TABS.find((t) => t.key === tab.value) ?? TABS[0])

const rows = computed(() => workerRows(props.workers))
const health = computed(() => poolHealth(rows.value))
const bars = computed(() => ackBars(rows.value))
const poolRows = computed(() => [...props.pool.values()])
const readRows = computed(() => [...props.reads.values()])

// How far the pool's own fold has reached: the furthest any vehicle in its
// bucket has been folded to, the same measure the other two sides use.
const foldSeq = computed(() =>
  poolRows.value.reduce((m, r) => Math.max(m, Number(r.lastSeq) || 0), 0),
)

const laneRows = computed(() => poolLaneRows(props.head, rows.value, foldSeq.value))
const damage = computed(() => foldDamage(poolRows.value, readRows.value))
const kill = computed(() => redelivery(rows.value))

const STATUS_TONE = { working: 'on', waiting: 'off', killed: 'lost' }

function km(n) {
  return `${Math.round(Number(n) || 0).toLocaleString('en-GB')} km`
}
</script>

<template>
  <section
    class="lesson"
    data-testid="pool-panel"
  >
    <header>
      <p class="eyebrow">
        Lesson 02 · one consumer, many workers
      </p>
      <Tag
        v-if="health.running"
        severity="info"
        :value="`${health.workers} workers`"
        data-testid="pool-workers"
      />
      <Tag
        v-if="health.idle"
        severity="warn"
        :value="`${health.idle} idle`"
      />
      <Tag
        v-if="health.killed"
        severity="danger"
        :value="`${health.killed} silent`"
      />
      <code class="cmd">{{ current.cmd }}</code>
    </header>

    <p class="lead">
      A watermark makes redelivery safe. It does not make reordering safe — it
      makes reordering silent. Many workers on one durable consumer are handed
      the log out of order, and the fold refuses what arrives behind it
      (BR-OD08). This lesson counts what that costs.
    </p>

    <Tabs
      v-model:value="tab"
      class="panel-tabs"
    >
      <TabList>
        <Tab
          v-for="t in TABS"
          :key="t.key"
          :value="t.key"
          :data-testid="`lesson-02-tab-${t.key}`"
        >
          {{ t.label }}
        </Tab>
      </TabList>
      <TabPanels>
        <!-- LIVE — the pool as it is running right now. -->
        <TabPanel value="live">
          <div
            v-if="!health.running"
            class="card idle-card"
            data-testid="pool-not-running"
          >
            <p class="note">
              No worker is reporting. The pool is a lesson you start yourself —
              nothing on this page can start it, and nothing on this page is
              read back by a worker.
            </p>
            <code class="run">{{ current.cmd }}</code>
          </div>

          <template v-else>
            <LagLane
              title="Where each worker is, against the head of the log"
              :head="head"
              :rows="laneRows"
              scope="the pool"
            />

            <div
              class="cards"
              data-testid="worker-cards"
            >
              <article
                v-for="w in rows"
                :key="w.id"
                class="card worker"
                :class="STATUS_TONE[w.status] ?? 'off'"
                :data-testid="`worker-${w.worker}`"
              >
                <header class="wh">
                  <span
                    class="led"
                    :class="STATUS_TONE[w.status] ?? 'off'"
                  />
                  <b>worker {{ w.worker }}</b>
                </header>
                <p class="doing">
                  <template v-if="w.status === 'killed'">
                    silent, still holding #{{ w.holding }}
                  </template>
                  <template v-else-if="w.holding">
                    folding #{{ w.holding }}
                  </template>
                  <template v-else>
                    waiting
                  </template>
                </p>
                <p class="counts">
                  acked {{ w.acked }}
                  <template v-if="w.dropped">
                    · <span class="lost">dropped {{ w.dropped }}</span>
                  </template>
                </p>
              </article>
            </div>

            <div
              v-if="damage"
              class="card damage"
              data-testid="fold-damage"
            >
              <p class="eyebrow">
                Fold damage · {{ POOL_KV }} against {{ READ_KV }}
              </p>
              <dl class="kv">
                <dt>events acked</dt>
                <dd>{{ health.acked }}</dd>
                <dt>refused by the watermark</dt>
                <dd :class="{ lost: health.dropped > 0 }">
                  {{ health.dropped }}
                </dd>
                <dt>total, pool fold</dt>
                <dd :class="{ lost: damage.damaged }">
                  {{ km(damage.poolKm) }}
                </dd>
                <dt>total, read model</dt>
                <dd>{{ km(damage.readKm) }}</dd>
                <dt>drift</dt>
                <dd :class="{ lost: damage.damaged }">
                  {{ damage.drift > 0 ? '+' : '' }}{{ km(damage.drift) }}
                </dd>
              </dl>
              <p class="note">
                The pool's total is <b>short</b>, never double. A dropped event
                is a fact that is gone, and nothing reports it.
              </p>
            </div>
            <p
              v-else
              class="note"
            >
              Nothing folded yet, so there is nothing to compare — the drift is
              left blank rather than shown as zero.
            </p>
          </template>
        </TabPanel>

        <!-- STARVATION — MaxAckPending belongs to the consumer, not a worker. -->
        <TabPanel value="starvation">
          <div
            v-if="!health.running"
            class="card idle-card"
            data-testid="starvation-not-running"
          >
            <p class="note">
              Eight workers on a cap of three. Start it and watch five of them
              do nothing:
            </p>
            <code class="run">{{ current.cmd }}</code>
          </div>

          <div
            v-else
            class="card"
            data-testid="ack-bars"
          >
            <p class="eyebrow">
              Events acked per worker
            </p>
            <div
              v-for="b in bars"
              :key="b.worker"
              class="bar-row"
            >
              <span class="who">worker {{ b.worker }}</span>
              <span class="track"><span
                class="fill"
                :class="{ idle: b.idle }"
                :style="{ width: `${b.pct}%` }"
              /></span>
              <span class="right">{{ b.acked }} acked</span>
            </div>
            <p class="note">
              The cap is shared across the consumer, not given to each worker.
              Three messages in flight is three messages in flight however many
              workers are asking for them.
            </p>
          </div>
        </TabPanel>

        <!-- REDELIVERY — a watermark makes this safe, and slow. -->
        <TabPanel value="redelivery">
          <div
            v-if="!kill"
            class="card idle-card"
            data-testid="redelivery-not-running"
          >
            <p class="note">
              Nothing has gone silent. <code>-kill-at</code> is real fault
              injection, not a label: the worker that fetches that sequence
              stops fetching, and never acks and never naks — exactly what the
              server sees when a process is killed.
            </p>
            <code class="run">{{ current.cmd }}</code>
          </div>

          <div
            v-else
            class="card damage"
            data-testid="redelivery"
          >
            <p class="eyebrow">
              What the server is doing with #{{ kill.seq }}
            </p>
            <dl class="kv">
              <dt>held by</dt>
              <dd class="lost">
                worker {{ kill.worker }}, silent
              </dd>
              <dt>acked</dt>
              <dd class="lost">
                no
              </dd>
              <dt>naked</dt>
              <dd class="lost">
                no — a nak would redeliver at once and hide the wait
              </dd>
              <dt>double-counted</dt>
              <dd class="ok">
                no — the watermark holds (BR-OD07)
              </dd>
            </dl>
            <p class="note">
              The server will redeliver #{{ kill.seq }} after
              <code>AckWait</code>. Until then the fold is stuck here and the
              read model is stale. How long that took is not measured on this
              screen — read it off the run.
            </p>
          </div>
        </TabPanel>

        <!-- 1 VS 4 — deliberately empty. See the file header. -->
        <TabPanel value="scaling">
          <div
            class="card idle-card"
            data-testid="scaling-unmeasured"
          >
            <p class="eyebrow">
              Not measured yet
            </p>
            <p class="note">
              Time to drain 10 000 events at 1, 2, 4 and 8 workers. This demo
              has not run it, so this tab draws nothing. A table of times that
              were never measured would be the one thing this demo must not do.
              Run it and the numbers go in the README:
            </p>
            <code class="run">cqrs seed -vehicle truck-7 -n 10000</code>
            <code class="run">for w in 1 2 4 8; do cqrs pool -workers $w -drain; done</code>
            <p class="note">
              <code>-drain</code> is what makes the numbers comparable: it
              rebuilds the pool's projection from sequence 1, so every run
              answers the same question against the same seed.
            </p>
          </div>
        </TabPanel>
      </TabPanels>
    </Tabs>
  </section>
</template>

<style scoped>
.lesson {
  margin-top: 20px;
}

header {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}

.eyebrow {
  margin: 0;
}

.cmd {
  margin-left: auto;
  color: var(--p-text-disabled-color);
  font-family: ui-monospace, 'SF Mono', Menlo, Consolas, monospace;
  font-size: 11px;
}

.lead {
  max-width: 84ch;
  margin: 12px 0 0;
  color: var(--p-text-muted-color);
}

/* Flush strip, card on the content — the AboutPanel.vue idiom. */
.panel-tabs {
  margin-top: 14px;
}

.card {
  padding: 14px 16px;
  border: 1px solid var(--lab-panel-border);
  border-radius: 6px;
  background: var(--lab-panel-bg);
}

.idle-card {
  max-width: 84ch;
}

.note {
  max-width: 84ch;
  margin: 0;
  color: var(--p-text-muted-color);
  font-size: 12px;
}

.run {
  display: block;
  margin-top: 8px;
  color: var(--p-text-color);
  font-family: ui-monospace, 'SF Mono', Menlo, Consolas, monospace;
  font-size: 12px;
}

/* One card per worker. They wrap rather than scroll: eight workers is a
   normal run and a horizontal scrollbar would hide half the lesson. */
.cards {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(220px, 1fr));
  gap: 12px;
  margin-top: 20px;
}

.worker.lost {
  border-color: var(--d4-lost);
}

.wh {
  display: flex;
  align-items: center;
  gap: 8px;
}

.wh b {
  font-family: ui-monospace, 'SF Mono', Menlo, Consolas, monospace;
  font-size: 13px;
}

.led {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: var(--p-text-disabled-color);
}

.led.on {
  background: var(--d4-read);
}

.led.lost {
  background: var(--d4-lost);
}

.doing {
  margin: 10px 0 0;
  color: var(--p-text-muted-color);
  font-family: ui-monospace, 'SF Mono', Menlo, Consolas, monospace;
  font-size: 12px;
}

.worker.lost .doing {
  color: var(--d4-lost);
}

.counts {
  margin: 6px 0 0;
  color: var(--p-text-disabled-color);
  font-family: ui-monospace, 'SF Mono', Menlo, Consolas, monospace;
  font-size: 11px;
}

.lost {
  color: var(--d4-lost);
}

.ok {
  color: var(--d4-read);
}

.damage {
  margin-top: 20px;
}

.kv {
  display: grid;
  grid-template-columns: max-content 1fr;
  gap: 4px 20px;
  margin: 10px 0 12px;
}

.kv dt {
  color: var(--p-text-muted-color);
  font-size: 12px;
}

.kv dd {
  margin: 0;
  font-family: ui-monospace, 'SF Mono', Menlo, Consolas, monospace;
  font-size: 12px;
}

.bar-row {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-top: 8px;
  font-family: ui-monospace, 'SF Mono', Menlo, Consolas, monospace;
  font-size: 12px;
}

.who {
  min-width: 8ch;
  color: var(--p-text-muted-color);
}

.track {
  flex: 1;
  height: 10px;
  border-radius: 3px;
  background: var(--lab-panel-border);
  overflow: hidden;
}

.fill {
  display: block;
  height: 100%;
  background: var(--d4-read);
}

/* An idle worker is the lesson, so it is coloured as loss, not as "less". */
.fill.idle {
  background: var(--d4-lost);
}

.right {
  min-width: 10ch;
  text-align: right;
  color: var(--p-text-disabled-color);
}
</style>
