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
              // `src/views/*`, `src/plugins/*` and `src/demos*` — the feature
              // side of the tree, written at both depths a shell module can
              // sit at. NOT `src/shell/demos/*`, which is shell-owned: it
              // holds the readiness gate (BR-AS79), which knows a demo's
              // STATE and nothing about any demo's features.
              group: [
                '../views/*', '../../views/*',
                '../plugins/*', '../../plugins/*',
                '../../demos*', '../../../demos*',
              ],
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
  {
    /* The one allowance, and it is narrow on purpose.

       BR-AS79 asks for ONE presentation component drawn by BOTH the shell's
       pre-mount panel and a plugin's running-state errors. The shell half of
       that is `demoGate.js`, so this module — and only this module — may
       import `@ui-shell/DemoStatePanel.vue`.

       This is not a hole in BR-AS09. `@ui-shell` is the app FRAME, which the
       shell owns; the rule above bars it from every other shell module so a
       screen cannot quietly grow here. The gate renders no feature: it draws
       a demo's availability, which is the shell's own concern. */
    files: ['src/shell/demos/demoGate.js'],
    rules: {
      'no-restricted-imports': [
        'error',
        {
          patterns: [
            {
              group: ['primevue/*', '@primevue/*', '@unifi-theme/*'],
              message:
                'The gate draws one shared panel, not a screen of its own (BR-AS09).',
            },
          ],
        },
      ],
    },
  },
]
