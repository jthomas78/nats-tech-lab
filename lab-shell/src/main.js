import '@unifi-theme/unifi.css'
import 'primeicons/primeicons.css'

import { definePreset } from '@primevue/themes'
import Aura from '@primevue/themes/aura'
import { createPinia } from 'pinia'
import PrimeVue from 'primevue/config'
import { createApp } from 'vue'
import { createRouter, createWebHistory } from 'vue-router'

import { createUnifiPreset, enableDarkMode, themeOptions } from '@unifi-theme/preset.js'

import App from './App.vue'
import DemoIntroView from './views/DemoIntroView.vue'
import MenuView from './views/MenuView.vue'

enableDarkMode()

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', name: 'menu', component: MenuView },
    { path: '/demos/:id', name: 'demo-intro', component: DemoIntroView, props: true },
  ],
})

const app = createApp(App)
app.use(createPinia())
app.use(router)
app.use(PrimeVue, {
  theme: {
    preset: createUnifiPreset(definePreset, Aura),
    options: themeOptions,
  },
})
app.mount('#app')
