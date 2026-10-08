<script setup>
// The Live tab (PLAYGROUND-PLAN.md, Part 2). The left column acts on the rig
// and shows what the nine servers report; the right column is the guide and
// the history. It owns no controller: MetaLeaderRoute holds the one
// controller above the tabs and hands down its state and actions, so a tab
// switch keeps every observation, history line and pending command.
import ClientSettings from './ClientSettings.vue'
import ClusterPanel from './ClusterPanel.vue'
import Exercise10Guide from './Exercise10Guide.vue'
import GatewayLinks from './GatewayLinks.vue'
import HistoryLog from './HistoryLog.vue'
import LeadershipCard from './LeadershipCard.vue'
import MetaProbeCard from './MetaProbeCard.vue'
import MetaSummary from './MetaSummary.vue'
import RigBar from './RigBar.vue'
import TransitionsCard from './TransitionsCard.vue'
import { hms } from '../usePlayground.js'

defineProps({
  state: { type: Object, required: true },
  actions: { type: Object, required: true },
})
</script>

<template>
  <section
    class="pg"
    :class="{ unreachable: state.service.reachable === false }"
    data-testid="playground"
  >
    <div class="pg-main">
      <RigBar
        :state="state"
        :actions="actions"
      />
      <div
        v-for="r in state.refusals.slice(0, 3)"
        :key="`${r.at}-${r.path}`"
        class="refusal"
        data-testid="refusal"
      >
        <b>Refused</b> {{ r.path }}: {{ r.message }}
        <span class="n">{{ hms(r.at) }}</span>
      </div>
      <MetaSummary :state="state" />
      <ClientSettings
        :via="state.settings.via"
        :timeout-s="state.settings.timeoutS"
        @update:via="actions.setVia"
        @update:timeout-s="actions.setTimeoutS"
      />
      <div class="topo">
        <GatewayLinks :arrows="state.snap?.arrows ?? []" />
        <div class="toprow">
          <div class="corner">
            <LeadershipCard :state="state" />
            <MetaProbeCard
              :state="state"
              :actions="actions"
            />
          </div>
          <div class="slot">
            <ClusterPanel
              :state="state"
              :actions="actions"
              cluster="arb"
            />
          </div>
          <div class="corner">
            <TransitionsCard :state="state" />
          </div>
        </div>
        <div class="botrow">
          <div class="slot">
            <ClusterPanel
              :state="state"
              :actions="actions"
              cluster="za"
            />
          </div>
          <div class="slot">
            <ClusterPanel
              :state="state"
              :actions="actions"
              cluster="au"
            />
          </div>
        </div>
      </div>
    </div>
    <aside
      class="pg-side"
      aria-label="Guide and history"
    >
      <Exercise10Guide :state="state" />
      <HistoryLog :state="state" />
    </aside>
  </section>
</template>
