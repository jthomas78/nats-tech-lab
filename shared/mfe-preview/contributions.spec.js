import { describe, expect, it } from 'vitest'

import { buildItems } from './contributions.js'

const Component = { name: 'Stub' }

describe('mfe-preview contributions', () => {
  it('supplies a route contribution with a prop per path param', () => {
    const [item] = buildItems({
      manifest: { contributions: [{ kind: 'route', id: 'intro', path: '/demos/:id', component: 'intro' }] },
      components: { intro: Component },
    })
    expect(item.props).toEqual({ id: 'sample-id' })
    expect(item.stage).toBe('page')
  })

  it('prefers a fixture over the placeholder it would have invented', () => {
    const [item] = buildItems({
      manifest: { contributions: [{ kind: 'route', id: 'intro', path: '/demos/:id', component: 'intro' }] },
      components: { intro: Component },
      fixtures: { intro: { id: '01-dictionary' } },
    })
    expect(item.props.id).toBe('01-dictionary')
  })

  it('freezes the context an extension is handed, as its host would', () => {
    const [item] = buildItems({
      manifest: { contributions: [{ kind: 'extension', id: 'panel', target: 'shell/home-main/v1', component: 'panel' }] },
      components: { panel: Component },
    })
    expect(item.props.context.point).toBe('shell/home-main/v1')
    expect(Object.isFrozen(item.props.context)).toBe(true)
  })

  it('marks a contribution whose component the bundle does not export', () => {
    const [item] = buildItems({
      manifest: { contributions: [{ kind: 'route', id: 'gone', path: '/gone', component: 'missing' }] },
      components: {},
    })
    expect(item.missing).toBe(true)
    expect(item.component).toBeNull()
  })

  it('renders nothing for a contribution the shell draws from metadata alone', () => {
    const [item] = buildItems({
      manifest: { contributions: [{ kind: 'navigation', id: 'nav', label: 'Demos', route: 'catalog' }] },
      components: {},
    })
    expect(item.stage).toBeNull()
    expect(item.component).toBeNull()
  })

  it('lists an exported component that no contribution claims', () => {
    const items = buildItems({
      manifest: { contributions: [] },
      components: { orphan: Component },
    })
    expect(items).toHaveLength(1)
    expect(items[0].kind).toBe('unclaimed')
    expect(items[0].component).toBe(Component)
  })

  it('falls back to the exported components when there is no manifest', () => {
    const items = buildItems({ manifest: null, components: { only: Component } })
    expect(items.map((item) => item.id)).toEqual(['only'])
  })
})
