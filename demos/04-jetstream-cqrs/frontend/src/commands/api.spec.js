import { describe, expect, it } from 'vitest'

import { bodyFor, commandByName, describeOutcome, send, unreachable } from './api.js'

describe('bodyFor', () => {
  it('sends every field, so one endpoint can read the one it wants', () => {
    expect(bodyFor('travel', { id: 'V1', km: '12.5' })).toEqual({
      id: 'V1',
      plate: '',
      km: 12.5,
      reason: '',
    })
  })

  it('trims the text fields', () => {
    expect(bodyFor('register', { id: ' V1 ', plate: ' CA 41-208 ' })).toMatchObject({
      id: 'V1',
      plate: 'CA 41-208',
    })
  })

  // The browser must not hold BR-OD01. An empty or unparseable km is sent as
  // 0, which is exactly what the rule refuses.
  it('sends 0 for an empty km rather than refusing locally', () => {
    expect(bodyFor('travel', { id: 'V1', km: '' }).km).toBe(0)
    expect(bodyFor('travel', { id: 'V1', km: 'abc' }).km).toBe(0)
  })

  it('keeps a negative km, so BR-OD01 is the thing that rejects it', () => {
    expect(bodyFor('travel', { id: 'V1', km: '-4' }).km).toBe(-4)
  })
})

describe('commandByName', () => {
  it('finds the three endpoints serve.go exposes', () => {
    expect(commandByName('register').fields).toEqual(['plate'])
    expect(commandByName('travel').fields).toEqual(['km'])
    expect(commandByName('retire').fields).toEqual(['reason'])
  })

  it('is null for anything else', () => {
    expect(commandByName('delete')).toBeNull()
  })
})

describe('describeOutcome', () => {
  it('reads the sequence out of a 200', () => {
    expect(describeOutcome({ command: 'travel', id: 'V1', status: 200, body: { seq: 129 } })).toEqual(
      {
        kind: 'accepted',
        command: 'travel',
        id: 'V1',
        seq: 129,
        message: 'appended to ODOMETER at seq 129',
      },
    )
  })

  it('calls a 409 a refusal, and carries the rule code', () => {
    const out = describeOutcome({
      command: 'travel',
      id: 'V1',
      status: 409,
      body: { rule: 'BR-OD04', error: 'ErrRetired', message: 'vehicle is retired' },
    })
    expect(out.kind).toBe('refused')
    expect(out.rule).toBe('BR-OD04')
    expect(out.error).toBe('ErrRetired')
  })

  // A screen that prints a rule code when NATS is down teaches the wrong
  // lesson — serve.go never sends one, and this must not invent one either.
  it('never carries a rule code on a broken answer', () => {
    const out = describeOutcome({
      command: 'travel',
      id: 'V1',
      status: 502,
      body: { rule: 'BR-OD04', error: 'Unavailable', message: 'nats: no responders' },
    })
    expect(out.kind).toBe('broken')
    expect(out.rule).toBeUndefined()
    expect(out.error).toBe('Unavailable')
  })

  it('describes a 404 from an unknown command', () => {
    const out = describeOutcome({ command: 'scrap', id: 'V1', status: 404, body: {} })
    expect(out.kind).toBe('broken')
    expect(out.error).toBe('HTTP 404')
  })
})

describe('unreachable', () => {
  it('is broken, and names the thing that should be running', () => {
    const out = unreachable({ command: 'travel', id: 'V1', reason: 'Failed to fetch' })
    expect(out.kind).toBe('broken')
    expect(out.error).toBe('Unreachable')
    expect(out.message).toContain('cqrs serve')
  })
})

describe('send', () => {
  it('posts JSON to the endpoint named after the command', async () => {
    let seen = null
    const fetchImpl = async (url, init) => {
      seen = { url, init }
      return { status: 200, json: async () => ({ seq: 7 }) }
    }
    const out = await send('travel', { id: 'V1', km: 3 }, { fetchImpl, base: 'http://api' })
    expect(seen.url).toBe('http://api/commands/travel')
    expect(seen.init.method).toBe('POST')
    expect(JSON.parse(seen.init.body)).toMatchObject({ id: 'V1', km: 3 })
    expect(out).toMatchObject({ kind: 'accepted', seq: 7 })
  })

  it('returns an outcome when fetch throws, never an exception', async () => {
    const fetchImpl = async () => {
      throw new Error('Failed to fetch')
    }
    const out = await send('travel', { id: 'V1', km: 3 }, { fetchImpl })
    expect(out.kind).toBe('broken')
    expect(out.error).toBe('Unreachable')
  })

  it('survives a body that is not JSON', async () => {
    const fetchImpl = async () => ({
      status: 502,
      json: async () => {
        throw new Error('not json')
      },
    })
    const out = await send('travel', { id: 'V1', km: 3 }, { fetchImpl })
    expect(out.kind).toBe('broken')
    expect(out.error).toBe('HTTP 502')
  })
})
