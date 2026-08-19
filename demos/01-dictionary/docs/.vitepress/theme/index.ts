import DefaultTheme from 'vitepress/theme'
import type { Theme } from 'vitepress'
import EyebrowLabel from './components/EyebrowLabel.vue'
import VerdictBadge from './components/VerdictBadge.vue'
import './custom.css'

export default {
  extends: DefaultTheme,
  enhanceApp({ app }) {
    app.component('EyebrowLabel', EyebrowLabel)
    app.component('VerdictBadge', VerdictBadge)
  },
} satisfies Theme
