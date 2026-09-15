import { describe, expect, it } from 'vitest'

import { applyEntry } from './useOdometer.js'
import { lag, logEvent, maxSeq, poolWorker, readModel, writeSnapshot } from './model.js'
import { parseEventSubject, vehicleFromKey } from './subjects.js'

// These specs run with no NATS and no browser. Everything they cover is a
// pure function on purpose — the plumbing in useOdometer.js is verified
// against a live server instead, because a mocked WebSocket would prove that
// the mock works.

describe('parseEventSubject', () => {
  it('reads the vehicle and the event type out of the subject', () => {
    expect(parseEventSubject('evt.odometer.vehicle.truck-7.travelled')).toEqual({
      vehicle: 'truck-7',
      type: 'travelled',
    })
  })

  it('accepts all three types domain.go can produce', () => {
    for (const type of ['registered', 'travelled', 'retired']) {
      expect(parseEventSubject(`evt.odometer.vehicle.v1.${type}`)).toEqual({ vehicle: 'v1', type })
    }
  })

  it('refuses an event type it does not know', () => {
    expect(parseEventSubject('evt.odometer.vehicle.v1.scrapped')).toBeNull()
  })

  it('refuses another demo subject on the same server', () => {
    expect(parseEventSubject('evt.shipping.ship.s1.arrived')).toBeNull()
  })

  it('refuses a subject with the wrong number of tokens', () => {
    expect(parseEventSubject('evt.odometer.vehicle.v1')).toBeNull()
    expect(parseEventSubject('evt.odometer.vehicle.v1.a.travelled')).toBeNull()
  })

  it('refuses rubbish', () => {
    expect(parseEventSubject('')).toBeNull()
    expect(parseEventSubject(undefined)).toBeNull()
  })
})

describe('vehicleFromKey', () => {
  it('reverses snapshotKey from names.go', () => {
    expect(vehicleFromKey('vehicle.truck-7')).toBe('truck-7')
  })

  it('refuses a key that is not a vehicle key', () => {
    expect(vehicleFromKey('fleet.truck-7')).toBeNull()
    expect(vehicleFromKey('vehicle.')).toBeNull()
  })
})

describe('writeSnapshot', () => {
  it('unwraps {state, lastSeq}', () => {
    const doc = { state: { status: 'registered', plate: 'CA 41-208' }, lastSeq: 12 }
    expect(writeSnapshot('vehicle.truck-7', doc)).toEqual({
      id: 'truck-7',
      status: 'registered',
      plate: 'CA 41-208',
      lastSeq: 12,
    })
  })

  it('treats a missing snapshot as sequence 0, not as an error', () => {
    // loadSnapshot in write.go does the same: a missing key means replay from
    // the start, not a failure.
    expect(writeSnapshot('vehicle.v1', {})).toEqual({
      id: 'v1',
      status: '',
      plate: '',
      lastSeq: 0,
    })
  })
})

describe('readModel', () => {
  it('reads the flat document the projector writes', () => {
    const doc = {
      status: 'registered',
      plate: 'CA 41-208',
      totalKm: 126.5,
      trips: 3,
      lastTripAt: '2026-09-15T08:00:00Z',
      lastSeq: 9,
    }
    expect(readModel('vehicle.truck-7', doc)).toEqual({ id: 'truck-7', ...doc })
  })
})

describe('logEvent', () => {
  it('builds a row from the subject and the body', () => {
    const row = logEvent({
      subject: 'evt.odometer.vehicle.truck-7.travelled',
      seq: 42,
      time: new Date('2026-09-15T08:00:00Z'),
      body: { km: 12.5 },
    })
    expect(row).toEqual({
      seq: 42,
      at: '2026-09-15T08:00:00.000Z',
      vehicle: 'truck-7',
      type: 'travelled',
      detail: '12.5 km',
    })
  })

  it('summarises each type from its own body', () => {
    const at = new Date(0)
    const base = { seq: 1, time: at }
    expect(
      logEvent({ ...base, subject: 'evt.odometer.vehicle.v1.registered', body: { plate: 'AB 1' } })
        .detail,
    ).toBe('plate AB 1')
    expect(
      logEvent({ ...base, subject: 'evt.odometer.vehicle.v1.retired', body: { reason: 'scrapped' } })
        .detail,
    ).toBe('scrapped')
  })

  it('drops a message this demo did not publish', () => {
    expect(logEvent({ subject: '$SYS.SERVER.x.STATSZ', seq: 1, time: new Date(), body: {} })).toBeNull()
  })
})

