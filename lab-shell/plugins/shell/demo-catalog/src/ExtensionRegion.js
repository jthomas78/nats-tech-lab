import { defineComponent, h } from 'vue'
import { getShellApi } from './shellApi.js'

// A plugin-local wrapper, not an import from the host. No Vue provide() call
// is needed at activation, which runs outside a component setup context.
export default defineComponent({
  name: 'CatalogExtensionRegion',
  inheritAttrs: false,
  setup(_, { attrs, slots }) {
    return () => h(getShellApi().ui.ExtensionRegion, attrs, slots)
  },
})
