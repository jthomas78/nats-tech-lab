import { describe, expect, it } from 'vitest'

import { LESSON_01_ABOUT, LESSON_02_ABOUT } from './sources.js'

// 04.12.2 — one place names the lesson files.
//
// AboutPanel.vue stopped naming them, so something has to. Keeping that in one
// module means the next lesson is an entry here, not an edit to the component.

describe('LESSON_01_ABOUT', () => {
  it('is the lab shell intro, named so the screen can print it', () => {
    expect(LESSON_01_ABOUT.notesFile).toBe('README.md')
    expect(LESSON_01_ABOUT.notes.length).toBeGreaterThan(1000)
  })

  it('carries lesson 01 and not lesson 02', () => {
    expect(LESSON_01_ABOUT.notes).toContain('## The finding')
    expect(LESSON_01_ABOUT.notes).not.toContain('## Lesson 02 — scaling a consumer')
  })

  it('carries the class and sequence drawings', () => {
    expect(LESSON_01_ABOUT.pageFile).toBe('diagrams/demo04-jetstream-cqrs.html')
    expect(LESSON_01_ABOUT.page).toContain('<svg')
  })

  it('tells the markdown where the block diagram went at build time', () => {
    expect(Object.keys(LESSON_01_ABOUT.images)).toContain('diagrams/cqrs-blocks.png')
  })
})

describe('LESSON_02_ABOUT', () => {
  it('is the lesson 02 document, named so the screen can print it', () => {
    expect(LESSON_02_ABOUT.notesFile).toBe('docs/LESSON-02.md')
    expect(LESSON_02_ABOUT.notes).toContain('## Lesson 02 — scaling a consumer')
  })

  it('carries lesson 02 and not lesson 01', () => {
    expect(LESSON_02_ABOUT.notes).not.toContain('## How to run it')
  })

  // 04.11 draws them. Until then there is one sub-tab, because a tab that
  // opens on nothing is a promise the screen does not keep.
  it('has no drawings yet', () => {
    expect(LESSON_02_ABOUT.page).toBe('')
  })

  // PoolPanel.spec.js forbids the lesson's <h1> wording appearing twice on the
  // screen. The eyebrow sits inside that screen now, so it must not repeat it.
  it('does not repeat the page heading', () => {
    expect(LESSON_02_ABOUT.eyebrow).not.toContain('one consumer, many workers')
  })

  it('is not lesson 01 wearing a different label', () => {
    expect(LESSON_02_ABOUT.notes).not.toBe(LESSON_01_ABOUT.notes)
    expect(LESSON_02_ABOUT.eyebrow).not.toBe(LESSON_01_ABOUT.eyebrow)
  })
})
