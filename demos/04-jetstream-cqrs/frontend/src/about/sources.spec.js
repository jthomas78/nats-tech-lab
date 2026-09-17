import { describe, expect, it } from 'vitest'

import { LESSON_01_ABOUT } from './sources.js'

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
