import '@unifi-theme/unifi.css'
import 'primeicons/primeicons.css'
import './styles/playground.css'

import { definePreset } from '@primevue/themes'
import Aura from '@primevue/themes/aura'
import PrimeVue from 'primevue/config'
import { createApp } from 'vue'

import { createUnifiPreset, enableDarkMode, themeOptions } from '@unifi-theme/preset.js'

import App from './App.vue'

enableDarkMode()

const app = createApp(App)
app.use(PrimeVue, {
  theme: {
    preset: createUnifiPreset(definePreset, Aura),
    options: themeOptions,
  },
})
app.mount('#app')
