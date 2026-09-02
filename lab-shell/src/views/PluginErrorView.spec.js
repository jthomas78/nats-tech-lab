import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

import { SHELL } from '../shell/shellKey.js'
import PluginErrorView from './PluginErrorView.vue'

/* BR-AS04: what the user is shown when a route's plugin will not load is
   shell-owned text. A federation failure quotes the remote's URL back at you,
   so the underlying message is exactly the thing that must not reach the
   screen. */
const meta = {
  pluginId: 'example-plugin-unreachable',
  title: 'Unreachable remote',
  contributionId: 'example-plugin-unreachable/main/v1',
}
const replace = vi.fn().mockResolvedValue(undefined)

vi.mock('vue-router', () => ({
  useRoute: () => ({ meta, path: '/example-unreachable', query: {}, name: meta.contributionId }),
  useRouter: () => ({ replace }),
}))

const REASON =
  '[ Federation Runtime ]: Failed to load script resources. args: ' +
  '{"resourceUrl":"http://localhost:7110/no-such-remoteEntry.js"}'

const manifest = { id: meta.pluginId, version: '0.3.1', shellApiVersion: 1 }

const mountView = (loader = { load: vi.fn().mockRejectedValue(new Error(REASON)) }) => {
  const wrapper = mount(PluginErrorView, {
    global: {
      provide: {
        [SHELL]: {
          statuses: new Map([
            [meta.pluginId, { reasonCode: 'chunk-load-failed', reason: REASON }],
          ]),
          manifestFor: (id) => (id === meta.pluginId ? manifest : null),
          loader,
        },
      },
      stubs: { 'router-link': { props: ['to'], template: '<a><slot /></a>' } },
    },
  })
  return { wrapper, loader }
}

describe('the route-level failure panel', () => {
  it('names the plugin and the cause code', () => {
    const text = mountView().wrapper.text()

    expect(text).toContain('Unreachable remote')
    expect(text).toContain('chunk-load-failed')
  })

  /* Stage and cause answer different questions — how far the load got, and
     what stopped it — and both are shell-authored constants. */
  it('names the stage the load reached, and the build that failed', () => {
    const text = mountView().wrapper.text()

    expect(text).toContain('remote entry fetch')
    expect(text).toContain('0.3.1')
  })

  it('retries through the loader rather than reloading the page', async () => {
    const { wrapper, loader } = mountView()

    await wrapper.get('button').trigger('click')

    expect(loader.load).toHaveBeenCalledWith(manifest)
  })

  it('shows neither the remote URL nor the underlying message', () => {
    const text = mountView().wrapper.text()

    expect(text).not.toContain('http')
    expect(text).not.toContain('7110')
    expect(text).not.toContain('remoteEntry')
    expect(text).not.toContain(REASON)
  })

  it('says where the detail went, rather than dropping it silently', () => {
    expect(mountView().wrapper.text()).toContain('console')
  })
})
