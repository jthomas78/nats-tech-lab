import { describe, expect, it } from 'vitest'

import {
  declareShellExtensionPoints,
  ExtensionPointRegistry,
  readonlyContext,
} from './extensionPoints.js'

describe('BR-AS07 — the owner of a region declares and versions it', () => {
  it('declares the three shell-owned points', () => {
    const registry = declareShellExtensionPoints()

    expect(registry.ids).toEqual([
      'shell/topbar-controls/v1',
      'shell/footer/v1',
      'shell/home-main/v1',
    ])
  })

  it('lets a built-in feature own a point the shell does not', () => {
    const registry = declareShellExtensionPoints()
    registry.declare({ id: 'demo-catalog/details-sidebar/v1', capacity: 2 })

    expect(registry.accepts('demo-catalog/details-sidebar/v1').ok).toBe(true)
    expect(registry.get('demo-catalog/details-sidebar/v1').owner).toBe('demo-catalog')
  })

  it('refuses two owners claiming one region', () => {
    const registry = declareShellExtensionPoints()

    expect(() => registry.declare({ id: 'shell/footer/v1' })).toThrow(/already declared/)
  })

  it('enforces the capacity the owner declared', () => {
    const registry = new ExtensionPointRegistry()
    registry.declare({ id: 'demo-catalog/details-sidebar/v1', capacity: 2 })

    expect(registry.accepts('demo-catalog/details-sidebar/v1', { placedCount: 1 }).ok).toBe(true)
    const full = registry.accepts('demo-catalog/details-sidebar/v1', { placedCount: 2 })
    expect(full.ok).toBe(false)
    expect(full.code).toBe('extension-point-full')
  })

  it('refuses over-capacity at index time, not at paint', () => {
    // The refusal has to be a property of the metadata, so it does not depend
    // on which contribution happened to render first.
    const registry = new ExtensionPointRegistry()
    registry.declare({ id: 'shell/home-main/v1', capacity: 1 })

    expect(registry.accepts('shell/home-main/v1', { placedCount: 1 }).code).toBe(
      'extension-point-full',
    )
  })
})

describe('BR-AS13 — extension point versions are part of the contract', () => {
  it('rejects a target at a major version the owner does not offer', () => {
    const registry = declareShellExtensionPoints()

    const result = registry.accepts('shell/footer/v2')

    expect(result.ok).toBe(false)
    expect(result.code).toBe('unsupported-extension-point-version')
  })

  it('distinguishes a wrong version from a region nobody owns', () => {
    const registry = declareShellExtensionPoints()

    // One is fixed by rebuilding the plugin; the other by a feature declaring
    // the point. Reporting both as "unknown" would send the reader after the
    // wrong fix.
    expect(registry.accepts('shell/footer/v9').code).toBe('unsupported-extension-point-version')
    expect(registry.accepts('shell/sidebar-bottom/v1').code).toBe('unknown-extension-point')
  })

  it('rejects a target that is not an extension-point id at all', () => {
    expect(declareShellExtensionPoints().accepts('footer').code).toBe('malformed')
  })

  it('refuses a declaration with a malformed id', () => {
    expect(() => new ExtensionPointRegistry().declare({ id: 'shell/footer' })).toThrow(/malformed/)
  })
})

describe('BR-AS02 — a contributor cannot reach back through its context', () => {
  it('freezes the context handed to contributors', () => {
    const context = readonlyContext({ demoId: '01-dictionary', running: false })

    expect(Object.isFrozen(context)).toBe(true)
  })

  it('does not hand out a live reference to the host object', () => {
    const hostState = { demoId: '01-dictionary' }
    const context = readonlyContext(hostState)

    hostState.demoId = '02-other'

    expect(context.demoId).toBe('01-dictionary')
  })
})
