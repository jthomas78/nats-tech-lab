/*
  BR-AS75 — the catalogue has exactly one source, chosen once, from an
  explicit environment variable, and `build` mode is not conditioned on a
  development build.
*/

import { beforeEach, describe, expect, it } from 'vitest'

import {
  PLUGIN_SOURCE_BUILD,
  PLUGIN_SOURCE_REGISTRY,
  PLUGIN_SOURCE_VAR,
  pluginSource,
  readPluginSource,
  resetPluginSourceForTests,
} from './pluginSource.js'

beforeEach(() => resetPluginSourceForTests())

describe('BR-AS75 — the source is read from one explicit variable', () => {
  it('takes `build` when the variable says so', () => {
    expect(readPluginSource({ [PLUGIN_SOURCE_VAR]: 'build' })).toBe(PLUGIN_SOURCE_BUILD)
  })

  it('takes `registry` when the variable says so', () => {
    expect(readPluginSource({ [PLUGIN_SOURCE_VAR]: 'registry' })).toBe(PLUGIN_SOURCE_REGISTRY)
  })

  it('is `registry` when nothing is declared, so an existing deployment does not move', () => {
    expect(readPluginSource({})).toBe(PLUGIN_SOURCE_REGISTRY)
    expect(readPluginSource(undefined)).toBe(PLUGIN_SOURCE_REGISTRY)
    expect(readPluginSource({ [PLUGIN_SOURCE_VAR]: '' })).toBe(PLUGIN_SOURCE_REGISTRY)
  })

  it('fails rather than falling back on an unknown value', () => {
    // A typo that quietly served the other catalogue is the outcome the rule exists to stop.
    expect(() => readPluginSource({ [PLUGIN_SOURCE_VAR]: 'local' })).toThrow(/VITE_PLUGIN_SOURCE/)
    expect(() => readPluginSource({ [PLUGIN_SOURCE_VAR]: 'Build' })).toThrow()
  })

  it('is not conditioned on a development build', () => {
    // `build` describes how the catalogue is obtained, not where the shell
    // runs, so a production environment selects it exactly the same way.
    const production = { [PLUGIN_SOURCE_VAR]: 'build', DEV: false, PROD: true, MODE: 'production' }
    expect(readPluginSource(production)).toBe(PLUGIN_SOURCE_BUILD)
  })
})

describe('BR-AS75 — it resolves once and cannot vary per plugin', () => {
  it('keeps the first answer even when the environment changes underneath it', () => {
    const env = { [PLUGIN_SOURCE_VAR]: 'build' }
    expect(pluginSource(env)).toBe(PLUGIN_SOURCE_BUILD)
    env[PLUGIN_SOURCE_VAR] = 'registry'
    expect(pluginSource(env)).toBe(PLUGIN_SOURCE_BUILD)
    expect(pluginSource({ [PLUGIN_SOURCE_VAR]: 'registry' })).toBe(PLUGIN_SOURCE_BUILD)
  })

  it('takes no plugin, so there is no per-plugin answer to give', () => {
    expect(pluginSource.length).toBeLessThanOrEqual(1)
  })
})
