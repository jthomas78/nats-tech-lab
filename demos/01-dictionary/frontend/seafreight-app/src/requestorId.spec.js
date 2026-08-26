import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

function memoryStorage() {
  const values = new Map()
  return {
    clear: () => values.clear(),
    getItem: (key) => values.get(key) ?? null,
    setItem: (key, value) => values.set(key, String(value)),
  }
}

describe('requestorId browser identity', () => {
  beforeEach(() => {
    vi.stubGlobal('localStorage', memoryStorage())
    vi.resetModules()
  })

  afterEach(() => vi.unstubAllGlobals())

  it('persists the same instance ID across page-module reloads', async () => {
    const firstLoad = await import('./requestorId.js')
    const firstID = firstLoad.BROWSER_ID

    vi.resetModules()
    const refreshedLoad = await import('./requestorId.js')

    expect(firstID).toMatch(/^[0-9a-f]{16}$/)
    expect(refreshedLoad.BROWSER_ID).toBe(firstID)
    expect(refreshedLoad.REST_REQUESTOR_ID).toBe(`seafreight-app/${firstID}`)
    expect(localStorage.getItem(firstLoad.BROWSER_ID_STORAGE_KEY)).toBe(firstID)
  })

  it('replaces an invalid stored identity', async () => {
    localStorage.setItem('seafreight-app.browserInstanceId', 'not-a-valid-id')

    const loaded = await import('./requestorId.js')

    expect(loaded.BROWSER_ID).toMatch(/^[0-9a-f]{16}$/)
    expect(loaded.BROWSER_ID).not.toBe('not-a-valid-id')
    expect(localStorage.getItem(loaded.BROWSER_ID_STORAGE_KEY)).toBe(loaded.BROWSER_ID)
  })
})
