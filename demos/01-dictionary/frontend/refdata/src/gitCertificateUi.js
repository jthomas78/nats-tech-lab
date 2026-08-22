export function isGitExpired(certificate, nowMs = Date.now()) {
  return certificate.expiresAt != null && certificate.expiresAt * 1000 <= nowMs
}

export function gitDisplayStatus(certificate, nowMs = Date.now()) {
  if (certificate.status === 'SUPERSEDED') return 'SUPERSEDED'
  if (isGitExpired(certificate, nowMs)) return 'EXPIRED'
  return certificate.status
}

export function carriesGitCover(certificate, nowMs = Date.now()) {
  return certificate.status === 'APPROVED' && !isGitExpired(certificate, nowMs)
}

// One mapping drives every row affordance. Keeping this outside the template
// makes BR-TP67/BR-TP70 regressions visible to Vitest instead of hiding them
// in a cluster of independent v-if expressions.
export function gitCertificateActions(certificate, nowMs = Date.now()) {
  const superseded = certificate.status === 'SUPERSEDED'
  const reviewable = certificate.status === 'PENDING' || certificate.status === 'FOR_REVIEW'
  return {
    edit: !superseded,
    correctExpiry: true,
    approve: reviewable && !isGitExpired(certificate, nowMs),
    reject: reviewable,
    resubmit: certificate.status === 'REJECTED',
  }
}
