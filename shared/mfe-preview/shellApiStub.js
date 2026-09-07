/*
  A stand-in for the shell API the loader passes to `activate(shellApi)`.

  It matters that this exists: `demo-catalog` refuses to activate without
  `{ version: 1, ui: { ExtensionRegion } }` and throws otherwise, so a harness
  with no shell API could not preview the one plugin that most needs previewing.

  The region component is a placeholder rather than the real one. In the shell,
  an extension point is filled by OTHER plugins' contributions; alone on its own
  port there are none, and drawing an empty box labelled with the point id is a
  truer picture than silently rendering nothing.
*/

/* A plain options object, not `defineComponent(...)`. Under Module Federation
   a bare `vue` import is filled in asynchronously (see ./federationReady.js),
   so calling any Vue function at module scope runs before Vue exists. Vue
   accepts a plain object component; `h` is only ever called inside render. */
import { h } from 'vue'

export const PreviewExtensionRegion = {
  name: 'PreviewExtensionRegion',
  props: {
    point: { type: String, required: true },
    context: { type: Object, default: () => ({}) },
  },
  setup(props) {
    return () => h('div', { class: 'mfe-preview-region', 'data-extension-point': props.point }, [
      h('span', { class: 'mfe-preview-region-label' }, 'extension point'),
      h('code', null, props.point),
      h('p', null, 'Filled by other plugins in the shell. Nothing to place here on this port.'),
    ])
  },
}

export function createPreviewShellApi() {
  return Object.freeze({
    version: 1,
    ui: Object.freeze({ ExtensionRegion: PreviewExtensionRegion }),
  })
}
