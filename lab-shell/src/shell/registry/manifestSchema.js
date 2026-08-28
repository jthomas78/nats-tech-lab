/*
  Plugin manifest validation — the gate BR-AS13 describes.

  Everything here runs on metadata alone, before any remote code is fetched,
  let alone executed. That ordering is the rule: an incompatible plugin has to
  be rejectable without running it, because running it is exactly what we do
  not trust. Nothing in this module imports the loader.

  A rejection is a *status*, not an exception (BR-AS04). One malformed entry in
  the registry document must not take the shell down, so validation returns a
  result object and the caller records `incompatible` against that plugin and
  carries on with the rest.
*/

import { REGISTRY_SCHEMA_VERSION, SHELL_API_VERSION } from '../versions.js'

/* Kebab-case, no leading/trailing/double hyphens. Deliberately narrow: these
   ids end up in route paths, DOM ids, Pinia store keys and log lines, and a
   permissive pattern would let a plugin author choose something that is legal
   in one of those and not the others. */
export const ID_PATTERN = /^[a-z0-9]+(?:-[a-z0-9]+)*$/

/* The five contribution kinds (BR-AS02). This list is closed — an unknown kind
   is a rejection, never a pass-through — because the shell renders each kind
   itself. A kind it does not know is a kind it cannot place. */
export const CONTRIBUTION_KINDS = Object.freeze([
  'route',
  'navigation',
  'extension',
  'shell-control',
  'shell-footer',
])

/* {owner}/{region}/v{major} — see shell/extensions/. Parsed here only far
   enough to reject something that is not an extension-point id at all; whether
   the point *exists* is the extension registry's business, since built-in
   features declare points too and the manifest validator has no view of them. */
export const EXTENSION_POINT_PATTERN = /^([a-z0-9]+(?:-[a-z0-9]+)*)\/([a-z0-9]+(?:-[a-z0-9]+)*)\/v(\d+)$/

const REJECT = (code, message) => ({ ok: false, code, message })

/**
 * Validate one plugin manifest from the registry document.
 *
 * Returns `{ ok: true, plugin }` with a normalized, frozen manifest — every
 * contribution carrying a globally unique `qualifiedId` and a resolved `order`
 * — or `{ ok: false, code, message }`. `code` is a stable machine-readable
 * token; `message` is for the Plugins screen and the log, never for branching.
 */
