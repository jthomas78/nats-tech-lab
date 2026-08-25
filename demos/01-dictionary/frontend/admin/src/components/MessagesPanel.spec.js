// Pending specs for Phase 43c (BUSINESS_RULES-SHIPPING.md's BR-048): a
// dedicated Admin UI Messages panel (not a RpcPanel tab) rendering
// obs.pubsub.* entries with an evt/notify family filter. Design approved
// (ADR-047), amended 2026-08-25 by a pre-implementation review — the tenant
// and default-filter specs below reflect that amendment (A1, A9).
// Implementation on hold. Deliberately not importing `./MessagesPanel.vue`
// — it doesn't exist yet — so these use `it.todo` rather than `it`, which
// needs no body and keeps `npm run test` green until Phase 43c lands.

import { describe, it } from 'vitest'

describe('MessagesPanel (Phase 43c, BR-048)', () => {
  it.todo('renders as its own SYSTEM → NATS nav entry, not a RpcPanel tab')

  it.todo('filters entries by evt/notify family via a toggle-chip control, mirroring RpcPanel\'s rpc/api filter')

  it.todo('defaults the family filter to evt only, since notify.* is largely a fan-out of events already visible on the evt side')

  it.todo('renders each entry\'s subject via SubjectPath.vue (clickable subject chips)')

  it.todo('names the originating tenant from the monitor.{tenant}.pubsub.> import remap, not from TraceWaterfall\'s coarse PLATFORM/TENANT gutter')

  it.todo('caps rendered rows and offers a pause control, reusing RpcPanel\'s, since evt.* volume exceeds RPC volume')

  it.todo('bootstraps from the Messages feed and stays live for anything published afterward')
})
