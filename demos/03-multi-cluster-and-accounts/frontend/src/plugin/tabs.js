// The Meta-Leader page's two tabs, and where the chosen tab is kept.
//
// - The URL holds the tab: /demo-03/meta-leader/<tab>. The shell hands a
//   route param to the component as a prop (BR-AS91), so a reload, a direct
//   link and back/forward all land on the tab the URL names.
// - The session remembers it. `sessionTab` lives as long as this module, so a
//   reader who leaves the page and comes back through the rail gets the tab
//   they left. A fresh launch of the frontend loads the module again and
//   starts on Overview.
// - The old links still work. /demo-03/playground opens Live and
//   /demo-03/overview opens Overview, through LegacyRoute. Neither has a
//   rail entry.
import { getCurrentInstance } from 'vue'

export const TABS = Object.freeze([
  Object.freeze({ key: 'overview', label: 'Overview' }),
  Object.freeze({ key: 'live', label: 'Live' }),
])
export const DEFAULT_TAB = 'overview'

/** The route id of an old link, and the tab it opens. */
export const LEGACY_TABS = Object.freeze({ playground: 'live', overview: 'overview' })

/* Must match the meta-leader route's path in public/manifest.json; a spec
   holds the two together. */
export const PAGE_PATH = '/demo-03/meta-leader'

export const isTab = (t) => TABS.some((x) => x.key === t)
export const tabPath = (t) => `${PAGE_PATH}/${t}`

let sessionTab = DEFAULT_TAB

export const sessionTabOf = () => sessionTab
export function rememberTab(t) {
  if (isTab(t)) sessionTab = t
}
/** For specs only: a fresh launch. */
export function forgetTab() {
  sessionTab = DEFAULT_TAB
}

/* The shell's router, as the app this page is mounted in exposes it. It is
   never imported: vue-router is the shell's own dependency and is not shared
   across the federation boundary, and a second copy would have no history
   (BR-AS91). Standalone and in specs there is none, and the tab is then
   local state only. Call it during setup. */
export function useShellRouter() {
  return getCurrentInstance()?.appContext.config.globalProperties.$router ?? null
}

/** Go to a tab, keeping the query and hash the URL already carries. */
export function goToTab(router, t, { replace = false } = {}) {
  const here = router.currentRoute.value
  const to = { path: tabPath(t), query: here.query, hash: here.hash }
  return replace ? router.replace(to) : router.push(to)
}
