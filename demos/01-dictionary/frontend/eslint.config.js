import pluginVue from 'eslint-plugin-vue'

export default [
  ...pluginVue.configs['flat/recommended'],
  {
    rules: {
      // v-model:value is valid Vue 3 named v-model syntax (e.g. PrimeVue Tabs).
      // This rule is a Vue 2 holdover.
      'vue/no-v-model-argument': 'off',
    },
  },
]
