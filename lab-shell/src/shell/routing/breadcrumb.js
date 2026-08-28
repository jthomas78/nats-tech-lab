/*
  The topbar trail: where the current screen sits, and who owns it.

  Two segments, never more. The shell's route table is one level deep by
  construction (a plugin contributes routes, not a hierarchy), so a trail that
  grew arms would be inventing structure the router does not have. The owner
  segment is the honest part: on a plugin screen it names the plugin, which is
  the only place in the chrome that says whose code is rendering.

  The owner is the plugin's *display name*, resolved by the caller from the
  status record — never the plugin id and never the remote URL (BR-AS04).
*/

export const SHELL_OWNER = 'Shell'

/**
 * @param {{pluginId?: string, title?: string}} meta the resolved route's meta
 * @param {(pluginId: string) => string|null} resolveOwner display name lookup
 * @returns {{owner: string, leaf: string}} `leaf` is '' on an unnamed route
 */
export function breadcrumbTrail(meta, resolveOwner = () => null) {
  const leaf = meta?.title ?? ''
  const pluginId = meta?.pluginId
  if (!pluginId) return { owner: SHELL_OWNER, leaf }

  /* A plugin whose record is missing still gets a trail. Falling back to the
     id here is safe — an id is curated registry content, not a URL — and a
     blank owner would read as a shell screen, which is the one thing this
     segment exists to distinguish. */
  return { owner: resolveOwner(pluginId) || pluginId, leaf }
}
