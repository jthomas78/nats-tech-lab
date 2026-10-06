<script setup>
// The playground route (PLAYGROUND-PLAN.md, Part 2). One controller, one
// state, handed to every part. The left column acts on the rig and shows
// what the nine servers report; the right column is the guide and the
// history. No AppShell and no router: the lab shell owns the frame.
import { onMounted, onUnmounted } from 'vue'

import ClientSettings from '../components/ClientSettings.vue'
import ClusterPanel from '../components/ClusterPanel.vue'
import Exercise10Guide from '../components/Exercise10Guide.vue'
import GatewayLinks from '../components/GatewayLinks.vue'
import HistoryLog from '../components/HistoryLog.vue'
import LeadershipCard from '../components/LeadershipCard.vue'
import MetaProbeCard from '../components/MetaProbeCard.vue'
import MetaSummary from '../components/MetaSummary.vue'
import RigBar from '../components/RigBar.vue'
import TransitionsCard from '../components/TransitionsCard.vue'
import { hms, usePlayground } from '../usePlayground.js'

defineProps({
  routeId: { type: String, default: 'playground' },
})

const { state, actions, start, stop } = usePlayground()

onMounted(start)
onUnmounted(stop)
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
        v-model:via="state.settings.via"
        v-model:timeout-s="state.settings.timeoutS"
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
