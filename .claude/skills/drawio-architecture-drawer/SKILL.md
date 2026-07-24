---
name: drawio-architecture-drawer
description: Create and maintain editable multi-page Draw.io architecture diagrams, apply the UniFi visual theme, export PNGs for Markdown, and validate the rendered outputs. Use for architecture maps, lifecycle diagrams, and sequence diagrams.
---

# Draw.io Architecture Drawer

Use this skill when an architecture diagram must remain editable in Draw.io and render cleanly in Markdown.

## Source model

- Prefer one native `.drawio` workbook with multiple named `<diagram>` pages.
- Keep stable page names for the architecture map, localized rendering lifecycle, and runtime sequence.
- Use native Draw.io shapes, connectors, swimlanes, and text. Do not replace the editable source with Mermaid or a flattened image.
- Keep generated PNGs beside the Markdown document that embeds them.
- Use relative Markdown links so the document remains portable.

For the dictionary demo, the source workbook and its exported images live in
the obsidian vault (not beside the generation scripts) — see
`obsidian/V3-Platform/Architecture/Dictionary-POC/ARCHITECTURE-COMMUNICATIONS.md`.
The workbook is:

`obsidian/V3-Platform/Architecture/Dictionary-POC/architecture-dictionary.drawio`

The generated images are:

`obsidian/V3-Platform/Architecture/Dictionary-POC/images/shipping-ui-dictionary-map.png`
`obsidian/V3-Platform/Architecture/Dictionary-POC/images/localized-rendering-lifecycle.png`
`obsidian/V3-Platform/Architecture/Dictionary-POC/images/shipping-ui-dictionary-sequence.png`
`obsidian/V3-Platform/Architecture/Dictionary-POC/images/docker-compose-network.png`
`obsidian/V3-Platform/Architecture/Dictionary-POC/images/rpc-proposed-dual-transport.png`

The generation scripts (`demos/01-dictionary/diagrams/sync-unifi-assets.mjs`,
`demos/01-dictionary/diagrams/export-png.sh`) remain in the repo and resolve
these vault paths internally — they don't need to live beside the workbook.

## UniFi visual language

Use the existing UniFi theme:

- Canvas: `#14171B`
- Primary UI and service nodes: `#006FFF`
- Authoritative data nodes: `#27C07F`
- Panels: `#1A1E23`
- Lane and lifeline strokes: `#4A515B`
- Primary text: `#DEE0E3`
- Secondary text: `#B7BCC2`
- Warning or fallback: `#9A7B1E`

Prefer restrained rounded rectangles, dashed system boundaries, clear directional arrows, and short labels. Keep titles and labels readable at the exported PNG size. For sequence diagrams, use explicit lifelines and visually separate cache-hit, fallback, and mutation-refresh phases.

**Dashed boundary corners must be sharp, not pill-shaped.** Draw.io's `rounded=1` alone uses a
radius *relative* to the shorter side, which balloons into a full pill/capsule on a tall, narrow
boundary rect — that reads as a rounded card, not a system/network boundary (compare AWS-style
diagrams, which use a small fixed-radius corner). Any dashed boundary/swimlane cell (network,
trust-zone, host, Docker project, etc.) must pin an absolute radius instead:
`rounded=1;arcSize=14;absoluteArcSize=1;...`. Solid service/data tiles (the small colored boxes,
not boundaries) can keep the default relative rounding — the pill effect only shows up on large
elongated shapes.

**Every page must carry an explicit full-page background rectangle, not just the
`mxGraphModel background="#14171B"` attribute.** That attribute is what the PNG/SVG export
scripts key off, and it does render correctly in exports — but some Draw.io Desktop builds show
the *editor canvas* as white regardless of it, which makes a dark-themed page look broken (and
its light-colored text illegible) the moment someone opens the `.drawio` file in the app itself,
even though nothing is actually wrong with the export. Fix this at the source, not per-symptom:
give every page's `root` a locked, non-editable rect sized to that page's exact `pageWidth`/
`pageHeight`, filled with the same canvas color, as the first child of `parent="1"` (so it
z-orders behind everything else):