export function validateManifest(manifest) {
  if (!manifest || typeof manifest !== 'object' || Array.isArray(manifest)) {
    return REJECT('malformed', 'Manifest is not an object')
  }

  const { id } = manifest
  if (typeof id !== 'string' || !ID_PATTERN.test(id)) {
    return REJECT('invalid-id', `Plugin id ${JSON.stringify(id)} is not kebab-case`)
  }

  /* Version checks come before anything structural: an incompatible plugin's
     manifest may legitimately use a shape this shell does not understand, so
     complaining about its contributions would be reporting a consequence
     rather than the cause. */
  if (manifest.schemaVersion !== REGISTRY_SCHEMA_VERSION) {
    return REJECT(
      'unsupported-schema-version',
      `Plugin ${id} declares schemaVersion ${manifest.schemaVersion}; this shell supports ${REGISTRY_SCHEMA_VERSION}`,
    )
  }
  if (manifest.shellApiVersion !== SHELL_API_VERSION) {
    return REJECT(
      'unsupported-shell-api-version',
      `Plugin ${id} declares shellApiVersion ${manifest.shellApiVersion}; this shell provides ${SHELL_API_VERSION}`,
    )
  }

  if (typeof manifest.name !== 'string' || manifest.name.trim() === '') {
    return REJECT('malformed', `Plugin ${id} has no name`)
  }

  const remote = validateRemote(id, manifest.remote)
  if (!remote.ok) return remote

  /* A plugin's routes live under one path segment, but not necessarily its own
     id: the built-in demo catalog is `demo-catalog` and serves `/demos`, and a
     migrated SeaFreight plugin will want `/fleet` rather than
     `/seafreight-flow`. What BR-AS12 needs is that the segment is namespaced,
     unique, and knowable from the URL alone — not that it repeats the id.
     Uniqueness across plugins is checked at index time, where the other
     plugins are in view. */
  const routePrefix = manifest.routePrefix ?? id
  if (typeof routePrefix !== 'string' || !ID_PATTERN.test(routePrefix)) {
    return REJECT('invalid-id', `Plugin ${id} route prefix ${JSON.stringify(routePrefix)} is not kebab-case`)
  }

  /* A plugin may own extension points of its own — the demo catalog owns
     `demo-catalog/details-sidebar/v1`, which other plugins target. Declaring
     them here rather than from `activate()` keeps the metadata-first rule
     intact: the shell knows the full region map before any plugin code runs,
     so a contribution can be placed into a region whose owner has never been
     loaded. The owner segment must be the declaring plugin's own id — a
     plugin cannot open a region in someone else's namespace, or it could
     shadow a host region and capture contributions meant for the shell. */
  const extensionPoints = []
  if (manifest.extensionPoints !== undefined) {
    if (!Array.isArray(manifest.extensionPoints)) {
      return REJECT('malformed', `Plugin ${id} extensionPoints is not an array`)
    }
    for (const raw of manifest.extensionPoints) {
      if (!raw || typeof raw !== 'object') {
        return REJECT('malformed', `Plugin ${id} declares a malformed extension point`)
      }
      const parts = typeof raw.id === 'string' ? EXTENSION_POINT_PATTERN.exec(raw.id) : null
      if (!parts) {
        return REJECT(
          'malformed',
          `Plugin ${id} extension point ${JSON.stringify(raw.id)} is not {owner}/{region}/v{major}`,
        )
      }
      if (parts[1] !== id) {
        return REJECT(
          'extension-point-not-owned',
          `Plugin ${id} declares ${raw.id}, which is owned by ${parts[1]}`,
        )
      }
      const capacity = raw.capacity === undefined ? Infinity : raw.capacity
      if (typeof capacity !== 'number' || !(capacity > 0)) {
        return REJECT(
          'malformed',
          `Plugin ${id} extension point ${raw.id} declares a non-positive capacity`,
        )
      }
      extensionPoints.push(
        Object.freeze({
          id: raw.id,
          capacity,
          description: typeof raw.description === 'string' ? raw.description : '',
        }),
      )
    }
  }

  if (!Array.isArray(manifest.contributions) || manifest.contributions.length === 0) {
    return REJECT('malformed', `Plugin ${id} contributes nothing`)
  }

  const contributions = []
  const seen = new Set()
  for (let index = 0; index < manifest.contributions.length; index += 1) {
    const result = validateContribution(id, manifest.contributions[index], index, routePrefix)
    if (!result.ok) return result
    if (seen.has(result.contribution.qualifiedId)) {
      return REJECT(
        'duplicate-id',
        `Plugin ${id} declares contribution ${result.contribution.id} twice`,
      )
    }
    seen.add(result.contribution.qualifiedId)
    contributions.push(result.contribution)
  }

  return {
    ok: true,
    plugin: Object.freeze({
      id,
      name: manifest.name,
      routePrefix,
      description: typeof manifest.description === 'string' ? manifest.description : '',
      schemaVersion: manifest.schemaVersion,
      shellApiVersion: manifest.shellApiVersion,
      /* Absent means enabled. An operator disables a plugin by setting it
         false in the registry document, which is BR-AS03's point: the shell
         is not rebuilt to turn one off. */
      enabled: manifest.enabled !== false,
      remote: remote.remote,
      extensionPoints: Object.freeze(extensionPoints),
      contributions: Object.freeze(sortContributions(contributions)),
    }),
  }
}

