/* Demo 03's plugin entry module — the T4 playground.

   The shape is the whole contract between a remote and the shell, as demo 04:

     { components: { [name]: Component }, activate?(): void }

   `components` is keyed by the `component` name each route contribution
   declares in `public/manifest.json`: the Meta-Leader page, and the one
   component the two old links share.

   Nothing here imports a shell module, and nothing renders
   `@ui-shell/AppShell` — `lab-shell` owns the frame when the demo is embedded.
   The demo's own stylesheet is imported here because `main.js` does not run in
   this mode. The shared theme and PrimeIcons are NOT imported: the shell
   already loads both. */
import './styles/playground.css'

import { EMBEDDED_COMMAND_API, setCommandApi } from './config.js'
import LegacyRoute from './plugin/LegacyRoute.vue'
import MetaLeaderRoute from './plugin/MetaLeaderRoute.vue'

export const components = {
  'meta-leader': MetaLeaderRoute,
  legacy: LegacyRoute,
}

/* Embedded, the page is on the shell's origin. An absolute call to 20302 is
   then cross-origin, and the service grants only 20301 and the shell's dev
   port. The shell proxies the routes `public/demo.json` declares, on its own
   origin, and this is where the page is told to use them. */
export function activate() {
  setCommandApi(EMBEDDED_COMMAND_API)
}