```xml
<mxCell id="page-bg" value="" style="rounded=0;whiteSpace=wrap;html=1;fillColor=#14171B;strokeColor=none;locked=1;editable=0;" vertex="1" parent="1"><mxGeometry x="0" y="0" width="{pageWidth}" height="{pageHeight}" as="geometry"/></mxCell>
```

Because its fill matches the `background` attribute exactly, this is invisible in every already-
correct PNG/SVG export — it only fixes what the *editor* shows. When adding a new page, add this
cell before anything else and keep its `width`/`height` in sync with that page's `pageWidth`/
`pageHeight` (a mismatch here is easy to miss since it also only shows up in the live editor, not
in exports).

## Workflow

1. Inspect the existing architecture document, theme assets, and current rendered diagrams.
2. Update the appropriate page inside the single workbook. Preserve page IDs and names unless a deliberate rename is required.
3. Use the canonical glyphs from `shared/unifi-theme/icons.svg`: `ico-browser`, `ico-service`, `ico-nats`, `ico-db`, `ico-kv`, `ico-stream`, `ico-cache`, `ico-sse`, `ico-container`, `ico-actor`, and `ico-volume`. Do not redraw substitute icons. If a diagram needs a concept none of these cover, add a new `<symbol>` to `shared/unifi-theme/icons.svg` (72×72 viewBox, `currentColor` line art, no fill except small solid accent dots) and a matching card to that file's own visual-index section — don't invent a one-off inline icon that only lives in the target workbook.
4. Synchronize the embedded Draw.io image cells and font stack before exporting:

   `node demos/01-dictionary/diagrams/sync-unifi-assets.mjs`

   The workbook embeds each glyph because standalone Draw.io exports cannot reliably resolve external SVG `<use>` references. The exact font stack is `Inter, -apple-system, 'Segoe UI', sans-serif`, matching `shared/unifi-theme/icons.svg`.
5. Keep page geometry and text within the page bounds. Use `background="#14171B"` and explicit dark fills on swimlanes.
6. Export the pages with the repository script:

   `cd demos/01-dictionary && ./diagrams/export-png.sh`

   The installed Draw.io Desktop build uses one-based `--page-index` values for this workbook. Keep its page-to-output mapping synchronized with the workbook.
7. Validate the result:
   - `xmllint --noout obsidian/V3-Platform/Architecture/Dictionary-POC/architecture-dictionary.drawio`
   - `git diff --check`
   - `file obsidian/V3-Platform/Architecture/Dictionary-POC/images/*.png`
   - visually inspect each PNG for clipping, overlap, unreadable labels, missing lifelines, and incorrect canvas colors.
8. Update Markdown image links and the editable workbook link together.

## Diagramming a docker-compose.yml (network topology)

Use this pattern when the source is a `docker-compose.yml` rather than application code — e.g.
`obsidian/V3-Platform/Architecture/Dictionary-POC/architecture-dictionary.drawio`'s `docker-compose-network` page,
generated from `demos/01-dictionary/docker-compose.yml`.

1. **Read the compose file directly** — don't diagram from memory. For each service note its
   `networks:` membership, `ports:` (host→container), and any `volumes:` mount. A service with
   no explicit `networks:` key is on the implicit default network; call that out rather than
   silently omitting it.
2. **One dashed boundary rect per top-level `networks:` entry**, not a swimlane — plain
   `rounded=1;fillColor=none;strokeColor=#4A515B;dashed=1;dashPattern=7 6;` cells with the
   network name as the `value`, top-aligned. Swimlanes clip children to the lane's own
   coordinate space, which breaks the next point.
3. **A service on N networks is a physical bridge, not a duplicate node.** Overlap the boundary
   rects so the bridge service's single node sits inside the shared region — size the overlap
   to fit the node width, then place bridge nodes only in that overlap band. Never draw the same
   service twice, one per network; that misrepresents it as two containers.
4. **Node label carries the port mapping** — second line, small `#B7BCC2` font, e.g.
   `container :8080 · host 18080→8080`. Use the exact `HOST:CONTAINER` order from the compose
   file's `ports:` list so the label matches `docker compose config` output.
