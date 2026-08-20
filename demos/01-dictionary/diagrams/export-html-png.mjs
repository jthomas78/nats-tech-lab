// Renders a self-contained HTML page to PNG for embedding in the
// ARCHITECTURE-*.md docs. Companion to export-png.sh (which handles the
// Draw.io workbook pages); this one covers hand-authored inline-SVG diagrams
// and UI mockups that have no Draw.io source.
//
//   node export-html-png.mjs <input.html> <output.png> [width] [--clip=sel,sel]
//
// width defaults to 1024 — for diagram pages that is the geometry the page was
// designed and reviewed at, so changing it changes the layout. deviceScaleFactor
// 2 doubles pixel density without changing layout.
//
// --clip takes one or more CSS selectors and captures the union of their
// bounding boxes plus a small margin, instead of the whole page. Use it to lift
// one component out of a page that also carries explanatory prose — e.g. the
// Admin UI mockup, where only the panel chrome belongs in the doc because the
// surrounding tables are already there as text.
import path from 'node:path'
import { createRequire } from 'node:module'

const MERMAID_CLI =
  '/opt/homebrew/Cellar/mermaid-cli/11.16.0/libexec/lib/node_modules/@mermaid-js/mermaid-cli'
const require = createRequire(path.join(MERMAID_CLI, 'package.json'))
const puppeteer = require('puppeteer')

const argv = process.argv.slice(2)
const clipArg = argv.find((a) => a.startsWith('--clip='))
const [input, output, width = '1024'] = argv.filter((a) => !a.startsWith('--'))
if (!input || !output) {
  console.error('usage: node export-html-png.mjs <input.html> <output.png> [width] [--clip=sel,sel]')
  process.exit(1)
}

const browser = await puppeteer.launch({ headless: 'shell' })
const page = await browser.newPage()
// Height starts small deliberately: Chrome floors document.documentElement's
// scrollHeight at the *initial* viewport height (the root element can't be
// shorter than the box that establishes the initial containing block), so a
// tall starting height pads every short page with dead space in the fullPage
// screenshot below. A tall page still captures correctly — fullPage grows the
// viewport to fit the real content regardless of where it started.
await page.setViewport({ width: Number(width), height: 100, deviceScaleFactor: 2 })
// These pages are dark-first: bare :root is the dark palette and a light
// override sits behind prefers-color-scheme. Headless Chrome reports "light",
// which would flip that override on, so pin the media feature to dark to match
// the data-theme attribute the export sources set. Every PNG in images/ is dark.
await page.emulateMediaFeatures([{ name: 'prefers-color-scheme', value: 'dark' }])
await page.goto('file://' + path.resolve(input), { waitUntil: 'load' })
await page.evaluate(() => document.fonts.ready)

const bg = await page.evaluate(() => getComputedStyle(document.body).backgroundColor)

let clip
if (clipArg) {
  const selectors = clipArg.slice('--clip='.length).split(',').map((s) => s.trim()).filter(Boolean)
  clip = await page.evaluate((sels) => {
    const PAD = 12
    const boxes = sels.flatMap((s) => [...document.querySelectorAll(s)]).map((el) => {
      const r = el.getBoundingClientRect()
      return { top: r.top + scrollY, left: r.left + scrollX, bottom: r.bottom + scrollY, right: r.right + scrollX }
    })
    if (!boxes.length) return null
    const top = Math.min(...boxes.map((b) => b.top))
    const left = Math.min(...boxes.map((b) => b.left))
    return {
      x: Math.max(0, left - PAD),
      y: Math.max(0, top - PAD),
      width: Math.max(...boxes.map((b) => b.right)) - left + PAD * 2,
      height: Math.max(...boxes.map((b) => b.bottom)) - top + PAD * 2,
    }
  }, selectors)
  if (!clip) {
    console.error(`--clip matched no elements: ${selectors.join(', ')}`)
    await browser.close()
    process.exit(1)
  }
  // The clip region can sit below the fold; grow the viewport so it is painted.
  await page.setViewport({
    width: Number(width),
    height: Math.ceil(clip.y + clip.height + 40),
    deviceScaleFactor: 2,
  })
  await page.evaluate(() => document.fonts.ready)
}

await page.screenshot(
  clip
    ? { path: path.resolve(output), clip, captureBeyondViewport: true }
    : { path: path.resolve(output), fullPage: true },
)

const box = clip
  ? { w: Math.round(clip.width), h: Math.round(clip.height) }
  : await page.evaluate(() => ({
      w: document.documentElement.scrollWidth,
      h: document.documentElement.scrollHeight,
    }))
await browser.close()
console.log(`${output}  ${box.w}x${box.h} css px @2x  body-bg ${bg}${clip ? '  (clipped)' : ''}`)
