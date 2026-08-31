/*
  Host-owned extension points (BR-AS07).

  The owner of a screen declares where it will accept contributions, how many
  it will render, and what contextual data contributors get. "Owner" is not a
  synonym for "shell": the demo catalog is a federated feature that owns
  `demo-catalog/details-sidebar/v1`, and a remote plugin contributing into it
  is the cross-owner case Phase 1b has to demonstrate. That is why the id
  carries the owner rather than the registry inferring it.

  Capacity is a layout decision, so it belongs to whoever draws the layout. A
  sidebar that fits two panels renders two; the third is refused *at index
  time*, with a reason, rather than being rendered into a broken column or
  silently dropped at paint. Refusing late would make the outcome depend on
  render order, which no one can reason about.

  Context passed to contributors is frozen. A contributor mutating the host's
  state through the object it was handed is the shape of coupling this whole
  design exists to prevent — it would be BR-AS02 violated through a legal API.
*/

import { parseExtensionPointId } from '../registry/manifestSchema.js'

export class ExtensionPointRegistry {
  /** @type {Map<string, object>} keyed by the full versioned id */
  #points = new Map()

  /**
   * @param {object} declaration
   * @param {string} declaration.id `{owner}/{region}/v{major}`
   * @param {number} [declaration.capacity] Infinity when the region grows
   * @param {string} [declaration.description] shown on the Plugins screen
   */
  declare({ id, capacity = Infinity, description = '' }) {
    const parts = parseExtensionPointId(id)
    if (!parts) throw new Error(`Extension point id ${JSON.stringify(id)} is malformed`)
    /* A duplicate declaration is a shell/feature bug — two owners believing
       they own one region — and there is no sane resolution, so it throws
       rather than last-write-wins. */
    if (this.#points.has(id)) throw new Error(`Extension point ${id} is already declared`)
    if (!(capacity > 0)) throw new Error(`Extension point ${id} declares capacity ${capacity}`)

    const point = Object.freeze({ id, ...parts, capacity, description })
    this.#points.set(id, point)
    return point
  }

  get(id) {
    return this.#points.get(id) ?? null
  }

  has(id) {
    return this.#points.has(id)
  }

  get ids() {
    return [...this.#points.keys()]
  }

  /**
   * Can this contribution target this point? Returns a reason code rather
   * than a boolean, because the Plugins screen has to explain the refusal and
   * "unknown point" and "wrong major version" are different conversations —
   * one is a typo or a missing feature, the other is a plugin that needs
   * rebuilding.
   *
   * @returns {{ok: true, point: object} | {ok: false, code: string, message: string}}
   */
  accepts(targetId, { placedCount = 0 } = {}) {
    const parts = parseExtensionPointId(targetId)
    if (!parts) {
      return { ok: false, code: 'malformed', message: `${targetId} is not an extension-point id` }
    }
    const point = this.#points.get(targetId)
    if (!point) {
      /* Distinguish "this region does not exist" from "this region exists at
         another major". The second means the plugin was built against a
         contract that has since moved, which is BR-AS13's case and is fixed by
         rebuilding the plugin, not by the host declaring something new. */
      const otherMajor = this.ids.some((id) => {
        const p = parseExtensionPointId(id)
        return p.owner === parts.owner && p.region === parts.region
      })
      return otherMajor
        ? {
            ok: false,
            code: 'unsupported-extension-point-version',
            message: `${parts.owner}/${parts.region} is not available at v${parts.major}`,
          }
        : {
            ok: false,
            code: 'unknown-extension-point',
            message: `No feature declares the extension point ${targetId}`,
          }
    }
    if (placedCount >= point.capacity) {
      return {
        ok: false,
        code: 'extension-point-full',
        message: `${targetId} renders at most ${point.capacity} contribution(s)`,
      }
    }
    return { ok: true, point }
  }
}

/**
 * The context a host hands its contributors. Frozen on the way out, every
 * time, so a host cannot accidentally pass a live reference to its own state.
 */
export function readonlyContext(values) {
  return Object.freeze({ ...values })
}

/* The three regions the shell itself owns (Design decision: the shell owns the
   frame — BR-AS09). Everything else is owned by a federated plugin.
   Capacities come from the Phase 1 mockups, reviewed at 1920x1080. */
export function declareShellExtensionPoints(registry = new ExtensionPointRegistry()) {
  registry.declare({
    id: 'shell/topbar-controls/v1',
    capacity: 3,
    description: 'Route-scoped controls in the topbar, right of the breadcrumb',
  })
  registry.declare({
    id: 'shell/footer/v1',
    capacity: 6,
    description: 'Status items in the shell footer bar',
  })
  registry.declare({
    id: 'shell/home-main/v1',
    capacity: 4,
    description: 'Panels on the shell home screen',
  })
  return registry
}
