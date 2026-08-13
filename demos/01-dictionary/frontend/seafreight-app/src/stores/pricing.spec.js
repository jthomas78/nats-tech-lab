import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { usePricingStore } from './pricing.js'

// Mirrors stores/port.spec.js's conventions: mock '../api' so connect()'s
// bootstrap reads and the register actions never touch a real NATS
// connection.
vi.mock('../api', () => ({
  listFeeScales: vi.fn(),
  listRateSheets: vi.fn(),
  listFixedRates: vi.fn(),
  listDieselPrices: vi.fn(),
  registerFeeScale: vi.fn(),
  registerRateSheet: vi.fn(),
  registerFixedRate: vi.fn(),
  indexDieselPrice: vi.fn(),
}))

import {
  indexDieselPrice,
  listDieselPrices,
  listFeeScales,
  listFixedRates,
  listRateSheets,
  registerFeeScale,
  registerFixedRate,
  registerRateSheet,
} from '../api'

describe('usePricingStore.connect() loading state (Phase 25g, mirrors BR-029)', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
  })

  it('sets loading true and clears the previous context\'s lists synchronously, then clears loading once the bootstrap reads land', async () => {
    let resolveFeeScales
    listFeeScales.mockReturnValue(new Promise((resolve) => { resolveFeeScales = resolve }))
    listRateSheets.mockResolvedValue([])
    listFixedRates.mockResolvedValue([])
    listDieselPrices.mockResolvedValue([])

    const store = usePricingStore()
    store.feeScales = [{ name: 'stale-from-old-context' }]

    await store.connect()

    expect(store.loading).toBe(true)
    expect(store.feeScales).toEqual([])

    resolveFeeScales([{ name: 'standard' }])
    await vi.waitFor(() => expect(store.loading).toBe(false))

    expect(store.feeScales).toEqual([{ name: 'standard' }])
  })

  it('clears loading even when a bootstrap read fails', async () => {
    listFeeScales.mockRejectedValue(new Error('boom'))
    listRateSheets.mockResolvedValue([])
    listFixedRates.mockResolvedValue([])
    listDieselPrices.mockResolvedValue([])

    const store = usePricingStore()
    await store.connect()

    await vi.waitFor(() => expect(store.loading).toBe(false))
    expect(store.feeScales).toEqual([])
  })
})

describe('usePricingStore register actions (Phase 25h)', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
  })

  it('registerFeeScale appends a newly-registered fee scale', async () => {
    registerFeeScale.mockResolvedValue({ name: 'standard', context: 'acme-pacific-fleet', deleted: false })

    const store = usePricingStore()
    store.context = 'acme-pacific-fleet'
    await store.registerFeeScale('standard')

    expect(registerFeeScale).toHaveBeenCalledWith('acme-pacific-fleet', 'standard')
    expect(store.feeScales).toEqual([{ name: 'standard', context: 'acme-pacific-fleet', deleted: false }])
  })

  it('registerRateSheet updates an existing row in place rather than duplicating it', async () => {
    const store = usePricingStore()
    store.feeScales = []
    store.rateSheets = [{ name: 'acme-standard', customerKey: 'cust-1', type: 'normal', active: true }]

    registerRateSheet.mockResolvedValue({ name: 'acme-standard', customerKey: 'cust-1', type: 'normal', active: false })
    await store.registerRateSheet({ name: 'acme-standard', customerKey: 'cust-1', type: 'normal', active: false })

    expect(store.rateSheets).toHaveLength(1)
    expect(store.rateSheets[0].active).toBe(false)
  })

  it('toggleRateSheetActive re-registers with the active flag flipped', async () => {
    const store = usePricingStore()
    registerRateSheet.mockResolvedValue({ name: 'acme-standard', customerKey: 'cust-1', type: 'normal', active: false })

    await store.toggleRateSheetActive({ name: 'acme-standard', customerKey: 'cust-1', type: 'normal', active: true })

    expect(registerRateSheet).toHaveBeenCalledWith(store.context, {
      name: 'acme-standard',
      customerKey: 'cust-1',
      type: 'normal',
      active: false,
    })
  })

  it('toggleFixedRateActive re-registers with the active flag flipped', async () => {
    const store = usePricingStore()
    registerFixedRate.mockResolvedValue({ name: 'acme-la-nyc', customerKey: 'cust-1', routeKey: 'la-nyc', active: false })

    await store.toggleFixedRateActive({ name: 'acme-la-nyc', customerKey: 'cust-1', routeKey: 'la-nyc', active: true })

    expect(registerFixedRate).toHaveBeenCalledWith(store.context, {
      name: 'acme-la-nyc',
      customerKey: 'cust-1',
      routeKey: 'la-nyc',
      active: false,
    })
  })
})

describe('usePricingStore.indexDieselPrice (Phase 25i, BR-P18)', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
  })

  it('appends a newly-indexed diesel price, sorted by activeDate', async () => {
    indexDieselPrice.mockResolvedValue(undefined)

    const store = usePricingStore()
    store.dieselPrices = [{ activeDate: '2026-01-01T00:00:00.000Z', coastalCents: 400, inlandCents: 380 }]

    await store.indexDieselPrice({ activeDate: '2026-06-01T00:00:00.000Z', coastalCents: 450, inlandCents: 420 })

    expect(indexDieselPrice).toHaveBeenCalledWith(store.context, { activeDate: '2026-06-01T00:00:00.000Z', coastalCents: 450, inlandCents: 420 })
    expect(store.dieselPrices).toEqual([
      { activeDate: '2026-01-01T00:00:00.000Z', coastalCents: 400, inlandCents: 380 },
      { activeDate: '2026-06-01T00:00:00.000Z', coastalCents: 450, inlandCents: 420 },
    ])
  })

  it('re-indexing an existing activeDate updates the row in place rather than duplicating it', async () => {
    indexDieselPrice.mockResolvedValue(undefined)

    const store = usePricingStore()
    store.dieselPrices = [{ activeDate: '2026-01-01T00:00:00.000Z', coastalCents: 400, inlandCents: 380 }]

    await store.indexDieselPrice({ activeDate: '2026-01-01T00:00:00.000Z', coastalCents: 410, inlandCents: 390 })

    expect(store.dieselPrices).toEqual([{ activeDate: '2026-01-01T00:00:00.000Z', coastalCents: 410, inlandCents: 390 }])
  })
})
