import { existsSync, readFileSync } from 'node:fs'
import { resolve } from 'node:path'

import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

import { demoCatalogueDocument, scanDemoManifests } from '../../../tools/buildCatalogue/scanDemos.js'
import { LAB_DEMOS, LAB_DEMO_ROUTE, labDemo } from './labDemos.js'
import DemoCards from '../ui/DemoCards.vue'
import LabDemoView from '../../views/LabDemoView.vue'
import { SHELL } from '../shellKey.js'

/*
  Task 16g, closing questions — demos 02 and 03 are on the menu, and they carry
  NO health indicator and NO readiness check.

  Two kinds of spec here, and they fail for different reasons.

  The first kind checks the DATA against the working tree. These entries name
  scripts, compose files and reports by path, and a path in prose rots the
  moment somebody renames a file. Asserting each one exists moves that failure
  out of the reader's afternoon and into the suite.

  The second kind checks the RENDER. Decision 8 forbids drawing a mark nobody
  took, so the absence of a status on these cards is the requirement, not a
  styling choice — it is asserted directly, because an absence is exactly the
  sort of thing a later refactor restores by accident while "making the two
  lists consistent".
*/
const routeParams = { demo: '02-multi-region' }
vi.mock('vue-router', () => ({ useRoute: () => ({ params: routeParams }) }))

const repoRoot = resolve(process.cwd(), '..')

/* One probed demo alongside the two unprobed ones. An empty catalogue would
   make "no status is drawn" true by accident. */
const shellStub = () => ({
  demos: {
    byPlugin: { 'demo-04': { pluginId: 'demo-04', name: 'JetStream CQRS' } },
    results: { 'demo-04': { state: 'ready' } },
  },
  contributions: { routes: [{ pluginId: 'demo-04', qualifiedId: 'demo-04/main' }] },
})

const routerLink = { props: ['to'], template: '<a><slot /></a>' }

const mountCards = (shell = shellStub()) => mount(DemoCards, {
  global: {
    provide: { [SHELL]: shell },
    /* Registered, not stubbed: one card resolves the link by NAME through
       `<component :is>`, which a `stubs` entry does not reach. */
    components: { 'router-link': routerLink, RouterLink: routerLink },
  },
})

describe('the lab demos themselves', () => {
  it('is demos 02 and 03, and nothing else', () => {
    expect(LAB_DEMOS.map((d) => d.id)).toEqual(['02-multi-region', '03-multi-cluster-and-accounts'])
  })

  it('finds one by id, and answers null for a demo it does not have', () => {
    expect(labDemo('02-multi-region')?.name).toBe('Multi-Region Cluster Mechanics')
    expect(labDemo('01-dictionary')).toBeNull()
  })

  it('is frozen, because a menu entry is not state', () => {
    expect(Object.isFrozen(LAB_DEMOS)).toBe(true)
    expect(LAB_DEMOS.every((d) => Object.isFrozen(d))).toBe(true)
  })

  it('carries no health, no readiness and no plugin-source field', () => {
    const banned = ['state', 'status', 'health', 'readiness', 'pluginSource', 'plugin-source', 'tone']
    for (const demo of LAB_DEMOS) {
      expect(Object.keys(demo).filter((k) => banned.includes(k))).toEqual([])
    }
  })

  it('says what each demo asks, and how to run it', () => {
    for (const demo of LAB_DEMOS) {
      expect(demo.question.length).toBeGreaterThan(20)
      expect(demo.summary.length).toBeGreaterThan(40)
      expect(demo.run.length).toBeGreaterThan(0)
      expect(demo.findings.length).toBeGreaterThan(0)
    }
  })
})

describe('every path it prints', () => {
  const runPaths = LAB_DEMOS.flatMap((demo) => demo.run.map((step) => ({
    demo: demo.id,
    label: step.label,
    /* The last token that looks like a repo path — covers both a bare script
       and a `docker compose -f <file> up -d`. */
    path: step.command.split(/\s+/u).find((word) => word.startsWith('demos/')),
  })))

  it.each(runPaths)('$demo — $label runs $path, which exists', ({ path }) => {
    expect(path).toBeTruthy()
    expect(existsSync(resolve(repoRoot, path))).toBe(true)
  })

  const findingPaths = LAB_DEMOS.flatMap((demo) => demo.findings.map((f) => ({
    demo: demo.id, label: f.label, path: f.path,
  })))

  it.each(findingPaths)('$demo — $label is at $path, which exists', ({ path }) => {
    expect(existsSync(resolve(repoRoot, path))).toBe(true)
  })
})

