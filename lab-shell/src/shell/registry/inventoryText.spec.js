import { describe, expect, it } from 'vitest'

import { describeContributions, statusDetail } from './inventoryText.js'
import { PLUGIN_STATUS } from './pluginStatus.js'

describe('the contributions column', () => {
  it('counts by kind and pluralizes', () => {
    expect(describeContributions(['route', 'route', 'navigation', 'shell-footer'])).toBe(
      '2 routes · 1 nav · 1 footer item',
    )
  })

  it('is empty for a plugin that placed nothing', () => {
    expect(describeContributions([])).toBe('')
  })
})

describe('the detail column', () => {
  it('distinguishes indexed-but-not-loaded from loaded', () => {
    expect(statusDetail({ status: PLUGIN_STATUS.AVAILABLE })).toContain('no code loaded yet')
    expect(statusDetail({ status: PLUGIN_STATUS.ACTIVE })).toContain('activate()')
  })

  it('says that an incompatible plugin never executed', () => {
    const detail = statusDetail({
      status: PLUGIN_STATUS.INCOMPATIBLE,
      reasonCode: 'unsupported-shell-api-version',
    })

    expect(detail).toContain('unsupported-shell-api-version')
    expect(detail).toContain('no code executed')
  })

  /* BR-AS04: the cause code and a shell-authored stage, never the caught
     error's message — which for a federation failure is the remote's URL. */
  it('reports a failure as stage and cause code only', () => {
    const detail = statusDetail({
      status: PLUGIN_STATUS.FAILED,
      reasonCode: 'chunk-load-failed',
      reason: 'Failed to load http://localhost:7110/remoteEntry.js',
    })

    expect(detail).toBe('remote entry fetch — chunk-load-failed')
    expect(detail).not.toContain('http')
  })
})
