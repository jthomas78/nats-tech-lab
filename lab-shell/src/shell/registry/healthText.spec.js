/*
  BR-AS60 — what the frame says about health, and what it refuses to say.
*/

import { describe, expect, it } from 'vitest'

import { HEALTH_STATE } from './healthPlane.js'
import { healthAttention, healthCheckedAt, healthLabel, healthTone } from './healthText.js'

const signal = (state, cause = '', lastCheckAt = '') => ({ state, cause, lastCheckAt })

describe('BR-AS60 — a health signal is drawn without overstating it', () => {
  it('draws an unavailable dependency as a warning, not a failure', () => {
    // The plugin has not failed. Something it depends on is not answering,
    // and it may be answering again in five seconds.
    expect(healthTone(HEALTH_STATE.UNAVAILABLE)).toBe('warn')
    expect(healthTone(HEALTH_STATE.UNAVAILABLE)).not.toBe('bad')
  })

  it('draws healthy as healthy and everything uncertain as quiet', () => {
    expect(healthTone(HEALTH_STATE.HEALTHY)).toBe('ok')
    expect(healthTone(HEALTH_STATE.STALE)).toBe('off')
    expect(healthTone(HEALTH_STATE.UNKNOWN)).toBe('off')
    expect(healthTone(HEALTH_STATE.NOT_CONFIGURED)).toBe('off')
    expect(healthTone(HEALTH_STATE.NOT_APPLICABLE)).toBe('off')
  })

  it('says the state, and the cause only when there is one', () => {
    expect(healthLabel(signal(HEALTH_STATE.UNAVAILABLE, 'not-ready'))).toBe('unavailable (not-ready)')
    expect(healthLabel(signal(HEALTH_STATE.HEALTHY))).toBe('healthy')
  })

  it('says unknown for a signal it was never given', () => {
    expect(healthLabel(undefined)).toBe('unknown')
    expect(healthLabel({})).toBe('unknown')
  })

  it('shows when the reading was taken, and nothing when there was none', () => {
    expect(healthCheckedAt(signal(HEALTH_STATE.HEALTHY, '', '2026-09-01T12:00:00Z'))).not.toBe('')
    expect(healthCheckedAt(signal(HEALTH_STATE.UNKNOWN))).toBe('')
    expect(healthCheckedAt(signal(HEALTH_STATE.HEALTHY, '', 'not-a-date'))).toBe('')
  })
})

describe('BR-AS60 — the nav takes one mark, and only when it means something', () => {
  it('marks a plugin whose backend is down', () => {
    expect(healthAttention({ frontend: signal(HEALTH_STATE.HEALTHY), backend: signal(HEALTH_STATE.UNAVAILABLE) })).toBe('warn')
  })

  it('marks a plugin whose frontend origin is down, independently', () => {
    // The two signals are separate facts. Either one alone earns the mark.
    expect(healthAttention({ frontend: signal(HEALTH_STATE.UNAVAILABLE), backend: signal(HEALTH_STATE.HEALTHY) })).toBe('warn')
  })

  it('leaves a healthy plugin clean', () => {
    expect(healthAttention({ frontend: signal(HEALTH_STATE.HEALTHY), backend: signal(HEALTH_STATE.HEALTHY) })).toBeNull()
  })

  it('leaves an unwatched or unread plugin clean', () => {
    // A dot that is always there stops being a signal. "Nothing is
    // configured to watch" and "nobody has looked yet" are not problems to
    // act on, and neither is a reading that has merely gone stale.
    expect(healthAttention({ frontend: signal(HEALTH_STATE.NOT_CONFIGURED), backend: signal(HEALTH_STATE.NOT_APPLICABLE) })).toBeNull()
    expect(healthAttention({ frontend: signal(HEALTH_STATE.UNKNOWN), backend: signal(HEALTH_STATE.UNKNOWN) })).toBeNull()
    expect(healthAttention({ frontend: signal(HEALTH_STATE.STALE), backend: signal(HEALTH_STATE.STALE) })).toBeNull()
    expect(healthAttention(undefined)).toBeNull()
  })
})
