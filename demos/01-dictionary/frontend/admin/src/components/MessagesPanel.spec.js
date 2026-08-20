// Pending specs for Phase 47c (BUSINESS_RULES-SHIPPING.md's BR-048): a
// dedicated Admin UI Messages panel (not a RpcPanel tab) rendering
// obs.pubsub.* entries with an evt/notify family filter. Design approved
// (ADR-047); implementation on hold. Deliberately not importing
// `./MessagesPanel.vue` — it doesn't exist yet — so these use `it.todo`
// rather than `it`, which needs no body and keeps `npm run test` green
// until Phase 47c lands.

import { describe, it } from 'vitest'

describe('MessagesPanel (Phase 47c, BR-048)', () => {
  it.todo('renders as its own SYSTEM → NATS nav entry, not a RpcPanel tab')

  it.todo('filters entries by evt/notify family via a toggle-chip control, mirroring RpcPanel\'s rpc/api filter')

  it.todo('renders each entry\'s subject via SubjectPath.vue (clickable subject chips)')

  it.todo('renders which account an entry crossed via TraceWaterfall\'s account-swimlane convention')

  it.todo('bootstraps from the Messages feed and stays live for anything published afterward')
})
