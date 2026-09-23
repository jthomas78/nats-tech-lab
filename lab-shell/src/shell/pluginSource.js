/*
  BR-AS75 — the catalogue has exactly one source, chosen once.

  `plugin-source` says how the catalogue is OBTAINED, not where the shell runs.
  That distinction is the whole reason this file exists rather than a
  `import.meta.env.DEV` check at the call site: `build` mode is a legitimate
  deployment (Phase 16, decision 2, amended at the gate), so a development
  build is neither necessary nor sufficient for it.

  Resolved once, memoised, and read before any catalogue read. There is no
  per-plugin variant and no way to ask for one: the resolver takes no plugin
  and returns one value for the process.

  Unset means `registry`. That is today's behaviour said out loud rather than a
  second mode — the variable selects, it does not enable — so an existing
  deployment that declares nothing keeps the boot order, subjects and grants it
  already had. A value that is neither known name FAILS BOOT rather than
  falling back, because a typo silently serving the other catalogue is the one
  outcome this rule exists to prevent.
*/

export const PLUGIN_SOURCE_BUILD = 'build'
export const PLUGIN_SOURCE_REGISTRY = 'registry'

const KNOWN = [PLUGIN_SOURCE_BUILD, PLUGIN_SOURCE_REGISTRY]

/** The environment variable that selects the source. Nothing else may. */
export const PLUGIN_SOURCE_VAR = 'VITE_PLUGIN_SOURCE'

/* Pure, and injectable, so a spec can assert every value without touching the
   memo. The host never calls this one — it calls `pluginSource()`. */
export function readPluginSource(env) {
  const declared = env?.[PLUGIN_SOURCE_VAR]
  if (declared === undefined || declared === null) return PLUGIN_SOURCE_REGISTRY
  const value = String(declared).trim()
  if (value === '') return PLUGIN_SOURCE_REGISTRY
  if (!KNOWN.includes(value)) {
    throw new Error(
      `${PLUGIN_SOURCE_VAR} must be one of ${KNOWN.join(', ')}; got ${JSON.stringify(declared)}`,
    )
  }
  return value
}

let resolved = null

/**
 * The one answer, for this shell, for its whole life.
 * @returns {'build'|'registry'}
 */
export function pluginSource(env = import.meta.env) {
  if (resolved === null) resolved = readPluginSource(env)
  return resolved
}

/* Specs only. The memo is the point of the file, so it has to be clearable
   somewhere — and doing it through a named export keeps that seam visible
   rather than hidden behind a module reset. */
export function resetPluginSourceForTests() {
  resolved = null
}
