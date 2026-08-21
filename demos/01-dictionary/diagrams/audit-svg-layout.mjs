// Geometry auditor for the hand-authored inline-SVG diagram pages in this
// folder. Companion to export-html-png.mjs: that one renders a page, this one
// checks the page's *layout* before (or after) it is rendered, so the classic
// hand-placed-SVG defects — a label sitting on top of another label, text
// spilling out of its box, a connector drawn straight through a node or across
// a label, two boxes overlapping — are caught mechanically instead of by
// eyeballing the PNG.
//
//   node audit-svg-layout.mjs <page.html> [more.html ...] [--json] [--quiet]
//
// Exits 1 if any ERROR-severity finding is reported, 0 otherwise (WARNs do not
// fail the run). Loads the page in the same headless Chrome as the exporter, at
// the same 1024px width the diagrams are designed at, so the measurements are
// the ones the exported PNG will actually have.
import path from 'node:path'
import { createRequire } from 'node:module'

const MERMAID_CLI =
  '/opt/homebrew/Cellar/mermaid-cli/11.16.0/libexec/lib/node_modules/@mermaid-js/mermaid-cli'
const require = createRequire(path.join(MERMAID_CLI, 'package.json'))
const puppeteer = require('puppeteer')

const argv = process.argv.slice(2)
const asJson = argv.includes('--json')
const quiet = argv.includes('--quiet')
const widthArg = argv.find((a) => a.startsWith('--width='))
const width = Number(widthArg ? widthArg.slice('--width='.length) : 1024)
const files = argv.filter((a) => !a.startsWith('--'))
if (!files.length) {
  console.error('usage: node audit-svg-layout.mjs <page.html> [more.html ...] [--json] [--quiet] [--width=1024]')
  process.exit(1)
}

