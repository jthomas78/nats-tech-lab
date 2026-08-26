// Renders a self-contained, print-styled HTML page (A4 @page rules) to PDF via
// headless Chrome. Companion to export-html-png.mjs, which screenshots instead.
//
//   node export-html-pdf.mjs <input.html> <output.pdf>
import path from 'node:path'
import { createRequire } from 'node:module'

const MERMAID_CLI =
  '/opt/homebrew/Cellar/mermaid-cli/11.16.0/libexec/lib/node_modules/@mermaid-js/mermaid-cli'
const require = createRequire(path.join(MERMAID_CLI, 'package.json'))
const puppeteer = require('puppeteer')

const [input, output] = process.argv.slice(2)
if (!input || !output) {
  console.error('usage: node export-html-pdf.mjs <input.html> <output.pdf>')
  process.exit(1)
}

const browser = await puppeteer.launch({ headless: 'shell' })
const page = await browser.newPage()
// Same rationale as export-html-png.mjs: these pages are dark-only, and
// headless Chrome otherwise reports a light color scheme.
await page.emulateMediaFeatures([{ name: 'prefers-color-scheme', value: 'dark' }])
await page.goto('file://' + path.resolve(input), { waitUntil: 'load' })
await page.evaluate(() => document.fonts.ready)
await page.pdf({
  path: path.resolve(output),
  format: 'A4',
  printBackground: true,
  preferCSSPageSize: true,
})
await browser.close()
console.log('wrote', output)
