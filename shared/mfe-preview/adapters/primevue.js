/*
  PrimeVue, for a plugin that declares it. This module is imported only when
  the server saw `primevue` in the plugin's package.json — a plugin without it
  (example-plugin) never loads this file at all.
*/

import PrimeVue from 'primevue/config'

export async function install(app, { theme: wantsTheme = false } = {}) {
  let theme = null
  if (wantsTheme) {
    /* Only when the plugin declares @primevue/themes. Reaching for it
       otherwise fails the transform of THIS file and takes PrimeVue with it —
       see the note in vitePreview.js's declaredDeps(). */
    const { unifiTheme } = await import('./unifiPreset.js')
    theme = unifiTheme()
  }
  /* Without the preset, PrimeVue's styled defaults. Components still render,
     and everything the plugin styles itself still uses the lab tokens from
     unifi.css. */
  app.use(PrimeVue, theme ? { theme } : {})
}
