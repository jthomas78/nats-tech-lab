// Size every page of the pattern cards deck to its card.
//
// From v0.3 a card is one page, and the page is as tall as the card: 210mm
// wide, never shorter than A4. Chrome prints each `.page[data-p="N"]` on its
// own named `@page pN`, so after any edit the sizes must be measured again.
// This script measures each `.page` at print width and rewrites the block
// between the fit-pages markers in place. Run it before exporting the PDF.
//
//   node docs/fit-pages.mjs docs/03-multi-cluster-and-accounts-pattern-cards-v0.3.html
import path from 'node:path'
import fs from 'node:fs'
import { createRequire } from 'node:module'

const MERMAID_CLI =
  '/opt/homebrew/Cellar/mermaid-cli/11.16.0/libexec/lib/node_modules/@mermaid-js/mermaid-cli'
const require = createRequire(path.join(MERMAID_CLI, 'package.json'))
const puppeteer = require('puppeteer')

const input = process.argv[2]
if (!input) {
  console.error('usage: node fit-pages.mjs <deck.html>')
  process.exit(1)
}
const START = '/* fit-pages:start */'
const END = '/* fit-pages:end */'
const html = fs.readFileSync(input, 'utf8')
const a = html.indexOf(START)
const b = html.indexOf(END)
if (a < 0 || b < a) {
  console.error('no fit-pages markers in', input)
  process.exit(1)
}

const browser = await puppeteer.launch({ headless: 'shell' })
const page = await browser.newPage()
await page.setViewport({ width: 1920, height: 1080 })
await page.emulateMediaType('print')
await page.emulateMediaFeatures([{ name: 'prefers-color-scheme', value: 'dark' }])
await page.goto('file://' + path.resolve(input), { waitUntil: 'load' })
await page.evaluate(() => document.fonts.ready)
const pages = await page.evaluate(() =>
  [...document.querySelectorAll('section.page')].map((s) => ({
    p: s.dataset.p,
    mm: (s.getBoundingClientRect().height * 25.4) / 96,
  }))
)
await browser.close()

const missing = pages.filter((x) => x.p === undefined).length
if (missing) {
  console.error(missing, 'page(s) have no data-p attribute')
  process.exit(1)
}
// Round up, plus 1mm, so a fraction of a pixel never spills onto a new page.
const rules = pages
  .map(({ p, mm }) => {
    const h = Math.max(297, Math.ceil(mm + 0.5))
    return `  @page p${p} { size: 210mm ${h}mm; }\n  .page[data-p="${p}"]{ page: p${p}; }\n`
  })
  .join('')
fs.writeFileSync(input, html.slice(0, a + START.length) + '\n' + rules + '  ' + html.slice(b))
console.log(pages.length, 'pages:', pages.map(({ mm }) => Math.max(297, Math.ceil(mm + 0.5))).join(' '), 'mm')
