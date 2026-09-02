/*
  The machine both hinted planes run on (BR-AS28, AS29, AS64, AS65).

  These specs are deliberately written against a bare `read` and a bare
  `subscribe` — no catalogue, no health, no revisions. That is the point of
  the seam: the catalogue and the health plane inherit every behaviour proved
  here, and each adds only what it alone knows (a revision policy, and ageing
  readings, respectively).
*/
import { describe, expect, it, vi } from 'vitest'

import { createHintedReader } from './hintedReader.js'

const SUBJECT = 'notify.test.thing.changed'

const flush = async () => {
  for (let i = 0; i < 12; i += 1) await Promise.resolve()
}

const deferred = () => {
  let resolve
  const promise = new Promise((r) => { resolve = r })
  return { promise, resolve }
}

/* A reader whose reads resolve immediately, plus the hint handler the
   subscription was given. */
const readerWith = (read, hints = undefined) => {
  let handler = null
  let unsubscribed = 0
  const reader = createHintedReader({
    subject: SUBJECT,
    subscribe: (subject, fn) => {
      expect(subject).toBe(SUBJECT)
      handler = fn
      return { unsubscribe: () => { unsubscribed += 1 } }
    },
    read,
    hints,
  })
  return { reader, hint: (message) => handler(message), unsubscribedCount: () => unsubscribed }
}

describe('one read at a time', () => {
  it('never runs two reads concurrently, whoever asked for them', async () => {
    /* Two reads in flight can install in the order they FINISH rather than
       the order they were asked for, which lets an older answer overwrite a
       newer one. */
    let running = 0
    let maxConcurrent = 0
    const gate = deferred()
    const read = vi.fn(async () => {
      running += 1
      maxConcurrent = Math.max(maxConcurrent, running)
      await gate.promise
      running -= 1
      return { ok: true }
    })
    const { reader } = readerWith(read)

    const a = reader.refresh({ reason: 'one' })
    const b = reader.refresh({ reason: 'two' })
    gate.resolve()
    await Promise.all([a, b])
    await flush()

    expect(read).toHaveBeenCalledTimes(2)
    expect(maxConcurrent).toBe(1)
  })

  it('keeps reading after one read rejects', async () => {
    // A rejection that broke the chain would silently end every later read.
    const read = vi.fn()
      .mockRejectedValueOnce(new Error('no responders'))
      .mockResolvedValue({ ok: true })
    const { reader } = readerWith(read)

    await expect(reader.refresh({ reason: 'first' })).resolves.toBeNull()
    await expect(reader.refresh({ reason: 'second' })).resolves.toEqual({ ok: true })
  })

  it('hands the caller back what the read returned', async () => {
    const { reader } = readerWith(vi.fn(async () => ({ ok: true, revision: 4 })))
    await expect(reader.refresh({ reason: 'boot' })).resolves.toEqual({ ok: true, revision: 4 })
  })
})

describe('a hint is a reason to read, never a payload', () => {
  it('reads nothing until the subscription is started', async () => {
    const read = vi.fn(async () => ({ ok: true }))
    const { reader, hint } = readerWith(read)
    reader.start()
    hint({ anything: 'at all' })
    await flush()

    expect(read).toHaveBeenCalledTimes(1)
    expect(read.mock.calls[0][0]).toMatchObject({ unconditional: false, reason: 'notify' })
  })

  it('holds a hint that lands mid-read and answers it with exactly one more', async () => {
    const gate = deferred()
    let first = true
    const read = vi.fn(async () => {
      if (first) { first = false; await gate.promise }
      return { ok: true }
    })
    const { reader, hint } = readerWith(read)
    reader.start()

    const inFlight = reader.refresh({ reason: 'boot' })
    await flush()
    // A burst of three during one read is still one read afterwards.
    hint({})
    hint({})
    hint({})
    gate.resolve()
    await inFlight
    await flush()

    expect(read.mock.calls.map((c) => c[0].reason)).toEqual(['boot', 'notify'])
  })

  it('stops reading once stopped, and lets the subscription go', async () => {
    const read = vi.fn(async () => ({ ok: true }))
    const { reader, hint, unsubscribedCount } = readerWith(read)
    reader.start()
    await reader.refresh({ reason: 'boot' })
    reader.stop()

    hint({})
    await reader.refresh({ reason: 'after-stop' })
    await flush()

    expect(read).toHaveBeenCalledTimes(1)
    expect(unsubscribedCount()).toBe(1)
    await expect(reader.refresh({ reason: 'again' })).resolves.toBeNull()
  })
})

