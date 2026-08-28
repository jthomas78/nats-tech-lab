/*
  The demo catalog's plugin module — the shape every plugin exports:
  an optional `activate()` called once, and a `components` record keyed by the
  `component` name each route contribution declares.
*/

import DemoIntroView from '../../views/DemoIntroView.vue'
import MenuView from '../../views/MenuView.vue'

export const components = {
  catalog: MenuView,
  intro: DemoIntroView,
}

export function activate() {
  /* Nothing to set up: the catalog's data is a static module in the shell's
     own bundle. The hook is here because the contract says it runs once, and
     a plugin that grows a connection or a store later should not have to
     change shape to acquire one. */
}
