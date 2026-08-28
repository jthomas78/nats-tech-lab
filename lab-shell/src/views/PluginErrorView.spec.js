import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

import { SHELL } from '../shell/shellKey.js'
import PluginErrorView from './PluginErrorView.vue'

/* BR-AS04: what the user is shown when a route's plugin will not load is
   shell-owned text. A federation failure quotes the remote's URL back at you,
   so the underlying message is exactly the thing that must not reach the
   screen. */
const meta = { pluginId: 'example-plugin-unreachable', title: 'Unreachable remote' }

vi.mock('vue-router', () => ({ useRoute: () => ({ meta }) }))

const REASON =
  '[ Federation Runtime ]: Failed to load script resources. args: ' +
  '{"resourceUrl":"http://localhost:7110/no-such-remoteEntry.js"}'

const mountView = () =>
  mount(PluginErrorView, {
    global: {
      provide: {
        [SHELL]: {
          statuses: new Map([
            [meta.pluginId, { reasonCode: 'chunk-load-failed', reason: REASON }],
          ]),
        },
      },
    },
  })

describe('the route-level failure panel', () => {
  it('names the plugin and the cause code', () => {
    const text = mountView().text()

    expect(text).toContain('Unreachable remote')
    expect(text).toContain('chunk-load-failed')
  })

  it('shows neither the remote URL nor the underlying message', () => {
    const text = mountView().text()

    expect(text).not.toContain('http')
    expect(text).not.toContain('7110')
    expect(text).not.toContain('remoteEntry')
    expect(text).not.toContain(REASON)
  })

  it('says where the detail went, rather than dropping it silently', () => {
    expect(mountView().text()).toContain('console')
  })
})
