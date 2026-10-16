// Phase 54c (decision 17, BR-072) — the live service list. One
// operator-only request subject on observability-service; the reply is the
// lab.servicelist.v1 snapshot (servicelist.go). The body is always {}: a
// client cannot name a discovery subject.
export const SERVICE_LIST_SUBJECT = 'api._platform.observability.service.list.v1'

export function listServices(request) {
  return request(SERVICE_LIST_SUBJECT, {})
}
