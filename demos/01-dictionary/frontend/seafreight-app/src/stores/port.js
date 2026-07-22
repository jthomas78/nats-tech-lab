// Pinia store = the browser-side projected read model, fed by two SSE
// channels: /api/watch/{context} (ship states, Shape A bucket) and
// /api/watch-terminal/{context} (container states + meta.* lookup sets).
// The ship manifest is a client-side join: containers with onShipID == shipID
// — the same join the backend terminal queries and Shape C perform.
import { defineStore } from 'pinia'

import { getPorts, registerPort, watchTerminalUrl, watchUrl } from '../api'

export const CONTEXTS = ['global', 'atlantic-fleet', 'pacific-fleet']

export const usePortStore = defineStore('port', {
  state: () => ({
    context: CONTEXTS[0],
    port: '', // selected port — scopes every panel
    knownPorts: [],
    knownContainers: [],
    ships: {},      // shipID → ShipState
    containers: {}, // containerID → ContainerState
    shipsConnected: false,
    terminalConnected: false,
    _shipSource: null,
    _terminalSource: null,
  }),

  getters: {
    connected: (state) => state.shipsConnected && state.terminalConnected,

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

    setPort(port) {
      this.port = port
    },

    // Registers a new port in the Postgres-backed ports registry (BR-017/
    // BR-018) via POST /api/ports, then makes it active. Unlike ship/container
    // commands this is a direct write, not an event — ports are reference
    // data, not an event-sourced aggregate.
    async addShippingPort(port) {
      const trimmed = port.trim()
      if (!trimmed) return
      await registerPort(this.context, trimmed)
      this.mergeKnownPorts([trimmed])
      this.port = trimmed
    },

    connect() {
      this.disconnect()
      this.ships = {}
      this.containers = {}
      this.knownPorts = []
      this.knownContainers = []

      // Seed the port selector from the Postgres-backed ports registry
      // before the SSE streams open; live META events keep it current
      // afterwards (registering a port also merges it in immediately).
      getPorts(this.context)
        .then((res) => {
          this.mergeKnownPorts(res?.values ?? [])
          if (!this.port && this.knownPorts.length > 0) {
            this.port = this.knownPorts[0]
          }
        })
        .catch(() => {})

      const ships = new EventSource(watchUrl(this.context))
      this._shipSource = ships
      ships.onopen = () => { this.shipsConnected = true }
      ships.onerror = () => { this.shipsConnected = false }
      ships.onmessage = (msg) => { this.applyShipEvent(JSON.parse(msg.data)) }

      const terminal = new EventSource(watchTerminalUrl(this.context))
      this._terminalSource = terminal
      terminal.onopen = () => { this.terminalConnected = true }
      terminal.onerror = () => { this.terminalConnected = false }
      terminal.onmessage = (msg) => { this.applyTerminalEvent(JSON.parse(msg.data)) }
    },

    disconnect() {
      this._shipSource?.close()
      this._shipSource = null
      this._terminalSource?.close()
      this._terminalSource = null
      this.shipsConnected = false
      this.terminalConnected = false
    },

    mergeKnownPorts(ports) {
      const merged = new Set([...this.knownPorts, ...ports])
      this.knownPorts = [...merged].sort()
    },

    // /api/watch delivers both Shape A and Shape B bucket changes; the port
    // view only needs one copy of each ship, so it follows Shape A.
    applyShipEvent(event) {
      if (event.shape !== 'A') return
      if (event.op === 'PUT' && event.value?.shipID) {
        this.ships[event.value.shipID] = event.value
      } else if (event.key?.startsWith('ship.')) {
        delete this.ships[event.key.slice('ship.'.length)]
      }
    },

    applyTerminalEvent(event) {
      if (event.shape === 'CONTAINER') {
        if (event.op === 'PUT' && event.value?.containerID) {
          this.containers[event.value.containerID] = event.value
        } else if (event.key?.startsWith('container.')) {
          delete this.containers[event.key.slice('container.'.length)]
        }
        return
      }
      if (event.shape === 'META' && event.op === 'PUT' && event.key === 'known-containers') {
        this.knownContainers = event.value ?? []
      }
    },
  },
})
