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
  {
    // BR-AS09 — the shell frame owns no feature. This is the editor-time half
    // of the rule; `tools/frameOwnership.js` (run by its spec, and standalone
    // in a build) is the half that sees the whole graph, including bare
    // packages this pattern list cannot enumerate.
    files: ['src/shell/**/*.js'],
    ignores: ['src/shell/**/*.spec.js'],
    rules: {
      'no-restricted-imports': [
        'error',
        {
          patterns: [
            {
              group: ['../views/*', '../../views/*', '../plugins/*', '../../plugins/*', '../demos*', '../../demos*'],
              message:
                'The shell frame renders plugins through the contribution API; it does not import features (BR-AS09).',
            },
            {
              group: ['primevue/*', '@primevue/*', '@unifi-theme/*', '@ui-shell/*'],
              message:
                'A shell module that renders a widget is building a screen. Keep presentation in the app frame or a plugin (BR-AS09).',
            },
          ],
        },
      ],
    },
  },
]
