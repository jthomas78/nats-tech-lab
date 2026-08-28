import { describe, expect, it } from 'vitest'

import { breadcrumbTrail, SHELL_OWNER } from './breadcrumb.js'

describe('the breadcrumb trail', () => {
  it('attributes a plugin screen to the plugin by display name', () => {
    const trail = breadcrumbTrail({ pluginId: 'example-plugin', title: 'Example' }, () => 'Example plugin')

    expect(trail).toEqual({ owner: 'Example plugin', leaf: 'Example' })
  })

  it('owns the shell screens itself', () => {
    expect(breadcrumbTrail({ title: 'Plugins' })).toEqual({ owner: SHELL_OWNER, leaf: 'Plugins' })
  })

  it('falls back to the curated id when the plugin has no record', () => {
    expect(breadcrumbTrail({ pluginId: 'ghost', title: 'Gone' }, () => null).owner).toBe('ghost')
  })

  it('survives a route with no title', () => {
    expect(breadcrumbTrail(undefined).leaf).toBe('')
  })
})