const audit = () => {
  // ---- tunables (page-side, so they stay next to the geometry they judge) ----
  const TEXT_PAD = 1.0 // px of slack before two text boxes count as overlapping
  const BOX_PAD = 0.5 // px a glyph may exceed its containing box by
  const EDGE_PAD = 1.5 // px inflation of a label box when testing edge crossings
  const NODE_INSET = 3 // px inset before an edge counts as "inside" a node
  const ENDPOINT_SKIP = 7 // px of an edge near its own ends exempt from node hits
  const DANGLE = 16 // px an endpoint may float from the nearest shape
  const STEP = 2 // px sampling step along an edge

  const findings = []
  const add = (severity, svgIndex, label, code, message, detail) =>
    findings.push({ severity, svg: svgIndex, diagram: label, code, message, detail })

  const rectOf = (el) => {
    const r = el.getBoundingClientRect()
    return { x: r.left, y: r.top, w: r.width, h: r.height, r: r.right, b: r.bottom }
  }
  const inter = (a, b, pad = 0) => {
    const x = Math.min(a.r, b.r) - Math.max(a.x, b.x) - pad * 2
    const y = Math.min(a.b, b.b) - Math.max(a.y, b.y) - pad * 2
    return x > 0 && y > 0 ? { w: x, h: y } : null
  }
  const contains = (outer, innr, pad = 0) =>
    innr.x >= outer.x - pad && innr.y >= outer.y - pad && innr.r <= outer.r + pad && innr.b <= outer.b + pad
  const inside = (rect, pt, inset = 0) =>
    pt.x > rect.x + inset && pt.x < rect.r - inset && pt.y > rect.y + inset && pt.y < rect.b - inset
  const where = (el) => {
    const c = el.getAttribute('class')
    return `<${el.tagName}${c ? ` class="${c}"` : ''}>`
  }
  const short = (s) => (s.length > 42 ? s.slice(0, 41) + '…' : s)
  // Where a finding lives, in the SVG's own user-space coordinates — the
  // numbers actually written in the source, not rendered client px.
  const at = (el) => {
    try {
      const b = el.getBBox()
      return `${Math.round(b.x)},${Math.round(b.y)}`
    } catch {
      return '?'
    }
  }
  // Dashed grouping rects, account/process boundaries, lifelines, axes: drawn
  // to be straddled and crossed. They are not nodes and not connectors.
  const DECOR = /(^|\s)(nb|bnd|bound|boundary|group|lane|life|lifeline|axis|grid|tick|divider|sep)(\s|$)/
  const isDecor = (el) => DECOR.test(el.getAttribute('class') || '')

  // A text is exempt from the edge-crossing check if it paints a background
  // knockout halo (paint-order: stroke + an opaque stroke wide enough to read).
  const hasHalo = (el) => {
    const cs = getComputedStyle(el)
    const w = parseFloat(cs.strokeWidth) || 0
    return (
      cs.paintOrder.trim().startsWith('stroke') &&
      cs.stroke !== 'none' &&
      !/rgba\(.*,\s*0\)/.test(cs.stroke) &&
      w >= 2.5
    )
  }

  const samples = (el) => {
    const ctm = el.getScreenCTM()
    if (!ctm || typeof el.getTotalLength !== 'function') return []
    let len = 0
    try {
      len = el.getTotalLength()
    } catch {
      return []
    }
    if (!len) return []
    const out = []
    const svg = el.ownerSVGElement
    for (let d = 0; d <= len; d += STEP) {
      const p = el.getPointAtLength(Math.min(d, len))
      const pt = svg.createSVGPoint()
      pt.x = p.x
      pt.y = p.y
      const t = pt.matrixTransform(ctm)
      out.push({ x: t.x, y: t.y, d })
    }
    return out
  }

  // Only diagram SVGs are audited: this skill requires every diagram to carry
  // role="img" + a real aria-label, so that pair doubles as the selector. Chart
  // sparklines, icons and other chrome inside a UI mockup carry neither and are
  // skipped; add data-audit="skip" to opt a labelled SVG out deliberately.
  const audited = [...document.querySelectorAll('svg[role="img"][aria-label]')].filter(
    (el) => el.getAttribute('data-audit') !== 'skip',
  )
  const skipped = document.querySelectorAll('svg').length - audited.length
  audited.forEach((svg, svgIndex) => {
    const label = svg.getAttribute('aria-label') || svg.id || `svg #${svgIndex + 1}`
    const svgRect = rectOf(svg)
    const els = [...svg.querySelectorAll('*')]

    const texts = els
      .filter((el) => el.tagName === 'text' && el.textContent.trim())
      .map((el) => ({ el, rect: rectOf(el), text: el.textContent.trim().replace(/\s+/g, ' ') }))

    const geo = els.filter((el) =>
      ['path', 'line', 'polyline', 'polygon', 'rect', 'circle', 'ellipse'].includes(el.tagName),
    )
    // Markers live in <defs> and are drawn by the edge that references them;
    // auditing their own geometry would be noise.
    const drawn = geo.filter((el) => !el.closest('defs, marker, clipPath, mask, pattern'))

    // A connector is: anything carrying an arrowhead marker, a bare
    // <line>/<polyline>, or a <path> classed with one of the semantic edge
    // classes this skill defines. Everything else — including an open
    // cylinder-body path (`.n`) — is a shape.
    const EDGE_CLS = /(^|\s)(flow|proj|rep|err|edge|link|conn|arrow|hop|sync|store|evtl|bad|msg|call|ret)(\s|$)/
    const isEdge = (el) => {
      const cs = getComputedStyle(el)
      if (cs.markerEnd !== 'none' || cs.markerStart !== 'none') return true
      if (['line', 'polyline'].includes(el.tagName)) return true
      if (el.tagName !== 'path') return false
      return EDGE_CLS.test(el.getAttribute('class') || '')
    }
    const edges = drawn.filter((el) => isEdge(el) && !isDecor(el))
    const decor = drawn.filter((el) => isDecor(el)).map((el) => ({ el, rect: rectOf(el) }))
    let shapes = drawn
      .filter((el) => !edges.includes(el) && !isDecor(el))
      .map((el) => ({ el, rect: rectOf(el) }))
    // A stream cylinder is drawn as two elements — a flat <ellipse> cap over an
    // open <path> body — which overlap and share their labels by construction.
    // Merge each such pair into one composite shape so the cap is not reported
    // as a collision and a label sitting under it is not reported as spilling.
    const isCap = ({ el }) => {
      if (!['ellipse', 'circle'].includes(el.tagName)) return false
      const b = el.getBBox()
      return b.height <= 26 && b.width >= b.height * 2.5
    }
    for (const cap of shapes.filter(isCap)) {
      const body = shapes.find(
        (s) => s !== cap && s.el.tagName === 'path' && inter(s.rect, cap.rect, -2),
      )
      if (!body) continue
      body.rect = {
        x: Math.min(body.rect.x, cap.rect.x), y: Math.min(body.rect.y, cap.rect.y),
        r: Math.max(body.rect.r, cap.rect.r), b: Math.max(body.rect.b, cap.rect.b),
        w: 0, h: 0,
      }
      body.rect.w = body.rect.r - body.rect.x
      body.rect.h = body.rect.b - body.rect.y
      shapes = shapes.filter((s) => s !== cap)
    }
    // Boundary lines/rects still get the label-crossing check, at WARN: a
    // dashed rule through a caption is a legibility smudge, not a broken graph.
    const decorLines = decor.filter(({ el }) => isEdge(el))

    // 1. text vs. text
    for (let i = 0; i < texts.length; i++) {
      for (let j = i + 1; j < texts.length; j++) {
        const hit = inter(texts[i].rect, texts[j].rect, TEXT_PAD)
        if (hit)
          add('ERROR', svgIndex, label, 'text-overlap',
            `labels overlap by ${hit.w.toFixed(1)}×${hit.h.toFixed(1)}px`,
            `"${short(texts[i].text)}" at ${at(texts[i].el)} ↔ "${short(texts[j].text)}" at ${at(texts[j].el)}`)
      }
    }

    // 2. text vs. its containing shape, and text vs. the svg's own bounds
    for (const t of texts) {
      const holders = shapes
        .filter((s) => inside(s.rect, { x: (t.rect.x + t.rect.r) / 2, y: (t.rect.y + t.rect.b) / 2 }))
        .sort((a, b) => a.rect.w * a.rect.h - b.rect.w * b.rect.h)
      const holder = holders[0]
      if (holder && !contains(holder.rect, t.rect, BOX_PAD)) {
        const over = Math.max(
          holder.rect.x - t.rect.x, t.rect.r - holder.rect.r,
          holder.rect.y - t.rect.y, t.rect.b - holder.rect.b,
        )
        add('ERROR', svgIndex, label, 'text-overflows-box',
          `label spills ${over.toFixed(1)}px outside ${where(holder.el)} at ${at(holder.el)}`,
          `"${short(t.text)}" at ${at(t.el)} — widen the box or shorten the label`)
      }
      if (!contains(svgRect, t.rect, 0.5))
        add('ERROR', svgIndex, label, 'text-clipped',
          'label falls outside the svg viewport and will be cut off in the PNG',
          `"${short(t.text)}" at ${at(t.el)}`)
    }

    // 3. connectors vs. labels, and connectors through unrelated nodes
    const runEdge = (edge, severity) => {
      const pts = samples(edge)
      if (!pts.length) return
      const end = pts[pts.length - 1].d
      const a = pts[0]
      const z = pts[pts.length - 1]

      const crossed = new Set()
      for (const t of texts) {
        if (hasHalo(t.el)) continue
        if (pts.some((p) => inside(t.rect, p, -EDGE_PAD))) crossed.add(t)
      }
      for (const t of crossed)
        add(severity, svgIndex, label, 'edge-crosses-label',
          `${where(edge)} at ${at(edge)} is drawn through a label`,
          `"${short(t.text)}" at ${at(t.el)} — move the label clear of the line, or give it a background halo`)

      if (severity !== 'ERROR') return

      const pierced = new Set()
      for (const s2 of shapes) {
        if (inside(s2.rect, a, -DANGLE) || inside(s2.rect, z, -DANGLE)) continue
        const body = pts.filter((p) => p.d > ENDPOINT_SKIP && p.d < end - ENDPOINT_SKIP)
        if (body.some((p) => inside(s2.rect, p, NODE_INSET))) pierced.add(s2)
      }
      for (const s2 of pierced)
        add('ERROR', svgIndex, label, 'edge-pierces-node',
          `${where(edge)} at ${at(edge)} runs through ${where(s2.el)} it does not attach to`,
          `box at ${at(s2.el)} — route the connector around it, or move it into a free lane`)

      // Lifelines and boundary rects are legitimate anchors too — a sequence
      // diagram's messages start and end on a lifeline, not on a box.
      const anchors = shapes.concat(decor)
      for (const [name, p] of [['start', a], ['end', z]]) {
        if (!anchors.some((s2) => inside(s2.rect, p, -DANGLE)))
          add('WARN', svgIndex, label, 'edge-dangles',
            `${where(edge)} at ${at(edge)} ${name} floats >${DANGLE}px from any shape`,
            'an arrow should start and end at a node edge, not in open space')
      }
    }
    for (const edge of edges) runEdge(edge, 'ERROR')
    for (const { el } of decorLines) runEdge(el, 'WARN')

    // 4. shape vs. shape — overlapping node boxes (containment is fine: that is
    // a cylinder cap over its body, or a nested sub-box).
    for (let i = 0; i < shapes.length; i++) {
      for (let j = i + 1; j < shapes.length; j++) {
        const A = shapes[i], B = shapes[j]
        if (contains(A.rect, B.rect, 2) || contains(B.rect, A.rect, 2)) continue
        const hit = inter(A.rect, B.rect, 1)
        if (hit)
          add('ERROR', svgIndex, label, 'box-overlap',
            `shapes overlap by ${hit.w.toFixed(1)}×${hit.h.toFixed(1)}px`,
            `${where(A.el)} at ${at(A.el)} ↔ ${where(B.el)} at ${at(B.el)}`)
      }
    }
  })

  return { findings, audited: audited.length, skipped }
}

