import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import { createMemoryHistory, createRouter, RouterView } from 'vue-router'

import { createShellRoutes, resolveRouteComponent } from './shellRoutes.js'

const route = (overrides = {}) => ({
  id: 'catalog',
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
const manifestForOnly = (...ids) => (id) => (ids.includes(id) ? { id } : null)

describe('BR-AS12 — plugin routes are addressable', () => {
  it('gives every route contribution a path and a globally unique name', () => {
    const records = createShellRoutes({
      contributions: {
        routes: [route(), route({ qualifiedId: 'fleet-ops/vessels', pluginId: 'fleet-ops', path: '/fleet/vessels' })],
      },
      loader: loaderFor({}),
      manifestFor: manifestForOnly('demo-catalog', 'fleet-ops'),
    })

    expect(records.map((r) => [r.path, r.name])).toEqual([
      ['/demos', 'demo-catalog/catalog'],
      ['/fleet/vessels', 'fleet-ops/vessels'],
    ])
  })

  it('builds the table without loading anything', () => {
    const loader = loaderFor({})

    createShellRoutes({ contributions: { routes: [route()] }, loader, manifestFor: manifestForOnly('demo-catalog') })

    expect(loader.load).not.toHaveBeenCalled()
  })

  it('loads the plugin only when the route is entered', async () => {
    const loader = loaderFor({ components: { default: Catalog } })
    const [record] = createShellRoutes({
      contributions: { routes: [route()] },
      loader,
      manifestFor: manifestForOnly('demo-catalog'),
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
      manifestFor: manifestForOnly('fleet-ops'),
    })

    expect(records).toEqual([])
  })
})

describe('BR-AS04 — a route that will not resolve does not take the shell down', () => {
  it('renders the error component when the remote fails to load', async () => {
    const component = await resolveRouteComponent({
      route: route(),
      loader: failingLoader('chunk-load-failed'),
      manifestFor: manifestForOnly('demo-catalog'),
      errorComponent: ErrorPanel,
    })

    /* Not the bare panel: vue-router would cache that for the life of the
       page and Retry could never replace it. A wrapper that draws the panel
       (routeRetry.spec.js covers the retry itself). */
    const wrapper = mount(component)
    expect(wrapper.findComponent(ErrorPanel).exists()).toBe(true)
  })

  it('renders the error component when the module lacks the promised component', async () => {
    const component = await resolveRouteComponent({
      route: route({ component: 'detail' }),
      loader: loaderFor({ components: { default: Catalog } }),
      manifestFor: manifestForOnly('demo-catalog'),
      errorComponent: ErrorPanel,
    })

    expect(component).toBe(ErrorPanel)
  })

  it('renders the error component when the plugin is not in the inventory', async () => {
    const component = await resolveRouteComponent({
      route: route(),
      loader: loaderFor({ components: { default: Catalog } }),
      manifestFor: () => null,
      errorComponent: ErrorPanel,
    })

    expect(component).toBe(ErrorPanel)
  })
})

describe('D17-7 — the bare prefix redirects, and the PLUGIN declares it', () => {
  const prefixed = (id) => (pluginId) => (pluginId === id ? { id, routePrefix: id } : null)

  const lessons = (overrides = {}) => [
    route({ qualifiedId: 'demo-04/lesson-01', pluginId: 'demo-04', path: '/demo-04/lesson-01', default: true, ...overrides }),
    route({ qualifiedId: 'demo-04/lesson-02', pluginId: 'demo-04', path: '/demo-04/lesson-02' }),
  ]

  const build = (routes) => createShellRoutes({
    contributions: { routes },
    loader: loaderFor({}),
    manifestFor: prefixed('demo-04'),
  })

  it('registers /<prefix> as a redirect to the declared default', () => {
    const records = build(lessons())
    const redirect = records.find((r) => r.path === '/demo-04')

    expect(redirect.redirect).toEqual({ name: 'demo-04/lesson-01' })
    expect(redirect.name).toBe('default-route:demo-04')
  })

  it('names no demo: the prefix comes from the manifest, not from the shell', () => {
    const records = createShellRoutes({
      contributions: {
        routes: [route({ qualifiedId: 'fleet-ops/vessels', pluginId: 'fleet-ops', path: '/fleet/vessels', default: true })],
      },
      loader: loaderFor({}),
      manifestFor: (id) => (id === 'fleet-ops' ? { id, routePrefix: 'fleet' } : null),
    })

    expect(records.find((r) => r.redirect)).toMatchObject({
      path: '/fleet',
      redirect: { name: 'fleet-ops/vessels' },
    })
  })

  it('leaves the bare prefix alone when no route declared a default', () => {
    expect(build(lessons({ default: false })).some((r) => r.redirect)).toBe(false)
  })

  it('adds no redirect when the default route IS the bare prefix', () => {
    const records = build([route({ qualifiedId: 'demo-04/main', pluginId: 'demo-04', path: '/demo-04', default: true })])

    expect(records.some((r) => r.redirect)).toBe(false)
    expect(records).toHaveLength(1)
  })

  it('leaves no dead prefix when the default route was refused or not permitted', () => {
    // A refused route never reaches this funnel, so the redirect cannot be
    // built and /demo-04 falls through to not-found (amendment A4).
    const records = build(lessons().slice(1))

    expect(records.some((r) => r.redirect)).toBe(false)
  })

  it('carries the owning plugin, so the withdrawal guard already covers it', () => {
    // A withdrawal is not a refusal (A6): the record stays and the existing
    // guard refuses entry by meta.pluginId.
    const redirect = build(lessons()).find((r) => r.redirect)

    expect(redirect.meta).toEqual({ pluginId: 'demo-04', defaultFor: 'demo-04/lesson-01' })
  })

  it('is skipped for a plugin whose manifest went away', () => {
    const records = createShellRoutes({
      contributions: { routes: lessons() },
      loader: loaderFor({}),
      manifestFor: () => null,
    })

    expect(records.some((r) => r.redirect)).toBe(false)
  })

  it('does not bypass the readiness gate, because it lands on the real record', async () => {
    const demoStore = { isReady: () => false }
    const records = createShellRoutes({
      contributions: { routes: lessons() },
      loader: loaderFor({ components: { default: Catalog } }),
      manifestFor: prefixed('demo-04'),
      demoStore,
    })
    const target = records.find((r) => r.name === 'demo-04/lesson-01')

    expect(records.find((r) => r.redirect).redirect).toEqual({ name: target.name })
    await expect(target.component()).resolves.not.toBe(Catalog)
  })
})

describe('D17-7 — the redirect against a real router', () => {
  // The record shape above is only half the claim. This is the other half:
  // a reader who trims the URL to the bare prefix arrives at the default.
  const routerWith = async (records) => {
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [{ path: '/', component: { template: '<div />' } }, ...records],
    })
    /* Memory history has no location of its own, so `isReady` waits for a
       first navigation that nobody else is going to make. */
    router.push('/')
    await router.isReady()
    return router
  }

  const records = () => createShellRoutes({
    contributions: {
      routes: [
        route({ qualifiedId: 'demo-04/lesson-01', pluginId: 'demo-04', path: '/demo-04/lesson-01', default: true }),
        route({ qualifiedId: 'demo-04/lesson-02', pluginId: 'demo-04', path: '/demo-04/lesson-02' }),
      ],
    },
    loader: loaderFor({ components: { default: Catalog } }),
    manifestFor: (id) => (id === 'demo-04' ? { id, routePrefix: 'demo-04' } : null),
  })

  it('lands a bare /demo-04 on lesson 01', async () => {
    const router = await routerWith(records())
    /* Pushed, not resolved: `resolve` reports the record a path matches and
       the router follows the redirect when it navigates. */
    await router.push('/demo-04')

    expect(router.currentRoute.value.name).toBe('demo-04/lesson-01')
    expect(router.currentRoute.value.redirectedFrom?.path).toBe('/demo-04')
  })

  it('leaves a full lesson link exactly where it points', async () => {
    const router = await routerWith(records())

    expect(router.resolve('/demo-04/lesson-02').name).toBe('demo-04/lesson-02')
  })

  it('rewrites the address bar, so a refresh does not bounce again', async () => {
    const router = await routerWith(records())
    await router.push('/demo-04')

    expect(router.currentRoute.value.path).toBe('/demo-04/lesson-01')
  })
})


