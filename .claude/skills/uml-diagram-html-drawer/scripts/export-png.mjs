// Renders a uml-diagram-html-drawer page to PNG, on request only — the HTML
// page is the deliverable.
//
//   node export-png.mjs <input.html> <output.png> [width] [--clip=sel,sel]
//
// width defaults to 1920, the design viewport, so the PNG has the geometry the
// page was reviewed at. deviceScaleFactor 2 doubles pixel density without
// changing layout.
//
// --clip takes one or more CSS selectors and captures the union of their
// bounding boxes plus a small margin. Use it to lift one figure out of a page:
// --clip='figure:nth-of-type(2)'.
//
// Puppeteer is found, in order: $PUPPETEER_PATH (a package folder whose
// node_modules holds puppeteer, e.g. a mermaid-cli install), then a normal
// `puppeteer` install next to this script or in the current folder.
import path from 'node:path'
import { createRequire } from 'node:module'

function loadPuppeteer() {
  const bases = [
    process.env.PUPPETEER_PATH && path.join(process.env.PUPPETEER_PATH, 'package.json'),
    import.meta.url,
    path.join(process.cwd(), 'package.json'),
  ].filter(Boolean)
  for (const base of bases) {
    try {
      return createRequire(base)('puppeteer')
    } catch {}
  }
  console.error('puppeteer not found. Run `npm i puppeteer`, or set PUPPETEER_PATH to a folder whose node_modules holds it.')
  process.exit(2)
}
const puppeteer = loadPuppeteer()

const argv = process.argv.slice(2)
const clipArg = argv.find((a) => a.startsWith('--clip='))
const [input, output, width = '1920'] = argv.filter((a) => !a.startsWith('--'))
if (!input || !output) {
  console.error('usage: node export-png.mjs <input.html> <output.png> [width] [--clip=sel,sel]')
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
// the dark tokens the page defines on :root.
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
