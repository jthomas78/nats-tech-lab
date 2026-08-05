// Shared parser for notify.{context}.kv.{bucket}.{...key}.changed subjects
// (internal/kvstore.Store.EnableNotify, Phase 23) — used by both
// stores/dictionary.js (context fixed, bucket fixed) and KvInspector.vue
// (context wildcarded, bucket fixed). The key portion itself can contain
// dots (e.g. "ship.SHIP1"), so only the fixed notify/kv/changed tokens are
// peeled off; everything between the bucket and the trailing "changed"
// marker is the key, rejoined.
export function parseKvNotifySubject(subject) {
  const parts = subject.split('.')
  if (parts.length < 6 || parts[0] !== 'notify' || parts[2] !== 'kv' || parts[parts.length - 1] !== 'changed') {
    return null
  }
  return { context: parts[1], bucket: parts[3], key: parts.slice(4, -1).join('.') }
}
