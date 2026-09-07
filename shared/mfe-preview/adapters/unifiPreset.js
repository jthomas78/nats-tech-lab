/*
  The PrimeVue theme preset, in its own module ON PURPOSE.

  `@primevue/themes` is not a dependency of every plugin, and Vite resolves a
  module's imports when it transforms it — an unresolvable one is a build
  error and a full-screen overlay, not a runtime miss. Kept here, the file is
  imported only after the server has confirmed the package is declared, and a
  plugin without it never causes the resolution to be attempted.
*/

import { definePreset } from '@primevue/themes'
import Aura from '@primevue/themes/aura'

import { createUnifiPreset, themeOptions } from '../../unifi-theme/preset.js'

export function unifiTheme() {
  return { preset: createUnifiPreset(definePreset, Aura), options: themeOptions }
}
