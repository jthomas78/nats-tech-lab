import PrimeVue from 'primevue/config'
import { createPinia, setActivePinia } from 'pinia'
import { mount, flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import PostgresTablesPanel from './PostgresTablesPanel.vue'
import { useDictionaryStore } from '../stores/dictionary'

vi.mock('../api', () => ({
  getPortsTable: vi.fn(),
}))

import { getPortsTable } from '../api'

describe('PostgresTablesPanel', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
  })

  it('does not request the ports table before a context is known', async () => {
    // The store's context is '' until loadContexts() resolves. Requesting
    // /api/admin/ports/ with an empty {context} segment matches no route on
    // shipping-service and only logged a console 404 on every page load.
    mount(PostgresTablesPanel, { global: { plugins: [PrimeVue, createPinia()] } })
    await flushPromises()

    expect(getPortsTable).not.toHaveBeenCalled()
  })

  it('requests the ports table once a context arrives', async () => {
    getPortsTable.mockResolvedValue({ rows: [{ name: 'Rotterdam', createdAt: null }] })
    const pinia = createPinia()
    setActivePinia(pinia)
    const wrapper = mount(PostgresTablesPanel, { global: { plugins: [PrimeVue, pinia] } })
    await flushPromises()

    useDictionaryStore().setContext('acme')
    await flushPromises()

    expect(getPortsTable).toHaveBeenCalledWith('acme')
    expect(wrapper.text()).toContain('Rotterdam')
  })
})
