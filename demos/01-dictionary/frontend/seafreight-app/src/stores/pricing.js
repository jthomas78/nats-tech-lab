// Pinia store for the "Pricing" tab (Phase 25g/25h). Bootstraps the three
// lists via api.*.pricing.{entity}.list.v1 on connect, same shape as
// stores/port.js's round trip, EXCEPT there is no notify.* subscription
// step: pricing-service publishes no change-notification stream yet
// (BUSINESS_RULES-PRICING.md's Phase 25g note), so this store never stays
// fresh on its own — only a reconnect (tenant/context switch) re-reads.
//
// register*/toggle*Active are store actions (not plain api.js calls from a
// component) because they mutate the list arrays below — mirrors
// stores/port.js's addShippingPort, the one existing action with the same
// shape. Everything else in the manual-entry UX (create-draft, add-range/
// add-entry, publish, rollback, versions/active detail) is per-row detail
// that never changes these lists, so FeeScalePanel/RateSheetPanel/
// FixedRatePanel call api.js directly for those and keep their own local
// draft/expansion state — same split ShipsAtPortPanel/TerminalPanel already
// use for arrive/depart/load/unload.
import { defineStore } from 'pinia'

import {
  indexDieselPrice as apiIndexDieselPrice,
  listDieselPrices,
  listFeeScales,
  listFixedRates,
  listRateSheets,
  registerFeeScale as apiRegisterFeeScale,
  registerFixedRate as apiRegisterFixedRate,
  registerRateSheet as apiRegisterRateSheet,
} from '../api'

export const usePricingStore = defineStore('pricing', {
  state: () => ({
    context: '',
    feeScales: [],
    rateSheets: [],
    fixedRates: [],
    dieselPrices: [],
    connected: false,
    loading: false, // true from connect() until its bootstrap reads land — same BR-029 rationale as stores/port.js
  }),

  actions: {
    setContext(context) {
      if (context === this.context) return
      this.context = context
      this.connect()
    },

    async connect() {
      this.loading = true
      this.feeScales = []
      this.rateSheets = []
      this.fixedRates = []
      this.dieselPrices = []

      const bootstrap = Promise.allSettled([
        listFeeScales(this.context)
          .then((values) => {
            this.feeScales = values ?? []
          })
          .catch(() => {}),

        listRateSheets(this.context)
          .then((values) => {
            this.rateSheets = values ?? []
          })
          .catch(() => {}),

        listFixedRates(this.context)
          .then((values) => {
            this.fixedRates = values ?? []
          })
          .catch(() => {}),

        listDieselPrices(this.context)
          .then((values) => {
            this.dieselPrices = values ?? []
          })
          .catch(() => {}),
      ])

      this.connected = true
      bootstrap.finally(() => {
        this.loading = false
      })
    },

    disconnect() {
      this.connected = false
    },

    // Register upserts (pricing-service's Register is ON CONFLICT DO
    // UPDATE), so this also covers "edit an existing entry's fields" and
    // "toggle active" (see toggleRateSheetActive/toggleFixedRateActive
    // below) — there is no separate update endpoint to call instead.
    async registerFeeScale(name) {
      const feeScale = await apiRegisterFeeScale(this.context, name)
      this.upsertByName(this.feeScales, feeScale)
      return feeScale
    },

    async registerRateSheet(input) {
      const rateSheet = await apiRegisterRateSheet(this.context, input)
      this.upsertByName(this.rateSheets, rateSheet)
      return rateSheet
    },

    async registerFixedRate(input) {
      const fixedRate = await apiRegisterFixedRate(this.context, input)
      this.upsertByName(this.fixedRates, fixedRate)
      return fixedRate
    },

    // IndexDieselPrice upserts on (context, activeDate) server-side (BR-P18),
    // so this replaces a same-date row in place rather than duplicating it —
    // same upsertByName rationale as registerRateSheet, keyed on activeDate.
    async indexDieselPrice(price) {
      await apiIndexDieselPrice(this.context, price)
      const index = this.dieselPrices.findIndex((existing) => existing.activeDate === price.activeDate)
      if (index === -1) {
        this.dieselPrices.push(price)
      } else {
        this.dieselPrices[index] = price
      }
      this.dieselPrices.sort((a, b) => a.activeDate.localeCompare(b.activeDate))
      return price
    },

    toggleRateSheetActive(rateSheet) {
      return this.registerRateSheet({
        name: rateSheet.name,
        customerKey: rateSheet.customerKey,
        type: rateSheet.type,
        active: !rateSheet.active,
      })
    },

    toggleFixedRateActive(fixedRate) {
      return this.registerFixedRate({
        name: fixedRate.name,
        customerKey: fixedRate.customerKey,
        routeKey: fixedRate.routeKey,
        active: !fixedRate.active,
      })
    },

    // Replaces the row with a matching `name`, or appends — Register's
    // upsert means a re-register of a known name must update in place
    // rather than duplicate the row.
    upsertByName(list, entry) {
      const index = list.findIndex((existing) => existing.name === entry.name)
      if (index === -1) {
        list.push(entry)
      } else {
        list[index] = entry
      }
    },
  },
})
