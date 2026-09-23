import { join } from 'node:path'

import { describe, expect, it } from 'vitest'

import { checkFrame, frameViolations, importsOf, SHELL_DIR, shellSourceFiles } from './frameOwnership.js'

describe('BR-AS09 — the shell frame owns no feature', () => {
  it('finds the shell modules to check', () => {
    expect(shellSourceFiles().length).toBeGreaterThan(8)
  })

  it('passes on the shell as it stands', () => {
    expect(checkFrame()).toEqual([])
  })
})

describe('the check itself catches what it is for', () => {
  const file = join(SHELL_DIR, 'bootShell.js')
  const violationsIn = (source) => frameViolations(file, source).map((v) => v.specifier)

  it('catches a shell module importing a view', () => {
    expect(violationsIn("import MenuView from '../views/MenuView.vue'\n")).toEqual([
      '../views/MenuView.vue',
    ])
  })

  it('catches a shell module importing a plugin', () => {
    expect(violationsIn("import { demoCatalogManifest } from '../plugins/demo-catalog/manifest.js'\n")).toEqual([
      '../plugins/demo-catalog/manifest.js',
    ])
  })

  it('catches a shell module importing the demo registry', () => {
    expect(violationsIn("import { demos } from '../demos.js'\n")).toEqual(['../demos.js'])
  })

  it('catches a shell module reaching for a PrimeVue widget', () => {
    // Not "app-specific" by name, but a frame that renders a DataTable is
    // building a screen.
    expect(violationsIn("import DataTable from 'primevue/datatable'\n")).toEqual([
      'primevue/datatable',
    ])
  })

  it('allows the framework the frame is written in', () => {
    expect(violationsIn("import { computed } from 'vue'\nimport { createRouter } from 'vue-router'\n")).toEqual([])
  })

  it('allows a sibling shell module', () => {
    expect(violationsIn("import { PLUGIN_STATUS } from './registry/pluginStatus.js'\n")).toEqual([])
  })

  it('does not read prose in a comment as an import', () => {
    expect(importsOf("/* the value comes from 'somewhere else' entirely */\n")).toEqual([])
  })
})

/* The named exception (BR-AS79, task 16e). It buys ONE component for ONE
   file, and these specs exist so it cannot quietly become a doorway. */
describe('the one named frame exception', () => {
  const gate = join(SHELL_DIR, 'demos/demoGate.js')
  const other = join(SHELL_DIR, 'bootShell.js')
  const panel = "import DemoStatePanel from '@ui-shell/DemoStatePanel.vue'\n"

  it('lets the readiness gate draw the one shared panel', () => {
    expect(frameViolations(gate, panel)).toEqual([])
  })

  it('does not let any other shell module draw it', () => {
    expect(frameViolations(other, panel).map((v) => v.specifier)).toEqual([
      '@ui-shell/DemoStatePanel.vue',
    ])
  })

  it('does not open the rest of @ui-shell to the gate', () => {
    const source = "import AppShell from '@ui-shell/AppShell.vue'\n"
    expect(frameViolations(gate, source).map((v) => v.specifier)).toEqual([
      '@ui-shell/AppShell.vue',
    ])
  })
})
