import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

import {
  DEMOS_README_FILE,
  SOURCE_BUILD,
  SOURCE_REGISTRY,
  composeFrontends,
  demoTitle,
  demosReadme,
  generateDemosReadme,
} from '../../../tools/buildCatalogue/demosReadme.js'

/*
  BR-AS81 — where a demo's source is listed OUTSIDE the shell, it is derived
  from the same scan, never restated by hand.

  `demos/README.md` is generated, so the only way it can be wrong is by going
  stale. The last spec in this file is the gate that stops that: it regenerates
  from the working tree and compares. A demo added, renamed, re-ported or given
  a manifest fails the suite until the page is regenerated.
*/
const repoRoot = resolve(process.cwd(), '..')

describe('the one-line description', () => {
  it('is the demo README\'s own H1', () => {
    expect(demoTitle('# EventSourcing and CQRS\n\nlong prose\n')).toBe('EventSourcing and CQRS')
  })

  it('drops the `Demo NN —` prefix, because the demo column just said it', () => {
    expect(demoTitle('# Demo 04 — JetStream as an Event Source')).toBe('JetStream as an Event Source')
  })

  it('is empty rather than invented when a demo has no heading', () => {
    expect(demoTitle('no heading here')).toBe('')
    expect(demoTitle(undefined)).toBe('')
  })
})

describe('reading a registry-sourced frontend out of a compose band', () => {
  const compose = [
    'services:',
    '  refdata-service:',
    '    build:',
    '      dockerfile: demos/01-dictionary/backend/refdata-service/Dockerfile',
    '    ports:',
    '      - "${REFDATA_PORT:-8080}:8080"',
    '',
    '  admin-frontend:',
    '    build:',
    '      dockerfile: demos/01-dictionary/frontend/admin/Dockerfile',
    '    ports:',
    '      - "${ADMIN_UI_PORT:-7100}:80"',
    '',
    '  example-plugin-frontend:',
    '    build:',
    '      dockerfile: lab-shell/plugins/example-plugin/Dockerfile',
    '    ports:',
    '      - "${PLUGIN_EXAMPLE_PORT:-7111}:80"',
    '',
  ].join('\n')

  it('finds a demo frontend and its published host port', () => {
    expect(composeFrontends(compose)).toEqual([
      { demo: '01-dictionary', frontend: 'admin', port: 7100 },
    ])
  })

  it('ignores a backend and a plugin fixture', () => {
    /* The pattern is exact on purpose: a service it misses is a service that
       is not a demo frontend. */
    const found = composeFrontends(compose).map((f) => f.frontend)
    expect(found).not.toContain('refdata-service')
    expect(found).not.toContain('example-plugin')
  })

  it('takes the port from the service it belongs to, not the one above', () => {
    expect(composeFrontends(compose)[0].port).toBe(7100)
  })
})

describe('the rendered table', () => {
  const rows = [
    { demo: '01-dictionary', title: 'EventSourcing and CQRS', frontend: 'admin', source: SOURCE_REGISTRY, port: 7100 },
    { demo: '01-dictionary', title: 'EventSourcing and CQRS', frontend: 'refdata', source: SOURCE_REGISTRY, port: 7102 },
    { demo: '02-multi-region', title: 'Multi-Region Cluster Mechanics', frontend: null, source: null, port: null },
  ]
  const text = demosReadme(rows)

  it('says the source is a property of the shell, not of a demo', () => {
    expect(text).toContain('`plugin-source` is a property of the running')
    expect(text).toContain('shell, not of a demo')
  })

  it('says a demo with no frontend has nothing to source, rather than a mode', () => {
    expect(text).toContain('| `02-multi-region` | — | — | — |')
  })

  it('does not repeat one demo\'s description down its own rows', () => {
    expect(text.match(/EventSourcing and CQRS/g)).toHaveLength(1)
    // …and a continuation row is blank, not a dash, because a dash means none.
    expect(text).toContain('| `01-dictionary` | refdata | registry | 7102 |  |')
  })

  it('warns that the file is generated', () => {
    expect(text).toContain('GENERATED FILE — do not edit by hand')
  })
})

describe('the committed page is the folder', () => {
  it('lists demo 01 as registry and demo 04 as build, read from the tree', () => {
    const { rows } = generateDemosReadme({ repoRoot })
    const sourceOf = (demo) => rows.filter((r) => r.demo === demo).map((r) => r.source)

    expect(sourceOf('01-dictionary')).toContain(SOURCE_REGISTRY)
    expect(sourceOf('04-jetstream-cqrs')).toEqual([SOURCE_BUILD])
    /* Demos 02 and 03 have no frontend at all, so a mode label would be wrong
       for them rather than merely missing. */
    expect(sourceOf('02-multi-region')).toEqual([null])
    expect(sourceOf('03-multi-cluster-and-accounts')).toEqual([null])
  })

  it('is not stale', () => {
    const { text } = generateDemosReadme({ repoRoot })
    const committed = readFileSync(resolve(repoRoot, DEMOS_README_FILE), 'utf8')

    expect(committed).toBe(text)
  })
})
