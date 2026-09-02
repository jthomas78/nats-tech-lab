/*
  The preload fixture, the announced manifests and the compose file have to
  agree, and nothing else checks that they do.

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

  Phase 13e split the fixture in two. The preload file now curates
  `demo-catalog` alone (BR-AS66); the five example plugins arrive announced.
  Phase 14 moved announcement into the four served plugin processes and kept
  only the intentionally unreachable fixture on the CLI announcer. Each
  publisher signs its own build-owned `public/manifest.json`. The same silent disagreements are possible across
  that seam — a manifest whose origin is not allowlisted, or an announced
  plugin with no sidecar to announce it — so the checks below now cover both
  halves and the compose wiring between them.

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
const publishers = JSON.parse(repoFile('demos/01-dictionary/nats/keys/publishers.json'))

/* The announced half. Each id is a directory under lab-shell/plugins/ holding
   the manifest its own build owns and its sidecar announces. */
const announcedIds = [
  'example-plugin',
  'example-plugin-slow',
  'example-plugin-unreachable',
  'example-plugin-activate-throws',
  'example-plugin-incompatible',
]
const manifests = Object.fromEntries(
  announcedIds.map((id) => [
    id,
    JSON.parse(repoFile(`lab-shell/plugins/${id}/public/manifest.json`)),
  ]),
)

const serviceBlock = (service) => {
  const lines = compose.split('\n')
  const start = lines.findIndex((line) => line === `  ${service}:`)
  if (start < 0) return ''
  const end = lines.findIndex((line, index) => index > start && /^ {2}\S/.test(line))
  return lines.slice(start, end < 0 ? undefined : end).join('\n')
}

const publisherService = (id) =>
  id === 'example-plugin-unreachable' ? `${id}-announcer` : `${id}-frontend`

const publicOriginOf = (id) => {
  const hit = serviceBlock(publisherService(id)).match(/PLUGIN_PUBLIC_ORIGIN:\s*(\S+)/)
  if (!hit) throw new Error(`missing PLUGIN_PUBLIC_ORIGIN for ${id}`)
  return hit[1]
}

const announcedEntry = (id) => ({
  ...manifests[id],
  remote: {
    ...manifests[id].remote,
    url: new URL(manifests[id].remote.url, publicOriginOf(id)).href,
  },
})

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
/* Everything the lab can end up serving, whichever door it came in by. */
const everyEntry = [...registry.plugins, ...announcedIds.map(announcedEntry)]