function validateRemote(pluginId, remote) {
  if (!remote || typeof remote !== 'object') {
    return REJECT('malformed', `Plugin ${pluginId} has no remote`)
  }
  /* `kind` is what lets Phase 1a ship with no Module Federation at all: a
     built-in plugin names a module the shell already bundles, and the loader
     resolves it synchronously. Phase 1b adds 'federated' beside it without
     the manifest shape changing. */
  if (remote.kind !== 'builtin' && remote.kind !== 'federated') {
    return REJECT('malformed', `Plugin ${pluginId} declares unknown remote kind ${JSON.stringify(remote.kind)}`)
  }
  if (remote.kind === 'builtin') {
    if (typeof remote.module !== 'string' || remote.module === '') {
      return REJECT('malformed', `Plugin ${pluginId} builtin remote has no module`)
    }
    return { ok: true, remote: Object.freeze({ kind: 'builtin', module: remote.module }) }
  }
  if (typeof remote.url !== 'string' || remote.url === '') {
    return REJECT('malformed', `Plugin ${pluginId} federated remote has no url`)
  }
  if (typeof remote.module !== 'string' || remote.module === '') {
    return REJECT('malformed', `Plugin ${pluginId} federated remote has no module`)
  }
  /* `name` is the Module Federation *container* name — the identifier the
     remote was built under, which the loader must ask for by exactly that
     spelling. It is separate from the plugin id because the two answer to
     different constraints: an id is kebab-case because it lands in URLs and
     store keys, while a container name becomes a global identifier in some
     federation output formats and is conventionally snake_case. Defaulting it
     to the id keeps the common case a single field.

     Phase 1b addition, recorded as a revision of the 1a contract rather than a
     silent edit (task 1b-1). It is optional, so no Phase 1a manifest changes
     meaning: an entry that omits it gets its id with the hyphens an identifier
     cannot carry turned into underscores. */
  const name = remote.name ?? pluginId.replaceAll('-', '_')
  if (typeof name !== 'string' || !/^[A-Za-z_$][\w$]*$/.test(name)) {
    return REJECT(
      'malformed',
      `Plugin ${pluginId} federated remote name ${JSON.stringify(name)} is not a valid container name`,
    )
  }
  return {
    ok: true,
    remote: Object.freeze({ kind: 'federated', url: remote.url, module: remote.module, name }),
  }
}

function validateContribution(pluginId, raw, index, routePrefix) {
  if (!raw || typeof raw !== 'object') {
    return REJECT('malformed', `Plugin ${pluginId} contribution ${index} is not an object`)
  }
  if (!CONTRIBUTION_KINDS.includes(raw.kind)) {
    return REJECT(
      'unknown-contribution-kind',
      `Plugin ${pluginId} contribution ${index} declares unknown kind ${JSON.stringify(raw.kind)}`,
    )
  }
  if (typeof raw.id !== 'string' || !ID_PATTERN.test(raw.id)) {
    return REJECT(
      'invalid-id',
      `Plugin ${pluginId} contribution ${index} id ${JSON.stringify(raw.id)} is not kebab-case`,
    )
  }

  const base = {
    /* The plugin declares a *local* id and the shell qualifies it. Global
       uniqueness (BR-AS06) then follows from plugin ids being unique, rather
       than from every plugin author independently choosing well — the same
       reasoning as the {plugin-id}/{store-id} Pinia rule. */
    id: raw.id,
    qualifiedId: `${pluginId}/${raw.id}`,
    pluginId,
    kind: raw.kind,
    /* Declaration index is the tiebreak, so two contributions with the same
       `order` still render in a defined sequence (BR-AS06). */
    order: Number.isInteger(raw.order) ? raw.order : 0,
    declarationIndex: index,
    permission: typeof raw.permission === 'string' ? raw.permission : null,
  }

  switch (raw.kind) {
    case 'route':
      return validateRoute(pluginId, raw, base, routePrefix)
    case 'navigation':
      return validateNavigation(pluginId, raw, base)
    case 'extension':
      return validateExtension(pluginId, raw, base)
    case 'shell-control':
      return validateShellControl(pluginId, raw, base)
    case 'shell-footer':
      return ok({ ...base, component: componentOf(raw) })
    default:
      /* Unreachable — CONTRIBUTION_KINDS is checked above. Kept so adding a
         kind to that list without handling it here fails loudly. */
      return REJECT('unknown-contribution-kind', `Unhandled kind ${raw.kind}`)
  }
}

function validateRoute(pluginId, raw, base, routePrefix) {
  if (typeof raw.path !== 'string' || !raw.path.startsWith('/')) {
    return REJECT('malformed', `Plugin ${pluginId} route ${raw.id} has no absolute path`)
  }
  /* BR-AS12: every plugin route lives under that plugin's one prefix segment,
     so two plugins can never contend for a path and a path in the address bar
     names its owner without a lookup — which is what makes a deep link into an
     unloaded remote resolvable from the URL alone. Prefix matching is on the
     whole segment: '/demos-archive' is not inside '/demos'. */
  if (raw.path !== `/${routePrefix}` && !raw.path.startsWith(`/${routePrefix}/`)) {
    return REJECT(
      'route-not-namespaced',
      `Plugin ${pluginId} route ${raw.id} declares ${raw.path}, which is outside /${routePrefix}`,
    )
  }
  if (typeof raw.title !== 'string' || raw.title.trim() === '') {
    return REJECT('malformed', `Plugin ${pluginId} route ${raw.id} has no title`)
  }
  return ok({ ...base, path: raw.path, title: raw.title, component: componentOf(raw) })
}

