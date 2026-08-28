/*
  The two prose columns of the Plugins screen, as pure functions.

  They live here rather than in the view because both are BR-AS04 surfaces:
  every string either side returns is shell-authored. Neither ever reads a
  plugin's `reason` — that is the caught error's own message, and a federation
  failure's message is the remote's URL.
*/

import { failureStage } from '../loader/failureStage.js'
import { PLUGIN_STATUS } from './pluginStatus.js'

const KIND_LABEL = Object.freeze({
  route: ['route', 'routes'],
  navigation: ['nav', 'nav'],
  extension: ['extension', 'extensions'],
  'shell-control': ['control', 'controls'],
  'shell-footer': ['footer item', 'footer items'],
})

/**
 * '2 routes · 1 nav · 1 footer item' — placed contributions, by kind, in a
 * stable order so a plugin's row does not reshuffle between renders.
 */
export function describeContributions(kinds = []) {
  const counts = new Map()
  for (const kind of kinds) counts.set(kind, (counts.get(kind) ?? 0) + 1)

  const parts = []
  for (const kind of Object.keys(KIND_LABEL)) {
    const n = counts.get(kind) ?? 0
    if (n === 0) continue
    const [one, many] = KIND_LABEL[kind]
    parts.push(`${n} ${n === 1 ? one : many}`)
  }
  return parts.join(' · ')
}

/**
 * The Detail column: why the row is in the status it is in, in shell words.
 * @param {{status: string, reasonCode: string|null}} row
 */
export function statusDetail(row) {
  switch (row?.status) {
    case PLUGIN_STATUS.AVAILABLE:
      return 'metadata indexed · no code loaded yet'
    case PLUGIN_STATUS.LOADING:
      return 'first use · fetching the remote'
    case PLUGIN_STATUS.ACTIVE:
      return 'loaded once · activate() returned'
    case PLUGIN_STATUS.DISABLED:
      return row.reasonCode === 'permission-withheld'
        ? 'no contribution the viewer may see'
        : 'enabled: false in the registry'
    case PLUGIN_STATUS.INCOMPATIBLE:
      return `${row.reasonCode ?? 'rejected on metadata'} · no code executed`
    case PLUGIN_STATUS.FAILED:
      return `${failureStage(row.reasonCode)} — ${row.reasonCode ?? 'unknown'}`
    default:
      return 'in the registry · not yet validated'
  }
}
