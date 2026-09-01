import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

import { SHELL } from '../shell/shellKey.js'
import PluginWithdrawnView from './PluginWithdrawnView.vue'

/*
  The shell-owned explanation the occupant of a withdrawn route is given
  (BR-AS57). It replaces the plugin's view in place; the URL is untouched.
  Shell text only, like every other failure panel — a publisher does not get
  to write on the shell's screen (BR-AS04).
*/
const meta = {
  pluginId: 'fleet-ops',
  title: 'Vessels',
  contributionId: 'fleet-ops/vessels',
}

vi.mock('vue-router', () => ({
  useRoute: () => ({ meta, path: '/fleet-ops/vessels', name: meta.contributionId }),
}))

const mountView = () =>
  mount(PluginWithdrawnView, {
    global: {
      provide: {
        [SHELL]: {
          statuses: new Map([[meta.pluginId, { reasonCode: 'publisher-withdrawn' }]]),
          plugins: new Map([[meta.pluginId, { id: meta.pluginId, name: 'Fleet Ops', version: '0.3.1' }]]),
        },
      },
      stubs: { 'router-link': { props: ['to'], template: '<a :href="to"><slot /></a>' } },
    },
  })

describe('BR-AS57 — the withdrawal explanation', () => {
  it('says the publisher withdrew this feature, and names it', () => {
    const text = mountView().text()

    expect(text).toContain('Vessels')
    expect(text).toMatch(/withdrawn/i)
    expect(text).toContain('fleet-ops')
  })

  it('offers a way out without moving anyone', () => {
    const links = mountView().findAll('a').map((a) => a.attributes('href'))

    expect(links).toContain('/')
  })

  it('offers no retry, because there is nothing to retry', () => {
    expect(mountView().find('button').exists()).toBe(false)
  })

  it('quotes no URL', () => {
    expect(mountView().text()).not.toMatch(/https?:\/\//)
  })
})
