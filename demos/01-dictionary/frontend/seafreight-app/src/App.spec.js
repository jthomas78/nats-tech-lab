import { readFileSync } from 'node:fs'
import { dirname, resolve as resolvePath } from 'node:path'
import { fileURLToPath } from 'node:url'

import PrimeVue from 'primevue/config'
import ToastService from 'primevue/toastservice'
import { createPinia, setActivePinia } from 'pinia'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { nextTick, ref } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import App from './App.vue'
import { usePortStore } from './stores/port.js'
import { parseUiCopySeed } from '../scripts/parseUiCopySeed.mjs'
import { useUiCopy } from '@refdata/useUiCopy.js'

vi.mock('@refdata/useRefdataLabels.js', () => {
  const selectedLocale = ref('en')
  return {
    useRefdataLabels: () => ({
      selectedLocale,
      locales: ref(['en', 'es']),
      connected: ref(false),
      connect: vi.fn(),
      disconnect: vi.fn(),
      statusLabel: (code) => code,
    }),
  }
})

vi.mock('@refdata/useUiCopy.js', () => {
  const switching = ref(false)
  return {
    useUiCopy: () => ({
      usingFallback: ref(false),
      partialFallback: ref(false),
      switching,
      connect: vi.fn(),
      disconnect: vi.fn(),
    }),
  }
})

// Deliberately avoids `new URL('...', import.meta.url)` — Vite's import
// analysis special-cases that exact literal pattern for asset resolution and
// rewrites it to a dev-server `/@fs/...` URL, which breaks a plain readFileSync.
function seedCatalogs() {
  const seedPath = resolvePath(dirname(fileURLToPath(import.meta.url)), '../../../backend/refdata-service/refdata/seed.go')
  const seed = readFileSync(seedPath, 'utf8')
  return parseUiCopySeed(seed)
}

function mountApp({ ships = {}, containers = {}, port = 'Hamburg' } = {}) {
  const pinia = createPinia()
  setActivePinia(pinia)
  const store = usePortStore(pinia)
  store.$patch({
    port,
    knownPorts: ['Hamburg', 'Valencia'],
    ships,
    containers,
  })
  vi.spyOn(store, 'connect').mockImplementation(() => {})
  vi.spyOn(store, 'disconnect').mockImplementation(() => {})

  const i18n = createI18n({
    legacy: false,
    locale: 'en',
    fallbackLocale: 'en',
    messages: seedCatalogs(),
  })
  const wrapper = mount(App, {
    global: {
      plugins: [pinia, i18n, PrimeVue, ToastService],
      stubs: { teleport: true },
    },
  })
  return { wrapper, i18n, store }
}

async function switchLocale(i18n, locale) {
  i18n.global.locale.value = locale
  await nextTick()
}

async function openPortView(wrapper) {
  const portButton = wrapper.findAll('.nav-item').find((button) =>
    ['Port Management', 'Gestión portuaria'].includes(button.text()),
  )
  expect(portButton).toBeDefined()
  await portButton.trigger('click')
}

// Maps each fleet row's manifest count to its own shipID rather than trusting
// DataTable render order, which is an implementation detail the test
// shouldn't depend on.
function manifestCountsByShipId(wrapper) {
  const rows = wrapper.findAll('[data-testid="fleet-view"] tbody tr')
  return Object.fromEntries(
    rows.map((row) => [row.findAll('td')[0].text(), row.get('.manifest-count').text()]),
  )
}

