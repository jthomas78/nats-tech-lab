/* Demo 04's plugin entry module (task 16d).

   The exported shape is the whole contract between a remote and the shell:

     { components: { [name]: Component }, activate?(): void|Promise<void> }

   `components` is keyed by the `component` name each contribution declares in
   `public/manifest.json`. There is exactly ONE key, because demo 04 makes
   exactly one `route` contribution: its existing panel container, with its
   panel and tab interaction unchanged.

   Nothing here imports a shell module, and the entry never renders
   `@ui-shell/AppShell` — `lab-shell` owns the outer chrome when the demo is
   embedded (BR-AS09). The standalone entry (`src/main.js` → `App.vue`) keeps
   its own `AppShell`, and both are produced by the same build from these same
   sources (BR-AS78).

   The demo's own global stylesheet is imported here because `main.js` does not
   run in this mode. The shared theme and the PrimeIcons font are NOT imported:
   the shell already loads both, and a second copy would fight it. */
import './styles/sides.css'

import OdometerRoute from './plugin/OdometerRoute.vue'

export const components = {
  odometer: OdometerRoute,
}

/* Called at most once per plugin, by the loader, after the chunk arrives and
   before anything renders. Demo 04 needs no setup here: the NATS watch opens
   in the route component's own `onMounted`, so it starts when the demo is
   opened and stops when it is left. */
export function activate() {}
