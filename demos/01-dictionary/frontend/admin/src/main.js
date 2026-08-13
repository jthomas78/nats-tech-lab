import '@unifi-theme/unifi.css'
import 'primeicons/primeicons.css'

import { definePreset } from '@primevue/themes'
import Aura from '@primevue/themes/aura'
import { createPinia } from 'pinia'
import PrimeVue from 'primevue/config'
import ToastService from 'primevue/toastservice'
import Tooltip from 'primevue/tooltip'
import { createApp } from 'vue'

import { createUnifiPreset, enableDarkMode, themeOptions } from '@unifi-theme/preset.js'

import App from './App.vue'
import { i18n } from './i18n.js'

enableDarkMode()

const app = createApp(App)
app.use(createPinia())
app.use(PrimeVue, {
  theme: {
    preset: createUnifiPreset(definePreset, Aura),
    options: themeOptions,
  },
})
app.use(ToastService)
app.directive('tooltip', Tooltip)
app.use(i18n)
app.mount('#app')
