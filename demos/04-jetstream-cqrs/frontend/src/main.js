import '@unifi-theme/unifi.css'
import 'primeicons/primeicons.css'
import './styles/sides.css'

import { definePreset } from '@primevue/themes'
import Aura from '@primevue/themes/aura'
import PrimeVue from 'primevue/config'
import { createApp } from 'vue'

import { createUnifiPreset, enableDarkMode, themeOptions } from '@unifi-theme/preset.js'

import App from './App.vue'

// No Pinia here, unlike demo 01's apps. This UI's state is not its own: it is
// a projection of two KV buckets and one stream, and the NATS watch already
// holds it. A store in front of that would be a third read model of the same
// events, which is the opposite of what this demo teaches.
enableDarkMode()

const app = createApp(App)
app.use(PrimeVue, {
  theme: {
    preset: createUnifiPreset(definePreset, Aura),
    options: themeOptions,
  },
})
app.mount('#app')
