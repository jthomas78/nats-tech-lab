import { existsSync, readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'

import PrimeVue from 'primevue/config'
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'

import AboutPanel from './AboutPanel.vue'

// 04.12.2 — the Overview panel is one component, used twice.
//
// It used to import README.md and the class-diagram page by name, which made
// it lesson 01's panel and nothing else. Lesson 02 needs the same panel with
// its own files, and a copy of it would be a second copy of 250 lines of CSS
// to render the same markdown (D21).
//
// A parameterised component is only PROVED parameterised by handing it two
// different inputs, so every spec below hands it made-up sources.

const LESSON_A = {
  eyebrow: 'Demo 04 · lesson A eyebrow',
  lead: 'Lead sentence for lesson A.',
  notes: '# Alpha heading\n\nAlpha body text.\n',
  notesFile: 'docs/ALPHA.md',
  page: '<html><body><h1>Alpha drawing</h1></body></html>',
  pageFile: 'diagrams/alpha.html',
  pageLabel: 'Alpha drawings',
  frameTitle: 'Alpha drawings for the test',
}

const LESSON_B = {
  eyebrow: 'Demo 04 · lesson B eyebrow',
  lead: 'Lead sentence for lesson B.',
  notes: '# Beta heading\n\nBeta body text.\n',
  notesFile: 'docs/BETA.md',
}

const mountIt = (props) => mount(AboutPanel, { props, global: { plugins: [PrimeVue] } })

describe('AboutPanel', () => {
  it('renders the markdown it is handed', () => {
    const w = mountIt(LESSON_A)
    expect(w.get('[data-testid="about-notes"]').text()).toContain('Alpha body text.')
  })

  it('renders a different lesson when handed a different source', () => {
    const w = mountIt(LESSON_B)
    const text = w.get('[data-testid="about-notes"]').text()
    expect(text).toContain('Beta body text.')
    expect(text).not.toContain('Alpha body text.')
  })

  it('names the file it was handed, so the reader can go and read it', () => {
    expect(mountIt(LESSON_A).text()).toContain('docs/ALPHA.md')
    expect(mountIt(LESSON_B).text()).toContain('docs/BETA.md')
  })

  it('takes its eyebrow and its lead from the lesson, not from itself', () => {
    const w = mountIt(LESSON_A)
    expect(w.text()).toContain(LESSON_A.eyebrow)
    expect(w.text()).toContain(LESSON_A.lead)
    expect(mountIt(LESSON_B).text()).toContain(LESSON_B.eyebrow)
  })

  it('shows the second tab, with its own label, when a page is handed over', () => {
    const w = mountIt(LESSON_A)
    expect(w.find('[data-testid="about-tab-diagrams"]').exists()).toBe(true)
    expect(w.get('[data-testid="about-tab-diagrams"]').text()).toBe(LESSON_A.pageLabel)
  })

  // Lesson 02 has no drawings until 04.11. A tab that opens on nothing is a
  // promise the screen does not keep, so the tab arrives with the page.
  it('shows no second tab when the lesson has no page yet', () => {
    const w = mountIt(LESSON_B)
    expect(w.find('[data-testid="about-tab-diagrams"]').exists()).toBe(false)
    expect(w.find('[data-testid="about-tab-notes"]').exists()).toBe(true)
  })

  // A live guard, not a unit test. The component may not know which lesson it
  // is drawing — that is the whole of 04.12.2, and an import would undo it
  // while every test above still passed.
  it('does not name either lesson in its own source', () => {
    // Walked up from the runner's cwd, like recorded.spec.js: import.meta.url
    // is not a file URL once vitest has transformed this file.
    let dir = process.cwd()
    let hit = ''
    for (let i = 0; i < 8; i += 1) {
      const guess = resolve(dir, 'src/components/AboutPanel.vue')
      if (existsSync(guess)) {
        hit = guess
        break
      }
      const up = dirname(dir)
      if (up === dir) break
      dir = up
    }
    expect(hit, 'AboutPanel.vue not found above ' + process.cwd()).not.toBe('')
    const src = readFileSync(hit, 'utf8')
    const code = src.split('<style')[0]
    expect(code).not.toContain('README.md?raw')
    expect(code).not.toContain('demo04-jetstream-cqrs.html')
    expect(code).not.toContain('LESSON-02.md')
  })
})
