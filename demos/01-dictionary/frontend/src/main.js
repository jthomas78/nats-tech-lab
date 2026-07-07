import '@unifi-theme/unifi.css'
import 'primeicons/primeicons.css'

import { definePreset } from '@primevue/themes'
import Aura from '@primevue/themes/aura'
import { createPinia } from 'pinia'
import PrimeVue from 'primevue/config'
import ToastService from 'primevue/toastservice'
import { createApp } from 'vue'

import { createUnifiPreset, enableDarkMode, themeOptions } from '@unifi-theme/preset.js'

import App from './App.vue'

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
app.mount('#app')
