/*
  BR-AS28 and BR-AS29 — the notification is a hint, the read is authoritative.

  Decision 55 in one module. A `notify.*` message says the registry moved; it
  never says what it moved to, and the shell never believes the body of one.
  Everything that reaches `applyRegistry` comes from a read the shell made
  itself, over the subject its own credential names (BR-AS27).

  BR-AS29 is the other half: core NATS is fire-and-forget. There is no gap
  detection and no redelivery, so a message sent while the socket was down is
  simply gone. Every re-establishment therefore reads unconditionally —
  decision 56 — rather than assuming the revision it holds is still current.
*/

import { describe, expect, it, vi } from 'vitest'

import { createChangeSubscription, NOTIFY_SUBJECT } from './changeSubscription.js'

const flush = () => new Promise((resolve) => setTimeout(resolve, 0))

const harness = ({ held = 12, read } = {}) => {
  const handlers = []
  const unsubscribe = vi.fn()
  const subscribe = vi.fn((_subject, handler) => {
    handlers.push(handler)
    return { unsubscribe }
  })
  const doRead = read ?? vi.fn().mockResolvedValue({ ok: true, unchanged: true })
  const sub = createChangeSubscription({
    subscribe,
    read: doRead,
    currentRevision: () => held,
  })
  return { sub, subscribe, unsubscribe, read: doRead, deliver: (msg) => handlers[0]?.(msg) }
}

describe('BR-AS28 — a change notification is a hint, never a payload', () => {
  it('listens on the notify subject and nowhere else', () => {
    const { sub, subscribe } = harness()
    sub.start()

    expect(NOTIFY_SUBJECT).toBe('notify._platform.registry.frontend-plugins.changed')
    expect(subscribe).toHaveBeenCalledTimes(1)
    expect(subscribe.mock.calls[0][0]).toBe(NOTIFY_SUBJECT)
  })

  /* The rule that makes the subject safe to be widely publishable: whatever
     is in the body, the shell installs nothing from it. A message carrying a
     complete, plausible catalog changes exactly one thing — it causes a read. */
  it('installs nothing from a message that carries a catalog', async () => {
    const read = vi.fn().mockResolvedValue({ ok: true, unchanged: false, revision: 13, plugins: [] })
    const applied = []
    const { sub, deliver } = harness({
      read: (...args) => {
        applied.push(args)
        return read(...args)
      },
    })
    sub.start()

    deliver({
      revision: 13,
      entries: [{ id: 'evil', remote: { kind: 'federated', url: 'https://evil.example.com/remoteEntry.js' } }],
    })
    await flush()

    expect(read).toHaveBeenCalledTimes(1)
    // Nothing from the message travelled with the read.
    expect(JSON.stringify(applied)).not.toContain('evil.example.com')
    expect(JSON.stringify(applied)).not.toContain('evil')
  })

  it('performs no read at all when the revision matches what it holds', async () => {
    const { sub, read, deliver } = harness({ held: 12 })
    sub.start()

    deliver({ revision: 12 })
    await flush()

    expect(read).not.toHaveBeenCalled()
  })

  it('performs no read for a revision older than the one it holds', async () => {
    const { sub, read, deliver } = harness({ held: 12 })
    sub.start()

    deliver({ revision: 11 })
    await flush()

    expect(read).not.toHaveBeenCalled()
  })

  it('reads conditionally, exactly once, for a higher revision', async () => {
    const { sub, read, deliver } = harness({ held: 12 })
    sub.start()

    deliver({ revision: 13 })
    await flush()

    expect(read).toHaveBeenCalledTimes(1)
    expect(read).toHaveBeenCalledWith({ unconditional: false, reason: 'notify' })
  })

  /* A message the shell cannot read is still evidence something moved. It is
     resolved the same way everything else is — by reading. */
  it('reads when the message names no revision it can use', async () => {
    const { sub, read, deliver } = harness({ held: 12 })
    sub.start()

    deliver({})
    deliver(null)
    await flush()

    expect(read).toHaveBeenCalledTimes(2)
  })

  /* A burst of writes is one operator transaction most of the time. Reading
     once per message would answer the same revision several times over. */
  it('coalesces a burst arriving while a read is still in flight', async () => {
    let resolveRead
    const read = vi.fn(() => new Promise((resolve) => { resolveRead = resolve }))
    const { sub, deliver } = harness({ held: 12, read })
    sub.start()

    deliver({ revision: 13 })
    deliver({ revision: 14 })
    deliver({ revision: 15 })
    await flush()
    expect(read).toHaveBeenCalledTimes(1)

    resolveRead({ ok: true, unchanged: false, revision: 15 })
    await flush()
  })

  it('a failed read does not tear the subscription down', async () => {
    const read = vi.fn().mockRejectedValue(new Error('no responders'))
    const { sub, deliver } = harness({ held: 12, read })
    sub.start()

    deliver({ revision: 13 })
    await flush()
    deliver({ revision: 14 })
    await flush()

    expect(read).toHaveBeenCalledTimes(2)
  })

  it('stops listening on stop, and is safe to stop twice', () => {
    const { sub, unsubscribe } = harness()
    sub.start()
    sub.stop()
    sub.stop()

    expect(unsubscribe).toHaveBeenCalledTimes(1)
  })
})

describe('BR-AS29 — a reconnect re-reads unconditionally', () => {
  /* Core NATS delivers nothing that was published while the socket was down,
     and there is no sequence to compare against. The shell cannot know
     whether it missed a change, so it stops trying to know: it reads. */
  it('reads without naming the revision it holds after every re-establishment', async () => {
    const { sub, read } = harness({ held: 12 })
    sub.start()

    sub.onReconnect()
    await flush()

    expect(read).toHaveBeenCalledWith({ unconditional: true, reason: 'reconnect' })
  })

  it('reads again on the second re-establishment — a reconnect is never assumed redundant', async () => {
    const { sub, read } = harness({ held: 12 })
    sub.start()

    sub.onReconnect()
    await flush()
    sub.onReconnect()
    await flush()

    expect(read).toHaveBeenCalledTimes(2)
  })

  /* The one case where "unchanged" is the wrong question: after an outage the
     shell may hold a revision the service has moved past AND come back to, so
     a conditional read could answer 304 for a document it never saw. */
  it('does not send the held revision even when a notification just set one', async () => {
    const { sub, read, deliver } = harness({ held: 12 })
    sub.start()

    deliver({ revision: 13 })
    await flush()
    sub.onReconnect()
    await flush()

    expect(read.mock.calls[0][0].unconditional).toBe(false)
    expect(read.mock.calls[1][0].unconditional).toBe(true)
  })
})
