// The lesson files, in one place (04.12.2, D21).
//
// AboutPanel.vue renders "a markdown file and an HTML page" and no longer
// knows which. This module is where the knowing lives: one entry per lesson,
// spread onto the panel by the screen that owns it.
//
// `?raw` compiles the file in at build time. Nothing is fetched, so the screen
// cannot show an older copy than the repo holds.

import readme from '../../../README.md?raw'
import lesson01Page from '../../../diagrams/demo04-jetstream-cqrs.html?raw'
import blocksPng from '../../../diagrams/cqrs-blocks.png'

export const LESSON_01_ABOUT = {
  eyebrow: 'Demo 04 · JetStream as an event source, with CQRS',
  lead:
    'One log is the only source of truth. The write side reads it to check a ' +
    'rule. The read side folds it into an answer. Both are below — first in ' +
    'words, then drawn.',
  notes: readme,
  notesFile: 'README.md',
  images: { 'diagrams/cqrs-blocks.png': blocksPng },
  page: lesson01Page,
  pageFile: 'diagrams/demo04-jetstream-cqrs.html',
  pageLabel: 'Classes and sequences',
  frameTitle: 'Class and sequence diagrams for demo 04',
}
