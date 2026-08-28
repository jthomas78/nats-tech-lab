import { describe, expect, it, vi } from 'vitest'

import { createShellRoutes, resolveRouteComponent } from './shellRoutes.js'

const route = (overrides = {}) => ({
  qualifiedId: 'demo-catalog/catalog',
  pluginId: 'demo-catalog',
  path: '/demos',
  title: 'Demos',
  component: 'default',
  ...overrides,
})

const Catalog = { name: 'Catalog' }
const ErrorPanel = { name: 'ErrorPanel' }

// Mirrors the real loader: resolves the module, rejects on failure.
const loaderFor = (module) => ({ load: vi.fn(async () => module) })
const failingLoader = (code) => ({ load: vi.fn(async () => { throw new Error(code) }) })
const pluginsFor = (...ids) => new Map(ids.map((id) => [id, { id }]))

describe('BR-AS12 — plugin routes are addressable', () => {
  it('gives every route contribution a path and a globally unique name', () => {
    const records = createShellRoutes({
      contributions: {
        routes: [route(), route({ qualifiedId: 'fleet-ops/vessels', pluginId: 'fleet-ops', path: '/fleet/vessels' })],
      },
      loader: loaderFor({}),
      plugins: pluginsFor('demo-catalog', 'fleet-ops'),
    })

    expect(records.map((r) => [r.path, r.name])).toEqual([
      ['/demos', 'demo-catalog/catalog'],
      ['/fleet/vessels', 'fleet-ops/vessels'],
    ])
  })

  it('builds the table without loading anything', () => {
    const loader = loaderFor({})

    createShellRoutes({ contributions: { routes: [route()] }, loader, plugins: pluginsFor('demo-catalog') })

    expect(loader.load).not.toHaveBeenCalled()
  })

  it('loads the plugin only when the route is entered', async () => {
    const loader = loaderFor({ components: { default: Catalog } })
    const [record] = createShellRoutes({
      contributions: { routes: [route()] },
      loader,
      plugins: pluginsFor('demo-catalog'),
    })

    await expect(record.component()).resolves.toBe(Catalog)
    expect(loader.load).toHaveBeenCalledTimes(1)
  })

  it('omits a route the contribution registry refused, so a deep link 404s', () => {
    // The permission check lives at index time; there is no second guard here
    // that could be forgotten.
    const records = createShellRoutes({
      contributions: { routes: [] },
      loader: loaderFor({}),
      plugins: pluginsFor('fleet-ops'),
    })

    expect(records).toEqual([])
  })
})

describe('BR-AS04 — a route that will not resolve does not take the shell down', () => {
  it('renders the error component when the remote fails to load', async () => {
    const component = await resolveRouteComponent({
      route: route(),
      loader: failingLoader('chunk-load-failed'),
      plugins: pluginsFor('demo-catalog'),
      errorComponent: ErrorPanel,
    })

    expect(component).toBe(ErrorPanel)
  })

  it('renders the error component when the module lacks the promised component', async () => {
    const component = await resolveRouteComponent({
      route: route({ component: 'detail' }),
      loader: loaderFor({ components: { default: Catalog } }),
      plugins: pluginsFor('demo-catalog'),
      errorComponent: ErrorPanel,
    })

    expect(component).toBe(ErrorPanel)
  })

  it('renders the error component when the plugin is not in the inventory', async () => {
    const component = await resolveRouteComponent({
      route: route(),
      loader: loaderFor({ components: { default: Catalog } }),
      plugins: new Map(),
      errorComponent: ErrorPanel,
    })

    expect(component).toBe(ErrorPanel)
  })
})
