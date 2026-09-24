/* Demo 04's plugin entry module (task 16d).

   The exported shape is the whole contract between a remote and the shell:

     { components: { [name]: Component }, activate?(): void|Promise<void> }

   `components` is keyed by the `component` name each contribution declares in
   `public/manifest.json`. There is exactly ONE key, and demo 04 makes TWO
   `route` contributions — one per lesson. Both name the same component, which
   is the point: task 17h split the ROUTES, not the UI. The shell tells the
   component which of the two it is showing (BR-AS91), and the panels and tabs
   below it are unchanged.

   Nothing here imports a shell module, and the entry never renders
   `@ui-shell/AppShell` — `lab-shell` owns the outer chrome when the demo is
   embedded (BR-AS09). The standalone entry (`src/main.js` → `App.vue`) keeps
   its own `AppShell`, and both are produced by the same build from these same
   sources (BR-AS78).

   The demo's own global stylesheet is imported here because `main.js` does not
   run in this mode. The shared theme and the PrimeIcons font are NOT imported:
   the shell already loads both, and a second copy would fight it. */
import './styles/sides.css'

import { EMBEDDED_COMMAND_API, setCommandApi } from './config.js'
import LessonRoute from './plugin/LessonRoute.vue'

export const components = {
  lesson: LessonRoute,
}

/* Called at most once per plugin, by the loader, after the chunk arrives and
   before anything renders. That timing is what this hook is for here.

   Embedded, the page is served on the shell's origin. An absolute call to
   `http://127.0.0.1:20402` is then cross-origin, and the shim answers 200 with
   `Access-Control-Allow-Origin: http://localhost:20401` — so the browser
   discards it and every button on the page reads `Failed to fetch`. Widening
   that grant is forbidden (F-3, and this demo's own CLAUDE.md). The shell
   instead proxies the routes `public/demo.json` declares, on its own origin,
   and this is where the page is told to use them.

   The NATS watch is NOT moved, and does not need to be. It opens in the route
   component's own `onMounted` and goes straight to `20403`. A WebSocket is not
   subject to CORS at all, so no proxy would have helped it; it is gated by the
   server's own `allowed_origins` list in `deploy/nats.conf`, which now names
   the shell's origin as well. A different door, opened by a different change. */
export function activate() {
  setCommandApi(EMBEDDED_COMMAND_API)
}
