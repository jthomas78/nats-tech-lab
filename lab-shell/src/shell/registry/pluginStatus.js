/*
  The seven observable plugin statuses (BR-AS04, BR-AS08), as one machine.

  They are one machine rather than a set of independent booleans because the
  Plugins screen has to answer "why is this not on screen?" with a single
  word. A plugin that is both `incompatible` and `disabled` is a question the
  UI should never have to render, so the transition table below makes it
  unrepresentable.

  The split that carries the most weight is `available` vs `active`: available
  means the shell has the plugin's metadata and has placed its contributions —
  nav entries exist, routes resolve — while none of its code has been fetched.
  That gap is BR-AS08. Collapsing the two would make lazy loading unobservable,
  and therefore untestable.
*/

export const PLUGIN_STATUS = Object.freeze({
  /* Present in the registry document; nothing checked yet. */
  DISCOVERED: 'discovered',
  /* Rejected on metadata (BR-AS13). Terminal — no retry can help, since the
     shell's own version is what does not match. */
  INCOMPATIBLE: 'incompatible',
  /* Valid and compatible, switched off by the operator (BR-AS03) or withheld
     because the viewer's claims do not permit any of its contributions
     (BR-AS05). Terminal for this session. */
  DISABLED: 'disabled',
  /* Metadata indexed, code not fetched. The steady state before first use. */
  AVAILABLE: 'available',
  LOADING: 'loading',
  /* Module fetched and activate() has returned. */
  ACTIVE: 'active',
  /* Fetch failed, activate() threw, or a contribution threw while rendering
     (BR-AS04). Not terminal: a retry goes back through loading. */
  FAILED: 'failed',
})

const TRANSITIONS = Object.freeze({
  [PLUGIN_STATUS.DISCOVERED]: [
    PLUGIN_STATUS.INCOMPATIBLE,
    PLUGIN_STATUS.DISABLED,
    PLUGIN_STATUS.AVAILABLE,
  ],
  [PLUGIN_STATUS.INCOMPATIBLE]: [],
  [PLUGIN_STATUS.DISABLED]: [],
  /* FAILED is reachable without ever loading: the loader's pre-adapter guards
     (an uncurated remote, no adapter for the remote kind) reject a plugin that
     is placed and permitted but will never run. Routing that through LOADING
     first would claim a fetch that never happened — and the Loading artboard
     would flash a spinner for work nobody started. */
  [PLUGIN_STATUS.AVAILABLE]: [PLUGIN_STATUS.LOADING, PLUGIN_STATUS.FAILED],
  [PLUGIN_STATUS.LOADING]: [PLUGIN_STATUS.ACTIVE, PLUGIN_STATUS.FAILED],
  [PLUGIN_STATUS.ACTIVE]: [PLUGIN_STATUS.FAILED],
  /* Retry is the only reason this is not terminal, and it goes back through
     loading rather than straight to active — the loading state is what the
     user sees while the retry runs. */
  [PLUGIN_STATUS.FAILED]: [PLUGIN_STATUS.LOADING],
})

export function canTransition(from, to) {
  return (TRANSITIONS[from] ?? []).includes(to)
}

/**
 * A plugin's observable state. One record per plugin id, owned by the plugin
 * registry; the Plugins screen reads these and nothing else.
 */
export class PluginStatusRecord {
  constructor(id, { name = id } = {}) {
    this.id = id
    this.name = name
    this.status = PLUGIN_STATUS.DISCOVERED
    /* Why it is where it is. Set on every non-happy transition; the machine
       code (`unsupported-schema-version`, `chunk-load-failed`) is what tests
       and UI branch on, the message is prose for the panel. */
    this.reasonCode = null
    this.reason = null
    this.history = [PLUGIN_STATUS.DISCOVERED]
  }

  transition(to, { code = null, message = null } = {}) {
    if (!canTransition(this.status, to)) {
      /* Thrown, not recorded: an illegal transition is a shell bug, not a
         plugin failure, and swallowing it would hide the bug behind the very
         isolation BR-AS04 asks for. Callers never catch this. */
      throw new Error(`Illegal plugin status transition ${this.status} -> ${to} for ${this.id}`)
    }
    this.status = to
    this.reasonCode = code
    this.reason = message
    this.history.push(to)
    return this
  }

  /* True once the shell has placed the plugin's contributions, whether or not
     its code has loaded — the predicate the nav tree and router use. */
  get isPlaced() {
    return (
      this.status === PLUGIN_STATUS.AVAILABLE ||
      this.status === PLUGIN_STATUS.LOADING ||
      this.status === PLUGIN_STATUS.ACTIVE ||
      this.status === PLUGIN_STATUS.FAILED
    )
  }
}