describe('BR-D16 Port UI localization', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    useUiCopy().switching.value = false // the mock's `switching` ref is module-level and outlives each test
  })

  it('reactively switches visible chrome from English to Spanish', async () => {
    const { wrapper, i18n } = mountApp()

    expect(wrapper.get('.brandmark').text()).toBe('SSeaFreight Flow')
    expect(wrapper.findAll('.nav-item').map((node) => node.text())).toEqual([
      'Fleet Management',
      'Port Management',
    ])
    expect(wrapper.get('.topbar .lab-muted').text()).toBe('fleet overview · docked ships · manifests')
    expect(wrapper.get('label[for="locale"]').text()).toBe('Language')
    expect(wrapper.get('.topbar-right .p-tag-label').text()).toBe('disconnected')

    await openPortView(wrapper)
    expect(wrapper.get('.topbar .lab-muted').text()).toBe('terminal yard · ships at port · container operations')

    await switchLocale(i18n, 'es')

    // Title is treated as a brand name — unchanged across locales, unlike
    // the rest of the chrome.
    expect(wrapper.get('.brandmark').text()).toBe('SSeaFreight Flow')
    expect(wrapper.findAll('.nav-item').map((node) => node.text())).toEqual([
      'Gestión de flota',
      'Gestión portuaria',
    ])
    expect(wrapper.get('.topbar .lab-muted').text()).toBe(
      'patio de terminal · buques en puerto · operaciones de contenedores',
    )
    expect(wrapper.get('label[for="locale"]').text()).toBe('Idioma')
    expect(wrapper.get('.topbar-right .p-tag-label').text()).toBe('desconectado')

    // No assertion-targeted en string survives the switch to es. (app.title
    // is deliberately excluded — it's a brand name, unchanged across locales.)
    for (const enString of [
      'Fleet Management',
      'Port Management',
      'fleet overview · docked ships · manifests',
      'terminal yard · ships at port · container operations',
      'Language',
      'disconnected',
    ]) {
      expect(wrapper.text()).not.toContain(enString)
    }
  })

  it('localizes interpolated port headings in both locales', async () => {
    const { wrapper, i18n } = mountApp({ port: 'Hamburg' })
    await openPortView(wrapper)

    expect(wrapper.text()).toContain('Terminal Yard — Hamburg')
    expect(wrapper.text()).toContain('Ships at Port — Hamburg')

    await switchLocale(i18n, 'es')

    expect(wrapper.text()).toContain('Patio de terminal — Hamburg')
    expect(wrapper.text()).toContain('Buques en puerto — Hamburg')
    expect(wrapper.text()).not.toContain('Terminal Yard — Hamburg')
    expect(wrapper.text()).not.toContain('Ships at Port — Hamburg')
  })

  it('renders localized plural forms for zero, one, and multiple containers', async () => {
    const ships = {
      atlas: { shipID: 'atlas', shipName: 'Atlas', status: 'docked', currentPort: 'Hamburg' },
      borealis: { shipID: 'borealis', shipName: 'Borealis', status: 'docked', currentPort: 'Hamburg' },
      calypso: { shipID: 'calypso', shipName: 'Calypso', status: 'docked', currentPort: 'Hamburg' },
    }
    const containers = {
      one: { containerID: 'one', onShipID: 'borealis' },
      two: { containerID: 'two', onShipID: 'calypso' },
      three: { containerID: 'three', onShipID: 'calypso' },
    }
    const { wrapper, i18n } = mountApp({ ships, containers })

    expect(manifestCountsByShipId(wrapper)).toEqual({
      atlas: '0 containers',
      borealis: '1 container',
      calypso: '2 containers',
    })

    await switchLocale(i18n, 'es')

    expect(manifestCountsByShipId(wrapper)).toEqual({
      atlas: '0 contenedores',
      borealis: '1 contenedor',
      calypso: '2 contenedores',
    })
  })

  it('never renders the fleet and port views at the same time', async () => {
    const { wrapper } = mountApp()

    expect(wrapper.find('[data-testid="fleet-view"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="port-view"]').exists()).toBe(false)

    await openPortView(wrapper)

    expect(wrapper.find('[data-testid="fleet-view"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="port-view"]').exists()).toBe(true)
  })

  it('shows a loading spinner on the locale control while a switch is in flight', async () => {
    const { wrapper } = mountApp()
    const { switching } = useUiCopy()

    expect(wrapper.find('#locale [data-pc-section="loadingicon"]').exists()).toBe(false)

    switching.value = true
    await nextTick()
    expect(wrapper.find('#locale [data-pc-section="loadingicon"]').exists()).toBe(true)

    switching.value = false
    await nextTick()
    expect(wrapper.find('#locale [data-pc-section="loadingicon"]').exists()).toBe(false)
  })

  it('BR-D21: clicking a docked ship\'s port in Fleet Management jumps to Port Management scoped to that port', async () => {
    const ships = {
      atlas: { shipID: 'atlas', shipName: 'Atlas', status: 'docked', currentPort: 'Valencia' },
    }
    const { wrapper, store } = mountApp({ ships, port: 'Hamburg' })

    expect(wrapper.find('[data-testid="fleet-view"]').exists()).toBe(true)
    expect(store.port).toBe('Hamburg')

    await wrapper.get('[data-testid="fleet-view"] .port-link').trigger('click')

    expect(store.port).toBe('Valencia')
    expect(wrapper.find('[data-testid="port-view"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('Terminal Yard — Valencia')
  })

  it('BR-D21: a ship at sea has no clickable port link', async () => {
    const ships = {
      atlas: { shipID: 'atlas', shipName: 'Atlas', status: 'in-transit', currentPort: '' },
    }
    const { wrapper } = mountApp({ ships })

    expect(wrapper.find('[data-testid="fleet-view"] .port-link').exists()).toBe(false)
  })
})
