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
// NO NUMBER ON THIS PANEL IS INVENTED. Four of the five tabs are read live
// from KV odometer-pool-workers and KV odometer-pool. The fifth, "1 vs 4", is
// a RECORDED MEASUREMENT: four real `-drain` runs, made 2026-09-15 (plan task
// 04.7.4), kept as data in view/drain.js and repeated verbatim in the README.
// It is the one exception, and it is allowed only because the runs happened.
// A plausible-looking table of times this demo never measured would break the
// one promise it makes, so a row may not be added there before it is run.
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

import { POOL_KV, POOL_TRUTH_KV } from '../config.js'
import { DRAIN_SOURCE, MEASURED_AT, drainRows } from '../view/drain.js'
import RedeliveryTimeline from './RedeliveryTimeline.vue'
import {
  REDELIVERY_MEASURED_AT,
  REDELIVERY_SOURCE,
  redeliveryRows,
} from '../view/redelivery.js'
import {
  STARVATION_MEASURED_AT,
  STARVATION_REPEAT,
  STARVATION_SOURCE,
  starvationRows,
} from '../view/starvation.js'
import {
  ackBars,
  foldDamage,
  poolCaughtUp,
  poolHealth,
  poolLaneRows,
  redelivery,
  workerRows,
} from '../view/pool.js'
import { SEED_CMD, tabsFor } from '../view/lessons.js'
import BucketKeys from './BucketKeys.vue'
import LagLane from './LagLane.vue'
import PoolStrip from './PoolStrip.vue'

const props = defineProps({
  head: { type: Number, default: 0 },
  // KV odometer-pool-workers, one entry per worker.
  workers: { type: Object, default: () => new Map() },
  // KV odometer-pool — the pool's own, deliberately damaged fold.
  pool: { type: Object, default: () => new Map() },
  // KV odometer-pool-truth — the SAME log, folded correctly, one message at
  // a time. This is what the damage is measured against (04.8.8, D3).
  // Deliberately not odometer-read: that folds ODOMETER, a log lesson 02 no
  // longer touches, so subtracting it would call the whole pool total damage.
  truth: { type: Object, default: () => new Map() },
})

const TABS = tabsFor('lesson-02')
const tab = ref('live')
const current = computed(() => TABS.find((t) => t.key === tab.value) ?? TABS[0])

const rows = computed(() => workerRows(props.workers))
const health = computed(() => poolHealth(rows.value))
const bars = computed(() => ackBars(rows.value))
const poolRows = computed(() => [...props.pool.values()])
const truthRows = computed(() => [...props.truth.values()])

// How far the pool's own fold has reached: the furthest any vehicle in its
// bucket has been folded to, the same measure the other two sides use.
const foldSeq = computed(() =>
  poolRows.value.reduce((m, r) => Math.max(m, Number(r.lastSeq) || 0), 0),
)

const laneRows = computed(() => poolLaneRows(props.head, rows.value, foldSeq.value))
// A pool that has folded up to the head has nothing left to do. Every worker
// then reads `waiting · 0 acked`, which is correct and looks broken, so the
// panel says which one it is and prints the way out.
const caughtUp = computed(() => poolCaughtUp(props.head, foldSeq.value, health.value))

const damage = computed(() => foldDamage(poolRows.value, truthRows.value))
const kill = computed(() => redelivery(rows.value))

// Recorded, not watched. The only numbers on this panel that did not come
// off the wire in front of you — see view/drain.js.
const drain = drainRows()

// Recorded the same way, for the same reason — see view/redelivery.js. The
// live panel cannot time a redelivery; the server never reports the wait.
const redeliveries = redeliveryRows()

