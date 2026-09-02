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
  /* The publisher said the plugin is gone (BR-AS54, BR-AS56). Its
     contributions are off screen; its module, if any, is still in memory. Not
     reached through the transition table below — see `withdraw()`. */
  WITHDRAWN: 'withdrawn',
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

/* The statuses a withdrawn plugin may be settled to while it is away. Both are
   outcomes of work that had already started: `loading` is deliberately absent,
   because a plugin returning into `loading` would be waiting on a fetch that
   finished long ago, and `available` is absent because a load that ran is not
   a load that never happened. */
const RETURNABLE = new Set([PLUGIN_STATUS.ACTIVE, PLUGIN_STATUS.FAILED])

export function canTransition(from, to) {
  /* Withdrawal is deliberately outside the table. It comes from the registry
     rather than from the plugin's own progress, it can land on any placed
     status, and the status it lands on is the one a return must go back to. A
     table entry for it would have to be written into every row and would
     still not carry the memory. */
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
    /* Where a withdrawal came from, so a return can go back to it. */
    this.restoreTo = null
  }

  transition(to, { code = null, message = null } = {}) {
    if (this.status === PLUGIN_STATUS.WITHDRAWN) {
      /* The one status nothing may leave by the ordinary route. An import or
         an activation that finishes after the withdrawal lands here, and
         letting it through would put a plugin the publisher retracted back on
         screen. Only `restore()` leaves. */
      throw new Error(`Plugin ${this.id} is withdrawn and cannot transition to ${to}`)
    }
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

  /* The publisher retracted the plugin. Returns false when there was nothing
     placed to take away — a disabled or incompatible plugin was never on
     screen, and saying "withdrawn" about it would replace the reason the
     Plugins screen needs to show. */
  withdraw({ code = 'publisher-withdrawn', message = null } = {}) {
    if (!this.isPlaced) return false
    /* Remembered, not recomputed: a plugin withdrawn while active must come
       back active, so that a return does not call activate() again
       (BR-AS59). */
    this.restoreTo = this.status
    this.status = PLUGIN_STATUS.WITHDRAWN
    this.reasonCode = code
    this.reason = message
    this.history.push(PLUGIN_STATUS.WITHDRAWN)
    return true
  }

  /*
    Work that finished after the withdrawal landed (BR-AS56, BR-AS59).

    A plugin can be withdrawn while its chunk is in flight. The import or the
    activate() then settles against a record that is already `withdrawn`, and
    `transition()` refuses that by design — the plugin must not come back on
    screen. But the OUTCOME still matters: a load that succeeded must come back
    `active` so a return does not call activate() a second time, and one that
    failed must come back `failed` rather than pretending it is ready to use.

    That rule used to be two bare assignments to `restoreTo` from inside the
    loader, reaching past this interface. It was enforced nowhere: a third
    caller could have written `loading`, and the plugin would have returned
    into a state with no fetch behind it. Here it is one call, and only a
    status a withdrawn plugin may legally return to is accepted.

    (`restoreTo` is a plain field rather than a `#private` one because these
    records are wrapped in `reactive()`, and a private field is unreadable
    through a Proxy. Nothing outside this class writes it.)
  */
  settleWhileWithdrawn(status) {
    if (this.status !== PLUGIN_STATUS.WITHDRAWN) return false
    if (!RETURNABLE.has(status)) {
      /* Thrown for the same reason an illegal transition is: a shell bug that
         would otherwise surface much later, as a plugin returning into a state
         nothing put it in. */
      throw new Error(`Plugin ${this.id} cannot be settled to ${status} while withdrawn`)
    }
    this.restoreTo = status
    return true
  }

  /* Back to exactly where it was. */
  restore() {
    if (this.status !== PLUGIN_STATUS.WITHDRAWN) return false
    this.status = this.restoreTo ?? PLUGIN_STATUS.AVAILABLE
    this.restoreTo = null
    this.reasonCode = null
    this.reason = null
    this.history.push(this.status)
    return true
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
