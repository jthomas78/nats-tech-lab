import { describe, expect, it } from 'vitest'
import { carriesGitCover, gitCertificateActions, gitDisplayStatus } from './gitCertificateUi'

const now = Date.parse('2026-08-22T12:00:00Z')
const future = Math.floor(Date.parse('2027-01-01T00:00:00Z') / 1000)
const past = Math.floor(Date.parse('2026-01-01T00:00:00Z') / 1000)

describe('GIT certificate row presentation', () => {
  it.each([
    [{ status: 'PENDING', expiresAt: future }, 'PENDING'],
    [{ status: 'FOR_REVIEW', expiresAt: future }, 'FOR_REVIEW'],
    [{ status: 'REJECTED', expiresAt: future }, 'REJECTED'],
    [{ status: 'APPROVED', expiresAt: future }, 'APPROVED'],
    [{ status: 'APPROVED', expiresAt: past }, 'EXPIRED'],
    [{ status: 'SUPERSEDED', expiresAt: past }, 'SUPERSEDED'],
  ])('derives display status without hiding the superseded lock', (certificate, expected) => {
    expect(gitDisplayStatus(certificate, now)).toBe(expected)
  })

  it('highlights only the live approved certificate, even beneath a newer renewal', () => {
    const newest = { status: 'FOR_REVIEW', expiresAt: future }
    const incumbent = { status: 'APPROVED', expiresAt: future }
    expect(carriesGitCover(newest, now)).toBe(false)
    expect(carriesGitCover(incumbent, now)).toBe(true)
    expect(carriesGitCover({ status: 'APPROVED', expiresAt: past }, now)).toBe(false)
  })

  it.each([
    ['PENDING', future, { edit: true, correctExpiry: true, approve: true, reject: true, resubmit: false }],
    ['FOR_REVIEW', future, { edit: true, correctExpiry: true, approve: true, reject: true, resubmit: false }],
    ['REJECTED', future, { edit: true, correctExpiry: true, approve: false, reject: false, resubmit: true }],
    ['APPROVED', future, { edit: true, correctExpiry: true, approve: false, reject: false, resubmit: false }],
    ['APPROVED', past, { edit: true, correctExpiry: true, approve: false, reject: false, resubmit: false }],
    ['SUPERSEDED', past, { edit: false, correctExpiry: true, approve: false, reject: false, resubmit: false }],
  ])('maps %s rows to exactly the backend-supported actions', (status, expiresAt, expected) => {
    expect(gitCertificateActions({ status, expiresAt }, now)).toEqual(expected)
  })

  it('does not offer approval for an expired reviewable certificate, but still permits rejection', () => {
    expect(gitCertificateActions({ status: 'FOR_REVIEW', expiresAt: past }, now)).toEqual({
      edit: true, correctExpiry: true, approve: false, reject: true, resubmit: false,
    })
  })
})
