<script setup>
// The overview route: what the playground is, what each control does, and
// what the page cannot tell you. Static text. Measured results stay in the
// reports and the deck; this page only links to them.
defineProps({
  routeId: { type: String, default: 'overview' },
})

const START = 'go run ./demos/03-multi-cluster-and-accounts/playground serve -lab demos/03-multi-cluster-and-accounts/lab'
</script>

<template>
  <section
    class="pg-overview"
    data-testid="overview"
  >
    <div>
      <h1>T4 playground</h1>
      <p class="q">
        Freeze a region, resume it, or request the meta leader in another cluster. Then watch what the nine servers
        report, and try a write and a metadata change.
      </p>
      <p>
        The rig is T4: three clusters of three nats-server processes on this machine, za, au and the hub arb, joined by
        gateways. All nine vote on JetStream metadata. Each cluster has its own stream, ODOMETER_ZA, ODOMETER_ARB and
        ODOMETER_AU, with three copies inside that cluster.
      </p>
      <h2>What each control does</h2>
      <ul>
        <li>
          <b>Start rig</b> runs <span class="mono">lab/rig-t4.sh up</span>. The service owns that rig and stops it when
          it shuts down.
        </li>
        <li>
          <b>Attach</b> uses a rig that is already running. The service checks all nine processes first. It never stops
          a rig it attached to.
        </li>
        <li>
          <b>Freeze</b> turns Dark on: SIGSTOP to the cluster's three processes. This is a clean, total stop. It is not a
          network partition.
        </li>
        <li><b>Resume</b> turns Dark off: SIGCONT. <b>Restore all frozen clusters</b> does it for every cluster.</li>
        <li>
          <b>Request leadership here</b> asks the meta leader to step down in favour of that cluster. NATS can refuse, or
          elect elsewhere. The page shows the leader it then observed.
        </li>
        <li>
          <b>Publish new</b> writes one message with a new ID. <b>Retry same ID</b> sends the same message again.
          JetStream drops a copy it already holds and says so.
        </li>
        <li>
          <b>Verify storage</b> reads the stream back and says, for each message ID, present or absent at that time.
        </li>
        <li>
          <b>Try metadata operation</b> creates and deletes a small stream. It needs a meta leader. A stream write does
          not.
        </li>
        <li>
          <b>Gateway arrows</b> join the panels: arb at the top, za and au below it. Each arrow is one direction, read
          from the server that dials out. The tag n/3 counts the servers whose fresh reading lists that direction. A
          dashed arrow has no fresh reading from its own end. A listed gateway is not proof that the far side answers.
        </li>
        <li>
          <b>Client connection</b> picks the cluster the service connects through. The destination stream is set by the
          panel. So you can publish through au to ODOMETER_ZA.
        </li>
      </ul>
      <h2>What the page cannot tell you</h2>
      <ul>
        <li>
          A monitor that does not answer is not proof that the process stopped, or that quorum is lost. Process state is
          shown on its own.
        </li>
        <li>A missing or stale reading is unknown. It never counts as a vote, and never as zero.</li>
        <li>Times to agreement are polled every 0.5 s, so each is good to about half a second.</li>
        <li>A publish that timed out has an unknown outcome. Only a later Verify storage can say present or absent.</li>
        <li>What the playground shows is not evidence. Measured results live in REPORT-10.</li>
      </ul>
    </div>
    <div>
      <div class="box">
        <h3>Start</h3>
        <div>The control service, from the repo root:</div>
        <pre>{{ START }}</pre>
        <div style="margin-top: 8px">
          Then open the Playground and press Start rig, or Attach.
        </div>
      </div>
      <div class="box">
        <h3>Measured results</h3>
        <div>In <span class="mono">demos/03-multi-cluster-and-accounts/</span>:</div>
        <ul style="margin: 0; padding-left: 18px">
          <li><span class="mono">REPORT-10.html</span>: exercise 10, every check</li>
          <li>
            <span class="mono">docs/03-multi-cluster-and-accounts-pattern-cards-v0.6.pdf</span>: card 14
          </li>
          <li><span class="mono">exercises/EXERCISE-10-TERMINAL-STEPS.md</span>: the same actions by hand</li>
        </ul>
      </div>
      <div class="box">
        <h3>Do not run beside the lab</h3>
        <p style="margin: 0">
          A lab script stops every t- server when it starts. Stop the playground before you run
          <span class="mono">run-all.sh</span> or exercise 10.
        </p>
      </div>
    </div>
  </section>
</template>
