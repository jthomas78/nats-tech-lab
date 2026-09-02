import { describe, expect, it, vi } from 'vitest'

import { createFederatedAdapter } from './federatedAdapter.js'

/* The runtime is injected, so these specs prove the adapter's contract with
   federation without a network, a container, or a build. What federation does
   with a registration is federation's problem; what the shell promises about
   *when* it registers is this file's. */
const fakeRuntime = (loadRemote = vi.fn(async () => ({ components: {} }))) => ({
  init: vi.fn(),
  registerRemotes: vi.fn(),
  loadRemote,
})

const remote = (overrides = {}) => ({
  kind: 'federated',
  name: 'example_plugin',
  url: 'http://localhost:7110/remoteEntry.js',
  module: 'plugin',
  ...overrides,
})

describe('BR-AS03 — the federated adapter registers containers at runtime', () => {
  it('registers the container from the manifest and returns the exposed module', async () => {
    const module = { components: { overview: {} }, activate() {} }
    const runtime = fakeRuntime(vi.fn(async () => module))
    const adapter = createFederatedAdapter({ runtime })

    await expect(adapter.load(remote())).resolves.toBe(module)

    expect(runtime.registerRemotes).toHaveBeenCalledWith([
      { name: 'example_plugin', entry: 'http://localhost:7110/remoteEntry.js', type: 'module' },
    ])
    expect(runtime.loadRemote).toHaveBeenCalledWith('example_plugin/plugin')
  })

  it('registers the remote as an ES module, which is what Vite emits', async () => {
    // Federation defaults to a classic script; a Vite-built remoteEntry then
    // loads and dies on its first `import` with a SyntaxError naming neither
    // the plugin nor the cause.
    const runtime = fakeRuntime()
    await createFederatedAdapter({ runtime }).load(remote())

    expect(runtime.registerRemotes.mock.calls[0][0][0].type).toBe('module')
  })

  it('gives the host an identity once, however many remotes it loads', async () => {
    const runtime = fakeRuntime()
    const adapter = createFederatedAdapter({ runtime, hostName: 'lab_shell' })

    await adapter.load(remote())
    await adapter.load(remote({ name: 'other_plugin' }))

    expect(runtime.init).toHaveBeenCalledTimes(1)
    expect(runtime.init).toHaveBeenCalledWith(
      expect.objectContaining({ name: 'lab_shell', remotes: [] }),
    )
  })

  it('declares no remotes at init — the host build names none', async () => {
    const runtime = fakeRuntime()
    await createFederatedAdapter({ runtime }).load(remote())

    expect(runtime.init.mock.calls[0][0].remotes).toEqual([])
  })

  it('registers a container name once, so a second entry cannot re-point it', async () => {
    const runtime = fakeRuntime()
    const adapter = createFederatedAdapter({ runtime })

    await adapter.load(remote())
    await adapter.load(remote({ url: 'http://localhost:7110/other.js' }))

    expect(runtime.registerRemotes).toHaveBeenCalledTimes(1)
    expect(runtime.loadRemote).toHaveBeenCalledTimes(2)
  })

  it('loads two exposed modules of one container without re-registering it', async () => {
    // The adapter supports multiple exposes generically, even though every
    // deployed fixture now has its own service and a single plugin expose.
    const runtime = fakeRuntime()
    const adapter = createFederatedAdapter({ runtime })

    await adapter.load(remote())
    await adapter.load(remote({ module: 'secondary' }))

    expect(runtime.registerRemotes).toHaveBeenCalledTimes(1)
    expect(runtime.loadRemote).toHaveBeenLastCalledWith('example_plugin/secondary')
  })

  it("accepts an expose spelled with federation's own './' prefix", async () => {
    const runtime = fakeRuntime()
    await createFederatedAdapter({ runtime }).load(remote({ module: './plugin' }))

    expect(runtime.loadRemote).toHaveBeenCalledWith('example_plugin/plugin')
  })

  it('fetches nothing until a load is asked for (BR-AS13, task 1b-5)', async () => {
    const runtime = fakeRuntime()
    createFederatedAdapter({ runtime })

    expect(runtime.init).not.toHaveBeenCalled()
    expect(runtime.registerRemotes).not.toHaveBeenCalled()
  })

  it('rejects when the container resolves nothing under that name', async () => {
    const runtime = fakeRuntime(vi.fn(async () => undefined))

    await expect(createFederatedAdapter({ runtime }).load(remote())).rejects.toThrow(
      /exposes no module/,
    )
  })

  it('propagates an unreachable container so the loader can mark it failed', async () => {
    const runtime = fakeRuntime(
      vi.fn(async () => {
        throw new Error('Failed to fetch remoteEntry.js')
      }),
    )

    await expect(createFederatedAdapter({ runtime }).load(remote())).rejects.toThrow(/fetch/)
  })
})

describe('BR-AS72 — same-origin remote paths', () => {
  it('resolves a path-only entry against the shell document origin', async () => {
    const runtime = fakeRuntime()
    const adapter = createFederatedAdapter({
      runtime,
      documentURL: 'https://shell.example/app/plugins',
    })

    await adapter.load(remote({ url: '/plugins/example-plugin/remoteEntry.js' }))

    expect(runtime.registerRemotes).toHaveBeenCalledWith([
      {
        name: 'example_plugin',
        entry: 'https://shell.example/plugins/example-plugin/remoteEntry.js',
        type: 'module',
      },
    ])
  })
})
