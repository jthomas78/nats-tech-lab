import { createMemoryHistory, createRouter } from 'vue-router'
import { defineComponent, h } from 'vue'
import { describe, expect, it } from 'vitest'

import { installWithdrawalGuard, isWithdrawnRoute } from './withdrawnRoutes.js'

/*
  What happens to the person standing on a route when its plugin is withdrawn
  (BR-AS57). Two separate answers, and keeping them separate is the rule:

  * The occupant is not moved. Their URL is theirs, a redirect would throw away
    where they were, and the shell has nothing better to put them.
  * Nobody NEW may enter. A nav click, a deep link or a back button aimed at a
    withdrawn route is refused, because there is no longer a plugin to render.
*/

const View = defineComponent({ render: () => h('main', 'plugin view') })

const build = (withdrawnIds = []) => {
  const withdrawn = new Set(withdrawnIds)
  const contributions = { isWithdrawn: (id) => withdrawn.has(id) }
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', name: 'home', component: View },
      {
        path: '/fleet-ops',
        name: 'fleet-ops/home',
        component: View,
        meta: { pluginId: 'fleet-ops' },
      },
      { path: '/billing', name: 'billing/home', component: View, meta: { pluginId: 'billing' } },
    ],
  })
  installWithdrawalGuard({ router, contributions })
  return { router, withdrawn, contributions }
}

describe('BR-AS57 — the occupant stays at the withdrawn route', () => {
  it('leaves the URL alone when the plugin under it is withdrawn', async () => {
    const { router, withdrawn } = build()
    await router.push('/fleet-ops')

    withdrawn.add('fleet-ops')

    // No redirect, no navigation of any kind: the shell replaces what is
    // rendered, not where the person is.
    expect(router.currentRoute.value.fullPath).toBe('/fleet-ops')
  })

  it('knows the occupant is standing on a withdrawn route, and only then', async () => {
    const { router, contributions, withdrawn } = build()
    await router.push('/fleet-ops')
    expect(isWithdrawnRoute(contributions, router.currentRoute.value)).toBe(false)

    withdrawn.add('fleet-ops')
    expect(isWithdrawnRoute(contributions, router.currentRoute.value)).toBe(true)

    await router.push('/')
    expect(isWithdrawnRoute(contributions, router.currentRoute.value)).toBe(false)
  })

  it('says nothing about a shell-owned route, which has no plugin to withdraw', () => {
    const { router, contributions } = build(['fleet-ops'])

    expect(isWithdrawnRoute(contributions, router.resolve('/'))).toBe(false)
  })
})

describe('BR-AS57 — nobody new may enter a withdrawn route', () => {
  it('refuses a deep link into it, leaving the person where they were', async () => {
    const { router } = build(['fleet-ops'])
    await router.push('/')

    await router.push('/fleet-ops').catch(() => {})

    expect(router.currentRoute.value.fullPath).toBe('/')
  })

  it('refuses a return to it after the occupant has navigated away', async () => {
    const { router, withdrawn } = build()
    await router.push('/fleet-ops')
    withdrawn.add('fleet-ops')

    await router.push('/')
    await router.push('/fleet-ops').catch(() => {})

    expect(router.currentRoute.value.fullPath).toBe('/')
  })

  it('lets every other route through, including a sibling plugin', async () => {
    const { router } = build(['fleet-ops'])

    await router.push('/billing')

    expect(router.currentRoute.value.fullPath).toBe('/billing')
  })

  it('lets the route back in when the plugin returns unchanged', async () => {
    const { router, withdrawn } = build(['fleet-ops'])
    await router.push('/')

    withdrawn.delete('fleet-ops')
    await router.push('/fleet-ops')

    expect(router.currentRoute.value.fullPath).toBe('/fleet-ops')
  })
})
