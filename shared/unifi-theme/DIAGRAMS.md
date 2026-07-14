# UniFi architecture diagrams

Hand-authored SVG → PNG, rendered with headless Chrome in the UniFi dark
palette. Icons come from a single shared library so every diagram stays
visually consistent; edit the library once and re-render.

## Files

| File | Role |
|---|---|
| `icons.svg` | Canonical icon library — one `symbol` per glyph, `currentColor` line art. Doubles as a visual index (open it to browse). |
| `icons-preview.png` | Rendered index of the icon set. |
| `render-diagram.sh` | Build step: inlines the library's symbols into a diagram and rasterizes to PNG at 2×. |

Diagram **sources** live next to their demo, e.g.
`demos/01-dictionary/diagrams/*.svg`; rendered PNGs sit in the demo root.

## Authoring a diagram

1. In the SVG `<defs>`, add the marker comment `<!--UNIFI-ICONS-->` (the build
   replaces it with the icon symbols — do **not** paste symbols by hand).
2. Place an icon with a coloured tile behind it:
   ```xml
   <g color="#fff">
     <rect x="80" y="80" width="72" height="72" rx="12" fill="#006fff"/>
     <use href="#ico-browser" x="80" y="80" width="72" height="72"/>
   </g>
   ```
   Tile colour convention: `#006fff` for UI / compute, `#27c07f` for stateful
   data stores. Glyphs inherit the group's `color` (white on a coloured tile).
3. Palette (from `unifi.css`): bg `#14171b`, panel `#1a1e23`, border `#2c3138`,
   dashed lane `#4a515b`, text `#dee0e3`, muted `#8b929b`, accent `#006fff`.

## Rendering

```bash
shared/unifi-theme/render-diagram.sh <src.svg> <out.png> [width] [height]
```

Why a build step: headless Chrome blocks cross-file `<use href="other.svg#id">`
in standalone SVGs, so symbols must be inlined at render time. The script does
that from `icons.svg`, keeping it the one place glyphs are defined.

## Available icons

`ico-browser` (frontend) · `ico-service` (Go service) · `ico-nats` (message
mesh) · `ico-db` (database) · `ico-kv` (key-value) · `ico-stream` (event
stream) · `ico-cache` (cache) · `ico-sse` (live/SSE).

Add a new glyph as a 72×72 `symbol` in `icons.svg` using `currentColor`, then
reference it by id — no other wiring needed.