5. **Volumes are small dashed child nodes, not full service nodes** — `fillColor=#1A1E23`,
   `strokeColor=#4A515B`, `dashed=1`, labeled `volume: <name>`, connected to their owning
   service with a plain dashed no-arrowhead edge (`endArrow=none`). This visually distinguishes
   "data this service persists" from "service this service talks to."
6. **Solid arrows for network reachability** (service → service/broker it calls, labeled with
   the target `host:port` it dials), **dashed arrows for volume mounts** only. Don't reuse one
   edge style for both relationships — the point of the diagram is to show what can reach what.
7. Color by trust tier the same way as elsewhere in this theme: `#006FFF` for ordinary
   services/frontends, `#27C07F` for the authoritative datastore (e.g. `postgres`), `#1A1E23`
   dashed for anything that's storage rather than a running process (volumes).
8. **Icons**: `ico-container` (isometric box) for every service that's actually a running
   application container — this is the "Docker microservice" glyph, distinct from the generic
   hexagon `ico-service` used elsewhere for non-container diagrams. `ico-nats` for a
   NATS/JetStream broker, `ico-db` for the relational datastore, `ico-volume` for persisted
   mounts (a disk glyph — don't reuse `ico-cache`, which reads as "fast path", not "storage on
   disk"), `ico-actor` for a human/browser client outside the stack.
9. **Nest a `HOST` boundary around the whole diagram and a `DOCKER` boundary just inside it**
   when the diagram's purpose is to show where compose-level isolation sits relative to the
   physical machine. `HOST` (outermost, grey `#4A515B` dashed) represents the machine; `DOCKER`
   (blue `#2C7BE5` dashed, labeled with the compose file's path) represents the compose project —
   every container and every `networks:` boundary rect nests inside `DOCKER`. Skip this pair
   entirely if the diagram is only about the networks and the host/Docker framing adds nothing
   (e.g. a diagram that's purely about network reachability between containers, with no
   non-containerized host process in the picture).
10. **Represent the human actor** as a single `ico-actor` node placed *outside* the `HOST`
    boundary (fillColor=none, no tile — it isn't a container), with one edge per entry point it
    uses, each labeled with the host-side port it dials (e.g. `5173`). One actor node fanning out
    to N frontends reads more clearly than N duplicate actors; only draw multiple actors if they
    represent genuinely different real-world users (e.g. an admin vs. an end customer).
11. Register the new page in `sync-unifi-assets.mjs`'s `iconCells` map (one entry per icon,
    `[cellId, parentId, iconId, x, y]` — `x`/`y` are relative to the node's own geometry, matching
    the top-left inset used for existing pages) and add it to `export-png.sh`'s `pages` array with
    its 1-based `--page-index`, then re-run the export workflow steps below. The sync script
    hardcodes generated icons to 28×28 and strips/regenerates every `unifi-icon-*` cell on each
    run — if an icon needs a non-standard size or tile-less placement (e.g. the outside-the-Host
    actor), give its `mxCell` an id that does **not** start with `unifi-icon-` so the sync script
    leaves it alone, and size/position it by hand instead. If you reposition a node that has a
    registered icon, update both the node's `mxGeometry` **and** its `iconCells` `x`/`y` in the
    same edit — the icon's position is absolute page coordinates, not relative to its parent
    node, so moving one without the other visibly detaches the glyph from its tile (it drifts
    into the label text).
12. **When two edges share a source or a target, give them different `exitX/Y`/`entryX/Y`
    anchors.** Left at the mxGraph default, every edge between the same pair of node-sides picks
    the same anchor point, so N edges converging on one node draw exactly on top of each other —
    this reads as one edge/one label when it's actually several (the giveaway in a rendered PNG
    is a label that looks slightly bolder/doubled, or an arrowhead with no visible line feeding
    it). Fix: enumerate the edges sharing an endpoint and spread their `entryY`/`exitY` across
    distinct fractions (e.g. two edges into one node's left side get `entryY=0.25` and
    `entryY=0.75` instead of both defaulting to `0.5`). Do this for every node that is a fan-in or
    fan-out point — bridge services and shared brokers are the usual culprits.

    **Prefer a merged-trunk waypoint route over spread anchors when the sources are stacked in
    the same column and headed to the same target** (e.g. two bridge services both calling the
    same broker). Instead of entering the target at two different `entryY` fractions, give both
    edges an explicit `<Array as="points">` with one or two `mxPoint`s that bend each edge onto a
    **shared vertical (or horizontal) trunk** partway between source and target, converging into
    a single common entry point (e.g. both use `entryY=0.5` but reach it via
    `mxPoint x="{trunkX}" y="{sourceRowMidpoint}"` → `mxPoint x="{trunkX}" y="{targetMidpoint}"`).
    This reads as a schematic "bus line" merge (subway-map style) rather than two arrows stabbing
    into arbitrary points on the target's edge, and is the more polished result once you're past
    the mechanical "just don't overlap" fix. Spread-anchor (the plain `entryY=0.25`/`0.75` split)
    is still the right *first* move to kill an exact overlap — reach for the shared-trunk waypoint
    version as the follow-up polish pass, not as a replacement for checking fan-in/fan-out at all.
- **Offset edge labels off the line, not centered on it.** A label sitting exactly on a
  path — especially at a bend, or near another line — is hard to read against the line and any
  crossing edge. Give the label's own `<mxGeometry relative="1" x="{-1..1}" y="{offset}">` a small
  negative `y` (roughly `-7` to `-15`) to lift it just above the segment it's labeling, rather
  than leaving it at the default `y="0"` centered on the path.
- **Give overlapping/adjacent boundary rects margin beyond the exact fit.** When one boundary
  nests inside another (e.g. two `networks:` rects overlapping to hold a bridge node, or a network
  rect inside `DOCKER`), size the overlap or inner margin generously — tens of pixels of breathing
  room on each side of the contained node(s), not the tightest bounding box that technically
  contains them. A cramped fit reads as an accident; visible margin reads as deliberate.

## Layout discipline

- **Column/row grid, not free-hand pixel placement.** Before placing nodes, decide a small set of
  shared x-values (one per logical column — actor / frontend tier / bridge tier / backend tier)
  and y-values (one per row — either evenly spaced within a column, or shared across columns when
  two nodes are conceptually parallel, e.g. a bridge service and the specific backend it talks to
  most directly). Every node's `mxGeometry` should reuse one of those constants, not a one-off
  number picked to "look about right" — this is what produces AWS/GCP-style crisp alignment
  instead of a hand-drawn look, and it also makes the next redesign a matter of changing one
  constant instead of eyeballing N cells.
- **Prefer roomier row spacing (~200px+ between stacked node tops) over the tightest gap that
  avoids visual collision**, especially in a column whose edges bend/merge into a trunk (see
  item 12's shared-trunk routing) — cramped spacing leaves no room for a clean bend and forces
  edges to route through/behind neighboring nodes.
- **Explicit anchors are mandatory wherever a node has more than one incoming or outgoing edge**
  (see item 12 above) — treat "does this node fan in/out" as a checklist item before export, not
  something to notice only after spotting an overlap in the rendered PNG.
- **Re-verify after any reposition.** Moving a node's `y` (e.g. to align it onto a shared row)
  can silently orphan anything whose position was authored relative to the old value — most
  often a manually-placed icon (item 11) or an edge whose anchor fraction assumed the old
  geometry. Re-export and visually check the moved node's neighborhood specifically, not just
  "did the page still render."
- **For diagrams beyond ~10–12 nodes**, hand-placed grid constants stop scaling — that's the
  point to reach for an actual layout engine (Graphviz `dot` with `splines=ortho`, or ELK) to
  compute non-overlapping positions and orthogonal routes algorithmically, then translate its
  output into this workbook's mxGraph XML. Not needed for the diagrams in this repo today; note
  it here so a future larger diagram doesn't redo this evaluation from scratch.

## Agent portability

This skill intentionally uses plain Markdown, Draw.io XML, shell commands, and repository-relative paths. It does not depend on Codex-specific tools, Claude-specific commands, or provider-specific APIs. In Codex or Claude, use the host's normal shell and image inspection capability for the validation steps.