describe('lag', () => {
  it('measures both sides against the head of the log', () => {
    expect(lag({ head: 130, writeSeq: 126, readSeq: 118 })).toEqual({
      head: 130,
      writeSeq: 126,
      readSeq: 118,
      writeLag: 4,
      readLag: 12,
    })
  })

  it('reports zero lag when a side has caught up', () => {
    expect(lag({ head: 5, writeSeq: 5, readSeq: 5 })).toMatchObject({ writeLag: 0, readLag: 0 })
  })

  it('clamps a side that is ahead of what this browser has seen', () => {
    // The projector folded an event the browser's own tail has not reached.
    // The browser is behind; drawing a negative bar would say the opposite.
    expect(lag({ head: 3, writeSeq: 7, readSeq: 7 })).toMatchObject({ writeLag: 0, readLag: 0 })
  })

  it('starts at zero with nothing connected', () => {
    expect(lag()).toEqual({ head: 0, writeSeq: 0, readSeq: 0, writeLag: 0, readLag: 0 })
  })
})

describe('maxSeq', () => {
  it('takes the furthest a bucket has been folded to', () => {
    expect(maxSeq([{ lastSeq: 4 }, { lastSeq: 11 }, { lastSeq: 2 }])).toBe(11)
  })

  it('is zero for an empty bucket', () => {
    expect(maxSeq([])).toBe(0)
  })
})

describe('applyEntry', () => {
  const entry = (key, operation, doc, revision) => ({
    key,
    operation,
    revision,
    json: () => doc,
  })

  it('puts a shaped row into the map', () => {
    const into = new Map()
    applyEntry(into, entry('vehicle.v1', 'PUT', { state: { status: 'registered' }, lastSeq: 3 }, 8), writeSnapshot)
    expect(into.get('v1')).toMatchObject({ id: 'v1', status: 'registered', lastSeq: 3, revision: 8 })
  })

  it('removes the row when the key is deleted', () => {
    const into = new Map([['v1', { id: 'v1' }]])
    applyEntry(into, entry('vehicle.v1', 'DEL', null, 9), writeSnapshot)
    expect(into.has('v1')).toBe(false)
  })

  it('removes the row when the key is purged', () => {
    const into = new Map([['v1', { id: 'v1' }]])
    applyEntry(into, entry('vehicle.v1', 'PURGE', null, 9), writeSnapshot)
    expect(into.has('v1')).toBe(false)
  })

  it('ignores a key that is not a vehicle key', () => {
    const into = new Map()
    applyEntry(into, entry('something.else', 'PUT', {}, 1), writeSnapshot)
    expect(into.size).toBe(0)
  })

  it('survives a value that is not JSON', () => {
    const into = new Map()
    const broken = {
      key: 'vehicle.v1',
      operation: 'PUT',
      revision: 1,
      json: () => {
        throw new Error('not json')
      },
    }
    applyEntry(into, broken, readModel)
    expect(into.get('v1')).toMatchObject({ id: 'v1', totalKm: 0, trips: 0 })
  })
})

// Phase 04.7 — KV odometer-pool-workers. One key per worker, WorkerState in
// cqrs/pool.go. The browser watches it exactly the way it watches the other
// buckets, so it needs a shape function and nothing else.
describe('poolWorker', () => {
  it('shapes one worker heartbeat', () => {
    const w = poolWorker('worker.03', {
      worker: 3,
      status: 'working',
      holding: 94,
      acked: 30,
      dropped: 1,
      behind: 95,
      at: '2026-09-15T10:00:00Z',
    })
    expect(w.id).toBe('worker.03')
    expect(w.worker).toBe(3)
    expect(w.status).toBe('working')
    expect(w.holding).toBe(94)
    expect(w.acked).toBe(30)
    expect(w.dropped).toBe(1)
  })

  it('takes the worker number from the key when the value has none', () => {
    expect(poolWorker('worker.07', {}).worker).toBe(7)
  })

  it('refuses a key this demo does not write', () => {
    expect(poolWorker('vehicle.truck-7', {})).toBeNull()
  })

  it('calls a worker with no status waiting, not working', () => {
    expect(poolWorker('worker.01', {}).status).toBe('waiting')
  })
})
