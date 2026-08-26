// Covers the Admin UI's single PLATFORM connection.
import { beforeEach, describe, expect, it } from 'vitest'

import { usePlatformConnection } from './usePlatformConnection.js'

describe('usePlatformConnection (Phase 23)', () => {
  beforeEach(() => {
    usePlatformConnection().lastError.value = ''
  })

  it('subscribe() throws "not connected" when there is no live connection', () => {
    expect(() => usePlatformConnection().subscribe('notify._platform.refdata.>', () => {})).toThrow(
      'not connected',
    )
  })

  it('has no tenant field — this connection is not tenant-scoped', () => {
    expect(usePlatformConnection().tenant).toBeUndefined()
  })
})
