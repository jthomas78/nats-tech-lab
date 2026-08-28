/*
  The two version numbers the whole contract hangs off (BR-AS13).

  They are deliberately separate. REGISTRY_SCHEMA_VERSION describes the shape
  of the registry *document* — the operator-curated list served by
  accounts-service; SHELL_API_VERSION describes the runtime surface a plugin's
  code is compiled against. A registry document can gain a field without any
  plugin changing, and a plugin can be rebuilt against a newer shell API
  without the document's shape moving. Collapsing them into one number would
  force every plugin to be re-registered whenever the document changed.

  Extension points carry their own major version in their id
  ({owner}/{region}/v{major}) — see shell/extensions/. That is a third axis on
  purpose: an extension point is owned by whoever renders it, which is not
  always the shell.
*/

export const REGISTRY_SCHEMA_VERSION = 1
export const SHELL_API_VERSION = 1
