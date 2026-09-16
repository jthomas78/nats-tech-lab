import { describe, expect, it } from 'vitest'

import {
  POOL_KV,
  POOL_STREAM,
  POOL_TRUTH_KV,
  POOL_WORKERS_KV,
  READ_KV,
  STREAM,
  WRITE_KV,
} from '../config.js'
import { streamSize } from './model.js'
import { useOdometer } from './useOdometer.js'

// 04.8.7 — the frontend learns lesson 02's own names.
//
// Lesson 02 used to fold ODOMETER, the log every other screen is drawn from.
// It now has its own, ODOMETER_POOL, with its own correct fold beside it. The
// browser has to be told, and the names are the part that is easy to get
// wrong: ODOMETER is a PREFIX of ODOMETER_POOL, so a name half-copied still
// looks plausible on screen.

describe('the names lesson 02 reads', () => {
  it('names the pool log and the pool truth bucket', () => {
    expect(POOL_STREAM).toBe('ODOMETER_POOL')
    expect(POOL_TRUTH_KV).toBe('odometer-pool-truth')
  })

  // The whole point of 04.8: lesson 02 touches nothing lesson 01 owns.
  it('shares nothing with the demo own log', () => {
    expect(POOL_STREAM).not.toBe(STREAM)
    expect(POOL_TRUTH_KV).not.toBe(READ_KV)
    expect(POOL_TRUTH_KV).not.toBe(WRITE_KV)
  })

  // The truth fold is CORRECT; the pool fold is deliberately wrong. Lesson 02
  // compares them, so one bucket holding both would compare a number with
  // itself.
  it('keeps the correct fold apart from the damaged one', () => {
    expect(POOL_TRUTH_KV).not.toBe(POOL_KV)
    expect(POOL_TRUTH_KV).not.toBe(POOL_WORKERS_KV)
  })

  // Follows the Go names: streams SCREAMING_SNAKE, buckets lowercase-kebab.
  it('spells a stream like a stream and a bucket like a bucket', () => {
    expect(POOL_STREAM).toBe(POOL_STREAM.toUpperCase())
    expect(POOL_TRUTH_KV).toBe(POOL_TRUTH_KV.toLowerCase())
    expect(POOL_TRUTH_KV).not.toContain('_')
  })
})

// The standing rule, set by the user 2026-09-16: a count is never shown
// without its bytes. It is enforced HERE, at the one place a stream's size is
// read, so no screen can pick up a count on its own.
describe('a log size is a count AND its bytes', () => {
  it('reads both out of one stream info', () => {
    const size = streamSize({ state: { last_seq: 9, messages: 8, bytes: 2048 } })
    expect(size).toEqual({ head: 9, messages: 8, bytes: 2048 })
  })

  it('answers zero for a stream nobody has seeded, never undefined', () => {
    expect(streamSize(null)).toEqual({ head: 0, messages: 0, bytes: 0 })
    expect(streamSize({})).toEqual({ head: 0, messages: 0, bytes: 0 })
  })

  // A count that arrived without bytes would let a screen print a length
  // nobody can price. Both keys are always there.
  it('never returns one of the pair without the other', () => {
    for (const info of [null, {}, { state: { messages: 8 } }, { state: { bytes: 2048 } }]) {
      const size = streamSize(info)
      expect(Object.keys(size).sort()).toEqual(['bytes', 'head', 'messages'])
      expect(typeof size.messages).toBe('number')
      expect(typeof size.bytes).toBe('number')
    }
  })
})

describe('what the composable hands the screens', () => {
  it('carries the pool count and bytes together', () => {
    const o = useOdometer()
    expect(o.poolMessages.value).toBe(0)
    expect(o.poolBytes.value).toBe(0)
    expect(o.poolTruth).toBeInstanceOf(Map)
  })

  // Written as a pair check rather than two assertions: a later edit that
  // adds a third log has to add its bytes as well, or this fails.
  it('exposes a bytes for every count it exposes', () => {
    const o = useOdometer()
    for (const [count, size] of [['messages', 'bytes'], ['poolMessages', 'poolBytes']]) {
      expect(o[count], `${count} is missing`).toBeDefined()
      expect(o[size], `${size} must travel with ${count}`).toBeDefined()
    }
  })
})