function validateNavigation(pluginId, raw, base) {
  if (typeof raw.label !== 'string' || raw.label.trim() === '') {
    return REJECT('malformed', `Plugin ${pluginId} navigation ${raw.id} has no label`)
  }
  /* A nav entry points at a route contribution by its *local* id, not a path.
     The shell resolves it, so a nav entry naming a route that does not exist
     is caught at index time rather than on click, and a route can be moved
     without its nav entry going stale. */
  if (typeof raw.route !== 'string' || !ID_PATTERN.test(raw.route)) {
    return REJECT('malformed', `Plugin ${pluginId} navigation ${raw.id} names no route`)
  }
  return ok({
    ...base,
    label: raw.label,
    route: raw.route,
    routeQualifiedId: `${pluginId}/${raw.route}`,
    group: typeof raw.group === 'string' ? raw.group : null,
    icon: typeof raw.icon === 'string' ? raw.icon : null,
  })
}

function validateExtension(pluginId, raw, base) {
  const target = parseExtensionPointId(raw.target)
  if (!target) {
    return REJECT(
      'malformed',
      `Plugin ${pluginId} extension ${raw.id} target ${JSON.stringify(raw.target)} is not an extension-point id`,
    )
  }
  return ok({ ...base, target: raw.target, targetParts: target, component: componentOf(raw) })
}

function validateShellControl(pluginId, raw, base) {
  const region = parseExtensionPointId(raw.region)
  if (!region) {
    return REJECT(
      'malformed',
      `Plugin ${pluginId} shell-control ${raw.id} region ${JSON.stringify(raw.region)} is not an extension-point id`,
    )
  }
  /* Route scope is what stops a plugin parking a control in the topbar for the
     whole session. Absent means unscoped — allowed, but the mockups' case is
     the scoped one. Paths are matched by prefix against the current route. */
  const routes = Array.isArray(raw.routes) ? raw.routes.filter((r) => typeof r === 'string') : []
  return ok({ ...base, region: raw.region, regionParts: region, routes: Object.freeze(routes), component: componentOf(raw) })
}

/* The export name inside the plugin's module that renders this contribution.
   Resolved by the loader after activate(), never imported here. */
function componentOf(raw) {
  return typeof raw.component === 'string' && raw.component !== '' ? raw.component : 'default'
}

export function parseExtensionPointId(value) {
  if (typeof value !== 'string') return null
  const match = EXTENSION_POINT_PATTERN.exec(value)
  if (!match) return null
  return Object.freeze({ owner: match[1], region: match[2], major: Number(match[3]) })
}

function ok(contribution) {
  return { ok: true, contribution: Object.freeze(contribution) }
}

function sortContributions(contributions) {
  return [...contributions].sort(
    (a, b) => a.order - b.order || a.declarationIndex - b.declarationIndex,
  )
}

/**
 * Validate the registry document itself — the envelope accounts-service
 * serves. Its own version is checked before the plugins inside it, because a
 * document shape this shell does not understand makes every plugin in it
 * unreadable rather than incompatible.
 */
export function validateRegistryDocument(doc) {
  if (!doc || typeof doc !== 'object' || Array.isArray(doc)) {
    return REJECT('malformed', 'Registry document is not an object')
  }
  if (doc.schemaVersion !== REGISTRY_SCHEMA_VERSION) {
    return REJECT(
      'unsupported-schema-version',
      `Registry document declares schemaVersion ${doc.schemaVersion}; this shell supports ${REGISTRY_SCHEMA_VERSION}`,
    )
  }
  if (!Array.isArray(doc.plugins)) {
    return REJECT('malformed', 'Registry document has no plugins array')
  }
  return { ok: true, plugins: doc.plugins }
}
