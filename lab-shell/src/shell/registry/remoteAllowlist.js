/*
  Curation's second gate.

  A domain service must not be able to advertise its own frontend and have the
  browser run it: the only URLs the loader will fetch are the ones an operator
  put in the curated document (decision 21). The registry read produces the
  manifests; this produces the set of URLs those manifests are allowed to
  name, and the loader refuses anything outside it — see loader/.

  Two independent gates on purpose. "The loader only ever gets called with
  registry records" is an argument about today's call graph, not a property
  that can be asserted.

  It lives in its own file rather than beside a transport because it belongs
  to neither: whichever adapter reads the catalogue, this is the same set.
*/

/**
 * The set of remote entry URLs the operator curated. Built from validated
 * manifests only — a manifest rejected on metadata contributes no URL, so an
 * incompatible plugin cannot smuggle one in.
 */
export class RemoteAllowlist {
  #urls = new Set()

  add(plugin) {
    if (plugin?.remote?.kind === 'federated') this.#urls.add(plugin.remote.url)
    return this
  }

  allows(url) {
    return this.#urls.has(url)
  }

  get size() {
    return this.#urls.size
  }
}