describe('the fixture agrees with the allowlist (BR-AS20)', () => {
  it('offers no entry whose origin is outside the allowlist', () => {
    // Outside the allowlist an entry is refused on write and filtered on read.
    // Either way it never reaches the shell, and nothing says why. An
    // announced manifest is refused the same way a curated row is — the tier
    // buys it nothing here.
    const stray = everyEntry
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

describe('BR-AS66 — a fresh lab serves only its preloaded plugin', () => {
  it('curates demo-catalog and nothing else', () => {
    // First boot against an empty database must serve the catalog alone. The
    // five examples arrive announced and wait disabled for an operator, so
    // any of them reappearing here would hand them an enable nobody granted.
    expect(registry.plugins.map((p) => p.id)).toEqual(['demo-catalog'])
  })

  it('leaves every announced plugin out of the preload file', () => {
    const leaked = announcedIds.filter((id) => entry(id))
    expect(leaked).toEqual([])
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

describe('an announced manifest states only what a publisher may state', () => {
  it.each(announcedIds)('%s asserts no store-owned field', (id) => {
    // enabled, source, lifecycle, withheld and withdrawn are the store's to
    // decide. A manifest carrying one is not merely ignored — it is signed
    // over, so the bytes would keep claiming it after the operator disagreed.
    const asserted = ['enabled', 'source', 'lifecycle', 'withheld', 'withdrawn', 'revision']
      .filter((f) => f in manifests[id])
    expect(asserted).toEqual([])
  })

  it.each(announcedIds)('%s carries no release — the sidecar injects it (D11)', (id) => {
    // The release counter is the publisher's runtime sequence (BR-AS67), not
    // a build input. Committing one would freeze it at 1 and make every
    // withdraw-then-return replay a spent number.
    expect(manifests[id]).not.toHaveProperty('release')
  })
})

describe('Phase 14 — one publisher identity per announced plugin (decisions 4, 6)', () => {
  it.each(announcedIds)('%s has a publisher of its own', (id) => {
    // One publisher, one plugin. A publisher owning two would let one
    // compromised key withdraw both.
    const owners = publishers.filter((p) => p.plugin === id)
    expect(owners.map((p) => p.publisher)).toEqual([id])
  })

  it('gives no publisher more than one plugin', () => {
    const plugins = publishers.map((p) => p.plugin)
    expect(new Set(plugins).size).toBe(plugins.length)
    expect(new Set(publishers.map((p) => p.publisher)).size).toBe(publishers.length)
  })

  it.each(announcedIds)('%s is wired to its own manifest, seed and creds', (id) => {
    // A process mounting another plugin's manifest or seed would announce the
    // wrong plugin, or sign with a key it does not own — both silent until a
    // verify fails in the registry.
    const block = serviceBlock(publisherService(id))
    expect(block).toContain(`PUBLISHER_ID: ${id}`)
    expect(block).toContain(`NATS_CONNECTION_NAME: ${id}`)
    expect(block).toContain(`./nats/creds/plugins/${id}.creds:/etc/nats/creds/plugin.creds:ro`)
    expect(block).toContain(`./nats/keys/publisher-${id}.nk:/etc/plugin/signing.nk:ro`)
    if (id === 'example-plugin-unreachable') {
      expect(block).toContain(`../../lab-shell/plugins/${id}/public/manifest.json:/etc/plugin/manifest.json:ro`)
    } else {
      expect(block).toContain('PLUGIN_MANIFEST_PATH: /srv/manifest.json')
      expect(compose).not.toContain(`\n  ${id}-announcer:\n`)
    }
  })

  it.each(announcedIds)('%s keeps its release counter outside the container', (id) => {
    // BR-AS67: N must survive a restart for N+1 / N+2 to be reachable.
    expect(compose).toContain(`- ${id}-release:/var/lib/announcer`)
    expect(compose).toContain(`\n  ${id}-release:\n`)
  })

  it('gives every publisher process room to finish its unregister on SIGTERM', () => {
    // The unregister is a request/reply round trip. Compose's default 10s
    // grace turns a slow bus into a kill, and a killed sidecar leaves the
    // entry standing — the opposite of the controlled withdrawal BR-AS54
    // describes.
    const graces = announcedIds.map((id) =>
      Number(serviceBlock(publisherService(id)).match(/stop_grace_period:\s*(\d+)s/)?.[1]),
    )
    expect(graces.every((g) => g >= 20)).toBe(true)
  })

  /* Compose's provenance label (decision 80) is operator-authored
     documentation, read by no code on the trust path — which is exactly why
     it can drift without anything failing. It read `preload` on all five
     frontends until 13g moved the examples to the announced tier. A wrong
     badge here is a wrong answer to "how did this plugin get here" for the
     next reader of the compose file. */
  const sourceLabelOf = (service) => {
    /* Line-walked rather than regex-matched: a service block runs from its
       own 2-space key to the next one, and a blank line inside it must not
       end it. */
    const lines = compose.split('\n')
    const start = lines.findIndex((l) => l === `  ${service}:`)
    if (start < 0) return null
    for (const line of lines.slice(start + 1)) {
      if (/^ {2}\S/.test(line)) break
      const hit = line.match(/com\.nats-tech-lab\.mfe\.source:\s*(\S+)/)
      if (hit) return hit[1]
    }
    return null
  }

  it('labels each frontend with the tier it actually arrives by', () => {
    expect({
      'demo-catalog-frontend': sourceLabelOf('demo-catalog-frontend'),
      'example-plugin-frontend': sourceLabelOf('example-plugin-frontend'),
      'example-plugin-slow-frontend': sourceLabelOf('example-plugin-slow-frontend'),
      'example-plugin-activate-throws-frontend': sourceLabelOf('example-plugin-activate-throws-frontend'),
      'example-plugin-incompatible-frontend': sourceLabelOf('example-plugin-incompatible-frontend'),
    }).toEqual({
      'demo-catalog-frontend': 'preload',
      'example-plugin-frontend': 'announced',
      'example-plugin-slow-frontend': 'announced',
      'example-plugin-activate-throws-frontend': 'announced',
      'example-plugin-incompatible-frontend': 'announced',
    })
  })

  it('gives the preloaded catalog no sidecar', () => {
    // demo-catalog is curated by the lab, not announced by a publisher. A
    // sidecar for it would put the same plugin in two tiers at once.
    expect(compose).not.toContain('demo-catalog-announcer')
  })
})

describe('8f — one service per plugin (decisions 87, 93, 94)', () => {
  it('gives each buildable variant its own origin', () => {
    // The point of the split: several independently deployed plugins
    // registering and appearing together, not one image wearing four hats.
    expect({
      'example-plugin': publicOriginOf('example-plugin'),
      'example-plugin-slow': publicOriginOf('example-plugin-slow'),
      'example-plugin-activate-throws': publicOriginOf('example-plugin-activate-throws'),
      'example-plugin-incompatible': publicOriginOf('example-plugin-incompatible'),
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
    const modules = everyEntry.map((p) => p.remote.module.replace(/^\.\//, ''))
    expect([...new Set(modules)]).toEqual(['plugin'])
  })

  it('keeps example-plugin-unreachable service-less on a served origin (decision 88)', () => {
    // Its fixture is a 404, and only a *served* origin can produce one. Give
    // it an origin of its own and the entry flips from `failed` to `withheld`
    // — a different failure mode, tested elsewhere, and not the one wanted.
    // 13e gave it a sidecar but still no web server: the announcer publishes
    // the manifest, nothing serves the chunk it names.
    const unreachable = announcedEntry('example-plugin-unreachable')
    expect(originOf(unreachable.remote.url)).toBe('http://localhost:7111')
    expect(unreachable.remote.url).toMatch(/no-such-remoteEntry\.js$/)
    expect(compose).not.toContain('lb-example-plugin-unreachable\n')
  })

  it('curates the federated demo catalog on its own allowed origin', () => {
    expect(entry('demo-catalog').remote).toEqual({ kind: 'federated', name: 'demo_catalog', url: 'http://localhost:7112/remoteEntry.js', module: 'plugin' })
    expect(allowlist).toContain('http://localhost:7112')
  })
})
