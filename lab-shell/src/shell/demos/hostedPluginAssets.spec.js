/* The packaged arrangement itself (BR-AS77, task 16l).

   `demoAssetsGeneration.spec.js` holds the GENERATOR against fixtures. This
   file holds the real repository, because the reported failure was not a bug
   in a function — every function passed. It was three files that did not add
   up to a served path: a registry catalogue that enabled demo 04, a shell
   image that packaged no plugin, and no demo image to forward to.

   So these read what is actually on disk. They are the reason a later change
   that drops any one of the three fails here instead of in a browser. */
import { existsSync, readFileSync } from 'node:fs'
import { join, resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

import { scanDemoManifests } from '../../../tools/buildCatalogue/scanDemos.js'
import { ASSETS_CONF_ASSET, assetsNginxConf } from '../../../tools/buildCatalogue/demoAssets.js'

const repoRoot = resolve(process.cwd(), '..')
const read = (...parts) => readFileSync(join(repoRoot, ...parts), 'utf8')

/** Every demo the shell would be asked to serve assets for. */
const declaring = scanDemoManifests({ repoRoot }).entries.filter((entry) => entry.assets !== null)

describe('a demo that declares a hosted asset upstream', () => {
  it('is at least one, or this whole arrangement is dead code', () => {
    expect(declaring.map((entry) => entry.demo)).toContain('04-jetstream-cqrs')
  })

  /* The upstream is a promise that something answers there. A declaration
     with no image behind it is the same 404 in a new place. */
  it('ships an image that can answer it', () => {
    for (const entry of declaring) {
      expect(existsSync(join(repoRoot, 'demos', entry.demo, 'frontend', 'Dockerfile')), entry.demo).toBe(true)
      expect(existsSync(join(repoRoot, 'demos', entry.demo, 'frontend', 'nginx.conf')), entry.demo).toBe(true)
    }
  })

  /* The requirement in one assertion: a missing asset must be a 404 and never
     a page of HTML. An SPA fallback here would answer 200 for a chunk that
     does not exist, and the failure would surface much later as a syntax
     error inside the module loader. */
  it('answers a missing asset with 404, never its own index.html', () => {
    for (const entry of declaring) {
      const conf = read('demos', entry.demo, 'frontend', 'nginx.conf')
      expect(conf, entry.demo).toContain('=404')
      expect(conf, entry.demo).not.toMatch(/try_files[^;]*\/index\.html\s*;/)
    }
  })

  /* The image serves the PUBLIC path, not a private one, so nothing has to be
     rewritten between the shell and the demo. */
  it('serves the files at the same prefix the browser asks for', () => {
    for (const entry of declaring) {
      const dockerfile = read('demos', entry.demo, 'frontend', 'Dockerfile')
      expect(dockerfile, entry.demo).toContain(`/usr/share/nginx/html/plugins/${entry.id}`)
      const vite = read('demos', entry.demo, 'frontend', 'vite.config.js')
      expect(vite, entry.demo).toContain(`base: '/plugins/${entry.id}/'`)
    }
  })
})

describe('the shell that forwards to it', () => {
  it('includes the generated rule, and names no demo of its own', () => {
    const conf = read('lab-shell', 'nginx.conf')
    expect(conf).toContain('include /etc/nginx/demo-assets.conf;')
    for (const entry of declaring) expect(conf).not.toContain(entry.id)
  })

  it('ships that rule into the image', () => {
    expect(read('lab-shell', 'Dockerfile'))
      .toContain(`dist/${ASSETS_CONF_ASSET.split(/[\\/]/).join('/')} /etc/nginx/demo-assets.conf`)
  })

  /* BR-AS03 restated as a test. The whole point of proxying rather than
     copying is that the shell's image still contains no plugin. */
  it('still copies no plugin into its own image', () => {
    /* The COPY lines only. The prose around them talks about plugins at
       length — that is the file explaining why it copies none. */
    const copies = read('lab-shell', 'Dockerfile')
      .split('\n')
      .filter((line) => line.startsWith('COPY'))
    for (const line of copies) {
      expect(line, line).not.toMatch(/frontend\/dist/)
      expect(line, line).not.toMatch(/lab-shell\/plugins/)
    }
  })

  it('generates a rule for every demo that declared one', () => {
    const rendered = assetsNginxConf({ repoRoot })
    for (const entry of declaring) {
      expect(rendered, entry.demo).toContain(`location /plugins/${entry.id}/ {`)
      expect(rendered, entry.demo).toContain(`"${entry.assets.hostedUpstream}"`)
    }
  })
})

describe('the network the two meet on', () => {
  const overlay = () => read('demos', '04-jetstream-cqrs', 'deploy', 'compose.shell.yaml')
  const cell = () => read('demos', '01-dictionary', 'deploy', 'cell', 'compose.plugins.yaml')

  /* The isolation rule, held from both ends. Neither side joins the other's
     network; both put a foot on one named edge, and only the containers that
     were meant to be called. */
  it('is a third network, not either side\'s own', () => {
    expect(overlay()).toContain('name: lab-shell-plugins')
    expect(cell()).toContain('name: lab-shell-plugins')
  })

  /* `external: true` is the "not silently" half: Compose will not create it,
     so a stack cannot drift onto a shared network on an ordinary `up`. */
  it('has to exist already, so no ordinary run can drift onto it', () => {
    expect(overlay()).toMatch(/shell-edge:\s*\n\s*external: true/)
    expect(cell()).toMatch(/shell-edge:\s*\n\s*external: true/)
  })

  it('is an overlay, so the demo and the cell still start without it', () => {
    expect(read('demos', '04-jetstream-cqrs', 'deploy', 'compose.yaml')).not.toContain('lab-shell-plugins')
    expect(read('demos', '01-dictionary', 'deploy', 'cell', 'compose.runtime.yaml'))
      .not.toContain('lab-shell-plugins')
  })

  /* Demo 04's NATS is not published across the edge. Only the two things the
     demo declared — its assets and its named API routes — are reachable. */
  it('carries only the containers the shell was told to call', () => {
    const edged = overlay().split(/^services:/m)[1].split(/^networks:/m)[0]
      .split(/\n  (?=[a-z])/)
      .filter((block) => block.includes('shell-edge'))
      .map((block) => block.trim().split(':')[0])
    expect(edged.sort()).toEqual(['demo04-cqrs', 'demo04-frontend'])
  })
})