// Recorded the same way again — see view/starvation.js. This one corrects a
// claim the tab used to make, so it is not decoration: it is the evidence.
const starvation = starvationRows()

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
            <!-- The strip first: it is the only drawing that can show WHY an
                 event disappears. The lane under it answers a different
                 question — how far behind each worker is. -->
            <PoolStrip
              :head="head"
              :fold-seq="foldSeq"
              :rows="rows"
            />

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
                Fold damage · {{ POOL_KV }} against {{ POOL_TRUTH_KV }}
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
                <dt>total, correct fold</dt>
                <dd>{{ km(damage.truthKm) }}</dd>
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

            <div
              v-if="caughtUp"
              class="card hint"
              data-testid="pool-caught-up"
            >
              <p class="eyebrow">
                Caught up — nothing left to fold
              </p>
              <p class="note">
                The workers say <code>waiting</code> and <code>0 acked</code>
                because the pool has folded the whole log, not because the page
                is broken. Give it new events and the cards start moving:
              </p>
              <code class="run">{{ SEED_CMD }}</code>
            </div>
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
              Eight workers on a cap of three. At any instant five of them are
              parked, waiting for a slot. Over a whole run, every one of them
              gets served — the card below is four runs that say so.
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
              workers are asking for them. So a bar at zero right now is a
              worker waiting its turn, not a worker shut out.
            </p>

            <div
              v-if="caughtUp"
              class="hint bare"
              data-testid="starvation-caught-up"
            >
              <p class="note">
                Every bar is at zero because the pool has caught up, so there
                is no work to share out. Seed it first,
                then read the bars:
              </p>
              <code class="run">{{ SEED_CMD }}</code>
            </div>
          </div>

          <!-- The measurement. Outside the v-if for the same reason as the
               redelivery one: recorded data is true whether or not a pool is
               running. This card exists because the tab used to claim the
               opposite of what the runs show. -->
          <div
            class="card measured"
            data-testid="starvation-measured"
          >
            <p class="eyebrow">
              What the cap actually does — four recorded runs
            </p>
            <table class="rt">
              <thead>
                <tr>
                  <th>MaxAckPending</th>
                  <th>time</th>
                  <th>rate</th>
                  <th>workers that acked</th>
                  <th>folded</th>
                  <th>dropped</th>
                  <th>loss</th>
                </tr>
              </thead>
              <tbody>
                <tr
                  v-for="r in starvation"
                  :key="r.maxPending"
                  :data-testid="`starvation-cap-${r.maxPending}`"
                  :class="{ control: r.control }"
                >
                  <td><code>{{ r.maxPending }}</code></td>
                  <td>
                    <span class="track wide"><span
                      class="fill"
                      :style="{ width: `${r.barPct}%` }"
                    /></span>
                    {{ r.seconds }}s
                  </td>
                  <td>{{ r.rate.toLocaleString('en-GB') }}/s</td>
                  <td class="ok">{{ r.busy }} of {{ r.workers }}</td>
                  <td>{{ r.folded.toLocaleString('en-GB') }}</td>
                  <td :class="r.dropped ? 'lost' : 'ok'">
                    {{ r.dropped.toLocaleString('en-GB') }}
                  </td>
                  <td :class="r.dropped ? 'lost' : 'ok'">
                    {{ r.lossPct.toFixed(1) }}%
                  </td>
                </tr>
              </tbody>
            </table>
            <p class="note">
              {{ STARVATION_MEASURED_AT }}. The cap of
              <code>{{ STARVATION_REPEAT.maxPending }}</code> was run twice —
              {{ STARVATION_REPEAT.seconds }}s and
              {{ STARVATION_REPEAT.dropped.toLocaleString('en-GB') }} dropped
              the second time — so the numbers are a race, not a constant.
            </p>
            <p class="note hard">
              <b>No worker starved, at any cap.</b> Not even at a cap of one:
              all eight acked, within 2% of each other. A worker acks, a slot
              frees, the next fetch is served. <code>MaxAckPending</code>
              throttles the <b>consumer</b>; it does not idle a worker.
            </p>
            <p class="note hard">
              What it really is, is the <b>loss dial</b>. A smaller cap is
              slower and drops less, because there is less in flight to
              reorder. At a cap of 1 this pool folds the whole log and drops
              <b>nothing</b> — same eight workers, same code, a third of the
              speed. The dropping is BR-OD07 doing its job on events that
              arrived out of order.
            </p>
            <p class="note cmp">
              The nats.io worker-pool page says a low cap "starves a large set
              of workers". Both are true, about different things. At an
              <b>instant</b>, yes — <code>num_waiting</code> on the consumer
              sits at 4 or 5 of the 8 while <code>num_ack_pending</code> holds
              at 3. Over a <b>run</b>, no — every worker gets a turn. Set the
              cap for the loss you can accept, not to keep workers busy.
            </p>
          </div>

          <div
            class="term"
            data-testid="starvation-term"
          >
            <span class="lead">Ran this</span>
            <span v-for="line in STARVATION_SOURCE" :key="line"><span class="pr">$</span> {{ line }}</span>
            <span class="foot">
              <code>-drain</code> rebuilds the pool's fold from sequence 1 and
              stops at zero pending, so every run reads the same 74 109 events
              and the times can be compared.
            </span>
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
            <p class="note cmp">
              Stop any pool you already have running first. Every pool joins
              the same durable consumer, so a second one just shares the work
              and you cannot tell whose worker went quiet.
            </p>
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
              read model is stale. This screen cannot time that wait — the
              card below was read off two runs that did.
            </p>
          </div>

          <!-- The measurement. It sits outside the v-if because it is
               recorded data, not live state: it is true whether or not a
               pool is running right now. -->
          <div
            class="card measured"
            data-testid="redelivery-measured"
          >
            <p class="eyebrow">
              What the wait actually cost — two recorded runs
            </p>
            <!-- The 30s run drawn out, because the table says WHAT happened
                 and the line says WHEN the drop became unavoidable. -->
            <RedeliveryTimeline :run="redeliveries[0]" />
            <table class="rt">
              <thead>
                <tr>
                  <th>AckWait</th>
                  <th>waited</th>
                  <th>handed to</th>
                  <th>fold ran on</th>
                  <th>outcome</th>
                </tr>
              </thead>
              <tbody>
                <tr
                  v-for="r in redeliveries"
                  :key="r.ackWait"
                  :data-testid="`redelivery-${r.ackWait}`"
                >
                  <td><code>{{ r.ackWait }}</code></td>
                  <td>{{ r.waitedSeconds }}s</td>
                  <td>worker {{ r.handoff }}</td>
                  <td>+{{ r.ranOn }} events</td>
                  <td class="lost">{{ r.outcome }}</td>
                </tr>
              </tbody>
            </table>
            <p class="note">
              {{ REDELIVERY_MEASURED_AT }}. The wait is the
              <code>AckWait</code> and nothing else — 30s came back in
              {{ redeliveries[0].waitedSeconds }}s, 5s in
              {{ redeliveries[1].waitedSeconds }}s. The server is not retrying
              after a failure; it is running a timer out.
            </p>
            <p class="note hard">
              And the wait bought nothing. Both events came back to a
              <b>different worker</b>, and by then the watermark had folded
              {{ redeliveries[0].ranOn }} and {{ redeliveries[1].ranOn }} more
              events — so both redeliveries were <b>dropped on arrival</b>
              (BR-OD07). In a pool, a redelivery after <code>AckWait</code> is
              not a recovery. The kilometres are still gone.
            </p>
          </div>

          <div
            class="term"
            data-testid="redelivery-term"
          >
            <span class="lead">Ran this</span>
            <span v-for="line in REDELIVERY_SOURCE" :key="line"><span class="pr">$</span> {{ line }}</span>
            <span class="foot">
              The seed after each pool is what makes the fold run on while the
              worker is silent. Without it the pool has nothing left to fold,
              the watermark stays put, and the redelivered event would be
              accepted — which is the one case that does not happen in a busy
              system.
            </span>
          </div>
        </TabPanel>

        <!-- 1 VS 4 — a recorded measurement, the one thing on this panel
             that is not read live from KV. Four real `-drain` runs, kept as
             data in view/drain.js and repeated verbatim in the README.

             The shape is the one the mockup settled on (diagrams/
             worker-pool-ui-mockup.html, Tab 4): bars for the speed, then two
             cards — what the pool bought, and what it cost — then the
             terminal that produced them. Two cards, because the trade IS the
             lesson and a single table lets a reader take the fast number and
             walk away. -->
        <TabPanel value="scaling">
          <div
            class="card"
            data-testid="scaling-measured"
          >
            <p class="eyebrow">
              Time to drain {{ drain[0].events }} events
            </p>
            <div
              v-for="r in drain"
              :key="r.workers"
              class="bar-row"
              :data-testid="`drain-${r.workers}`"
            >
              <span class="who">{{ r.workers }} {{ r.workers === 1 ? 'worker' : 'workers' }}</span>
              <span class="track"><span
                class="fill"
                :class="r.control ? 'base' : 'ok'"
                :style="{ width: `${r.barPct}%` }"
              /></span>
              <span class="right">{{ r.seconds.toFixed(1) }} s</span>
            </div>
            <p class="note cmp">
              {{ MEASURED_AT }}. Four workers drain the same log
              <b>{{ drain[2].speedup.toFixed(1) }}x</b> quicker than one, not
              4x — past four the curve flattens, because the KV write is a
              shared cost and no number of workers makes it cheaper.
            </p>
          </div>

          <div class="cols">
            <div class="card">
              <p class="eyebrow">
                What the pool bought
              </p>
              <dl
                class="kv"
                data-testid="pool-bought"
              >
                <dt>1 worker</dt>
                <dd>{{ drain[0].rate }} events/s</dd>
                <dt>4 workers</dt>
                <dd class="won">
                  {{ drain[2].rate }} events/s
                </dd>
                <dt>speed-up</dt>
                <dd>{{ drain[2].speedup.toFixed(1) }}x</dd>
                <dt>order kept</dt>
                <dd class="lost">
                  no
                </dd>
              </dl>
            </div>

            <div class="card bad">
              <p class="eyebrow">
                What it cost
              </p>
              <dl
                class="kv"
                data-testid="pool-cost"
              >
                <dt>dropped (1 worker)</dt>
                <dd class="won">
                  {{ drain[0].dropped }}
                </dd>
                <dt>dropped (4 workers)</dt>
                <dd class="lost">
                  {{ drain[2].dropped }} · {{ drain[2].lossPct.toFixed(0) }}%
                </dd>
                <dt>dropped (8 workers)</dt>
                <dd class="lost">
                  {{ drain[3].dropped }} · {{ drain[3].lossPct.toFixed(0) }}%
                </dd>
              </dl>
              <p class="note">
                This is the whole trade in two cards. A pool is right for work
                that is independent. A fold is not independent work:
                <code>totalKm += km</code> is not safe when two workers fold
                the same vehicle at once. One worker drops nothing, which is
                why it is the control — one worker cannot race itself.
              </p>
            </div>
          </div>

          <div
            class="term"
            data-testid="drain-term"
          >
            <span class="lead">Ran this</span>
            <span v-for="line in DRAIN_SOURCE" :key="line"><span class="pr">$</span> {{ line }}</span>
            <span class="foot">
              <code>-drain</code> rebuilds the pool's projection from sequence
              1 and stops when the consumer reports 0 pending, so every run
              answers the same question against the same seed. The times
              reproduce. The dropped counts will not match exactly — a race is
              a race.
            </span>
          </div>
        </TabPanel>

        <!-- odometer-pool — the read-only view, `nats kv ls` on the screen.
             Beside odometer-pool-truth, because the pool's damage is per
             vehicle: a single total tells you kilometres are missing, this
             tells you which vehicle lost them. Both fold the SAME log,
             ODOMETER_POOL, which is what makes a row-by-row comparison mean
             anything (04.8.8, D3). BucketKeys takes side="read" for both,
             since both hold the ReadEntry shape from cqrs/read.go — the same
             project() builds them, pointed at two different buckets. -->
        <TabPanel value="pool">
          <div
            class="cols"
            data-testid="pool-buckets"
          >
            <BucketKeys
              side="read"
              :bucket="POOL_KV"
              :rows="poolRows"
              :head="head"
            />
            <BucketKeys
              side="read"
              :bucket="POOL_TRUTH_KV"
              :rows="truthRows"
              :head="head"
            />
          </div>
          <p class="note cmp">
            Same log, same fold, two results. Every row where the left total is
            lower than the right is an event BR-OD08 refused, and nothing
            repairs it.
          </p>
        </TabPanel>
      </TabPanels>
    </Tabs>
  </section>