describe('BR-AS91 — a route record tells the component which route it is', () => {
  /* A plugin may point several of its own routes at ONE component — demo 04's
     two lessons are the first — and that component cannot ask a router which
     one it is showing, because `vue-router` is the shell's own dependency and
     is not shared across the federation boundary. The record knows, so the
     record says so. */
  const built = (overrides) => createShellRoutes({
    contributions: { routes: [route(overrides)] },
    loader: loaderFor({}),
    manifestFor: manifestForOnly('demo-catalog', 'demo-04'),
  })[0]

  const propsOf = (record, to = { params: {} }) => record.props(to)

  it('hands over the plugin\'s own LOCAL id, not the qualified one', () => {
    const record = built({ id: 'lesson-02', qualifiedId: 'demo-04/lesson-02', pluginId: 'demo-04', path: '/demo-04/lesson-02' })

    expect(propsOf(record).routeId).toBe('lesson-02')
  })

  it('gives every plugin route the prop, whether it needs it or not', () => {
    expect(propsOf(built()).routeId).toBe('catalog')
  })

  it('still passes the route params through', () => {
    // `props: true` used to do this alone. The function must not lose it.
    expect(propsOf(built(), { params: { vesselId: '7' } })).toEqual({ vesselId: '7', routeId: 'catalog' })
  })

  it('lets a param of the same name win, so a path is never shadowed', () => {
    // Spread order is the rule, written down: params first, routeId last.
    expect(propsOf(built(), { params: { routeId: 'from-the-path' } }).routeId).toBe('catalog')
  })
})

