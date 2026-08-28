import { describe, expect, it } from 'vitest'

import { failureStage } from './failureStage.js'

describe('the failure stage shown beside a cause', () => {
  it('separates a fetch failure from an activation failure', () => {
    expect(failureStage('chunk-load-failed')).toBe('remote entry fetch')
    expect(failureStage('activate-threw')).toBe('plugin activation')
  })

  it('puts every metadata rejection at the same stage', () => {
    expect(failureStage('unsupported-shell-api-version')).toBe('manifest validation')
    expect(failureStage('malformed')).toBe('manifest validation')
  })

  it('names an unmapped code rather than printing a blank stage', () => {
    expect(failureStage('something-new')).toBe('unknown')
    expect(failureStage(null)).toBe('unknown')
  })
})
