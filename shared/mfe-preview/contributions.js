/*
  Turning a plugin's registry manifest into a list of previewable things.

  The shell decides placement from the same manifest, so reading it here — not
  a hand-written list inside each plugin — is what keeps the preview honest: a
  contribution that is missing from `public/manifest.json` is missing from the
  preview too, and a component the manifest names but the bundle does not
  export shows up as the same "not exported" fault the shell would report.
*/

/* Which kinds render a component, and how the stage should frame them. Kinds
   the shell draws from metadata alone (navigation) render nothing — they are
   listed so the page still accounts for every contribution. */
const KINDS = {
  route: { stage: 'page', label: 'Route' },
  extension: { stage: 'region', label: 'Extension' },
  'shell-control': { stage: 'topbar', label: 'Shell control' },
  'shell-footer': { stage: 'footer', label: 'Shell footer' },
  navigation: { stage: null, label: 'Navigation' },
}

/* `/demos/:id` → { id: 'sample-id' }. A route contribution is mounted by the
   shell's router with its params as props; the harness has no router, so it
   supplies the same shape. Anything better than a placeholder comes from the
   plugin's optional fixtures file. */
function routeParams(path = '') {
  const params = {}
  for (const match of path.matchAll(/:([A-Za-z0-9_]+)/g)) params[match[1]] = `sample-${match[1]}`
  return params
}

/* The context a host would hand a contribution. The shell freezes it on the
   way in (BR-AS02/AS07) and so does this, so a contribution that mutates its
   context fails here the same way it fails there. */
function defaultProps(contribution) {
  switch (contribution.kind) {
    case 'route':
      return routeParams(contribution.path)
    case 'extension':
      return { context: { point: contribution.target, region: contribution.target } }
    case 'shell-control':
      return { context: { region: contribution.region, route: contribution.routes?.[0] ?? null } }
    default:
      return {}
  }
}

export function buildItems({ manifest, components = {}, fixtures = {} }) {
  const contributions = manifest?.contributions ?? []
  const items = contributions.map((contribution) => {
    const meta = KINDS[contribution.kind] ?? { stage: 'page', label: contribution.kind }
    const props = { ...defaultProps(contribution), ...(fixtures[contribution.id] ?? {}) }
    if (props.context) props.context = Object.freeze({ ...props.context })
    const key = contribution.component
    return {
      key: `${contribution.kind}:${contribution.id}`,
      id: contribution.id,
      kind: contribution.kind,
      kindLabel: meta.label,
      stage: key ? meta.stage : null,
      componentKey: key ?? null,
      component: key ? (components[key] ?? null) : null,
      missing: Boolean(key) && !components[key],
      props: Object.freeze(props),
      contribution,
      /* One line of "where the shell would put this", so the preview does not
         quietly imply a contribution is a page when it is a topbar button. */
      placement: placementOf(contribution),
    }
  })

  /* Anything the bundle exports that no contribution names. Usually a stale
     export or a component whose manifest entry was forgotten — either way it
     is invisible in the shell, so the harness says so rather than hiding it. */
  const claimed = new Set(contributions.map((c) => c.component).filter(Boolean))
  const orphans = Object.keys(components)
    .filter((key) => !claimed.has(key))
    .map((key) => ({
      key: `orphan:${key}`,
      id: key,
      kind: 'unclaimed',
      kindLabel: 'Not contributed',
      stage: 'page',
      componentKey: key,
      component: components[key],
      missing: false,
      props: Object.freeze({}),
      contribution: null,
      placement: 'Exported by the bundle, named by no contribution — the shell never renders it.',
    }))

  return [...items, ...orphans]
}

function placementOf(contribution) {
  switch (contribution.kind) {
    case 'route':
      return `Routed at ${contribution.path}`
    case 'extension':
      return `Placed into ${contribution.target}`
    case 'shell-control':
      return `In ${contribution.region}${contribution.routes?.length ? ` on ${contribution.routes.join(', ')}` : ''}`
    case 'shell-footer':
      return 'In the shell footer'
    case 'navigation':
      return `Nav item "${contribution.label}" → route ${contribution.route}`
    default:
      return ''
  }
}
