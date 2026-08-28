/*
  The demo catalog's manifest (BR-AS15).

  This is the shell's own first-party feature, and it is deliberately a plugin
  like any other: no privileged path, no direct import of its views from the
  shell frame, the same metadata gate as a remote. That is the point of it —
  if the catalog needed a shortcut to render, so would every plugin, and we
  would learn that only after building four of them.

  It is `builtin` rather than `federated` only because it ships in the shell's
  bundle; the loader still fetches it through an adapter and still calls
  `activate()` exactly once.
*/

import { REGISTRY_SCHEMA_VERSION, SHELL_API_VERSION } from '../../shell/versions.js'

export const DEMO_CATALOG_MODULE = 'demo-catalog'

export const demoCatalogManifest = Object.freeze({
  id: 'demo-catalog',
  name: 'Demo Catalog',
  description: 'The lab menu: every demo, its intro, and how to launch it.',
  schemaVersion: REGISTRY_SCHEMA_VERSION,
  shellApiVersion: SHELL_API_VERSION,
  /* `/demos`, not `/demo-catalog` — BR-AS12 wants the segment namespaced and
     unique, not identical to the plugin id. */
  routePrefix: 'demos',
  remote: { kind: 'builtin', module: DEMO_CATALOG_MODULE },
  /* A region the catalog owns and other plugins may fill: a demo's intro page
     can carry panels contributed by the demo's own plugin. Capacity is small
     on purpose — a details sidebar with eight panels is a different design,
     and the refusal should surface then rather than the layout quietly
     degrading. */
  extensionPoints: [
    {
      id: 'demo-catalog/details-sidebar/v1',
      capacity: 4,
      description: 'Sidebar panels beside a demo intro.',
    },
  ],
  contributions: [
    { kind: 'route', id: 'catalog', path: '/demos', title: 'Demos', component: 'catalog' },
    {
      kind: 'route',
      id: 'intro',
      path: '/demos/:id',
      title: 'Demo intro',
      component: 'intro',
    },
    {
      kind: 'navigation',
      id: 'demos',
      label: 'Demos',
      route: 'catalog',
      icon: 'pi pi-th-large',
      order: -100,
    },
  ],
})
