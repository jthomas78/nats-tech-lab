/*
  The lab's demos that have no frontend at all (task 16g, closing questions).

  Demos 02 and 03 are six NATS servers and a pile of shell scripts. They have
  no frontend, so they have no plugin, so they appear in no catalogue — and
  before this they appeared nowhere in the shell either, which made the demo
  menu quietly wrong about what the lab contains.

  They are listed here, shell-owned, and they are deliberately NOT made into
  plugins to achieve it: a `manifest.json` would admit them to the catalogue
  and a `demo.json` would give them a readiness endpoint, and both would be
  inventions. The closing rule of the phase says the two inventories may
  differ — the demo MENU lists the lab's demos, the PLUGINS screen lists the
  active catalogue's plugins — and this is where they differ.

  What they must never carry, and do not:

  - no health indicator. Nothing is set up to watch them, and decision 8
    forbids drawing a mark nobody took.
  - no readiness check. There is no service to ask and no mount to gate; a
    `checking…` that never resolves would be worse than silence.
  - no `plugin-source`. They have no plugin to source, and BR-AS81 forbids
    wearing the label as a permanent property of a demo.

  Each `findings` and `run` path is asserted to exist by `labDemos.spec.js`, so
  a renamed script or a moved report fails the suite rather than the reader.

  Demo 03 later grew ONE frontend, the T4 playground (plugin `demo-03`). It
  stays here, because its lab is still run from a terminal, and it gains a
  `plugin` field naming that plugin. When the plugin is active, the Home card
  opens the plugin's default route instead of the intro page; when it is not,
  the card opens the intro page as before. One demo, one card (BR-AS94). The
  plugin declares no readiness, so it never gets a second, probed card.
*/

export const LAB_DEMOS = Object.freeze([
  Object.freeze({
    id: '02-multi-region',
    name: 'Multi-Region Cluster Mechanics',
    question: 'Does a tenant wall hold between regions, and what crosses a gateway?',
    summary:
      'Six NATS servers and one small Go binary. No database, no services, no web pages. '
      + 'Two regions joined by a gateway, and the odometer domain folded from a stream into KV.',
    run: Object.freeze([
      Object.freeze({ label: 'Start both regions', command: 'docker compose -f demos/02-multi-region/deploy/compose.au.yaml up -d' }),
      Object.freeze({ label: 'Then the second region', command: 'docker compose -f demos/02-multi-region/deploy/compose.za.yaml up -d' }),
      Object.freeze({ label: 'Walk the questions', command: 'demos/02-multi-region/lab/01-the-wall.sh' }),
    ]),
    findings: Object.freeze([
      Object.freeze({ label: 'The demo\'s own write-up', path: 'demos/02-multi-region/README.md' }),
      Object.freeze({ label: 'Plan and reasoning', path: 'demos/02-multi-region/docs/Multi-Region-Plan.md' }),
    ]),
  }),
  Object.freeze({
    id: '03-multi-cluster-and-accounts',
    name: 'Multi-Cluster Topologies and Accounts',
    /* The plugin id, from the demo's own manifest.json. Not a source label:
       BR-AS81's ban is on `plugin-source`, which this is not. */
    plugin: 'demo-03',
    question: 'When one region goes dark, who is still alive to take a JetStream write?',
    summary:
      'Validation and showcase. The topology itself is the variable: seven runnable shapes, '
      + 'built from nothing on every run, measured and written up automatically. '
      + 'The T4 playground is the showcase: freeze a region of the live nine-server rig, '
      + 'resume it, or request leadership, and watch what the servers report. '
      + 'A playground session is never evidence.',
    run: Object.freeze([
      Object.freeze({ label: 'Build and measure every topology', command: 'demos/03-multi-cluster-and-accounts/lab/run-all.sh' }),
      Object.freeze({ label: 'Start the playground control service', command: 'go run ./demos/03-multi-cluster-and-accounts/playground serve -lab demos/03-multi-cluster-and-accounts/lab' }),
    ]),
    findings: Object.freeze([
      Object.freeze({ label: 'Generated report', path: 'demos/03-multi-cluster-and-accounts/REPORT.md' }),
      Object.freeze({ label: 'Generated report, with diagrams', path: 'demos/03-multi-cluster-and-accounts/REPORT.html' }),
      Object.freeze({ label: 'Exercise 10 report, the hub meta-leader', path: 'demos/03-multi-cluster-and-accounts/REPORT-10.html' }),
      Object.freeze({ label: 'Pattern cards', path: 'demos/03-multi-cluster-and-accounts/docs/03-multi-cluster-and-accounts-pattern-cards-v0.6.pdf' }),
    ]),
  }),
])

/** The shell-owned route name for one of these intro pages. */
export const LAB_DEMO_ROUTE = 'shell/lab-demo'

export function labDemo(id) {
  return LAB_DEMOS.find((demo) => demo.id === id) ?? null
}
