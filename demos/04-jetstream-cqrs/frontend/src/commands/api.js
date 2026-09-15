// The write path. The only place in this app that sends anything.
//
// D1 says the browser never decides anything, and that has a sharp edge here:
// this file must not check a single business rule. It does not grey out
// `travel` for a retired vehicle, it does not stop a trip of 0 km, and it does
// not ask whether a vehicle is registered before offering `retire`. Every one
// of those is BR-OD01..05, they live in domain.go, and the demo is watching
// the domain refuse them.
//
// So the flow is always the same: send it, and render whatever comes back.
//
//   200  the domain said yes    -> the event is in the stream at that seq
//   409  the domain said no     -> a rule code, a Go error name, a message
//   else the demo is broken     -> NEVER given a rule code (see serve.go)
//
// Nothing here is a Vue thing, so all of it is testable without a browser.

import { COMMAND_API } from '../config.js'

// One entry per endpoint in serve.go. `fields` is what the form shows; it is a
// display list, not a validation list.
export const COMMANDS = Object.freeze([
  Object.freeze({
    name: 'register',
    label: 'Register',
    verb: 'register',
    fields: Object.freeze(['plate']),
    note: 'Puts a vehicle into service. Twice is BR-OD03.',
  }),
  Object.freeze({
    name: 'travel',
    label: 'Record trip',
    verb: 'record a trip for',
    fields: Object.freeze(['km']),
    note: 'A trip of 0 or less is BR-OD01. An unknown vehicle is BR-OD02.',
  }),
  Object.freeze({
    name: 'retire',
    label: 'Retire',
    verb: 'retire',
    fields: Object.freeze(['reason']),
    note: 'Takes a vehicle out of service. After this, a trip is BR-OD04.',
  }),
])

export function commandByName(name) {
  return COMMANDS.find((c) => c.name === name) ?? null
}

// bodyFor shapes the JSON serve.go expects. Every command sends the same
// object and each endpoint reads the one field it cares about.
//
// km is sent as a number, and an unparseable box sends 0 rather than refusing
// locally — 0 is exactly the value BR-OD01 exists to reject, so the demo shows
// the rule instead of hiding it behind a disabled button.
export function bodyFor(name, form = {}) {
  return {
    id: String(form.id ?? '').trim(),
    plate: String(form.plate ?? '').trim(),
    km: toNumber(form.km),
    reason: String(form.reason ?? '').trim(),
  }
}

function toNumber(value) {
  const n = Number(value)
  return Number.isFinite(n) ? n : 0
}

// describeOutcome turns one HTTP answer into one row of the result list.
//
// The three kinds are deliberately not "success" and "error". A refusal is not
// an error: the demo works correctly when a rule refuses, and calling it an
// error would teach the opposite of the point.
export function describeOutcome({ command, id, status, body = {} }) {
  if (status === 200) {
    return {
      kind: 'accepted',
      command,
      id,
      seq: Number(body.seq ?? 0),
      message: `appended to ODOMETER at seq ${Number(body.seq ?? 0)}`,
    }
  }
  if (status === 409) {
    return {
      kind: 'refused',
      command,
      id,
      rule: body.rule ?? '',
      error: body.error ?? '',
      message: body.message ?? 'the domain refused the command',
    }
  }
  // Anything else is the plumbing, not the domain. No rule code is carried
  // here even if the body somehow had one.
  return {
    kind: 'broken',
    command,
    id,
    error: body.error ?? `HTTP ${status}`,
    message: body.message ?? 'the command API did not answer with a decision',
  }
}

// unreachable is the case fetch itself fails: the shim is not running, or the
// browser blocked the request. It is a `broken`, never a refusal.
export function unreachable({ command, id, reason }) {
  return {
    kind: 'broken',
    command,
    id,
    error: 'Unreachable',
    message: `${COMMAND_API} did not answer (${reason}). Is \`cqrs serve\` running?`,
  }
}

// send does the HTTP. It resolves with an outcome for every path, including
// failure, because the screen renders outcomes and never exceptions.
export async function send(name, form, { fetchImpl = globalThis.fetch, base = COMMAND_API } = {}) {
  const body = bodyFor(name, form)
  try {
    const res = await fetchImpl(`${base}/commands/${name}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    })
    let parsed = {}
    try {
      parsed = await res.json()
    } catch {
      // A body that is not JSON is still an answer; the status carries it.
    }
    return describeOutcome({ command: name, id: body.id, status: res.status, body: parsed })
  } catch (err) {
    return unreachable({ command: name, id: body.id, reason: err?.message ?? String(err) })
  }
}
