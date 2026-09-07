/*
  Browser half of the harness. One module, loaded by the page `vitePreview.js`
  serves at `/__preview`, shared unmodified by every plugin: everything that
  differs between plugins arrives in the options blob or is read from the
  plugin's own manifest at runtime.
*/

import * as Vue from 'vue'
import { h } from 'vue'

import { whenSharedScopeReady } from './federationReady.js'
import '../unifi-theme/unifi.css'
import './preview.css'
import PreviewApp from './PreviewApp.vue'

/* Neither `vue-router` nor a router instance exists here, and most plugins do
   not depend on vue-router at all — they use `<router-link>` because the SHELL
   registers it globally. Stubs keep those templates rendering; the link is
   inert and says so, because navigation is the shell's job and a preview that
   pretended otherwise would be lying about the boundary. */
const RouterLink = {
  name: 'RouterLink',
  props: { to: { type: [String, Object], default: '' } },
  setup(props, { slots }) {
    const label = () => (typeof props.to === 'string' ? props.to : JSON.stringify(props.to))
    return () => h('a', {
      href: '#',
      class: 'mfe-preview-link',
      title: `Inert in preview — the shell routes this (${label()})`,
      onClick: (event) => event.preventDefault(),
    }, slots.default?.())
  },
}

const RouterView = {
  name: 'RouterView',
  setup: () => () => h('div', { class: 'mfe-preview-note' }, 'router-view — the shell owns routing'),
}

export async function mountPreview() {
  /* Before any Vue call: under federation these bindings start out empty. */
  await whenSharedScopeReady(Vue)

  const options = JSON.parse(document.getElementById('mfe-preview-options').textContent)
  /* The theme's dark palette is the default in this repo; the page is served
     with class="p-dark" already, this only survives a hot reload that replaced
     the element. */
  document.documentElement.classList.add('p-dark')

  const app = Vue.createApp(PreviewApp, { options })
  app.component('RouterLink', RouterLink)
  app.component('RouterView', RouterView)

  /* Guarded by the flag the server read out of this plugin's package.json,
     never by try/catch: an import Vite cannot resolve is a transform failure
     and a full-screen overlay, not a catchable miss. */
  if (options.deps?.primevue) {
    const primevue = await import('./adapters/primevue.js')
    await primevue.install(app, { theme: Boolean(options.deps.primevueThemes) })
  }

  app.mount('#mfe-preview')
}