describe('a re-established link is a gap in the hints', () => {
  it('reads unconditionally after a reconnect', async () => {
    const read = vi.fn(async () => ({ ok: true }))
    const { reader } = readerWith(read)

    await reader.onReconnect()

    expect(read).toHaveBeenCalledWith({ unconditional: true, reason: 'reconnect' })
  })

  it('honours a reconnect that lands before the subscription exists', async () => {
    /* The boot read is in flight, the link drops and comes back, and the
       subscription has not been started yet. This is the case that used to
       need a second reconnect counter in the caller. */
    const gate = deferred()
    let first = true
    const read = vi.fn(async () => {
      if (first) { first = false; await gate.promise }
      return { ok: true }
    })
    const { reader } = readerWith(read)

    const boot = reader.refresh({ unconditional: true, reason: 'boot' })
    await flush()
    void reader.onReconnect()
    gate.resolve()
    await boot
    await flush()

    expect(read.mock.calls.map((c) => c[0].reason)).toEqual(['boot', 'reconnect'])
  })

  it('reads nothing after stop, however the link behaves', async () => {
    const read = vi.fn(async () => ({ ok: true }))
    const { reader } = readerWith(read)
    reader.stop()

    await expect(reader.onReconnect()).resolves.toBeNull()
    expect(read).not.toHaveBeenCalled()
  })
})

describe('a hint policy is optional detail, never a requirement', () => {
  it('always reads again for a plane whose hints say nothing about themselves', async () => {
    // The health plane's case: a reading carries no version, so there is
    // never grounds to decide a held hint was already answered.
    const gate = deferred()
    let first = true
    const read = vi.fn(async () => {
      if (first) { first = false; await gate.promise }
      return { ok: true, revision: 99 }
    })
    const { reader, hint } = readerWith(read)
    reader.start()

    const inFlight = reader.refresh({ reason: 'start' })
    await flush()
    hint({ revision: 1 })
    gate.resolve()
    await inFlight
    await flush()

    expect(read).toHaveBeenCalledTimes(2)
  })

  it('drops a hint the policy calls stale, before it can cost a read', async () => {
    const read = vi.fn(async () => ({ ok: true }))
    const { reader, hint } = readerWith(read, {
      of: (m) => m.revision,
      stale: (revision) => revision <= 7,
    })
    reader.start()

    hint({ revision: 7 })
    await flush()
    expect(read).not.toHaveBeenCalled()

    hint({ revision: 8 })
    await flush()
    expect(read).toHaveBeenCalledTimes(1)
  })

  it('skips the trailing read the finished document already covered', async () => {
    // The catalogue's case: the hint asked for revision 5 and the read that
    // was already running came back at 6. Nothing is owed.
    const gate = deferred()
    let first = true
    const read = vi.fn(async () => {
      if (first) { first = false; await gate.promise }
      return { ok: true, revision: 6 }
    })
    const { reader, hint } = readerWith(read, {
      of: (m) => m.revision,
      covered: (revision, result) => revision <= result.revision,
    })
    reader.start()

    const inFlight = reader.refresh({ reason: 'boot' })
    await flush()
    hint({ revision: 5 })
    gate.resolve()
    await inFlight
    await flush()

    expect(read).toHaveBeenCalledTimes(1)
  })

  it('merges two held hints through the policy', async () => {
    const merge = vi.fn((a, b) => Math.max(a, b))
    const gate = deferred()
    let first = true
    const read = vi.fn(async () => {
      if (first) { first = false; await gate.promise }
      return { ok: true, revision: 6 }
    })
    const { reader, hint } = readerWith(read, {
      of: (m) => m.revision,
      merge,
      /* The merged token is what reaches `covered`: 9 beats the document's 6,
         so the trailing read still happens. */
      covered: (revision, result) => revision <= result.revision,
    })
    reader.start()

    const inFlight = reader.refresh({ reason: 'boot' })
    await flush()
    hint({ revision: 4 })
    hint({ revision: 9 })
    gate.resolve()
    await inFlight
    await flush()

    expect(merge).toHaveBeenCalledWith(4, 9)
    expect(read).toHaveBeenCalledTimes(2)
  })
})
