/*
  The preload fixture and the compose file have to agree, and nothing else
  checks that they do.

  `demos/01-dictionary/registry.json` is read by mfe-registry-service at start
  (8a) and REGISTRY_ALLOWED_ORIGINS is the envelope it is read inside
  (BR-AS20). The two are edited in different files, by different concerns, and
  a disagreement between them is silent in both: an entry outside the allowlist
  is withheld on read with no error, and an allowlisted origin nothing serves
  produces a plugin that fails in the browser long after boot looked fine.

  That second failure is not hypothetical — it shipped once. The demo catalog
  was curated at 7112 with no service behind it while it was still a shell
  built-in at the time, so the shell admitted that copy, refused the curated one as
  `duplicate-plugin-id`, and showed a red row for a plugin that worked.

  These specs are a *fixture* check, not a rule check: the business rules have
  their own specs in the Go suite. What is asserted here is that the fixture
  the lab actually boots with is internally consistent.
*/
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

import { describe, expect, it } from 'vitest'

/* Vitest runs from lab-shell/; both files being checked live outside it,
   which is the whole point — they are the lab's fixture, not the app's. */
const repoFile = (rel) => readFileSync(resolve(process.cwd(), '..', rel), 'utf8')

const registry = JSON.parse(repoFile('demos/01-dictionary/registry.json'))
const compose = repoFile('demos/01-dictionary/docker-compose.yml')

/* Read straight out of compose rather than duplicated here: a spec that
   restates the value it is checking proves only that someone typed it twice. */
const allowlist = compose
  .match(/REGISTRY_ALLOWED_ORIGINS:\s*"([^"]*)"/)[1]
  .split(',')
  .map((o) => o.trim())
  .filter(Boolean)

/* Every host port compose publishes, from `- "7111:80"` style mappings. */
const publishedPorts = new Set(
  [...compose.matchAll(/-\s*"(\d{4}):\d+"/g)].map((m) => m[1]),
)

const originOf = (url) => new URL(url).origin
const entry = (id) => registry.plugins.find((p) => p.id === id)

describe('the preload fixture agrees with the allowlist (BR-AS20)', () => {
  it('curates no entry whose origin is outside the allowlist', () => {
    // Outside the allowlist an entry is refused on write and filtered on read.
    // Either way it never reaches the shell, and nothing says why.
    const stray = registry.plugins
      .filter((p) => !allowlist.includes(originOf(p.remote.url)))
      .map((p) => `${p.id} -> ${originOf(p.remote.url)}`)
    expect(stray).toEqual([])
  })

  it('allowlists no origin that no compose service serves', () => {
    // The dead-origin bug, in one assertion. An allowlisted origin with
    // nothing behind it is an entry that boots clean and fails in the browser.
    const dead = allowlist.filter((o) => !publishedPorts.has(new URL(o).port))
    expect(dead).toEqual([])
  })
})

describe('the preload fixture states only what a preload file may state', () => {
  it('carries no revision — the service owns it (decision 82)', () => {
    expect(registry).not.toHaveProperty('revision')
  })

  it('lets no entry assert its own trust tier or lifecycle (BR-AS43)', () => {
    // A plugin describes itself; the platform classifies it. An entry that
    // arrived carrying these would be refused by ParsePreload at boot, which
    // fails the whole file — so catching it here is the cheaper failure.
    const asserted = registry.plugins.flatMap((p) =>
      ['source', 'lifecycle', 'revision'].filter((f) => f in p).map((f) => `${p.id}.${f}`),
    )
    expect(asserted).toEqual([])
  })
})

describe('8f — one service per plugin (decisions 87, 93, 94)', () => {
  it('gives each buildable variant its own origin', () => {
    // The point of the split: several independently deployed plugins
    // registering and appearing together, not one image wearing four hats.
    expect({
      'example-plugin': originOf(entry('example-plugin').remote.url),
      'example-plugin-slow': originOf(entry('example-plugin-slow').remote.url),
      'example-plugin-activate-throws': originOf(
        entry('example-plugin-activate-throws').remote.url,
      ),
      'example-plugin-incompatible': originOf(
        entry('example-plugin-incompatible').remote.url,
      ),
    }).toEqual({
      'example-plugin': 'http://localhost:7111',
      'example-plugin-slow': 'http://localhost:7113',
      'example-plugin-activate-throws': 'http://localhost:7114',
      'example-plugin-incompatible': 'http://localhost:7115',
    })
  })

  it('stops the module field carrying the variant', () => {
    // Each package exposes one module now. A variant selected by expose name
    // was how one image served four plugins; that arrangement is gone.
    const modules = registry.plugins.map((p) => p.remote.module.replace(/^\.\//, ''))
    expect([...new Set(modules)]).toEqual(['plugin'])
  })

  it('keeps example-plugin-unreachable service-less on a served origin (decision 88)', () => {
    // Its fixture is a 404, and only a *served* origin can produce one. Give
    // it an origin of its own and the entry flips from `failed` to `withheld`
    // — a different failure mode, tested elsewhere, and not the one wanted.
    const unreachable = entry('example-plugin-unreachable')
    expect(originOf(unreachable.remote.url)).toBe('http://localhost:7111')
    expect(unreachable.remote.url).toMatch(/no-such-remoteEntry\.js$/)
  })

  it('curates the federated demo catalog on its own allowed origin', () => {
    expect(entry('demo-catalog').remote).toEqual({ kind: 'federated', name: 'demo_catalog', url: 'http://localhost:7112/remoteEntry.js', module: 'plugin' })
    expect(allowlist).toContain('http://localhost:7112')
  })
})
