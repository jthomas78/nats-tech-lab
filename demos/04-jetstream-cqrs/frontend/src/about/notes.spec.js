import { describe, expect, it } from 'vitest'

import { renderNotes } from './notes.js'

describe('renderNotes', () => {
  it('turns the README headings into HTML', () => {
    expect(renderNotes('# Demo 04\n\n## The shape\n')).toContain('<h1>Demo 04</h1>')
  })

  it('keeps a fenced block as a code block', () => {
    const html = renderNotes('```bash\ndocker compose up -d\n```\n')
    expect(html).toContain('<pre>')
    expect(html).toContain('docker compose up -d')
  })

  // The README path is relative to the README, not to the page.
  it('swaps a README image path for the URL the bundler gave the file', () => {
    const html = renderNotes('![blocks](diagrams/cqrs-blocks.png)', {
      'diagrams/cqrs-blocks.png': '/assets/cqrs-blocks-abc123.png',
    })
    expect(html).toContain('src="/assets/cqrs-blocks-abc123.png"')
  })

  it('leaves an image alone when nothing maps it', () => {
    const html = renderNotes('![blocks](diagrams/other.png)')
    expect(html).toContain('src="diagrams/other.png"')
  })
})