describe('BR-AS91 — the prop reaches the component, gate and all', () => {
  /* The record shape above is only half of it. The other half is that the
     value survives `withDemoGate`, which renders the plugin component with
     `inheritAttrs: false` and passes its attrs on by hand. */
  const Lesson = {
    name: 'Lesson',
    props: { routeId: { type: String, default: 'nothing' } },
    template: '<p>{{ routeId }}</p>',
  }

  const Host = { template: '<router-view />' }

  const mountAt = async (path, demoStore = null) => {
    const records = createShellRoutes({
      contributions: {
        routes: [
          route({ id: 'lesson-01', qualifiedId: 'demo-04/lesson-01', pluginId: 'demo-04', path: '/demo-04/lesson-01' }),
          route({ id: 'lesson-02', qualifiedId: 'demo-04/lesson-02', pluginId: 'demo-04', path: '/demo-04/lesson-02' }),
        ],
      },
      loader: loaderFor({ components: { default: Lesson } }),
      manifestFor: manifestForOnly('demo-04'),
      demoStore,
    })
    const router = createRouter({ history: createMemoryHistory(), routes: records })
    router.push(path)
    await router.isReady()

    const wrapper = mount(Host, { global: { plugins: [router] } })
    await flushAsync()
    return { wrapper, router }
  }

  /* Two ticks: one for the lazy component, one for the gate's own await. */
  const flushAsync = async () => {
    for (let i = 0; i < 4; i += 1) await Promise.resolve()
    await new Promise((resolve) => setTimeout(resolve, 0))
  }

  const ready = () => ({
    checkBeforeMount: vi.fn(async () => ({ state: 'available' })),
    entryFor: () => null,
    isReady: () => true,
  })

  it('opens a COLD link to lesson 02 on lesson 02', async () => {
    const { wrapper } = await mountAt('/demo-04/lesson-02')

    expect(wrapper.text()).toContain('lesson-02')
  })

  it('opens a cold link to lesson 01 on lesson 01', async () => {
    const { wrapper } = await mountAt('/demo-04/lesson-01')

    expect(wrapper.text()).toContain('lesson-01')
  })

  it('follows the reader back and forward between the two', async () => {
    const { wrapper, router } = await mountAt('/demo-04/lesson-01')

    await router.push('/demo-04/lesson-02')
    await flushAsync()
    expect(wrapper.text()).toContain('lesson-02')

    await router.back()
    await flushAsync()
    expect(wrapper.text()).toContain('lesson-01')

    await router.forward()
    await flushAsync()
    expect(wrapper.text()).toContain('lesson-02')
  })

  it('survives the readiness gate, which renders the component by hand', async () => {
    const { wrapper } = await mountAt('/demo-04/lesson-02', ready())

    expect(wrapper.text()).toContain('lesson-02')
  })

  it('probes readiness once per PLUGIN, not once per lesson (A4, BR-AS79)', async () => {
    const demoStore = ready()
    const { router } = await mountAt('/demo-04/lesson-01', demoStore)

    await router.push('/demo-04/lesson-02')
    await flushAsync()

    /* The gate re-checks on every mount, which is pre-existing behaviour and
       the same as navigating away and back. What matters here is that it asks
       about the PLUGIN — one id, never one per lesson. */
    expect(new Set(demoStore.checkBeforeMount.mock.calls.flat())).toEqual(new Set(['demo-04']))
  })
})
