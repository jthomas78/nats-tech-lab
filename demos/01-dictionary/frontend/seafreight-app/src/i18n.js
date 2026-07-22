// vue-i18n instance for the port UI (Phase 11.7). Lives in this app's own
// src/ tree (not shared/refdata/) because Vite/Rollup resolves bare npm
// specifiers relative to the importing file — a file outside the app root
// can't resolve a second package the way @refdata's vue-only composables do.
// Seeded with the bundled fallback catalog so chrome renders correctly
// before useUiCopy's first fetch resolves; refdata then overlays the live
// ui-copy catalog on top (passed into useUiCopy's connect()).
import { createI18n } from 'vue-i18n'

import { uiCopyFallbackEn } from '@refdata/uiCopyFallback.en.js'

export const i18n = createI18n({
  legacy: false,
  locale: 'en',
  fallbackLocale: 'en',
  messages: { en: { ...uiCopyFallbackEn } },
})
