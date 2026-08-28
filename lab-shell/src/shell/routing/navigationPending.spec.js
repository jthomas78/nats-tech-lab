import { describe, expect, it, vi } from 'vitest'

import { createNavigationPending } from './navigationPending.js'

const fakeRouter = () => {
  const hooks = { before: [], after: [], error: [] }
  return {
    hooks,
    beforeEach: (fn) => hooks.before.push(fn),
    afterEach: (fn) => hooks.after.push(fn),
    onError: (fn) => hooks.error.push(fn),
    go: (to) => hooks.before.forEach((fn) => fn(to)),
    settle: () => hooks.after.forEach((fn) => fn()),
    fail: () => hooks.error.forEach((fn) => fn(new Error('nope'))),
  }
}

describe('BR-AS08 — a deep link into an unloaded remote shows it is loading (task 1b-6)', () => {
  it('is pending while navigating into a plugin route', () => {
    const router = fakeRouter()
    const { pending } = createNavigationPending(router)

    expect(pending.value).toBe(false)
    router.go({ path: '/example', meta: { pluginId: 'example-plugin' } })
    expect(pending.value).toBe(true)
  })

  it('clears once the navigation settles', () => {
    const router = fakeRouter()
    const { pending } = createNavigationPending(router)

    router.go({ path: '/example', meta: { pluginId: 'example-plugin' } })
    router.settle()
    expect(pending.value).toBe(false)
  })

  it('clears when the navigation errors, rather than leaving a stuck skeleton', () => {
    const router = fakeRouter()
    const { pending } = createNavigationPending(router)

    router.go({ path: '/example', meta: { pluginId: 'example-plugin' } })
    router.fail()
    expect(pending.value).toBe(false)
  })

  it('names the contribution being waited for, so the placeholder can label itself', () => {
    const router = fakeRouter()
    const { target } = createNavigationPending(router)

    router.go({
      path: '/example',
      meta: { pluginId: 'example-plugin', title: 'Example', contributionId: 'example-plugin/main/v1' },
    })

    expect(target.value).toEqual({
      pluginId: 'example-plugin',
      title: 'Example',
      contributionId: 'example-plugin/main/v1',
    })
    router.settle()
    expect(target.value).toBeNull()
  })

  it('stays quiet for the shell\'s own routes, which are already in the bundle', () => {
    const router = fakeRouter()
    const { pending } = createNavigationPending(router)

    router.go({ path: '/plugins', meta: { title: 'Plugins' } })
    expect(pending.value).toBe(false)
  })
})
