import { describe, expect, it } from 'vitest'

import { validateManifest } from '../../shell/registry/manifestSchema.js'
import { DEMO_CATALOG_MODULE, demoCatalogManifest } from './manifest.js'

describe('BR-AS15 — the demo catalog is a plugin, not a special case', () => {
  it('passes the same manifest gate every other plugin passes', () => {
    const result = validateManifest(demoCatalogManifest)

    expect(result.ok).toBe(true)
  })

  it('contributes only through the public kinds', () => {
    const { plugin } = validateManifest(demoCatalogManifest)

    expect(new Set(plugin.contributions.map((c) => c.kind))).toEqual(new Set(['route', 'navigation']))
  })

  it('namespaces its routes under /demos', () => {
    const { plugin } = validateManifest(demoCatalogManifest)
    const routes = plugin.contributions.filter((c) => c.kind === 'route')

    expect(routes.map((r) => r.path)).toEqual(['/demos', '/demos/:id'])
  })

  it('owns the details-sidebar region other plugins target', () => {
    const { plugin } = validateManifest(demoCatalogManifest)

    expect(plugin.extensionPoints.map((p) => p.id)).toEqual(['demo-catalog/details-sidebar/v1'])
  })

  it('is bundled, so it needs no curated URL', () => {
    const { plugin } = validateManifest(demoCatalogManifest)

    expect(plugin.remote).toEqual({ kind: 'builtin', module: DEMO_CATALOG_MODULE })
  })

  it('exports a component for every component name its routes declare', async () => {
    const module = await import('./index.js')
    const { plugin } = validateManifest(demoCatalogManifest)

    for (const route of plugin.contributions.filter((c) => c.kind === 'route')) {
      expect(module.components[route.component]).toBeTruthy()
    }
  })
})
