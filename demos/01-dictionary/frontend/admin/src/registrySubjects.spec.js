import { describe, expect, it } from 'vitest'

import { REGISTRY_SUBJECTS } from './registrySubjects.js'

describe('MFE registry subject contract', () => {
  it('pins the six Admin subjects in one language-side list', () => {
    expect(REGISTRY_SUBJECTS).toEqual({
      curated: 'api._platform.mfe-registry.entries.curated.v1',
      upsert: 'api._platform.mfe-registry.entries.upsert.v1',
      setEnabled: 'api._platform.mfe-registry.entries.set-enabled.v1',
      audit: 'api._platform.mfe-registry.audit.list.v1',
      publishers: 'api._platform.mfe-registry.publishers.list.v1',
      publisherWrite: 'api._platform.mfe-registry.publishers.write.v1',
    })
  })
})
