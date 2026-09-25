/*
  The example plugin's entry module (BR-AS15).

  The exported shape is the whole contract between a remote and the shell:

    { components: { [name]: Component }, activate?(): void|Promise<void> }

  `components` is keyed by the `component` name each contribution declares in
  the registry entry. Nothing else crosses the boundary — the plugin imports no
  shell module, holds no reference to the router, and never touches DOM outside
  the container it is rendered into (BR-AS02, BR-AS09).
*/

import DemoSidebarPanel from './views/DemoSidebarPanel.vue'
import FooterItem from './views/FooterItem.vue'
import HomePanel from './views/HomePanel.vue'
import OverviewView from './views/OverviewView.vue'
import RenderThrows from './views/RenderThrows.vue'
import TopbarControl from './views/TopbarControl.vue'

export const components = {
  overview: OverviewView,
  'home-panel': HomePanel,
  'demo-sidebar': DemoSidebarPanel,
  'topbar-control': TopbarControl,
  'footer-item': FooterItem,
  'render-throws': RenderThrows,
}

/* Called at most once per plugin, by the loader, after the chunk arrives and
   before anything renders. This plugin ignores the additive shellApi argument;
   its contributions need only the context props supplied by their hosts. */
export function activate() {
   
  console.info('[example-plugin] activate() ran once')
}
