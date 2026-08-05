// Pinia store = the browser-side projected read model (Phase 15d). Bootstraps
// via api.*.shipping.{entity}.list.v1 calls on connect, then stays fresh via
// notify.*.shipping.{entity}.changed subscriptions over the single NATS
// WebSocket connection (nats/useNatsConnection.js) — replacing the two
// SSE channels (/api/watch/{context}, /api/watch-terminal/{context}) the
// pre-Phase-15 store used. The ship manifest is still a client-side join:
// containers with onShipID == shipID — the same join the backend terminal
// queries and Shape C perform.
import { defineStore } from 'pinia'

import {
  getPorts,
  getBusinessUnits,
  knownContainers as fetchKnownContainers,
  listContainers,
  listShips,
  notifySubject,
  registerPort,
} from '../api'
import { useNatsConnection } from '../nats/useNatsConnection'

export const usePortStore = defineStore('port', {
  state: () => ({
    context: '',
    availableContexts: [], // Phase 22: populated by loadContexts(); empty until fetched
    port: '', // selected port — scopes every panel
    knownPorts: [],
    knownContainers: [],
    ships: {},      // shipID → ShipState
    containers: {}, // containerID → ContainerState
    connected: false,
    loading: false, // true from connect() until its bootstrap reads land — lets a panel show a loading state instead of misreading the ships={}/containers={} reset below as "this tenant/context truly has none"
    _unsubscribers: [],
  }),

  getters: {
    // Ships currently docked at the selected port.
    dockedShips: (state) =>
      Object.values(state.ships)
        .filter((s) => s.currentPort === state.port)
        .sort((a, b) => a.shipID.localeCompare(b.shipID)),

    // Every ship in the fleet, regardless of port or status. Port-independent
    // (fleet-scoped only — the store holds one context's ships), so the Fleet
    // panel does not gate on the selected port. Docked-vs-in-transit is derived
    // from currentPort ('' == at sea); the Fleet panel filters on it client-side.
    allShips: (state) =>
      Object.values(state.ships).sort((a, b) => a.shipID.localeCompare(b.shipID)),

    // Containers in the terminal yard at the selected port (terminalPort
    // match — never branches on status).
    yardContainers: (state) =>
      Object.values(state.containers)
        .filter((c) => c.terminalPort === state.port)
        .sort((a, b) => a.containerID.localeCompare(b.containerID)),

    // manifestFor(shipID) — the onShipID join.
    manifestFor: (state) => (shipID) =>
      Object.values(state.containers)
        .filter((c) => c.onShipID === shipID)
        .sort((a, b) => a.containerID.localeCompare(b.containerID)),
  },

  actions: {
    setContext(context) {
      if (context === this.context) return
      this.context = context
      this.connect()
    },

    // Fetches this tenant's BU list from accounts-service (Phase 22). Called
    // from stores/tenant.js on init/switch with the newly-active tenant name.
    // Only visible BUs are returned; a failed fetch leaves availableContexts
    // empty with no stale fallback. `_default_bu` (BR-AC16 — auto-created,
    // visible by default, silently covers an account with zero registered
    // BUs) is filtered out of the *selectable* list here, same convention
    // frontend/admin's dictionary.js already applies: an account with no
    // real BU registered against it should read as "nothing to choose
    // between," not offer the reserved placeholder as if it were a normal
    // option. When that leaves the list empty, the store still targets
    // `_default_bu` internally (ships/ports/etc. genuinely live there) —
    // App.vue's Select just renders it as `<default>` and disables itself
    // rather than showing the literal reserved name.
    async loadContexts(tenant) {
      try {
        const contexts = (await getBusinessUnits(tenant)).filter((c) => !c.startsWith('_'))
        this.availableContexts = contexts
        if (contexts.length > 0 && !contexts.includes(this.context)) {
          this.context = contexts[0]
        } else if (contexts.length === 0) {
          this.context = '_default_bu'
        }
      } catch {
        this.availableContexts = []
      }
    },

    setPort(port) {
      this.port = port
    },

    // Registers a new port in the Postgres-backed ports registry (BR-017/
    // BR-018) via api.*.shipping.port.register.v1, then makes it active.
    // Unlike ship/container commands this is a direct write, not an event —
    // ports are reference data, not an event-sourced aggregate.
    async addShippingPort(port) {
      const trimmed = port.trim()
      if (!trimmed) return
      await registerPort(this.context, trimmed)
      this.mergeKnownPorts([trimmed])
      this.port = trimmed
    },

    // Bootstraps this context's state via api.*.list.v1 calls (replacing the
    // SSE initial-snapshot the pre-Phase-15 store relied on), then subscribes
    // to notify.* for live updates. Requires the NATS WebSocket connection
    // to already be open (useTenantStore.init()/setTenant() — this only
    // (re)scopes which fleet CONTEXT this store watches within whichever
    // tenant is currently connected, same role setContext/a tenant switch
    // always played).
    async connect() {
      this.disconnect()
      this.loading = true
      this.ships = {}
      this.containers = {}
      this.knownPorts = []
      this.knownContainers = []

      const { subscribe } = useNatsConnection()

      // Live notify.* subscriptions first, then the bootstrap reads — so a
      // change published in the narrow gap between the two isn't missed
      // (same "subscribe before draining backlog" ordering the old SSE
      // handlers used server-side).
      this._unsubscribers = [
        subscribe(notifySubject(this.context, 'ship'), (ship) => {
          if (ship?.shipID) this.ships[ship.shipID] = ship
        }),
        subscribe(notifySubject(this.context, 'container'), (container) => {
          if (container?.containerID) this.containers[container.containerID] = container
        }),
        subscribe(notifySubject(this.context, 'meta'), (values) => {
          this.knownContainers = values ?? []
        }),
        subscribe(notifySubject(this.context, 'port'), (values) => {
          this.mergeKnownPorts(values ?? [])
        }),
      ]

      const bootstrap = Promise.allSettled([
        // TODO(tenant-scoping): ports should be scoped to tenant/account, not
        // BU context. For now, always read from _default_bu where the seeded
        // ports live. Remove this hack when ports are re-keyed to tenant.
        getPorts('_default_bu')
          .then((ports) => {
            this.mergeKnownPorts(ports ?? [])
            if (!this.port && this.knownPorts.length > 0) {
              this.port = this.knownPorts[0]
            }
          })
          .catch(() => {}),

        listShips(this.context)
          .then((ships) => {
            for (const s of ships ?? []) this.ships[s.shipID] = s
          })
          .catch(() => {}),

        listContainers(this.context)
          .then((containers) => {
            for (const c of containers ?? []) this.containers[c.containerID] = c
          })
          .catch(() => {}),

        fetchKnownContainers(this.context)
          .then((values) => {
            this.knownContainers = values ?? []
          })
          .catch(() => {}),
      ])

      this.connected = true
      // Not awaited above (connect() returns as soon as the bootstrap reads
      // are in flight, same as before) — this just closes the loading
      // window a panel can key off of once they land, covering the empty
      // ships={}/containers={} reset above so a tenant/context switch reads
      // as "loading", not "empty".
      bootstrap.finally(() => {
        this.loading = false
      })
    },

    disconnect() {
      for (const unsubscribe of this._unsubscribers) unsubscribe()
      this._unsubscribers = []
      this.connected = false
    },

    mergeKnownPorts(ports) {
      const merged = new Set([...this.knownPorts, ...ports])
      this.knownPorts = [...merged].sort()
    },
  },
})