const results = []
let failed = false
const browser = await puppeteer.launch({ headless: 'shell' })
try {
  for (const file of files) {
    const page = await browser.newPage()
    await page.setViewport({ width, height: 900, deviceScaleFactor: 1 })
    await page.emulateMediaFeatures([{ name: 'prefers-color-scheme', value: 'dark' }])
    await page.goto('file://' + path.resolve(file), { waitUntil: 'load' })
    await page.evaluate(() => document.fonts.ready)
    const { findings, audited, skipped } = await page.evaluate(audit)
    await page.close()

    const errors = findings.filter((f) => f.severity === 'ERROR')
    if (errors.length) failed = true
    results.push({ file, audited, skipped, findings })

    if (!asJson) {
      const warns = findings.length - errors.length
      const head =
        `${file}  ${audited} diagram${audited === 1 ? '' : 's'}` +
        (skipped ? ` (${skipped} unlabelled svg${skipped === 1 ? '' : 's'} skipped)` : '') +
        `  ${errors.length} error${errors.length === 1 ? '' : 's'}, ${warns} warning${warns === 1 ? '' : 's'}`
      if (!findings.length) {
        if (!quiet) console.log(`${head}  — layout clean`)
      } else {
        console.log(head)
        let current = null
        for (const f of findings) {
          if (quiet && f.severity !== 'ERROR') continue
          if (f.diagram !== current) {
            current = f.diagram
            console.log(`  · ${current.length > 96 ? current.slice(0, 95) + '…' : current}`)
          }
          console.log(`    ${f.severity === 'ERROR' ? 'ERR ' : 'warn'} [${f.code}] ${f.message}`)
          if (f.detail) console.log(`         ${f.detail}`)
        }
      }
    }
  }
} finally {
  await browser.close()
}

if (asJson) console.log(JSON.stringify(results, null, 2))
process.exit(failed ? 1 : 0)
