/*
  The Module Federation loader adapter (BR-AS03, task 1b-1).

  It implements exactly the interface Phase 1a defined — `{ async load(remote) }`
  — and that is the whole point of the seam: everything the shell decides about
  loading (when, at most once, what a failure does to a plugin's status) lives
  in pluginLoader.js and was already proven with no network at all. Federation
  is only asked "fetch this container and give me this module".

  Nothing above this file knows the word "federation": a plugin's manifest says
  `kind: 'federated'` and names a container, a URL and a module, and swapping
  federation for import-maps later is a change to this file plus one line of
  main.js.

  Registration is deliberately *runtime*, not build-time. The host declares no
  remotes in its own build, so adding a plugin to the curated registry is a
  configuration change with no shell rebuild — the claim BR-AS03 makes and
  task 1b-8 measures.
*/

/** Loaded lazily so specs (and a shell with no federated plugin) never pull in
 *  the federation runtime at all. */
async function defaultRuntime() {
  return import('@module-federation/runtime')
}

/**
 * @param {object} [options]
 * @param {{registerRemotes: Function, loadRemote: Function, init?: Function}} [options.runtime]
 *   injected in specs; defaults to @module-federation/runtime
 * @param {string} [options.hostName] the federation identity of the shell itself
 */
export function createFederatedAdapter({ runtime = null, hostName = 'lab_shell' } = {}) {
  /* Container names already registered with the runtime. Re-registering the
     same name with a different entry is how a stale remote would be served
     from a previous URL, so registration is once per name and the loader's own
     "at most once" guarantee sits above it. */
  const registered = new Set()
  let runtimePromise = null

  const resolve = async () => {
    if (!runtimePromise) {
      runtimePromise = (async () => {
        const rt = runtime ?? (await defaultRuntime())
        /* init() is idempotent in the runtime and is what gives the host an
           identity for shared-module negotiation (Vue must be a singleton, or
           a plugin gets its own reactivity system — see the remote's
           vite.config.js). */
        rt.init?.({ name: hostName, remotes: [], shared: {} })
        return rt
      })()
    }
    return runtimePromise
  }

  return {
    async load(remote) {
      const rt = await resolve()
      if (!registered.has(remote.name)) {
        /* `type: 'module'` because every remote in this repo is built by Vite,
           whose output — dev server and `vite build` alike — is an ES module.
           Federation's default is a classic script, which loads the entry and
           then dies on its first `import` statement with a SyntaxError that
           names neither the plugin nor the cause. */
        rt.registerRemotes([{ name: remote.name, entry: remote.url, type: 'module' }])
        registered.add(remote.name)
      }
      /* Federation addresses an exposed module as `container/expose`, where
         the expose key has lost its leading './'. A registry entry may spell
         it either way — the plugin's own vite.config.js writes `./plugin`, so
         an operator copying from it should not get a 404 for the punctuation. */
      const expose = remote.module.replace(/^\.\//, '')
      const module = await rt.loadRemote(`${remote.name}/${expose}`)
      if (!module) {
        throw new Error(`Remote ${remote.name} exposes no module ${remote.module}`)
      }
      return module
    },
  }
}