describe('the demo menu', () => {
  it('lists the frontend-less demos below the catalogued ones', () => {
    const names = mountCards().findAll('.demo-card-name').map((el) => el.text())
    expect(names).toEqual(['JetStream CQRS', ...LAB_DEMOS.map((d) => d.name)])
  })

  it('draws no status and no dot on them, while the probed demo keeps both', () => {
    const wrapper = mountCards()
    expect(wrapper.findAll('.demo-card-status')).toHaveLength(1)
    expect(wrapper.findAll('.dot')).toHaveLength(1)
    const unprobed = wrapper.findAll('.demo-card').slice(1)
    expect(unprobed).toHaveLength(LAB_DEMOS.length)
    for (const card of unprobed) {
      expect(card.find('.demo-card-status').exists()).toBe(false)
      expect(card.find('.dot').exists()).toBe(false)
    }
  })

  it('links by route NAME, never by a hand-built path', () => {
    const source = readFileSync(resolve(repoRoot, 'lab-shell/src/shell/ui/DemoCards.vue'), 'utf8')
    expect(source).toContain('LAB_DEMO_ROUTE')
    expect(source).not.toMatch(/to:\s*['"`]\//u)
    expect(LAB_DEMOS.every((d) => LAB_DEMO_ROUTE === 'shell/lab-demo')).toBe(true)
  })

  it('registers that route away from the demo-catalog plugin\'s own /demos', () => {
    const main = readFileSync(resolve(repoRoot, 'lab-shell/src/main.js'), 'utf8')
    expect(main).toContain("path: '/lab-demos/:demo'")
    expect(main).not.toContain("path: '/demos/:demo'")
  })
})

/* BR-AS94 — one demo, one Home card. The demo catalogue is the REAL one, from
   this repository's scan, so a readiness block added to demo 03's demo.json
   (which would give it a second, probed card) fails here. */
describe('demo 03 on Home', () => {
  const demoCatalogue = demoCatalogueDocument(scanDemoManifests({ repoRoot }).entries)
  const byPlugin = Object.fromEntries(demoCatalogue.demos.map((d) => [d.pluginId, d]))
  const playground = { pluginId: 'demo-03', qualifiedId: 'demo-03/playground', default: true }
  const overview = { pluginId: 'demo-03', qualifiedId: 'demo-03/overview', default: false }
  const shellWith = (routes) => ({ demos: { byPlugin, results: {} }, contributions: { routes } })
  const demo03Cards = (wrapper) => wrapper.findAll('.demo-card')
    .filter((card) => /multi-cluster/iu.test(card.find('.demo-card-name').text()))

  it('is not in the probed demo catalogue, because it declares no readiness', () => {
    expect(Object.keys(byPlugin)).not.toContain('demo-03')
  })

  it('has one card in build mode, opening the plugin default route by name', () => {
    const wrapper = mountCards(shellWith([overview, playground]))
    const cards = demo03Cards(wrapper)
    expect(cards).toHaveLength(1)
    expect(cards[0].findComponent({ name: 'RouterLink' }).props('to')).toEqual({ name: 'demo-03/playground' })
    expect(cards[0].find('.demo-card-status').exists()).toBe(false)
    expect(cards[0].find('.dot').exists()).toBe(false)
  })

  it('has one card in registry mode, opening the intro page when the plugin is not admitted', () => {
    const wrapper = mountCards(shellWith([]))
    const cards = demo03Cards(wrapper)
    expect(cards).toHaveLength(1)
    expect(cards[0].findComponent({ name: 'RouterLink' }).props('to'))
      .toEqual({ name: LAB_DEMO_ROUTE, params: { demo: '03-multi-cluster-and-accounts' } })
  })

  it('names the plugin its own manifest declares', () => {
    const manifest = JSON.parse(readFileSync(resolve(repoRoot, 'demos/03-multi-cluster-and-accounts/frontend/public/manifest.json'), 'utf8'))
    expect(labDemo('03-multi-cluster-and-accounts').plugin).toBe(manifest.id)
  })
})

/* The intro page is the fallback for demo 03 (BR-AS94), so it must not say
   the demo has no frontend, nor that it is validation only. */
describe('the intro page', () => {
  const intro = (demo) => {
    routeParams.demo = demo
    return mount(LabDemoView).text()
  }

  it('names demo 03 playground plugin, and calls it validation and showcase', () => {
    const text = intro('03-multi-cluster-and-accounts')
    expect(text).toContain('its live playground is plugin demo-03')
    expect(text).toContain('Validation and showcase')
    expect(text).not.toMatch(/no frontend|Validation only/u)
    expect(text).toContain('REPORT-10.html')
  })

  it('still says demo 02 has no frontend', () => {
    expect(intro('02-multi-region')).toContain('no frontend, so no plugin and no catalogue entry')
  })
})