</template>

<style scoped>
/* The shell already puts 18px between the page heading and this panel. A
   second 20px on top of that read as a gap the heading had been left
   behind in. */
.lesson {
  margin-top: 0;
}

header {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
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

/* Two buckets, side by side, for the same reason lesson 01 does it: the
   point is the difference between them. */
.cols {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 16px;
  margin-top: 20px;
  align-items: start;
}

@media (max-width: 1100px) {
  .cols {
    grid-template-columns: 1fr;
  }
}

.cmp {
  margin-top: 12px;
}

/* "This is idle, not broken." It reads as an aside rather than a warning:
   nothing has gone wrong, there is simply nothing to fold. */
.hint {
  margin-top: 16px;
  max-width: 84ch;
  border-style: dashed;
}

.hint .eyebrow {
  margin-bottom: 8px;
}

/* Inside a card already, so it takes no border of its own. */
.hint.bare {
  margin-top: 14px;
  padding: 0;
  border: 0;
  background: none;
}

/* The mockup's two-card trade (diagrams/worker-pool-ui-mockup.html). The
   cost card is outlined in the loss colour, so the reader cannot take the
   fast number without seeing what paid for it. */
.card.bad {
  border-color: color-mix(in srgb, var(--d4-lost) 45%, var(--lab-panel-border));
}

.kv dd.won {
  color: var(--d4-read);
}

.kv dd.lost {
  color: var(--d4-lost);
}

/* A faster run is a shorter bar, AND a different colour. `.base` is the
   one-worker control — the slow, correct baseline — so it takes the write-side
   colour and the pooled runs take the read-side one. Neither says "better":
   the two cards beside the bars are what price the trade.

   This is a modifier, not a change to `.fill` itself, because the Starvation
   tab shares that class and its colours mean something else. */
.fill.base {
  background: var(--d4-write);
}

.fill.ok {
  background: var(--d4-read);
}

/* The commands that produced the numbers above, drawn as the terminal they
   were typed into. Nothing on this tab is a claim you cannot re-run. */
.term {
  display: flex;
  flex-direction: column;
  gap: 3px;
  margin-top: 20px;
  padding: 11px 13px;
  border: 1px solid var(--lab-panel-border);
  border-radius: 6px;
  background: var(--lab-term-bg, #0f1012);
  font-family: ui-monospace, 'SF Mono', Menlo, Consolas, monospace;
  font-size: 12px;
}

.term .lead {
  padding-bottom: 3px;
  color: var(--p-text-disabled-color);
  font-family: var(--p-font-family);
  font-size: 10px;
  text-transform: uppercase;
  letter-spacing: 0.08em;
}

.term .pr {
  color: var(--p-text-disabled-color);
}

.term .foot {
  max-width: 84ch;
  margin-top: 6px;
  color: var(--p-text-muted-color);
  font-family: var(--p-font-family);
  font-size: 12px;
}

/* One card per worker. They wrap rather than scroll: eight workers is a
   normal run and a horizontal scrollbar would hide half the lesson. */
.lesson :deep(.lane) {
  margin-top: 18px;
}

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

/* The recorded runs. A table, not bars: five short facts per row and none of
   them is a magnitude, so a bar would only decorate them. */
/* Wider than the cards around it, and deliberately so: the time line inside
   it is 1060 units across, and squeezing that into an 84ch card renders its
   labels at about six pixels. The prose in here keeps its own 84ch. */
.measured {
  margin-top: 20px;
}

.measured .rt {
  max-width: 110ch;
}

.rt {
  width: 100%;
  margin: 10px 0 12px;
  border-collapse: collapse;
  font-family: ui-monospace, 'SF Mono', Menlo, Consolas, monospace;
  font-size: 12px;
}

.rt th {
  padding: 0 14px 6px 0;
  border-bottom: 1px solid var(--lab-panel-border);
  color: var(--p-text-disabled-color);
  font-weight: 500;
  font-size: 11px;
  text-align: left;
  letter-spacing: 0.04em;
  text-transform: uppercase;
}

.rt td {
  padding: 7px 14px 7px 0;
  border-bottom: 1px solid var(--lab-panel-border);
  color: var(--p-text-color);
}

.rt tr:last-child td {
  border-bottom: 0;
}

/* The time column carries a magnitude, so it gets a bar as well as a number.
   `.track` is a flex child everywhere else on this panel; in a table cell it
   has to size itself. */
.rt .track.wide {
  display: inline-block;
  flex: none;
  width: 80px;
  margin-right: 9px;
  vertical-align: middle;
}

/* The cap-of-1 run is the CONTROL — the one that folded the whole log. Every
   faster row is faster by losing something, so the correct run is the one
   marked, not the quickest. */
.rt tr.control td:first-child {
  box-shadow: inset 2px 0 0 var(--d4-read);
  padding-left: 8px;
}

/* The finding the tab exists for. It is the sentence a reader is most
   likely to skip, so it is the one that gets the rule beside it. */
.note.hard {
  margin-top: 10px;
  padding-left: 10px;
  border-left: 2px solid var(--d4-lost);
  color: var(--p-text-color);
}

.note.hard b {
  color: var(--d4-lost);
  font-weight: 600;
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

/* Wide enough for the LONGEST label this class carries, which is "8 workers"
   at 9 and "worker 10" at 9 — not for the shortest. At 8ch the one-worker row
   fitted and the rest did not, so every bar started 7px right of the control
   row, which is the one row a reader compares the others against. */
.who {
  min-width: 10ch;
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
