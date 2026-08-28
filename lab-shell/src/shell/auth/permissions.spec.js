import { describe, expect, it } from 'vitest'

import { ANONYMOUS, createPermissionEvaluator } from './permissions.js'

const evaluator = (...permissions) =>
  createPermissionEvaluator({ sub: 'jeremy', tenant: 'acme', permissions })

describe('BR-AS05 — the shell evaluates contribution permissions', () => {
  it('grants an exact match', () => {
    expect(evaluator('fleet-ops.vessels.read').can('fleet-ops.vessels.read')).toBe(true)
  })

  it('refuses a permission the claims do not carry', () => {
    expect(evaluator('fleet-ops.vessels.read').can('fleet-ops.vessels.write')).toBe(false)
  })

  it('grants everything below a trailing wildcard', () => {
    const e = evaluator('fleet-ops.*')

    expect(e.can('fleet-ops.vessels.read')).toBe(true)
    expect(e.can('fleet-ops.vessels.write')).toBe(true)
  })

  it('does not let a wildcard leak across the segment it sits in', () => {
    // 'fleet-ops.*' must not grant 'fleet-opsx.anything' — the wildcard
    // replaces a whole segment, never part of one.
    expect(evaluator('fleet-ops.*').can('fleet-opsx.vessels.read')).toBe(false)
  })

  it('matches a wildcard in a middle position against exactly one segment', () => {
    const e = evaluator('fleet-ops.*.read')

    expect(e.can('fleet-ops.vessels.read')).toBe(true)
    expect(e.can('fleet-ops.vessels.detail.read')).toBe(false)
  })

  it('treats a contribution with no declared permission as unrestricted', () => {
    // null is an answer — "this contribution requires nothing" — not a
    // missing value to be defaulted into a denial.
    expect(evaluator().can(null)).toBe(true)
    expect(ANONYMOUS.can(null)).toBe(true)
  })

  it('refuses everything restricted for an anonymous viewer, without throwing', () => {
    expect(ANONYMOUS.can('fleet-ops.vessels.read')).toBe(false)
    expect(ANONYMOUS.subject).toBeNull()
  })

  it('ignores a malformed grant rather than treating it as a wildcard', () => {
    // A grant the shell cannot parse must fail closed. The failure mode worth
    // preventing is a typo in the registry silently granting everything.
    const e = createPermissionEvaluator({ permissions: ['Fleet Ops!!', 'fleet-ops.vessels.read'] })

    expect(e.can('fleet-ops.vessels.read')).toBe(true)
    expect(e.grants).toEqual(['fleet-ops.vessels.read'])
  })

  it('survives claims that are not shaped like claims at all', () => {
    expect(createPermissionEvaluator({ permissions: 'everything' }).can('x.y')).toBe(false)
    expect(createPermissionEvaluator(undefined).can('x.y')).toBe(false)
  })

  it('exposes identity for labelling but never decides from it', () => {
    const e = evaluator()

    expect(e.tenant).toBe('acme')
    // Being a known subject of a known tenant grants nothing on its own.
    expect(e.can('fleet-ops.vessels.read')).toBe(false)
  })
})

describe('BR-AS05 — hiding UI never replaces server-side authorization', () => {
  it('verifies no token — verification belongs to accounts-service', () => {
    // The evaluator accepts a decoded payload with no signature anywhere near
    // it. That is deliberate: anything this module concluded about
    // authenticity would be a conclusion drawn in the attacker's own runtime.
    const forged = createPermissionEvaluator({ sub: 'nobody', permissions: ['fleet-ops.*'] })

    expect(forged.can('fleet-ops.vessels.write')).toBe(true)
    // ...which is exactly why the shell's answer is only ever about drawing.
  })
})
