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
  getRefdataContexts,
  knownContainers as fetchKnownContainers,
  listContainers,
  listShips,
  notifySubject,
  registerPort,
} from '../api'
import { useNatsConnection } from '../nats/useNatsConnection'

// CONTEXTS is now only the offline/error fallback (Phase 16f) — the real
// list is fetched per-tenant via loadContexts()/GET /api/refdata/contexts.
// Kept as a literal, not deleted, so the fleet-context dropdown still shows
// something sensible if that fetch never succeeds (e.g. refdata-service is
// down): a demo shouldn't show an empty dropdown just because a read-only
// convenience list failed.
export const CONTEXTS = ['acme', 'acme-atlantic-fleet', 'acme-pacific-fleet']

export const usePortStore = defineStore('port', {
  state: () => ({
    context: CONTEXTS[0],
    availableContexts: [...CONTEXTS], // Phase 16f: replaced by loadContexts() once the tenant is known
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

    // Fetches this tenant's real context list (Phase 16f), replacing the
    // static CONTEXTS fallback. Called from stores/tenant.js on init/switch
    // — not from connect() below, so a plain fleet-context change doesn't
    // needlessly refetch the very list it's picking from. Falls back to the
    // existing (initially CONTEXTS) list on error rather than throwing —
    // this is a convenience list for a dropdown, not a required resource.
    //
    // Filters out "_"-reserved contexts (e.g. "_platform"): the fetched list
    // is refdata-service's context tree, which includes the shared platform
    // root every tenant inherits standards from — meaningful for reference-
    // data reads, but no ship or container ever belongs to it. Offering it
    // as a fleet-context choice here would let a click spin up real (if
    // empty) KV buckets for a context with no shipping domain meaning,
    // burning the tenant's limited JetStream stream quota for nothing.
    async loadContexts() {
      try {
        const contexts = (await getRefdataContexts()).filter((c) => !c.startsWith('_'))
        if (contexts.length > 0) {
          this.availableContexts = contexts
          if (!this.availableContexts.includes(this.context)) {
            this.context = this.availableContexts[0]
          }
        }
      } catch {
        // keep whatever availableContexts already held (CONTEXTS on first load)
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
        getPorts(this.context)
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
