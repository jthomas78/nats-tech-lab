/*
  A PrimeIcon class string, as the component `NavList` expects.

  `NavList` renders an item's `icon` with `<component :is>`, because demo 01's
  two frontends pass imported SVG components. The shell and every plugin
  manifest name an icon by CLASS instead (`pi pi-home`, and BR-AS12's schema
  accepts nothing else), so something has to bridge the two. It is here, and
  not in `shared/ui-shell/`, because the class convention is the shell's —
  BR-AS88 keeps `NavList` a renderer that learns nothing about plugins.

  Cached on the class string, so the same icon is the same component object on
  every render. Without that, each render makes a new component definition and
  Vue tears the old icon down and mounts a new one, on every keystroke that
  touches the rail.
*/

import { h } from 'vue'

const cache = new Map()

/**
 * @param {string|null|undefined} className
 * @returns {object|null} a component, or null when there is no icon to draw
 */
export function iconClass(className) {
  if (!className) return null
  let component = cache.get(className)
  if (!component) {
    component = {
      name: 'IconClass',
      render: () => h('i', { class: className, 'aria-hidden': 'true' }),
    }
    cache.set(className, component)
  }
  return component
}
