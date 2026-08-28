import { describe, expect, it } from 'vitest'

import { PLUGIN_STATUS } from './pluginStatus.js'
import { attentionTone, needsAttention, summarizeAttention } from './statusRollup.js'

const records = (...statuses) => statuses.map((status, i) => ({ id: `p${i}`, status }))

describe('which statuses the chrome marks', () => {
  it('marks failed in the error tone and incompatible in the warning tone', () => {
    expect(attentionTone(PLUGIN_STATUS.FAILED)).toBe('err')
    expect(attentionTone(PLUGIN_STATUS.INCOMPATIBLE)).toBe('warn')
  })

  it('leaves the healthy and transient statuses unmarked', () => {
    for (const status of [
      PLUGIN_STATUS.DISCOVERED,
      PLUGIN_STATUS.AVAILABLE,
      PLUGIN_STATUS.LOADING,
      PLUGIN_STATUS.ACTIVE,
    ]) {
      expect(needsAttention(status)).toBe(false)
    }
  })

  /* An operator switching a plugin off is not a fault. Dotting it would make
     the dot mean "something is different" rather than "something is wrong",
     and a signal that fires on intended states gets ignored. */
  it('does not mark a plugin the operator disabled', () => {
    expect(needsAttention(PLUGIN_STATUS.DISABLED)).toBe(false)
  })
})

describe('the topbar attention summary', () => {
  it('is silent when nothing needs attention', () => {
    const summary = summarizeAttention(records(PLUGIN_STATUS.ACTIVE, PLUGIN_STATUS.AVAILABLE))

    expect(summary.count).toBe(0)
    expect(summary.tone).toBeNull()
    expect(summary.label).toBe('')
  })

  it('names the failure when failures are the whole story', () => {
    expect(summarizeAttention(records(PLUGIN_STATUS.FAILED)).label).toBe('1 plugin failed')
    expect(summarizeAttention(records(PLUGIN_STATUS.FAILED, PLUGIN_STATUS.FAILED)).label).toBe(
      '2 plugins failed',
    )
  })

  it('falls back to a neutral phrase for a mixed set', () => {
    const summary = summarizeAttention(
      records(PLUGIN_STATUS.FAILED, PLUGIN_STATUS.INCOMPATIBLE, PLUGIN_STATUS.ACTIVE),
    )

    expect(summary.label).toBe('2 need attention')
    expect(summary.count).toBe(2)
  })

  it('keeps the error tone whenever anything failed', () => {
    expect(summarizeAttention(records(PLUGIN_STATUS.INCOMPATIBLE)).tone).toBe('warn')
    expect(summarizeAttention(records(PLUGIN_STATUS.INCOMPATIBLE, PLUGIN_STATUS.FAILED)).tone).toBe(
      'err',
    )
  })
})
