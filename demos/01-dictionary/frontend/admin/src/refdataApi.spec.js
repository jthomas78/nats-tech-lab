import { beforeEach, describe, expect, it, vi } from 'vitest'

const request = vi.fn()

vi.mock('./nats/usePlatformConnection.js', () => ({
  usePlatformConnection: () => ({ request }),
}))

const { getRefdataContexts } = await import('./api.js')

describe('Admin refdata reads over its PLATFORM connection', () => {
  beforeEach(() => {
    request.mockReset()
    request.mockResolvedValue({ contexts: [{ context: 'atlantic' }, { context: 'cape' }] })
  })

  it('uses the exact read-only subject and carries the REST-selected account as data', async () => {
    await expect(getRefdataContexts('acme')).resolves.toEqual(['atlantic', 'cape'])
    expect(request).toHaveBeenCalledWith('api._platform.refdata.context.list.v1', { tenant: 'acme' })
  })
})
