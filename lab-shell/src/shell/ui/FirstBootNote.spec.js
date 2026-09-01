import { mount } from '@vue/test-utils'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

import FirstBootNote from './FirstBootNote.vue'

/*
  BR-AS66 says a fresh lab serves only its preloaded plugin, and the phase's
  design decision 9 adds the part a rule alone cannot enforce: the intro copy
  has to say so, or a correct first run reads as a broken one. So the copy is
  the rule's surface, and these specs cover both halves of it — that the
  sentence says the right thing, and that it is actually on the two screens a
  first run lands on.
*/
describe('BR-AS66 — the first-boot note', () => {
  const text = () => mount(FirstBootNote).text()

  it('states the rule in the shell\'s own words', () => {
    expect(text()).toContain('A fresh lab serves only its preloaded plugin')
  })

  it('keeps the space between the claim and the plugin it names', () => {
    // Vue's `condense` whitespace handling drops a whitespace-only text node
    // that contains a newline, so a line break after `</b>` silently glues two
    // words together. It shipped that way once.
    expect(text()).toContain('preloaded plugin. demo-catalog')
  })

  it('says an entry awaiting an operator is not served at all', () => {
    // Otherwise the note explains a row that is not on the screen: a disabled
    // entry never reaches the shell, so the list is short rather than red.
    expect(text()).toContain('not served to this shell')
  })

  it('names the preloaded plugin and the announced fixtures separately', () => {
    // Two tiers with two different stories is the whole point of the note; a
    // reader who cannot tell which plugin is which learns nothing from it.
    expect(text()).toContain('demo-catalog')
    expect(text()).toContain('example-plugin*')
  })

  it('says disabled means awaiting an operator, not failed', () => {
    const t = text()
    expect(t).toContain('awaiting review, not failed')
    expect(t).toContain('Admin UI')
  })

  it('says the enable survives restarts — BR-AS66 is a first-boot property', () => {
    // Without this line the note would read as "these five are always
    // disabled", which is the opposite of what the rule says.
    expect(text()).toContain('survives restarts')
  })
})

describe('the note is placed where a first run actually lands', () => {
  const view = (name) => readFileSync(resolve(process.cwd(), 'src/views', name), 'utf8')

  // A component nothing renders is copy that does not exist. Home's empty
  // region and the plugin inventory are the two screens a fresh lab shows.
  it.each(['HomeView.vue', 'PluginsView.vue'])('%s renders it', (name) => {
    const src = view(name)
    expect(src).toContain("import FirstBootNote from '../shell/ui/FirstBootNote.vue'")
    expect(src).toContain('<FirstBootNote />')
  })
})
