// Keep the Admin registry transport in one reviewable list. This mirrors
// shared/mferegistry/subjects.go; adding a call site must not mean inventing
// another raw subject string that can silently drift at runtime.
export const REGISTRY_SUBJECTS = Object.freeze({
  curated: 'api._platform.mfe-registry.entries.curated.v1',
  upsert: 'api._platform.mfe-registry.entries.upsert.v1',
  setEnabled: 'api._platform.mfe-registry.entries.set-enabled.v1',
  audit: 'api._platform.mfe-registry.audit.list.v1',
  publishers: 'api._platform.mfe-registry.publishers.list.v1',
  publisherWrite: 'api._platform.mfe-registry.publishers.write.v1',
})
